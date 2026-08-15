from datetime import datetime, timedelta, timezone
import asyncio
from fastapi import APIRouter, Depends, HTTPException, Request, Response
from pydantic import BaseModel
from sqlalchemy.orm import Session
from ..auth import require_hmac
from ..config import settings
from ..database import get_db
from ..models import Instance, WorkspaceState, InstanceStatus, PlanType
from ..docker_manager import start_container, stop_container, PLAN_STORAGE_GB
from .. import docker_manager  # module ref so tests can monkeypatch update_container_resources
from ..token import generate_access_token, verify_access_token

router = APIRouter(prefix="/internal/instances", tags=["instances"])


class StartResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    started_at: str
    expires_at: str | None = None


class UpgradeRequest(BaseModel):
    plan: str
    cpu_quota: int
    mem_limit: str
    pids_limit: int


class UpgradeResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    plan: PlanType
    needs_upgrade: bool


class DowngradeRequest(BaseModel):
    plan: str
    expires_at: str  # ISO8601


class DowngradeResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    plan: PlanType
    expires_at: str | None = None


class SleepResponse(BaseModel):
    instance_id: str
    status: InstanceStatus


class AccessTokenResponse(BaseModel):
    token: str
    expires_at: str
    workspace_url: str


def _get_instance(instance_id: str, user_id: str, db: Session) -> Instance:
    inst = db.query(Instance).filter(Instance.instance_id == instance_id).first()
    if not inst:
        raise HTTPException(status_code=404, detail="Instance not found")
    if inst.user_id != user_id:
        raise HTTPException(status_code=403, detail="Not your instance")
    return inst


async def _wait_container_ready(container_name: str, port: int = 3000, timeout: int = 30) -> bool:
    """Poll the container's web service until it accepts TCP connections.

    Returns True if ready within timeout, False otherwise (non-fatal — the
    caller still returns success; the user just needs to wait a moment).
    Uses docker exec to probe from the manager side (avoids DNS resolution
    issues from outside the docker network).
    """
    from ..docker_manager import _client_or_none

    client = _client_or_none()
    if client is None:
        return False

    deadline = asyncio.get_event_loop().time() + timeout
    interval = 1.0
    while asyncio.get_event_loop().time() < deadline:
        try:
            c = client.containers.get(container_name)
            # wget is available in the hermes-unified image; fall back to
            # shell /dev/tcp probe if not.
            res = c.exec_run(
                ["sh", "-c", f"wget -q -O /dev/null --timeout=2 http://127.0.0.1:{port}/ 2>/dev/null || curl -s -o /dev/null --max-time 2 http://127.0.0.1:{port}/ 2>/dev/null"]
            )
            if getattr(res, "exit_code", 1) == 0:
                return True
        except Exception:
            pass
        await asyncio.sleep(interval)
        # tighten polling after first few attempts
        if interval < 2:
            interval = 1.5
    return False


class StartRequest(BaseModel):
    provider_api_key: str | None = None
    provider_base_url: str | None = None
    provider_model: str | None = None
    # authoritative plan from new-api user.group; aligns a drifted instance.plan
    # that missed the upgrade window (subscribed while container not RUNNING).
    expected_plan: str | None = None


