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
