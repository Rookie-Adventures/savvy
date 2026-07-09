import base64
from datetime import datetime, timedelta, timezone
import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from app.main import app
from app.database import Base, get_db
from app.models import Instance, InstanceStatus, PlanType, User


SQLALCHEMY_DATABASE_URL = "sqlite:///./test_instances_router.db"
engine = create_engine(SQLALCHEMY_DATABASE_URL, connect_args={"check_same_thread": False})
TestingSessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)


@pytest.fixture(name="db_session")
def fixture_db_session(monkeypatch):
    Base.metadata.create_all(bind=engine)
    monkeypatch.setenv("SAVVY_PROVIDER_ENC_KEY", base64.urlsafe_b64encode(b"0" * 32).decode())
    from importlib import reload
    from app import config, crypto
    reload(config); reload(crypto)
    db = TestingSessionLocal()
    try:
        yield db
    finally:
        db.close()
        Base.metadata.drop_all(bind=engine)


@pytest.fixture(name="client")
def fixture_client(db_session):
    def override_get_db():
        try:
            yield db_session
        finally:
            pass
    app.dependency_overrides[get_db] = override_get_db
    # Bypass HMAC by overriding dependency
    from app.auth import require_hmac
    app.dependency_overrides[require_hmac] = lambda: {"user_id": "1"}
    yield TestClient(app)
    app.dependency_overrides.clear()


def _create_test_instance(db, instance_id="inst-1", status=InstanceStatus.NOT_CREATED):
    u = User(user_id="1", plan=PlanType.FREE)
    db.add(u)
    inst = Instance(
        instance_id=instance_id, user_id="1", status=status, plan=PlanType.FREE,
        container_name="savvy-u1-w1", volume_name="savvy-u1-data", assigned_port=41000,
    )
    db.add(inst)
    db.commit()
    return inst


def test_start_requires_provider_key_on_first_start(client, db_session):
    """Spec §4 Path A: first-start hard lock — 400 (enveloped) if no key when
    provider_config_enc is None. Error message must mention provider_api_key."""
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    res = client.post("/internal/instances/inst-1/start", json={})
    body = res.json()
    # Envelope maps HTTPException(400) to {success: false, message: <detail>} status 200.
    assert body["success"] is False
    assert "provider_api_key" in body["message"]


def test_start_with_provider_key_encrypts_snapshot(client, db_session, monkeypatch):
    """Spec §4: providing key on first-start → DB snapshot encrypted, source='ours'."""
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    # Stub docker create/start: skip real docker
    from app import docker_manager
    monkeypatch.setattr(docker_manager, "start_container", lambda name: True)
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    # Stub probe_default_model: avoid real network call to new-api /v1/models.
    from app import provider_config
    monkeypatch.setattr(provider_config, "probe_default_model", lambda **k: "deepseek-v4-flash")

    res = client.post("/internal/instances/inst-1/start", json={
        "provider_api_key": "sk-abc1234567890123",
    })
    body = res.json()
    assert body["success"] is True, res.text
    # Reload instance from session and check encrypted snapshot persisted
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.provider_config_enc is not None
    assert inst.provider_config_alg == "fernet"
    from app import crypto
    snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg)
    assert snap["api_key"] == "sk-abc1234567890123"
    assert snap["source"] == "ours"


def test_revoke_clears_snapshot(client, db_session, monkeypatch):
    """Spec §4 C: revoke clears DB snapshot + key_set_at (best-effort container clear)."""
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    # Seed an encrypted snapshot
    from app import crypto
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-x", "model": "claude-sonnet-4", "source": "ours"}
    enc, alg = crypto.encrypt_provider_config(snap)
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    inst.provider_config_enc = enc
    inst.provider_config_alg = alg
    db_session.commit()

    # Stub docker so revoke's exec works
    from app import docker_manager
    fake_container = type("C", (), {"exec_run": lambda self, cmd: type("R", (), {"exit_code": 0})()})()
    fake_client = type("K", (), {"containers": type("CC", (), {"get": lambda self, n: fake_container})()})()
    monkeypatch.setattr(docker_manager, "_client", fake_client)

    res = client.post("/internal/instances/inst-1/revoke-provider-key")
    body = res.json()
    assert body["success"] is True
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.provider_config_enc is None
    assert inst.provider_key_set_at is None


