from datetime import datetime, timedelta, timezone
from fastapi import APIRouter, Depends, HTTPException, Request, Response
from pydantic import BaseModel
from sqlalchemy.orm import Session
from ..auth import require_hmac
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


@router.post("/{instance_id}/start", response_model=StartResponse)
async def start_instance(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status not in (InstanceStatus.SLEEPING, InstanceStatus.NOT_CREATED):
        raise HTTPException(
            status_code=409, detail=f"Cannot start from status {inst.status}"
        )

    now = datetime.now(timezone.utc)
    expires_at = None
    if inst.plan == PlanType.FREE:
        expires_at = now + timedelta(hours=3)

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

    result = generate_access_token(
        instance_id=instance_id,
        user_id=auth["user_id"],
        expires_in_minutes=30,
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

