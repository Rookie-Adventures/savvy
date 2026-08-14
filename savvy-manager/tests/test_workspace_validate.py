import time
from fastapi.testclient import TestClient
from app.main import app
from app.database import get_db
from app.models import Instance, InstanceStatus


def _make_token(instance_id="inst-test", user_id="u-1", ttl_minutes=30):
    from app.token import generate_access_token
    return generate_access_token(instance_id, user_id, expires_in_minutes=ttl_minutes)["token"]


class _FakeInstance:
    """Minimal stand-in for an Instance ORM row."""
    def __init__(self, instance_id, user_id):
        self.instance_id = instance_id
        self.user_id = user_id
        self.status = InstanceStatus.RUNNING
        self.container_name = "fake-ctr"


class _FakeQuery:
    """Mimics db.query(Instance).filter(...).first() chain."""
    def __init__(self, inst):
        self._inst = inst

    def filter(self, *a):
        return self

    def first(self):
        return self._inst


class _FakeDB:
    """Mimics the db session enough for validate_workspace_token."""
    def __init__(self, inst):
        self._inst = inst

    def query(self, model):
        return _FakeQuery(self._inst)


def _override_db(inst):
    """Return a get_db override that yields a _FakeDB wired to `inst`."""
    def _get_db_override():
        yield _FakeDB(inst)
    return _get_db_override


def test_validate_emits_renewed_token_when_near_expiry():
    """Token with <5min left must trigger X-Renewed-Token."""
    from app.token import generate_access_token, verify_access_token

    # token expiring in 2 minutes (< 300s threshold)
    near_token = generate_access_token("inst-near", "u-near", expires_in_minutes=2)["token"]

    inst = _FakeInstance("inst-near", "u-near")
    app.dependency_overrides[get_db] = _override_db(inst)
    try:
        client = TestClient(app, raise_server_exceptions=False)
        res = client.get("/internal/workspace/validate", headers={"X-Token": near_token})
        assert res.status_code == 200, f"Expected 200, got {res.status_code}: {res.text}"
        assert res.headers.get("x-renewed-token"), "Near-expiry token should produce X-Renewed-Token header"
        # renewed token must verify
        assert verify_access_token(res.headers["x-renewed-token"]) is not None
    finally:
        app.dependency_overrides.pop(get_db, None)


def test_validate_no_renewed_token_when_far_from_expiry():
    """Token with >5min left must NOT emit X-Renewed-Token."""
    from app.token import generate_access_token

    fresh_token = generate_access_token("inst-fresh", "u-fresh", expires_in_minutes=30)["token"]

    inst = _FakeInstance("inst-fresh", "u-fresh")
    app.dependency_overrides[get_db] = _override_db(inst)
    try:
        client = TestClient(app, raise_server_exceptions=False)
        res = client.get("/internal/workspace/validate", headers={"X-Token": fresh_token})
        assert res.status_code == 200, f"Expected 200, got {res.status_code}: {res.text}"
        assert not res.headers.get("x-renewed-token"), "Fresh token should NOT produce X-Renewed-Token header"
    finally:
        app.dependency_overrides.pop(get_db, None)
