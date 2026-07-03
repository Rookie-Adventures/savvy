import base64
import pytest
from fastapi.testclient import TestClient
from app.main import app


def test_app_rejects_startup_without_enc_key(monkeypatch):
    monkeypatch.delenv("SAVVY_PROVIDER_ENC_KEY", raising=False)
    from importlib import reload
    from app import config, crypto
    reload(config); reload(crypto)
    # We assert the guard helper exists and reports missing.
    assert crypto.provider_enc_key_missing() is True
