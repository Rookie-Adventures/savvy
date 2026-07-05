"""Column-existence test for the provider_config_* columns on `instances`.

Applies the alembic migration chain to a temp SQLite DB, then inspects the
`instances` table for the three new nullable columns. Uses a monkeypatched
SAVVY_DATABASE_URL so alembic's env.py (which reads `app.config.settings`)
operates on the temp DB rather than the production PostgreSQL URL.
"""
from importlib import reload

import pytest
from alembic import command
from alembic.config import Config
from sqlalchemy import create_engine, inspect


@pytest.fixture(name="upgraded_db")
def fixture_upgraded_db(tmp_path, monkeypatch):
    db_path = tmp_path / "test.db"
    db_url = f"sqlite:///{db_path.as_posix()}"
    monkeypatch.setenv("SAVVY_DATABASE_URL", db_url)

    # Re-import config so settings picks up the env override, then build the
    # alembic Config against the temp URL. env.py also re-reads settings, so
    # the override propagates to the migration connection.
    from app import config as _config
    reload(_config)
    cfg = Config("alembic.ini")
    cfg.set_main_option("sqlalchemy.url", db_url)

    command.upgrade(cfg, "head")

    engine = create_engine(db_url)
    yield engine
    engine.dispose()


def test_instance_has_provider_config_columns(upgraded_db):
    cols = {c["name"] for c in inspect(upgraded_db).get_columns("instances")}
    assert "provider_config_enc" in cols
    assert "provider_config_alg" in cols
    assert "provider_key_set_at" in cols


# --- Task 3: provider_config module snapshot/yaml/reconcile tests ---

from app import provider_config as pc


def test_build_snapshot_uses_provided_model():
    """build_snapshot takes model from caller (probe result), not a hardcoded default.
    base_url still falls back to settings.openai_base_url when None."""
    snap = pc.build_snapshot(api_key="sk-xxx", base_url=None, model="deepseek-v4-flash", source="ours")
    assert snap == {
        "base_url": "http://new-api:3000/v1",   # patched via settings monkeypatch below
        "api_key": "sk-xxx",
        "model": "deepseek-v4-flash",
        "provider": "custom",
        "source": "ours",
    }


def test_build_snapshot_rejects_none_model():
    """No hardcoded fallback: a None model is a programming error — probe must
    have run first. Raises instead of silently shipping a non-existent channel."""
    with pytest.raises(ValueError):
        pc.build_snapshot(api_key="sk-xxx", base_url=None, model=None, source="ours")


def test_render_config_yaml_shape():
    snap = {"base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "provider": "custom", "source": "ours"}
    yaml_text = pc.render_config_yaml(snap)
    assert "model:" in yaml_text
    assert "provider: custom" in yaml_text
    assert "base_url: http://new-api:3000/v1" in yaml_text
    assert "api_key: sk-xxx" in yaml_text
    assert "default: claude-sonnet-4" in yaml_text
    assert "source:" not in yaml_text  # metadata-only, never written


def test_parse_container_config_yaml_round_trip():
    snap = {"base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "provider": "custom", "source": "ours"}
    yaml_text = pc.render_config_yaml(snap)
    parsed = pc.parse_container_config_yaml(yaml_text)
    assert parsed == {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4"}


def test_reconcile_detects_user_change():
    db_snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "source": "ours"}
    container_yaml = """
model:
  provider: custom
  default: gpt-5
  base_url: https://api.openai.com/v1
  api_key: sk-user-own
"""
    new_snap, changed = pc.reconcile_snapshot(db_snapshot=db_snap, container_yaml=container_yaml)
    assert changed is True
    assert new_snap["source"] == "user"
    assert new_snap["api_key"] == "sk-user-own"
    assert new_snap["base_url"] == "https://api.openai.com/v1"


def test_reconcile_no_change():
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "source": "ours"}
    yaml_text = pc.render_config_yaml(snap)
    new_snap, changed = pc.reconcile_snapshot(db_snapshot=snap, container_yaml=yaml_text)
    assert changed is False
    assert new_snap == snap


def test_reconcile_both_empty():
    new_snap, changed = pc.reconcile_snapshot(db_snapshot=None, container_yaml=None)
    assert changed is False
    assert new_snap is None


def test_clear_provider_fields_yaml():
    yaml_text = """
model:
  provider: custom
  default: claude-sonnet-4
  base_url: http://new-api:3000/v1
  api_key: sk-xxx
other_section:
  keep: me
"""
    cleared = pc.clear_provider_fields_yaml(yaml_text)
    assert "provider:" not in cleared.replace("other_section:", "")
    assert "api_key: sk-xxx" not in cleared
    assert "other_section:" in cleared
    assert "keep: me" in cleared


def test_clear_provider_fields_yaml_parse_fail_returns_original():
    """Parse-fail branch must return the original yaml_text unchanged,
    NOT '' — so the revoke write-back guard can detect it and skip
    truncating the user's config.yaml."""
    broken = ": : : broken\n  model:\n    provider: custom"
    cleared = pc.clear_provider_fields_yaml(broken)
    assert cleared == broken
    assert cleared != ""


def test_clear_provider_fields_yaml_non_dict_returns_original():
    """A YAML scalar (not a dict) doc must return the original text unchanged,
    NOT '' — same preserve-user-data invariant as parse-fail."""
    scalar = "just a string\n"
    cleared = pc.clear_provider_fields_yaml(scalar)
    assert cleared == scalar
    assert cleared != ""


def test_probe_default_model_takes_first(monkeypatch):
    """probe_default_model returns data[0].id from /v1/models."""
    class _R:
        status_code = 200
        def json(self):
            return {"data": [{"id": "deepseek-v4-flash"}, {"id": "deepseek-v4-pro"}]}
        def raise_for_status(self):
            pass
    captured = {}
    def fake_get(url, headers=None, timeout=None):
        captured["url"] = url
        captured["auth"] = headers["Authorization"]
        return _R()
    import app.provider_config as _pc
    monkeypatch.setattr(_pc.requests, "get", fake_get, raising=True)
    model = _pc.probe_default_model(api_key="sk-ASx", base_url="http://new-api:3000/v1")
    assert model == "deepseek-v4-flash"
    assert captured["auth"] == "Bearer sk-ASx"
    assert captured["url"] == "http://new-api:3000/v1/models"


def test_probe_default_model_raises_on_empty(monkeypatch):
    """Empty model list -> raise (refuse to ship a guess)."""
    class _R:
        status_code = 200
        def json(self):
            return {"data": []}
        def raise_for_status(self):
            pass
    import app.provider_config as _pc
    monkeypatch.setattr(_pc.requests, "get", lambda *a, **k: _R(), raising=True)
    with pytest.raises(ValueError):
        _pc.probe_default_model(api_key="sk-bad", base_url=None)


def test_probe_default_model_raises_on_http_error(monkeypatch):
    """Non-2xx -> raise_for_status re-raises (no fallback)."""
    import requests as _requests
    class _R:
        status_code = 401
        def json(self):
            return {}
        def raise_for_status(self):
            raise _requests.HTTPError("401 Unauthorized")
    import app.provider_config as _pc
    monkeypatch.setattr(_pc.requests, "get", lambda *a, **k: _R(), raising=True)
    with pytest.raises(_requests.HTTPError):
        _pc.probe_default_model(api_key="sk-bad", base_url=None)
