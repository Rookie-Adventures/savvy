"""Shared test fixtures for savvy-manager tests."""
import pytest


@pytest.fixture(autouse=True)
def _settings_defaults(monkeypatch):
    """Default provider settings for provider_config tests.

    Patches the singleton instance attributes directly so `from .config import
    settings` inside provider_config reads the patched values without a reload.
    Autouse so every test gets deterministic defaults.
    """
    from app import config
    monkeypatch.setattr(config.settings, "openai_base_url", "http://new-api:3000/v1")


@pytest.fixture(autouse=True)
def _provider_enc_key_for_tests(monkeypatch):
    """Provide a deterministic Fernet key so app startup guard passes in tests.

    Production refuses to start without SAVVY_PROVIDER_ENC_KEY (fail-closed).
    Tests construct ``TestClient(app)`` which triggers the startup handler, so
    we set a valid 32-byte urlsafe base64 key for the test session. Tests that
    need to assert the missing-key behavior (test_crypto, test_main_startup)
    delete/override this env locally.
    """
    import base64
    monkeypatch.setenv(
        "SAVVY_PROVIDER_ENC_KEY",
        base64.urlsafe_b64encode(b"0" * 32).decode(),
    )
    # Sync the singleton so crypto.provider_enc_key_missing() reports present.
    from app import config
    monkeypatch.setattr(
        config.settings,
        "provider_enc_key",
        base64.urlsafe_b64encode(b"0" * 32).decode(),
    )