def test_revoke_skips_writeback_on_parse_failure(client, db_session, monkeypatch):
    """Spec §4 C guard: when container config.yaml is parse-broken, revoke must
    clear DB (canonical) but SKIP the docker exec write-back so it doesn't
    truncate the user's config to empty. Container config stays as-is."""
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    from app import crypto
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-x", "model": "claude-sonnet-4", "source": "ours"}
    enc, alg = crypto.encrypt_provider_config(snap)
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    inst.provider_config_enc = enc
    inst.provider_config_alg = alg
    db_session.commit()

    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    broken_yaml = b": : :\n  model:\n    provider: custom"
    calls = []

    def fake_exec_run(self, cmd):
        calls.append(cmd)
        # First call is the read (cat ...). Return the broken yaml bytes.
        if isinstance(cmd, list) and len(cmd) >= 3 and "cat" in str(cmd[2]) and "base64 -d" not in str(cmd[2]):
            return type("R", (), {"exit_code": 0, "output": broken_yaml})()
        # Any subsequent call would be the write-back ('echo ... | base64 -d > ...').
        return type("R", (), {"exit_code": 0, "output": b""})()

    fake_container = type("C", (), {"exec_run": fake_exec_run})()
    fake_client = type("K", (), {"containers": type("CC", (), {"get": lambda self, n: fake_container})()})()
    monkeypatch.setattr(docker_manager, "_client", fake_client)

    res = client.post("/internal/instances/inst-1/revoke-provider-key")
    assert res.json()["success"] is True

    # DB cleared (canonical).
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.provider_config_enc is None
    assert inst.provider_key_set_at is None

    # The write-back exec_run (echo '<b64>' | base64 -d > /opt/data/config.yaml) must NOT fire.
    writeback = [c for c in calls if isinstance(c, list) and "base64 -d > /opt/data/config.yaml" in str(c[2] if len(c) > 2 else "")]
    assert len(writeback) == 0, f"write-back should be skipped, but saw: {writeback}"


def test_provider_state_returns_source(client, db_session):
    """Spec §4: provider-state returns source/model/key_set_at, NEVER api_key."""
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    from app import crypto
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-x", "model": "claude-sonnet-4", "source": "user"}
    enc, alg = crypto.encrypt_provider_config(snap)
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    inst.provider_config_enc = enc
    inst.provider_config_alg = alg
    db_session.commit()

    res = client.get("/internal/instances/inst-1/provider-state")
    body = res.json()
    # Envelope wraps the endpoint dict into data.
    data = body["data"]
    assert data["source"] == "user"
    assert "api_key" not in res.text  # secret must not leak


def test_free_start_sets_2h_expiry(client, db_session, monkeypatch):
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    # Stub docker create/start: skip real docker
    from app import docker_manager
    monkeypatch.setattr(docker_manager, "start_container", lambda name: True)
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    # Stub probe_default_model
    from app import provider_config
    monkeypatch.setattr(provider_config, "probe_default_model", lambda **k: "deepseek-v4-flash")

    res = client.post("/internal/instances/inst-1/start", json={
        "provider_api_key": "sk-abc1234567890123",
    })
    body = res.json()
    assert body["success"] is True

    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.expires_at is not None
    assert inst.started_at is not None
    delta = inst.expires_at - inst.started_at
    assert 7100 <= delta.total_seconds() <= 7260


def test_create_instance_sets_not_created(client, db_session):
    # A brand-new instance must start as NOT_CREATED (the frontend maps this to
    # "creating" via normalizeStatus → isFirstStart=true → shows the key box).
    # Setting SLEEPING here breaks first-start UX: the dialog renders in the
    # wake branch (no key box). Regression guard for bug-2.
    db_session.add(User(user_id="1", plan=PlanType.FREE))
    db_session.commit()

    res = client.post("/internal/users/1/instance")

    assert res.status_code == 200
    body = res.json()
    assert body["success"] is True
    assert body["data"]["status"] == "NOT_CREATED"
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst is not None
    assert inst.status == InstanceStatus.NOT_CREATED


def test_create_instance_free_sets_storage_quota(client, db_session):
    # FREE 档新建实例必须落 storage_quota_gb=5 (PLAN_STORAGE_GB["FREE"]),
    # 否则 check_storage_quota 扫 isnot(None) 跳过 → 软配额永久失效。
    db_session.add(User(user_id="1", plan=PlanType.FREE))
    db_session.commit()

    res = client.post("/internal/users/1/instance")

    assert res.status_code == 200
    assert res.json()["success"] is True
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst is not None
    assert inst.storage_quota_gb == 5


