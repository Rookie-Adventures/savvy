import base64
import pytest
from app import crypto


def test_round_trip(monkeypatch):
    key = base64.urlsafe_b64encode(b"0" * 32).decode()
    monkeypatch.setenv("SAVVY_PROVIDER_ENC_KEY", key)
    # Reload settings to pick up new env
    from importlib import reload
    from app import config
    reload(config)
    reload(crypto)
    plaintext = {"base_url": "http://new-api:3000/v1", "api_key": "sk-xxx", "model": "claude-sonnet-4", "provider": "custom", "source": "ours"}
    ciphertext, alg = crypto.encrypt_provider_config(plaintext)
    assert alg == "fernet"
    assert ciphertext != str(plaintext)
    decrypted = crypto.decrypt_provider_config(ciphertext, alg)
    assert decrypted == plaintext


def test_missing_key_raises(monkeypatch):
    monkeypatch.delenv("SAVVY_PROVIDER_ENC_KEY", raising=False)
    from importlib import reload
    from app import config
    reload(config)
    reload(crypto)
    assert crypto.provider_enc_key_missing() is True
    with pytest.raises(RuntimeError, match="SAVVY_PROVIDER_ENC_KEY"):
        crypto.encrypt_provider_config({"api_key": "sk-x"})


def test_decrypt_invalid_token_raises(monkeypatch):
    key = base64.urlsafe_b64encode(b"0" * 32).decode()
    monkeypatch.setenv("SAVVY_PROVIDER_ENC_KEY", key)
    from importlib import reload
    from app import config
    reload(config)
    reload(crypto)
    with pytest.raises(Exception):
        crypto.decrypt_provider_config("not-a-valid-token", "fernet")
