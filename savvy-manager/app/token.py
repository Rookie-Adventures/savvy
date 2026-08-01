import hashlib
import hmac
import json
import os
import time
from base64 import urlsafe_b64decode, urlsafe_b64encode
from datetime import datetime, timedelta, timezone


def get_secret() -> str:
    return os.environ.get("SAVVY_HMAC_SECRET", "dev-hmac-secret-change-me")


def generate_access_token(
    instance_id: str,
    user_id: str,
    expires_in_minutes: int = 30,
    workspace_host: str = "localhost",
    workspace_port: int = 41000,
) -> dict:
    now = datetime.now(timezone.utc)
    expires_at = now + timedelta(minutes=expires_in_minutes)

    payload = {
        "instance_id": instance_id,
        "user_id": user_id,
        "iat": int(now.timestamp()),
        "exp": int(expires_at.timestamp()),
    }

    payload_b64 = urlsafe_b64encode(json.dumps(payload).encode()).decode()
    signature = hmac.new(
        get_secret().encode(),
        payload_b64.encode(),
        hashlib.sha256,
    ).hexdigest()

    token = f"{payload_b64}.{signature}"

    return {
        "token": token,
        "expires_at": expires_at.isoformat(),
        "workspace_url": (
            f"{workspace_host}:{workspace_port}/"
            if workspace_host.startswith(("http://", "https://"))
            else f"https://{workspace_host}:{workspace_port}/"
        ),
    }


def renew_access_token(
    instance_id: str,
    user_id: str,
    expires_in_minutes: int = 30,
) -> str:
    """Sign a fresh access token (sliding renewal). Reuses generate_access_token's
    signing path; returns just the token string, no workspace_url."""
    return generate_access_token(
        instance_id=instance_id,
        user_id=user_id,
        expires_in_minutes=expires_in_minutes,
    )["token"]


def verify_access_token(token: str) -> dict | None:
    try:
        from urllib.parse import unquote
        # URL decode the token to restore Base64 padded chars like %3D%3D into ==
        token = unquote(token)

        parts = token.split(".")
        if len(parts) != 2:
            return None

        payload_b64, signature = parts

        expected_signature = hmac.new(
            get_secret().encode(),
            payload_b64.encode(),
            hashlib.sha256,
        ).hexdigest()

        if not hmac.compare_digest(expected_signature, signature):
            return None

        payload = json.loads(urlsafe_b64decode(payload_b64))

        if payload.get("exp", 0) < time.time():
            return None

        return payload
    except Exception:
        return None
