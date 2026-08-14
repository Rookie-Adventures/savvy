import hashlib
import hmac
import time

import pytest
from app.auth import verify_hmac_signature
from app.token import generate_access_token, verify_access_token


SECRET = "test-secret"


def _sign(method: str, path: str, body: bytes, timestamp: str, nonce: str) -> str:
    body_hash = hashlib.sha256(body).hexdigest()
    message = f"{method}\n{path}\n{body_hash}\n{timestamp}\n{nonce}"
    return hmac.new(SECRET.encode(), message.encode(), hashlib.sha256).hexdigest()


class TestHMACVerification:
    def test_valid_signature(self):
        ts = str(int(time.time()))
        sig = _sign("POST", "/internal/users/upsert", b'{"user_id":"1"}', ts, "n1")
        assert verify_hmac_signature(SECRET, "POST", "/internal/users/upsert", b'{"user_id":"1"}', ts, "n1", sig)

    def test_invalid_signature(self):
        ts = str(int(time.time()))
        assert not verify_hmac_signature(SECRET, "POST", "/internal/users/upsert", b'{}', ts, "n1", "bad-sig")

    def test_stale_timestamp(self):
        ts = str(int(time.time()) - 600)
        sig = _sign("GET", "/health", b"", ts, "n1")
        assert not verify_hmac_signature(SECRET, "GET", "/health", b"", ts, "n1", sig)

    def test_empty_headers(self):
        assert not verify_hmac_signature(SECRET, "GET", "/health", b"", "", "", "")

    def test_wrong_method(self):
        ts = str(int(time.time()))
        sig = _sign("GET", "/health", b"", ts, "n1")
        assert not verify_hmac_signature(SECRET, "POST", "/health", b"", ts, "n1", sig)

    def test_wrong_secret(self):
        ts = str(int(time.time()))
        sig = _sign("GET", "/health", b"", ts, "n1")
        assert not verify_hmac_signature("wrong-secret", "GET", "/health", b"", ts, "n1", sig)


class TestAccessToken:
    def test_generate_and_verify_token(self):
        result = generate_access_token("inst-123", "user-456", expires_in_minutes=30)
        assert "token" in result
        assert "expires_at" in result
        assert "workspace_url" in result

        payload = verify_access_token(result["token"])
        assert payload is not None
        assert payload["instance_id"] == "inst-123"
        assert payload["user_id"] == "user-456"

    def test_invalid_token(self):
        assert verify_access_token("invalid-token") is None

    def test_tampered_token(self):
        result = generate_access_token("inst-123", "user-456")
        token = result["token"]
        # Tamper with the token
        tampered = token[:-5] + "XXXXX"
        assert verify_access_token(tampered) is None

    def test_renew_access_token_returns_valid_new_token(self):
        from app.token import renew_access_token
        token = renew_access_token("inst-123", "user-456", expires_in_minutes=30)
        assert isinstance(token, str)
        assert "." in token  # payload.signature shape
        payload = verify_access_token(token)
        assert payload is not None
        assert payload["instance_id"] == "inst-123"
        assert payload["user_id"] == "user-456"
        # renewed token must have a fresh exp in the future
        import time
        assert payload["exp"] > int(time.time())

    def test_renewed_token_independent_of_old(self):
        from app.token import renew_access_token
        old = generate_access_token("inst-1", "u-1", expires_in_minutes=1)
        new = renew_access_token("inst-1", "u-1", expires_in_minutes=30)
        assert old["token"] != new  # different exp → different payload → different token
