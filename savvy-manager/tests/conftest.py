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
    monkeypatch.setattr(config.settings, "provider_default_model", "claude-sonnet-4")