def _create_running_paid_instance(db, instance_id="inst-1", plan=PlanType.STARTER):
    u = User(user_id="1", plan=plan)
    db.add(u)
    inst = Instance(
        instance_id=instance_id, user_id="1", status=InstanceStatus.RUNNING,
        plan=PlanType.FREE, container_name="savvy-u1-w1", volume_name="savvy-u1-data",
        assigned_port=41000, expires_at=datetime.now(timezone.utc) + timedelta(hours=1),
    )
    db.add(inst)
    db.commit()
    return inst


def test_upgrade_success_changes_plan_and_clears_expiry(client, db_session, monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/upgrade", json={
        "plan": "STARTER", "cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512,
    })
    body = res.json()
    assert body["success"] is True
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.STARTER
    assert inst.expires_at is None          # 免睡
    assert inst.expected_plan == PlanType.STARTER
    assert inst.needs_upgrade is False
    assert inst.needs_rebuild is True       # log_config 升档 → 标重建


def test_upgrade_failure_marks_needs_upgrade(client, db_session, monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    # Force update_container_resources to fail
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: False)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/upgrade", json={
        "plan": "PRO", "cpu_quota": 400000, "mem_limit": "8g", "pids_limit": 1024,
    })
    body = res.json()
    assert body["success"] is False
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.needs_upgrade is True
    assert inst.expected_plan == PlanType.PRO
    assert inst.plan == PlanType.FREE        # 未改成


def test_downgrade_sets_free_and_2h_expiry_no_touch_container(client, db_session, monkeypatch):
    from app import docker_manager
    stop_called = []
    monkeypatch.setattr(docker_manager, "stop_container", lambda name: stop_called.append(name) or True)
    _create_running_paid_instance(db_session)
    res = client.post("/internal/instances/inst-1/downgrade", json={
        "plan": "FREE",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=2)).isoformat(),
    })
    body = res.json()
    assert body["success"] is True
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.FREE
    assert inst.expected_plan == PlanType.FREE
    assert inst.expires_at is not None       # 设了 2h 窗
    assert stop_called == []                 # 不碰运行容器


def test_start_expected_plan_aligns_drifted_instance(client, db_session, monkeypatch):
    """Closes the upgrade-window hole: a FREE instance (subscribed while NOT
    RUNNING, so the upgrade route never ran) must align inst.plan to
    expected_plan on start, clear drift flags, and skip the 2h free window."""
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    from app import docker_manager, provider_config
    monkeypatch.setattr(docker_manager, "start_container", lambda name: True)
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    monkeypatch.setattr(provider_config, "probe_default_model", lambda **k: "deepseek-v4-flash")

    res = client.post("/internal/instances/inst-1/start", json={
        "provider_api_key": "sk-abc1234567890123",
        "expected_plan": "STARTER",
    })
    assert res.json()["success"] is True
    db_session.expire_all()
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.STARTER
    assert inst.expected_plan is None
    assert inst.needs_upgrade is False
    assert inst.expires_at is None            # paid plan → no 2h window
    assert inst.storage_quota_gb == 20        # plan upgrade syncs the soft-quota tier


def test_start_expected_plan_free_keeps_2h_window(client, db_session, monkeypatch):
    """expected_plan=FREE aligns and still applies the 2h free window."""
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    from app import docker_manager, provider_config
    monkeypatch.setattr(docker_manager, "start_container", lambda name: True)
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)
    monkeypatch.setattr(provider_config, "probe_default_model", lambda **k: "deepseek-v4-flash")

    res = client.post("/internal/instances/inst-1/start", json={
        "provider_api_key": "sk-abc1234567890123",
        "expected_plan": "FREE",
    })
    assert res.json()["success"] is True
    db_session.expire_all()
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    assert inst.plan == PlanType.FREE
    assert inst.expires_at is not None


def test_get_instance_returns_spec_fields(client, db_session):
    """InstanceResponse must surface cpu_quota/mem_limit/pids_limit/
    storage_quota_gb (mapped from PLAN_RESOURCES by plan) for frontend display."""
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    inst = db_session.query(Instance).filter_by(instance_id="inst-1").first()
    inst.plan = PlanType.PRO
    inst.storage_quota_gb = 80
    db_session.commit()

    res = client.get("/internal/users/1/instance")
    data = res.json()["data"]
    assert data["plan"] == "PRO"
    assert data["cpu_quota"] == 400000
    assert data["mem_limit"] == "8g"
    assert data["pids_limit"] == 1024
    assert data["storage_quota_gb"] == 50

