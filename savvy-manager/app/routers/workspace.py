import time

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy.orm import Session
from ..database import get_db
from ..models import Instance, InstanceStatus
from ..token import verify_access_token, renew_access_token

router = APIRouter(prefix="/internal/workspace", tags=["workspace"])


def _token_from_uri_query(uri: str) -> str:
    """从 URI（可能相对路径）的 query 里提 token。"""
    if "token=" not in uri:
        return ""
    try:
        from urllib.parse import urlparse, parse_qs
        parsed = urlparse(uri if "://" in uri else f"http://dummy{uri}")
        token_list = parse_qs(parsed.query).get("token", [])
        return token_list[0] if token_list else ""
    except Exception:
        return ""


@router.get("/validate")
async def validate_workspace_token(request: Request, db: Session = Depends(get_db)):
    # Token 优先级在 Python 侧决策（nginx 1.31 的 map 链在 auth_request 子请求
    # 上下文取值不可靠，实测 arg 会被 cookie 覆盖）：
    # 1. X-Token (query arg，nginx 透传 $arg_token)
    # 2. X-Token-Cookie (workspace_token cookie，nginx 透传)
    # 3. X-Original-URI 的 query token (绕 nginx 变量作用域问题)
    # 4. Referer 的 query token (首屏并发子资源：favicon/sw.js 无 cookie 时兜底)
    token = request.headers.get("X-Token", "")

    if not token:
        token = request.headers.get("X-Token-Cookie", "")

    if not token:
        token = _token_from_uri_query(request.headers.get("X-Original-URI", ""))

    if not token:
        token = _token_from_uri_query(request.headers.get("Referer", ""))

    if not token:
        raise HTTPException(status_code=401, detail="Missing token")

    payload = verify_access_token(token)
    if not payload:
        raise HTTPException(status_code=401, detail="Invalid or expired token")

    instance_id = payload.get("instance_id")
    user_id = payload.get("user_id")

    inst = db.query(Instance).filter(Instance.instance_id == instance_id).first()
    if not inst or inst.user_id != user_id:
        # TEMP DIAG: 记 403 分支与 DB 实际值，定位完删。
        print(f"[DIAG_403] branch=invalid_instance tok_uid={user_id!r} db_uid={getattr(inst, 'user_id', None)!r} found={inst is not None}", flush=True)
        raise HTTPException(status_code=403, detail="Invalid instance")

    if inst.status != InstanceStatus.RUNNING:
        print(f"[DIAG_403] branch=not_running status={inst.status} inst={instance_id}", flush=True)
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
