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


class _FakeImage:
    def __init__(self, id_): self.id = id_


class _FakeContainer:
    def __init__(self, name, image_id): self.name = name; self.image = _FakeImage(image_id)


class _FakeContainerImageGone:
    """容器还在，但它绑的旧镜像被 prune 掉了 → c.image 抛 ImageNotFound。"""
    def __init__(self, name): self.name = name

    @property
    def image(self):
        raise RuntimeError("404 Client Error: image not found")


def test_check_image_staleness_flags_outdated(monkeypatch, db_session):
    """容器绑旧 image id != tag 当前 id → 标 needs_rebuild(等 sleep 闭合重建)。"""
    from app import scanner, docker_manager

    class _FakeClient:
        images = type("I", (), {"get": staticmethod(lambda tag: _FakeImage("NEWID"))})
        containers = type("C", (), {"list": staticmethod(lambda **kw: [_FakeContainer("c1", "OLDID")])})
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: _FakeClient())
    inst = _mk_instance(db_session, status=InstanceStatus.SLEEPING, needs_rebuild=False)
    scanner.check_image_staleness(db_session)
    db_session.refresh(inst)
    assert inst.needs_rebuild is True


def test_check_image_staleness_skips_fresh(monkeypatch, db_session):
    """容器 image id == tag 当前 id → 不动。"""
    from app import scanner, docker_manager

    class _FakeClient:
        images = type("I", (), {"get": staticmethod(lambda tag: _FakeImage("SAMEID"))})
        containers = type("C", (), {"list": staticmethod(lambda **kw: [_FakeContainer("c1", "SAMEID")])})
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: _FakeClient())
    inst = _mk_instance(db_session, needs_rebuild=False)
    scanner.check_image_staleness(db_session)
    db_session.refresh(inst)
    assert inst.needs_rebuild is False


def test_check_image_staleness_quiet_when_no_daemon(monkeypatch, db_session):
    """无 daemon / tag 缺失 → 安静跳过,不误标、不抛。"""
    from app import scanner, docker_manager
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: None)
    inst = _mk_instance(db_session, needs_rebuild=False)
    scanner.check_image_staleness(db_session)  # no raise, no flag
    db_session.refresh(inst)
    assert inst.needs_rebuild is False


def test_check_image_staleness_flags_missing_image(monkeypatch, db_session):
    """容器绑的旧镜像被 prune 掉(c.image 抛异常) → 不崩整个扫描,标 stale。"""
    from app import scanner, docker_manager

    class _FakeClient:
        images = type("I", (), {"get": staticmethod(lambda tag: _FakeImage("NEWID"))})
        containers = type("C", (), {"list": staticmethod(lambda **kw: [_FakeContainerImageGone("c1")])})
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: _FakeClient())
    inst = _mk_instance(db_session, status=InstanceStatus.SLEEPING, needs_rebuild=False)
    scanner.check_image_staleness(db_session)  # no raise
    db_session.refresh(inst)
    assert inst.needs_rebuild is True
