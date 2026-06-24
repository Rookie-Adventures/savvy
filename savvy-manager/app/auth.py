import hashlib
import hmac
import time
from fastapi import Request, HTTPException


def verify_hmac_signature(
    secret: str,
    method: str,
    path: str,
    body: bytes,
    timestamp: str,
    nonce: str,
    signature: str,
    max_age: int = 300,
) -> bool:
    """Verify HMAC-SHA256 signature from new-api backend."""
    try:
        ts = int(timestamp)
    except (ValueError, TypeError):
        return False

    if abs(time.time() - ts) > max_age:
        return False

    body_hash = hashlib.sha256(body).hexdigest()
    message = f"{method}\n{path}\n{body_hash}\n{timestamp}\n{nonce}"
    expected = hmac.new(
        secret.encode(), message.encode(), hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, signature)


async def require_hmac(request: Request) -> dict:
    """FastAPI dependency: verify HMAC headers and return verified claims."""
    from .config import settings

    timestamp = request.headers.get("X-Savvy-Timestamp", "")
    nonce = request.headers.get("X-Savvy-Nonce", "")
    signature = request.headers.get("X-Savvy-Signature", "")
    user_id = request.headers.get("X-Savvy-User-Id", "")

    if not all([timestamp, nonce, signature, user_id]):
        raise HTTPException(status_code=401, detail="Missing HMAC headers")

    body = await request.body()
    if not verify_hmac_signature(
        secret=settings.hmac_secret,
        method=request.method,
        path=str(request.url.path),
        body=body,
        timestamp=timestamp,
        nonce=nonce,
        signature=signature,
        max_age=settings.hmac_max_age,
    ):
        raise HTTPException(status_code=401, detail="Invalid HMAC signature")

    return {"user_id": user_id}
