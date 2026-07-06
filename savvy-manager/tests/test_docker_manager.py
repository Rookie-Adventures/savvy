import base64
import pytest
from unittest.mock import MagicMock, patch
from app import docker_manager as dm
from app.config import settings


def test_create_container_writes_config_yaml(monkeypatch):
    monkeypatch.setattr(settings, "mock_mode", False)
    fake_container = MagicMock()
    fake_container.id = "abc"
    fake_container.name = "savvy-u1-w1"
    fake_container.status = "created"
    fake_client = MagicMock()
    fake_client.containers.run.return_value = fake_container
    fake_client.containers.get.return_value = fake_container
    monkeypatch.setattr(dm, "_client", fake_client)

    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "source": "ours"}
    result = dm.create_container(
        container_name="savvy-u1-w1",
        volume_name="savvy-u1-data",
        user_id="1",
        workspace_id="inst-1",
        plan="FREE",
        expires_at=None,
        provider_config=snap,
    )
    assert "id" in result
    # Assert exec_run was called to write config.yaml
    calls = fake_container.exec_run.call_args_list
    assert any("cat > /opt/data/config.yaml" in str(c) for c in calls)


def test_create_container_does_not_log_api_key(monkeypatch, caplog):
    monkeypatch.setattr(settings, "mock_mode", False)
    fake_container = MagicMock()
    fake_container.id = "abc"
    fake_container.name = "savvy-u1-w1"
    fake_container.status = "created"
    fake_client = MagicMock()
    fake_client.containers.run.return_value = fake_container
    fake_client.containers.get.return_value = fake_container
    monkeypatch.setattr(dm, "_client", fake_client)

    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-very-secret-marker", "model": "claude-sonnet-4", "source": "ours"}
    import logging
    caplog.set_level(logging.DEBUG)
    dm.create_container(
        container_name="savvy-u1-w1", volume_name="savvy-u1-data",
        user_id="1", workspace_id="inst-1", plan="FREE",
        expires_at=None, provider_config=snap,
    )
    assert "sk-very-secret-marker" not in caplog.text


def test_create_container_skips_config_write_when_none(monkeypatch):
    monkeypatch.setattr(settings, "mock_mode", False)
    fake_container = MagicMock()
    fake_container.id = "abc"
    fake_container.name = "savvy-u1-w1"
    fake_client = MagicMock()
    fake_client.containers.run.return_value = fake_container
    fake_client.containers.get.return_value = fake_container
    monkeypatch.setattr(dm, "_client", fake_client)

    result = dm.create_container(
        container_name="savvy-u1-w1", volume_name="savvy-u1-data",
        user_id="1", workspace_id="inst-1", plan="FREE",
        expires_at=None, provider_config=None,
    )
    assert "id" in result
    # When provider_config is None, no exec_run to write config should occur.
    calls = fake_container.exec_run.call_args_list
    assert not any("cat > /opt/data/config.yaml" in str(c) for c in calls)


def test_start_container_old_start_with_mock_client():
    fake_container = MagicMock()
    # 模拟 docker start 后 status 从 pending 转 running
    statuses = iter(["pending", "pending", "running"])

    def fake_reload():
        fake_container.status = next(statuses)

    fake_container.reload.side_effect = fake_reload
    fake_container.start = MagicMock()

    fake_client = MagicMock()
    fake_client.containers.get.return_value = fake_container

    with patch("app.docker_manager._client_or_none", return_value=fake_client), \
         patch("app.docker_manager.settings") as fake_settings, \
         patch("app.docker_manager.time.sleep") as fake_sleep:  # 不真睡,加快测
        fake_settings.mock_mode = False
        from app.docker_manager import start_container
        result = start_container("ws-test")

    assert result is True
    fake_container.start.assert_called_once()
    # reload 至少调到看见 running
    assert fake_container.reload.call_count >= 2
    # 8s 固定缓冲被调用
    assert 8 in [c.args[0] for c in fake_sleep.call_args_list if c.args]


def test_start_container_timeout_returns_true():
    # status 永远 pending → 5 次轮询超时 → 仍 True(不卡死)
    fake_container = MagicMock()
    fake_container.status = "pending"
    fake_container.start = MagicMock()

    fake_client = MagicMock()
    fake_client.containers.get.return_value = fake_container

    with patch("app.docker_manager._client_or_none", return_value=fake_client), \
         patch("app.docker_manager.settings") as fake_settings, \
         patch("app.docker_manager.time.sleep"):
        fake_settings.mock_mode = False
        from app.docker_manager import start_container
        result = start_container("ws-test")

    assert result is True
    assert fake_container.reload.call_count == 5  # 最多 5 次


def test_start_container_mock_mode_short_circuits():
    with patch("app.docker_manager.settings") as fake_settings:
        fake_settings.mock_mode = True
        from app.docker_manager import start_container
        assert start_container("any") is True


def test_update_container_resources_calls_docker_update(monkeypatch):
    from app import docker_manager
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)

    captured = {}
    class FakeContainer:
        def update(self, **kw):
            captured["args"] = kw
    class FakeClient:
        class containers:
            @staticmethod
            def get(name):
                return FakeContainer()
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: FakeClient())

    ok = docker_manager.update_container_resources("c1", 200000, "2g", 512)
    assert ok is True
    assert captured["args"] == {
        "cpu_quota": 200000, "mem_limit": "2g",
        "memswap_limit": "2g", "pids_limit": 512,
    }


def test_update_container_resources_returns_false_when_not_found(monkeypatch):
    from app import docker_manager
    from docker.errors import NotFound
    monkeypatch.setattr(docker_manager.settings, "mock_mode", False)

    class FakeClient:
        class containers:
            @staticmethod
            def get(name):
                raise NotFound("nope")
    monkeypatch.setattr(docker_manager, "_client_or_none", lambda: FakeClient())

    assert docker_manager.update_container_resources("c1", 200000, "2g", 512) is False


def test_plan_resource_constants_present():
    from app.docker_manager import PLAN_RESOURCES, PLAN_LOG_CONFIG, PLAN_STORAGE_GB
    assert set(PLAN_RESOURCES) == {"FREE", "STARTER", "PRO"}
    assert PLAN_RESOURCES["STARTER"] == {"cpu_quota": 200000, "mem_limit": "2g", "pids_limit": 512}
    assert PLAN_STORAGE_GB == {"FREE": 10, "STARTER": 30, "PRO": 80}

