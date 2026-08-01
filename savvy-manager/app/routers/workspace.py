import time

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy.orm import Session
from ..database import get_db
from ..models import Instance, InstanceStatus
from ..token import verify_access_token, renew_access_token

router = APIRouter(prefix="/internal/workspace", tags=["workspace"])


@router.get("/validate")
async def validate_workspace_token(request: Request, db: Session = Depends(get_db)):
    # 1. Try to get token from X-Token header (custom injected header)
    token = request.headers.get("X-Token", "")

    # 2. Fallback: Parse the token from X-Original-URI header to bypass Nginx subrequest variable scope issues
    if not token:
        original_uri = request.headers.get("X-Original-URI", "")
        if "token=" in original_uri:
            try:
                from urllib.parse import urlparse, parse_qs
                # URI might be relative, add dummy host for proper parsing
                parsed = urlparse(original_uri if "://" in original_uri else f"http://dummy{original_uri}")
                queries = parse_qs(parsed.query)
                token_list = queries.get("token", [])
                if token_list:
                    token = token_list[0]
            except Exception:
                pass

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

    # Sliding renewal: if token has <5min left, sign a fresh one and hand it
    # to nginx via header so it can refresh the workspace_token cookie.
    renewed_token = None
    remaining = payload.get("exp", 0) - int(time.time())
    if remaining < 300:
        renewed_token = renew_access_token(instance_id, user_id, expires_in_minutes=30)

    headers = {
        "X-User-Id": user_id,
        "X-Instance-Id": instance_id,
        "X-Workspace-Upstream": upstream_url,
    }
    if renewed_token:
        headers["X-Renewed-Token"] = renewed_token

    return Response(status_code=200, headers=headers)
