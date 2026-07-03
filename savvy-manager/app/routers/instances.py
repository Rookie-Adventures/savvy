from datetime import datetime, timedelta, timezone
from fastapi import APIRouter, Depends, HTTPException, Request, Response
from pydantic import BaseModel
from sqlalchemy.orm import Session
from ..auth import require_hmac
from ..config import settings
from ..database import get_db
from ..models import Instance, WorkspaceState, InstanceStatus, PlanType
from ..docker_manager import start_container, stop_container
from ..token import generate_access_token, verify_access_token

router = APIRouter(prefix="/internal/instances", tags=["instances"])


class StartResponse(BaseModel):
    instance_id: str
    status: InstanceStatus
    started_at: str
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


class StartRequest(BaseModel):
    provider_api_key: str | None = None
    provider_base_url: str | None = None
    provider_model: str | None = None


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
    from ..provider_config import build_snapshot, reconcile_snapshot, render_config_yaml

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
        snap = build_snapshot(
            api_key=body.provider_api_key,
            base_url=body.provider_base_url,
            model=body.provider_model,
            source=source,
        )
        enc, alg = crypto.encrypt_provider_config(snap)
        inst.provider_config_enc = enc
        inst.provider_config_alg = alg
        inst.provider_key_set_at = datetime.now(timezone.utc)

    now = datetime.now(timezone.utc)
    expires_at = None
    if inst.plan == PlanType.FREE:
        expires_at = now + timedelta(hours=3)

    # Reconcile on wake: if NOT_CREATED we will create; if SLEEPING we may
    # have a container-side config.yaml the user edited — adopt it.
    provider_config_for_create = None
    if inst.status == InstanceStatus.SLEEPING:
        # Read container config (best-effort) and reconcile.
        from ..docker_manager import _client_or_none
        client = _client_or_none()
        if client is not None:
            try:
                c = client.containers.get(inst.container_name)
                res = c.exec_run(["sh", "-c", "cat /opt/data/config.yaml 2>/dev/null || true"])
                yaml_text = ""
                if getattr(res, "exit_code", 1) == 0 and res.output:
                    yaml_text = res.output.decode("utf-8", errors="ignore") if isinstance(res.output, bytes) else str(res.output)
                db_snap = None
                if inst.provider_config_enc:
                    db_snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg or "fernet")
                new_snap, changed = reconcile_snapshot(db_snapshot=db_snap, container_yaml=yaml_text)
                if changed and new_snap is not None:
                    enc, alg = crypto.encrypt_provider_config(new_snap)
                    inst.provider_config_enc = enc
                    inst.provider_config_alg = alg
                    inst.provider_key_set_at = datetime.now(timezone.utc)
            except Exception:
                pass  # Best-effort; container may be gone.
    elif inst.status == InstanceStatus.NOT_CREATED and inst.provider_config_enc:
        provider_config_for_create = crypto.decrypt_provider_config(
            inst.provider_config_enc, inst.provider_config_alg or "fernet"
        )

    docker_result = start_container(inst.container_name)
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
        if not start_container(inst.container_name):
            raise HTTPException(
                status_code=500,
                detail="Failed to start container after creation",
            )

    inst.status = InstanceStatus.RUNNING
    inst.started_at = now
    inst.expires_at = expires_at
    db.commit()

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

