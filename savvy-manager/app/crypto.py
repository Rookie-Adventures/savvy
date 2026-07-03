"""Fernet encryption helpers for instance provider config snapshots.

The snapshot is a small JSON dict (base_url, api_key, model, provider, source)
stored in the instances table as ciphertext. Key comes from
SAVVY_PROVIDER_ENC_KEY env (32-byte urlsafe base64). Missing key → fail-closed
(encryption refuses to run); shadowed only by tests via reload.
"""
from __future__ import annotations

import json
from typing import Tuple

from cryptography.fernet import Fernet, InvalidToken

from .config import settings

_ALG = "fernet"


def provider_enc_key_missing() -> bool:
    return not settings.provider_enc_key


def _fernet() -> Fernet:
    if provider_enc_key_missing():
        raise RuntimeError(
            "SAVVY_PROVIDER_ENC_KEY is not configured — provider config "
            "encryption requires a 32-byte urlsafe base64 key. Refusing to "
            "operate in plaintext fallback mode."
        )
    return Fernet(settings.provider_enc_key.encode())


def encrypt_provider_config(config: dict) -> Tuple[str, str]:
    """Encrypt a config dict. Returns (ciphertext, alg)."""
    plaintext = json.dumps(config, separators=(",", ":"), sort_keys=True).encode("utf-8")
    token = _fernet().encrypt(plaintext)
    return token.decode("utf-8"), _ALG


def decrypt_provider_config(ciphertext: str, alg: str = _ALG) -> dict:
    """Decrypt ciphertext produced by encrypt_provider_config. alg reserved for
    future migration to a different cipher; only 'fernet' is supported."""
    if alg != _ALG:
        raise ValueError(f"Unsupported provider_config_alg: {alg!r}")
    if not ciphertext:
        raise ValueError("Empty ciphertext")
    plaintext = _fernet().decrypt(ciphertext.encode("utf-8"))
    return json.loads(plaintext.decode("utf-8"))
