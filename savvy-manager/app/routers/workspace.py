from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy.orm import Session
from ..database import get_db
from ..models import Instance, InstanceStatus
from ..token import verify_access_token

router = APIRouter(prefix="/internal/workspace", tags=["workspace"])


@router.get("/validate")
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

    # In docker bridge networks, we can resolve container by name.
    # Workspace service runs on port 3000 inside the container.
    upstream_url = f"http://{inst.container_name}:3000"

    return Response(
        status_code=200,
        headers={
            "X-User-Id": user_id,
            "X-Instance-Id": instance_id,
            "X-Workspace-Upstream": upstream_url,
        },
    )
