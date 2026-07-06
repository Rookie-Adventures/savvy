import logging
import pytest
from app.models import Instance, InstanceStatus, PlanType
from app.database import Base
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker


@pytest.fixture(autouse=True)
def _enable_scanner_logger():
    """alembic env.py calls fileConfig(disable_existing_loggers=True) when
    test_provider_config runs migrations, which sets app.scanner.disabled=True
    at module scope (not restored between tests). Re-enable so caplog can
    capture scanner error logs. Ponytail: smallest in-scope fix — root cause
    is in alembic env.py (out of Task 4 scope)."""
    logging.getLogger("app.scanner").disabled = False
    yield


@pytest.fixture(name="db_session")
def fixture_db_session(monkeypatch):
    engine = create_engine("sqlite:///./test_scanner.db", connect_args={"check_same_thread": False})
    Base.metadata.create_all(bind=engine)
    Session = sessionmaker(autocommit=False, autoflush=False, bind=engine)
    db = Session()
    try:
        yield db
    finally:
        db.close()
        Base.metadata.drop_all(bind=engine)


def _mk_instance(db, **kw):
    defaults = dict(
        instance_id="i1", user_id="1", status=InstanceStatus.RUNNING,
        plan=PlanType.FREE, container_name="c1", volume_name="v1", assigned_port=41000,
    )
    defaults.update(kw)
    inst = Instance(**defaults)
    db.add(inst)
    db.commit()
    return inst


def test_check_needs_upgrade_retries_then_alerts(monkeypatch, db_session, caplog):
    from app import scanner, docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    # Force update failure
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: False)
    inst = _mk_instance(db_session, needs_upgrade=True, expected_plan=PlanType.STARTER, upgrade_retries=0)

    for _ in range(3):
        scanner.check_needs_upgrade(db_session)
    db_session.refresh(inst)
    assert inst.needs_upgrade is False   # 停手
    assert inst.upgrade_retries == 3
    assert any("upgrade" in r.message.lower() and "i1" in r.message for r in caplog.records)


def test_check_needs_upgrade_success_clears(monkeypatch, db_session):
    from app import scanner, docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)
    monkeypatch.setattr(docker_manager, "update_container_resources", lambda *a: True)
    inst = _mk_instance(db_session, needs_upgrade=True, expected_plan=PlanType.STARTER, upgrade_retries=1)

    scanner.check_needs_upgrade(db_session)
    db_session.refresh(inst)
    assert inst.needs_upgrade is False
    assert inst.plan == PlanType.STARTER
    assert inst.upgrade_retries == 0


def test_check_needs_downgrade_aligns_plan(monkeypatch, db_session):
    from app import scanner
    inst = _mk_instance(
        db_session, plan=PlanType.STARTER, expected_plan=PlanType.FREE,
        expires_at=None,
    )
    scanner.check_needs_downgrade(db_session)
    db_session.refresh(inst)
    assert inst.plan == PlanType.FREE
    assert inst.expected_plan is None
    assert inst.expires_at is not None   # FREE 降级设 2h 窗


def test_check_needs_rebuild_on_sleeping(monkeypatch, db_session):
    from app import scanner, docker_manager
    rebuilt = []
    monkeypatch.setattr(docker_manager, "remove_container", lambda name: rebuilt.append(name) or True)
    monkeypatch.setattr(docker_manager, "create_container", lambda **kw: {"id": "new", "status": "created"})
    inst = _mk_instance(
        db_session, status=InstanceStatus.SLEEPING, plan=PlanType.STARTER,
        needs_rebuild=True, provider_config_enc="enc", provider_config_alg="fernet",
    )
    scanner.check_needs_rebuild(db_session)
    db_session.refresh(inst)
    assert rebuilt == ["c1"]
    assert inst.needs_rebuild is False
    assert inst.status == InstanceStatus.NOT_CREATED


def test_check_needs_rebuild_skips_running(monkeypatch, db_session):
    """Rebuild 仅在已停(SLEEPING)时触发,避免打断运行中容器。"""
    from app import scanner, docker_manager
    rebuilt = []
    monkeypatch.setattr(docker_manager, "remove_container", lambda name: rebuilt.append(name))
    _mk_instance(db_session, status=InstanceStatus.RUNNING, needs_rebuild=True)
    scanner.check_needs_rebuild(db_session)
    assert rebuilt == []   # 不动 RUNNING