@router.post("/{instance_id}/start", response_model=StartResponse)
async def start_instance(
    instance_id: str,
    body: StartRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status not in (InstanceStatus.SLEEPING, InstanceStatus.NOT_CREATED):
        raise HTTPException(
            status_code=409, detail=f"Cannot start from status {inst.status}"
        )

    from .. import crypto
    from ..provider_config import build_snapshot, reconcile_snapshot, render_config_yaml, merge_provider_into_yaml, probe_default_model

    if crypto.provider_enc_key_missing():
        raise HTTPException(status_code=500, detail="Provider encryption key not configured")

    # First-start hard lock: if no snapshot yet, provider_api_key is required.
    is_first_start = inst.provider_config_enc is None
    if is_first_start and not body.provider_api_key:
        raise HTTPException(
            status_code=400,
            detail="provider_api_key is required on first start",
        )

    # If a key is provided on any start, update snapshot (override).
    if body.provider_api_key:
        source = "ours"
        # User never picks a model — the key decides. Probe new-api /v1/models
        # and take the first available. Probe failure is fatal: refuse to ship
        # a hardcoded default that may not be a real channel.
        resolved_model = body.provider_model
        if not resolved_model:
            try:
                resolved_model = probe_default_model(
                    api_key=body.provider_api_key,
                    base_url=body.provider_base_url,
                )
            except Exception as e:
                raise HTTPException(
                    status_code=400,
                    detail=f"Failed to list models from provider with this key: {e}",
                )
        snap = build_snapshot(
            api_key=body.provider_api_key,
            base_url=body.provider_base_url,
            model=resolved_model,
            source=source,
        )
        enc, alg = crypto.encrypt_provider_config(snap)
        inst.provider_config_enc = enc
        inst.provider_config_alg = alg
        inst.provider_key_set_at = datetime.now(timezone.utc)

    now = datetime.now(timezone.utc)

    # Align inst.plan to the authoritative expected_plan from new-api user.group.
    # Closes the upgrade-window hole: a subscription committed while the
    # container was not RUNNING never reached the upgrade route, leaving
    # inst.plan stuck at FREE (→ 2h expiry + wrong display) with no scanner
    # safety net (needs_upgrade was never set). Start is the single reliable
    # point every user passes through, so reconcile here.
    if body.expected_plan:
        try:
            target = PlanType(body.expected_plan)
        except ValueError:
            target = None
        if target is not None and target != inst.plan:
            inst.plan = target
            inst.expected_plan = None
            inst.needs_upgrade = False
            inst.upgrade_retries = 0
            # Keep the soft-quota storage in lockstep with the new plan, mirroring
            # scanner.py's expected_plan alignment. Without this, a plan upgrade on
            # start leaves storage_quota_gb at the old (FREE) value — front-end still
            # shows 10GB and the storage soft-quota never grows to the paid tier.
            inst.storage_quota_gb = PLAN_STORAGE_GB.get(target.value, PLAN_STORAGE_GB["FREE"])
            # paid plan → no free-window expiry; FREE alignment below still sets 2h.
            # Resource hot-change on wake is intentionally NOT done here (see plan:
            # start_container is docker start on the existing container, does not
            # reapply mem/cpu; rebuild closes that in scanner.check_needs_rebuild).

    expires_at = None
    if inst.plan == PlanType.FREE:
        expires_at = now + timedelta(hours=2)

    # Reconcile on wake: if NOT_CREATED we will create; if SLEEPING we may
    # have a container-side config.yaml the user edited — adopt it AFTER the
    # container is running (exec needs a running container; doing it pre-start
    # hits 409 Conflict).
    was_sleeping = inst.status == InstanceStatus.SLEEPING
    provider_config_for_create = None
    if inst.status == InstanceStatus.NOT_CREATED and inst.provider_config_enc:
        provider_config_for_create = crypto.decrypt_provider_config(
            inst.provider_config_enc, inst.provider_config_alg or "fernet"
        )

    docker_result = await asyncio.to_thread(start_container, inst.container_name)
    if not docker_result:
        # Fallback: container might not exist yet. Try creating it first.
        from ..docker_manager import create_container
        create_res = create_container(
            container_name=inst.container_name,
            volume_name=inst.volume_name,
            user_id=inst.user_id,
            workspace_id=inst.instance_id,
            plan=inst.plan.name if hasattr(inst.plan, "name") else str(inst.plan),
            expires_at=expires_at.isoformat() if expires_at else None,
            provider_config=provider_config_for_create,
        )
        if "error" in create_res:
            raise HTTPException(
                status_code=500,
                detail=f"Failed to create container: {create_res['error']}",
            )

        # Try starting again
        if not await asyncio.to_thread(start_container, inst.container_name):
            raise HTTPException(
                status_code=500,
                detail="Failed to start container after creation",
            )

    inst.status = InstanceStatus.RUNNING
    inst.started_at = now
    inst.expires_at = expires_at
    db.commit()

    # Wait for the container's web service (port 3000) to become reachable.
    # Without this, the frontend shows "Open Workspace" immediately but nginx
    # proxies to a not-yet-listening upstream → 502/403 for the user.
    await _wait_container_ready(inst.container_name, timeout=30)

    # Reconcile + write back provider config into the now-running container's
    # /opt/data/config.yaml. Done AFTER start because docker exec requires a
    # running container (exec on a stopped container 409s). For SLEEPING wake
    # this is the ONLY path that puts the DB key snapshot into the container —
    # create-time injection only fires for NOT_CREATED. We merge (not clobber)
    # so other sections (terminal/browser/...) in the live config.yaml survive.
    if was_sleeping:
        try:
            from ..docker_manager import _client_or_none, _write_container_config_yaml
            client = _client_or_none()
            if client is not None:
                c = client.containers.get(inst.container_name)
                res = c.exec_run(["sh", "-c", "cat /opt/data/config.yaml 2>/dev/null || true"])
                yaml_text = ""
                if getattr(res, "exit_code", 1) == 0 and res.output:
                    yaml_text = res.output.decode("utf-8", errors="ignore") if isinstance(res.output, bytes) else str(res.output)
                db_snap = None
                if inst.provider_config_enc:
                    db_snap = crypto.decrypt_provider_config(
                        inst.provider_config_enc, inst.provider_config_alg or "fernet"
                    )
                new_snap, changed = reconcile_snapshot(db_snapshot=db_snap, container_yaml=yaml_text)
                print(f"[DEBUG_INJECT] yaml_len={len(yaml_text)} db_snap={'Y' if db_snap else 'N'} new_snap={'Y' if new_snap else 'N'} changed={changed}")
                if changed and new_snap is not None:
                    enc, alg = crypto.encrypt_provider_config(new_snap)
                    inst.provider_config_enc = enc
                    inst.provider_config_alg = alg
                    inst.provider_key_set_at = datetime.now(timezone.utc)
                    db.commit()
                    db_snap = new_snap
                if db_snap:
                    merged = merge_provider_into_yaml(yaml_text, db_snap)
                    ok = _write_container_config_yaml(c, merged)
                    print(f"[DEBUG_INJECT] wrote merged len={len(merged)} ok={ok} snap_model={db_snap.get('model')} snap_base={db_snap.get('base_url')}")
        except Exception as _e:
            import traceback
            print(f"[DEBUG_INJECT] EXCEPTION: {repr(_e)}")
            print(traceback.format_exc())
    # ponytail: global docker exec retry loop; per-instance retry if a fleet
    # talks to manager concurrently and exec races start-up. Current single-
    # instance dev path doesn't need it.

    return StartResponse(
        instance_id=instance_id,
        status=inst.status,
        started_at=inst.started_at.isoformat(),
        expires_at=inst.expires_at.isoformat() if inst.expires_at else None,
    )


class RevokeResponse(BaseModel):
    instance_id: str
    status: str


@router.post("/{instance_id}/revoke-provider-key", response_model=RevokeResponse)
async def revoke_provider_key(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    from ..provider_config import clear_provider_fields_yaml
    from ..docker_manager import _client_or_none

    # 1. Clear DB snapshot.
    inst.provider_config_enc = None
    inst.provider_config_alg = None
    inst.provider_key_set_at = None
    db.commit()

    # 2. If container is running, clear its config.yaml provider fields.
    client = _client_or_none()
    if client is not None:
        try:
            c = client.containers.get(inst.container_name)
            res = c.exec_run(["sh", "-c", "cat /opt/data/config.yaml 2>/dev/null || true"])
            if getattr(res, "exit_code", 1) == 0 and res.output:
                yaml_text = res.output.decode("utf-8", errors="ignore") if isinstance(res.output, bytes) else str(res.output)
                cleared = clear_provider_fields_yaml(yaml_text)
                # Best-effort preserve: do NOT truncate the user's config.yaml.
                # Skip the write-back when (a) the clear produced an empty result
                # while the original was non-empty (parse failure path that
                # returned "" — old behavior; also covers empty original), or
                # (b) the clear produced the SAME text as the original (nothing
                # safely clearable, e.g. parse-broken yaml returned unchanged by
                # clear_provider_fields_yaml — no point writing identical bytes).
                # In both cases DB cleared is the canonical state; the container
                # config stays as-is until the user fixes the parse error.
                if (cleared.strip() == "" and yaml_text.strip() != "") or cleared == yaml_text:
                    import logging
                    logging.warning(
                        "revoke_provider_key: skipped config.yaml write-back for "
                        "instance %s — container config parse-broken or unchanged; "
                        "refusing to truncate. DB cleared is canonical.",
                        instance_id,
                    )
                else:
                    import base64
                    b64 = base64.b64encode(cleared.encode("utf-8")).decode("ascii")
                    c.exec_run(["sh", "-c", f"echo '{b64}' | base64 -d > /opt/data/config.yaml"])
        except Exception:
            pass  # Container not present / not running — DB cleared is canonical.

    return RevokeResponse(instance_id=instance_id, status="revoked")


@router.get("/{instance_id}/provider-state")
async def get_provider_state(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)
    from .. import crypto

    if not inst.provider_config_enc:
        return {"instance_id": instance_id, "source": "none", "model": None, "key_set_at": None}

    snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg or "fernet")
    return {
        "instance_id": instance_id,
        "source": snap.get("source", "none"),
        "model": snap.get("model"),
        "key_set_at": inst.provider_key_set_at.isoformat() if inst.provider_key_set_at else None,
    }


@router.post("/{instance_id}/sleep", response_model=SleepResponse)
async def sleep_instance(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status not in (InstanceStatus.RUNNING, InstanceStatus.STARTING):
        raise HTTPException(
            status_code=409, detail=f"Cannot sleep from status {inst.status}"
        )

    if not stop_container(inst.container_name):
        raise HTTPException(status_code=500, detail="Failed to stop container")

    inst.status = InstanceStatus.SLEEPING
    inst.started_at = None
    inst.expires_at = None
    db.commit()

    return SleepResponse(instance_id=instance_id, status=inst.status)


@router.post("/{instance_id}/stop")
async def stop_instance(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if not stop_container(inst.container_name):
        raise HTTPException(status_code=500, detail="Failed to stop container")

    inst.status = InstanceStatus.SLEEPING
    inst.started_at = None
    inst.expires_at = None
    db.commit()

    return {"instance_id": instance_id, "status": InstanceStatus.SLEEPING}


@router.post("/{instance_id}/access-token", response_model=AccessTokenResponse)
async def issue_access_token(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status != InstanceStatus.RUNNING:
        raise HTTPException(status_code=409, detail="Instance is not running")

    # 回填：升级前已存在的旧行 assigned_port 为 NULL
    if not inst.assigned_port:
        used_ports = {
            p[0] for p in db.query(Instance.assigned_port)
            .filter(Instance.assigned_port.isnot(None)).all() if p[0]
        }
        port = next(
            (p for p in range(settings.workspace_port_start, settings.workspace_port_end + 1)
             if p not in used_ports), None,
        )
        if port is None:
            raise HTTPException(status_code=503, detail="No available workspace ports")
        inst.assigned_port = port
        db.commit()

    result = generate_access_token(
        instance_id=instance_id,
        user_id=auth["user_id"],
        expires_in_minutes=30,
        workspace_host=settings.public_host,
        workspace_port=inst.assigned_port,
    )

    return AccessTokenResponse(
        token=result["token"],
        expires_at=result["expires_at"],
        workspace_url=result["workspace_url"],
    )


@router.post("/validate-workspace-token")
async def validate_workspace_token(request: Request, db: Session = Depends(get_db)):
    token = request.headers.get("X-Token", "")
    if not token:
        raise HTTPException(status_code=401, detail="Missing token")

    payload = verify_access_token(token)
    if not payload:
        raise HTTPException(status_code=401, detail="Invalid or expired token")

    instance_id = payload.get("instance_id")
    user_id = payload.get("user_id")

    inst = db.query(Instance).filter(Instance.instance_id == instance_id).first()
    if not inst or inst.user_id != user_id:
        raise HTTPException(status_code=403, detail="Invalid instance")

    if inst.status != InstanceStatus.RUNNING:
        raise HTTPException(status_code=403, detail="Instance is not running")

    return Response(
        status_code=200,
        headers={
            "X-User-Id": user_id,
            "X-Instance-Id": instance_id,
        },
    )


@router.get("/{instance_id}/state")
async def get_workspace_state(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    # Authenticate and get instance
    _get_instance(instance_id, auth["user_id"], db)

    ws_state = db.query(WorkspaceState).filter(WorkspaceState.instance_id == instance_id).first()
    if not ws_state:
        return {"state_data": {}}
    return {"state_data": ws_state.state_data, "last_synced_at": ws_state.last_synced_at.isoformat()}


@router.put("/{instance_id}/state")
async def update_workspace_state(instance_id: str, request: Request, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    # Authenticate and get instance
    _get_instance(instance_id, auth["user_id"], db)

    body = await request.json()
    state_data = body.get("state_data", {})

    ws_state = db.query(WorkspaceState).filter(WorkspaceState.instance_id == instance_id).first()
    if not ws_state:
        ws_state = WorkspaceState(instance_id=instance_id, state_data=state_data)
        db.add(ws_state)
    else:
        ws_state.state_data = state_data

    db.commit()
    return {"status": "ok"}


@router.post("/{instance_id}/upgrade", response_model=UpgradeResponse)
async def upgrade_instance(
    instance_id: str,
    body: UpgradeRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    """订阅生效:docker update 热改容器资源 + 改 plan + 清免费窗。
    成功标 needs_rebuild(log 重建闭合);失败标 needs_upgrade(scanner 补)。"""
    inst = _get_instance(instance_id, auth["user_id"], db)
    try:
        target_plan = PlanType(body.plan)
    except ValueError:
        raise HTTPException(status_code=400, detail=f"invalid plan: {body.plan}")

    ok = docker_manager.update_container_resources(
        inst.container_name, body.cpu_quota, body.mem_limit, body.pids_limit
    )
    if ok:
        inst.plan = target_plan
        inst.expected_plan = target_plan
        inst.expires_at = None
        inst.needs_upgrade = False
        inst.upgrade_retries = 0
        # log_config 升档需重建闭合(漏洞 3);仅当新 plan 的 log 配置不同于 FREE(升级必变)
        inst.needs_rebuild = True
        db.commit()
        return UpgradeResponse(
            instance_id=inst.instance_id, status=inst.status,
            plan=inst.plan, needs_upgrade=False,
        )
    else:
        # docker update 失败:落 needs_upgrade 给 scanner 补;envelope 把 500 映射成 success=False。
        inst.needs_upgrade = True
        inst.expected_plan = target_plan
        db.commit()
        raise HTTPException(status_code=500, detail="upgrade failed; marked needs_upgrade for scanner")


@router.post("/{instance_id}/downgrade", response_model=DowngradeResponse)
async def downgrade_instance(
    instance_id: str,
    body: DowngradeRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    """订阅过期:改 plan=FREE + 设免费 2h 窗。不碰运行容器(Q6 防止跑着任务 OOM)。
    不调 docker update — 留给下次启动按 FREE 档起 / 现成免费睡 scanner stop。"""
    inst = _get_instance(instance_id, auth["user_id"], db)
    try:
        target_plan = PlanType(body.plan)
    except ValueError:
        raise HTTPException(status_code=400, detail=f"invalid plan: {body.plan}")

    try:
        expires_at = datetime.fromisoformat(body.expires_at.replace("Z", "+00:00"))
    except ValueError:
        raise HTTPException(status_code=400, detail="invalid expires_at ISO8601")

    inst.plan = target_plan
    inst.expected_plan = target_plan
    inst.expires_at = expires_at
    inst.storage_quota_gb = PLAN_STORAGE_GB.get(target_plan.value, PLAN_STORAGE_GB["FREE"])
    db.commit()
    return DowngradeResponse(
        instance_id=inst.instance_id, status=inst.status,
        plan=inst.plan, expires_at=body.expires_at,
    )

