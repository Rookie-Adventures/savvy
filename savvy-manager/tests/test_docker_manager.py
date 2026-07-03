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
