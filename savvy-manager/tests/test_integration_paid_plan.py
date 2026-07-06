"""Task 7 集成测试:paid plan 全链 mock_mode FREE→STARTER→FREE。"""
import base64
from datetime import datetime, timedelta, timezone

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker

from app.database import Base, get_db
from app.main import app
from app.models import Instance, InstanceStatus, PlanType


SQLALCHEMY_DATABASE_URL = "sqlite:///./test_integration_paid_plan.db"
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
    from app.auth import require_hmac
    app.dependency_overrides[require_hmac] = lambda: {"user_id": "1"}
    yield TestClient(app)
    app.dependency_overrides.clear()


def test_upgrade_then_downgrade_lifecycle(client, db_session, monkeypatch):
    """全链:FREE RUNNING → upgrade STARTER(plan/expires/expected_plan 清窗)→ downgrade FREE(2h窗)。"""
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)

    inst = Instance(
        instance_id="i1", user_id="1", status=InstanceStatus.RUNNING, plan=PlanType.FREE,
        container_name="c1", volume_name="v1", assigned_port=41000,
        expires_at=datetime.now(timezone.utc) + timedelta(minutes=30),
    )
    db_session.add(inst)
    db_session.commit()

    # Upgrade to STARTER
    res = client.post("/internal/instances/i1/upgrade", json={
        "plan": "STARTER", "cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512,
    })
    assert res.json()["success"] is True
    db_session.refresh(inst)
    assert inst.plan == PlanType.STARTER
    assert inst.expires_at is None

    # Downgrade to FREE
    res = client.post("/internal/instances/i1/downgrade", json={
        "plan": "FREE",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=2)).isoformat(),
    })
    assert res.json()["success"] is True
    db_session.refresh(inst)
    assert inst.plan == PlanType.FREE
    assert inst.expires_at is not None
    assert inst.expected_plan == PlanType.FREE
