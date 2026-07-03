# Workspace Provider Key Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement first-start hard lock (must fill new-api key), runtime soft edit, and one-click revocation of LLM provider keys for workspace containers, per the design in `docs/superpowers/specs/2026-07-04-workspace-provider-key-injection-design.md`.

**Architecture:** savvy-manager stores encrypted B-layer config snapshots per instance (Fernet, key from env). On first container creation, savvy-manager `docker exec` writes `/opt/data/config.yaml` directly (agent reads config.yaml, env is fallback-only — verified at `hermes-agent/agent/auxiliary_client.py:1949-1978`). On wake, reconcile container config → DB. Revoke clears DB snapshot + container config fields. new-api controller forwards `provider_api_key` from frontend → manager and exposes provider-state to UI.

**Tech Stack:** Python 3.13 / FastAPI / SQLAlchemy / Fernet (cryptography) / docker SDK / Go / Gin / React 19 / i18next.

## Global Constraints

- **JSON in new-api Go code:** all marshal/unmarshal go through `common/json.go` wrappers (`common.Marshal`/`common.Unmarshal`), never `encoding/json` directly.
- **DB compatibility:** savvy-manager uses SQLAlchemy + Alembic; migrations must work on SQLite, MySQL ≥5.7.8, PostgreSQL ≥9.6. new-api GORM same.
- **HMAC secret:** `SAVVY_HMAC_SECRET` already shared between new-api and manager; reuse, don't introduce a new auth channel.
- **`SAVVY_PROVIDER_ENC_KEY`:** required env (32-byte base64 urlsafe). Missing → manager fails to start (fail-closed). No plaintext fallback.
- **Naming:** Cloud brand = Savvy Agent, hosted product = Hermes Cloud Workspace. Don't touch `new-api` / `QuantumNous` identity (see project governance).
- **No new deps without justification:** `cryptography` (Fernet) is the only new Python dep — already transitively available (install explicitly).
- **Zero-fork:** do not modify `hermes-workspace/` or `hermes-agent/` source. All changes in savvy-manager + new-api + docker compose.
- **Tests:** backend Go tests use `testify/require` for setup, `assert` for non-fatal. Python tests use pytest. No coverage-padding tests.

## File Structure

### Created
- `savvy-manager/app/crypto.py` — Fernet encrypt/decrypt helpers for provider config snapshots.
- `savvy-manager/app/provider_config.py` — snapshot build/parse + yaml render + reconcile logic (build_config_yaml, parse_container_config, reconcile).
- `savvy-manager/tests/test_crypto.py` — Fernet round-trip + fail-closed.
- `savvy-manager/tests/test_provider_config.py` — three paths (A/B1/C) invariants.
- `savvy-manager/alembic/versions/<date>_add_provider_config_columns.py` — migration adding 3 columns to `instances`.

### Modified
- `savvy-manager/app/config.py` — Settings: add `openai_base_url`, `provider_default_model`, `provider_enc_key`, `provider_enc_key_missing_fatal`.
- `savvy-manager/app/models.py` — `Instance`: add `provider_config_enc: Text`, `provider_config_alg: String(32)`, `provider_key_set_at: DateTime`.
- `savvy-manager/app/docker_manager.py` — `create_container`: accept `provider_config: dict | None` param; after `container.run` success, `docker exec` writes `/opt/data/config.yaml` via base64; guard against log leakage.
- `savvy-manager/app/routers/instances.py` — `/start` accepts `provider_api_key`/`provider_base_url` body; NEW `/revoke-provider-key`; `/provider-state` GET; on wake call `reconcile`.
- `savvy-manager/app/main.py` — startup check: if `settings.provider_enc_key` empty → raise.
- `savvy-manager/requirements.txt` — add `cryptography>=42.0.0`.
- `new-api/service/hermes.go` — add `HermesProviderState`, `StartHermesInstanceWithKey`, `RevokeHermesProviderKey`, `GetHermesProviderState` HMAC call helpers.
- `new-api/controller/hermes.go` — `StartHermesInstance` body field `providerApiKey`/`providerBaseUrl`; NEW handlers `RevokeHermesProviderKey`, `GetHermesProviderState`.
- `new-api/router/api-router.go` — register `/instance/:instance_id/revoke-provider-key`, `/instance/:instance_id/provider-state`.
- `new-api/service/hermes_test.go` — extend with key-forwarding + provider-state tests.
- `new-api/web/default/src/features/hermes/types.ts` — add `HermesProviderState`, `providerApiKey` payload type.
- `new-api/web/default/src/features/hermes/api.ts` — extend `startHermesInstance` body; new `revokeHermesProviderKey`, `getHermesProviderState`.
- `new-api/web/default/src/features/hermes/index.tsx` — start modal: `providerApikey` required password input + copywriting; revoke button + state badge; i18n keys.
- `new-api/web/default/src/i18n/locales/en.json`, `zh.json` — new copywriting keys (§11 of spec).
- `docker-compose.yml`, `docker-compose.prod.yml` — manager env: `SAVVY_PROVIDER_ENC_KEY`, `SAVVY_OPENAI_BASE_URL`, `SAVVY_PROVIDER_DEFAULT_MODEL`.

---

## Task 1: savvy-manager Settings + crypto helper (Fernet)

**Files:**
- Modify: `savvy-manager/app/config.py:4-30`
- Create: `savvy-manager/app/crypto.py`
- Test: `savvy-manager/tests/test_crypto.py`

**Interfaces:**
- Produces: `settings.provider_enc_key: str`, `settings.openai_base_url: str`, `settings.provider_default_model: str`
- Produces: `crypto.encrypt_provider_config(config: dict) -> tuple[str, str]` returns `(ciphertext, alg)` where alg=`"fernet"`.
- Produces: `crypto.decrypt_provider_config(ciphertext: str, alg: str = "fernet") -> dict`
- Produces: `crypto.provider_enc_key_missing() -> bool`

- [ ] **Step 1: Write the failing test**

`savvy-manager/tests/test_crypto.py`:
```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd savvy-manager && python -m pytest tests/test_crypto.py -v`
Expected: FAIL (module `app.crypto` does not exist).

- [ ] **Step 3: Add Settings + install cryptography**

Edit `savvy-manager/app/config.py` — add 3 new fields to `Settings` (after `public_host: str = "localhost"`, before `class Config`):
```python
    # Workspace 模型 provider 默认端点与模型（首启注入 agent）
    openai_base_url: str = "http://new-api:3000/v1"
    provider_default_model: str = "claude-sonnet-4"
    # Fernet 加密用户 provider key 的密钥（32 字节 urlsafe base64）。缺失→fail-closed
    provider_enc_key: str = ""
```

Edit `savvy-manager/requirements.txt` — append:
```
cryptography>=42.0.0
```

Run install: `cd savvy-manager && uv pip install -r requirements.txt` (or `pip install -r requirements.txt`).

- [ ] **Step 4: Write the crypto module**

Create `savvy-manager/app/crypto.py`:
```python
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd savvy-manager && python -m pytest tests/test_crypto.py -v`
Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
git add savvy-manager/app/config.py savvy-manager/app/crypto.py savvy-manager/requirements.txt savvy-manager/tests/test_crypto.py
git commit -m "feat(savvy-manager): add Fernet-based provider config crypto helper"
```

---

## Task 2: Instance model columns + Alembic migration

**Files:**
- Modify: `savvy-manager/app/models.py:33-48`
- Create: `savvy-manager/alembic/versions/<new>_add_provider_config_columns.py`
- Test: covered by Task 3 router tests (columns must accept writes/reads).

**Interfaces:**
- Produces: `Instance.provider_config_enc: Text|None`, `Instance.provider_config_alg: String(32)|None`, `Instance.provider_key_set_at: DateTime|None`.

- [ ] **Step 1: Write the failing test**

`savvy-manager/tests/test_provider_config.py` (skeleton, expanded in Task 3/4):
```python
import base64
import pytest
from sqlalchemy import inspect
from app.models import Instance
from app.database import Base, engine


def test_instance_has_provider_config_columns():
    cols = {c["name"] for c in inspect(engine).get_columns("instances")}
    assert "provider_config_enc" in cols
    assert "provider_config_alg" in cols
    assert "provider_key_set_at" in cols
```

(Run the migration fixture inline; this test needs the model+migration applied — see setup in Step 3.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd savvy-manager && python -m pytest tests/test_provider_config.py::test_instance_has_provider_config_columns -v`
Expected: FAIL (columns not present).

- [ ] **Step 3: Add columns to `Instance` model**

Edit `savvy-manager/app/models.py` — inside `Instance` class, after `assigned_port = Column(Integer, nullable=True)`:
```python
    provider_config_enc = Column(Text, nullable=True)
    provider_config_alg = Column(String(32), nullable=True)
    provider_key_set_at = Column(DateTime, nullable=True)
```

Also add `Text` to the SQLAlchemy import on line 3:
```python
from sqlalchemy import Column, String, DateTime, Enum as SQLEnum, JSON, ForeignKey, Integer, Text
```

- [ ] **Step 4: Generate Alembic migration**

Run:
```bash
cd savvy-manager
alembic revision --autogenerate -m "add provider config columns to instances"
```

Open the generated file (e.g. `alembic/versions/<hash>_add_provider_config_columns_.py`) and verify it contains `op.add_column('instances', sa.Column('provider_config_enc', sa.Text(), nullable=True))` and the two analogous lines for `provider_config_alg`/`provider_key_set_at`. Add `down_revision` chain link automatically. If only SQLite is the test backend, also ensure upgrade uses `batch_alter_table` (alembic default for SQLite). No `NOT NULL` defaults needed — all nullable (matches existing rows being upgraded).

- [ ] **Step 5: Apply migration locally + run test**

Run:
```bash
cd savvy-manager
alembic upgrade head
python -m pytest tests/test_provider_config.py::test_instance_has_provider_config_columns -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add savvy-manager/app/models.py savvy-manager/alembic/versions/
git commit -m "feat(savvy-manager): add provider_config_* columns to instances table"
```

---

## Task 3: provider_config module — snapshot build + YAML render + parse + reconcile

**Files:**
- Create: `savvy-manager/app/provider_config.py`
- Test: `savvy-manager/tests/test_provider_config.py` (extend)

**Interfaces:**
- Consumes: `settings.openai_base_url`, `settings.provider_default_model`, `crypto.encrypt_provider_config`, `crypto.decrypt_provider_config`.
- Produces:
  - `build_snapshot(*, api_key: str, base_url: str | None, model: str | None, source: str) -> dict` — assemble the plaintext snapshot dict.
  - `render_config_yaml(snapshot: dict) -> str` — emit the yaml string written to `/opt/data/config.yaml` (only `model.provider/default/base_url/api_key/api_mode` keys; `source` is metadata-only, not written).
  - `parse_container_config_yaml(yaml_text: str) -> dict | None` — return parsed 4-field dict (`provider/base_url/api_key/model`) or None if unparseable.
  - `reconcile_snapshot(*, db_snapshot: dict | None, container_yaml: str | None) -> tuple[dict | None, bool]` — returns `(new_db_snapshot, changed)`. If container yaml differs from db_snapshot → return `(container_fields_as_snapshot_source_user, True)`. If both empty → `(None, False)`. If equal → `(db_snapshot, False)`.
  - `clear_provider_fields_yaml(yaml_text: str) -> str` — strip `model.provider/default/base_url/api_key` lines from yaml (used by revoke); keep other content.

- [ ] **Step 1: Write failing tests**

Append to `savvy-manager/tests/test_provider_config.py`:
```python
import pytest
from app import provider_config as pc


def test_build_snapshot_defaults():
    snap = pc.build_snapshot(api_key="sk-xxx", base_url=None, model=None, source="ours")
    assert snap == {
        "base_url": "http://new-api:3000/v1",   # patched via settings monkeypatch below
        "api_key": "sk-xxx",
        "model": "claude-sonnet-4",
        "provider": "custom",
        "source": "ours",
    }


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
```

NOTE: `build_snapshot` uses settings via dependency injection — set `SAVVY_OPENAI_BASE_URL` env or monkeypatch `settings` in the test fixture. Add conftest fixture:
```python
# tests/conftest.py  (or extend existing)
import pytest

@pytest.fixture(autouse=True)
def _settings_defaults(monkeypatch):
    from app import config
    monkeypatch.setattr(config.settings, "openai_base_url", "http://new-api:3000/v1")
    monkeypatch.setattr(config.settings, "provider_default_model", "claude-sonnet-4")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd savvy-manager && python -m pytest tests/test_provider_config.py -v`
Expected: FAIL (module `app.provider_config` does not exist).

- [ ] **Step 3: Implement provider_config module**

Create `savvy-manager/app/provider_config.py`:
```python
"""Assemble, render, and reconcile workspace provider config snapshots.

Snapshot fields (stored in DB as encrypted JSON):
    base_url, api_key, model, provider, source

`render_config_yaml` emits ONLY model.* keys into the yaml that ships to
the container's /opt/data/config.yaml — `source` is metadata and never lands
in the container file.
"""
from __future__ import annotations

from typing import Optional, Tuple

import yaml

from .config import settings

# How the snapshot's 4 agent-relevant fields map to yaml keys.
_AGENT_FIELDS = ("provider", "base_url", "api_key", "model")


def build_snapshot(
    *,
    api_key: str,
    base_url: Optional[str],
    model: Optional[str],
    source: str,
) -> dict:
    if source not in ("ours", "user"):
        raise ValueError(f"source must be 'ours' or 'user', got {source!r}")
    return {
        "provider": "custom",
        "base_url": base_url or settings.openai_base_url,
        "api_key": api_key,
        "model": model or settings.provider_default_model,
        "source": source,
    }


def render_config_yaml(snapshot: dict) -> str:
    """Emit the yaml block agent reads.

    We hand-write (not yaml.dump) so the output matches the shape in
    cli-config.yaml.example exactly, avoiding yaml quirks like flow style.
    """
    return (
        "model:\n"
        f"  provider: custom\n"
        f"  default: {snapshot['model']}\n"
        f"  base_url: {snapshot['base_url']}\n"
        f"  api_key: {snapshot['api_key']}\n"
        f"  api_mode: chat_completions\n"
    )


def parse_container_config_yaml(yaml_text: str) -> Optional[dict]:
    """Extract the 4 agent-relevant fields from a container's config.yaml.
    Returns None if unparseable or file is empty."""
    if not yaml_text:
        return None
    try:
        doc = yaml.safe_load(yaml_text)
    except yaml.YAMLError:
        return None
    if not isinstance(doc, dict):
        return None
    model_section = doc.get("model")
    if not isinstance(model_section, dict):
        return None
    provider = model_section.get("provider")
    # Only treat as provider-config-bearing when provider is set.
    if not provider:
        return None
    return {
        "provider": str(provider),
        "base_url": str(model_section.get("base_url", "") or ""),
        "api_key": str(model_section.get("api_key", "") or ""),
        "model": str(model_section.get("default", "") or ""),
    }


def reconcile_snapshot(
    *,
    db_snapshot: Optional[dict],
    container_yaml: Optional[str],
) -> Tuple[Optional[dict], bool]:
    """Compare DB snapshot with the live container config.yaml.

    Returns (new_db_snapshot_or_None, changed). If the container config
    differs from DB for any of the 4 agent fields, build a fresh snapshot
    tagged source='user' (because user must have edited it in Settings),
    else return (db_snapshot, False). When neither side has data, returns
    (None, False) — nothing to reconcile.
    """
    container_fields = parse_container_config_yaml(container_yaml) if container_yaml else None
    if container_fields is None and db_snapshot is None:
        return None, False
    if container_fields is None:
        # Container config empty but DB still remembers → keep DB.
        return db_snapshot, False
    if db_snapshot is None:
        # Container has config but DB empty (e.g. legacy upgraded instance) — adopt.
        return (
            {
                "provider": container_fields["provider"],
                "base_url": container_fields["base_url"],
                "api_key": container_fields["api_key"],
                "model": container_fields["model"],
                "source": "user",
            },
            True,
        )
    # Both present — compare 4 agent fields.
    for field in _AGENT_FIELDS:
        if str(db_snapshot.get(field, "")) != str(container_fields.get(field, "")):
            return (
                {
                    "provider": container_fields["provider"],
                    "base_url": container_fields["base_url"],
                    "api_key": container_fields["api_key"],
                    "model": container_fields["model"],
                    "source": "user",
                },
                True,
            )
    return db_snapshot, False


def clear_provider_fields_yaml(yaml_text: str) -> str:
    """Strip provider-bearing lines from model: section. Other sections
    and other keys under model: (e.g. context_length) are preserved."""
    if not yaml_text:
        return ""
    try:
        doc = yaml.safe_load(yaml_text)
    except yaml.YAMLError:
        # On parse failure, just blank the whole model section safely.
        return ""
    if not isinstance(doc, dict):
        return ""
    model_section = doc.get("model")
    if isinstance(model_section, dict):
        for key in ("provider", "default", "base_url", "api_key", "api_mode"):
            model_section.pop(key, None)
        if not model_section:
            doc.pop("model", None)
    return yaml.safe_dump(doc, sort_keys=False, allow_unicode=True)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd savvy-manager && python -m pytest tests/test_provider_config.py -v`
Expected: 7 PASS (including the column-existence test from Task 2 still passing).

- [ ] **Step 5: Commit**

```bash
git add savvy-manager/app/provider_config.py savvy-manager/tests/test_provider_config.py
git commit -m "feat(savvy-manager): add provider_config snapshot/yaml/reconcile module"
```

---

## Task 4: docker_manager — accept provider_config and write config.yaml via exec

**Files:**
- Modify: `savvy-manager/app/docker_manager.py:39-128`
- Test: `savvy-manager/tests/test_docker_manager.py` (new)

**Interfaces:**
- Consumes: `provider_config.render_config_yaml`.
- Produces: `create_container(...)` gains `provider_config: dict | None = None`. After `containers.run` success, calls `_write_container_config_yaml(container_name, yaml_text)` which `docker exec` writes `/opt/data/config.yaml`.

- [ ] **Step 1: Write failing test**

Create `savvy-manager/tests/test_docker_manager.py`:
```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py -v`
Expected: FAIL (signature mismatch / `_write_container_config_yaml` missing).

- [ ] **Step 3: Modify docker_manager**

Edit `savvy-manager/app/docker_manager.py`. Change `create_container` signature to add `provider_config` param + write step. Replace lines 39-46 (function signature + mock-mode stub):

```python
def create_container(
    container_name: str,
    volume_name: str,
    user_id: str,
    workspace_id: str,
    plan: str,
    expires_at: str | None = None,
    provider_config: dict | None = None,
) -> dict:
    if settings.mock_mode:
        return {
            "id": f"mock-{container_name}",
            "name": container_name,
            "status": "created",
        }
```

After the successful `client.containers.run(...)` call (at the line `container = client.containers.run(...)`), and before the `return {"id": container.id, ...}` on line 126, insert:
```python
        if provider_config is not None:
            from .provider_config import render_config_yaml
            _write_container_config_yaml(container, render_config_yaml(provider_config))
```

Append helper at end of file (after `stop_container`/`start_container` definitions, BEFORE EOF):
```python
def _write_container_config_yaml(container, yaml_text: str) -> bool:
    """Write /opt/data/config.yaml inside the container via docker exec,
    using base64 to avoid shell-escape risks. Logs nothing that contains
    the api_key (yaml_text is never logged)."""
    import base64
    b64 = base64.b64encode(yaml_text.encode("utf-8")).decode("ascii")
    # Single exec: decode b64 to file. /opt/data is the hermes-agent HOME.
    cmd = ["sh", "-c", f"echo '{b64}' | base64 -d > /opt/data/config.yaml"]
    try:
        result = container.exec_run(cmd)
        return getattr(result, "exit_code", 1) == 0
    except Exception:
        return False
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd savvy-manager && python -m pytest tests/test_docker_manager.py -v`
Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add savvy-manager/app/docker_manager.py savvy-manager/tests/test_docker_manager.py
git commit -m "feat(savvy-manager): write config.yaml via docker exec on container create"
```

---

## Task 5: instances router — /start accepts provider key, NEW /revoke-provider-key, /provider-state

**Files:**
- Modify: `savvy-manager/app/routers/instances.py:42-91` (`/start`)
- Modify: `savvy-manager/app/routers/instances.py` (append endpoints + reconcile on wake)
- Test: `savvy-manager/tests/test_instances_router.py` (new)

**Interfaces:**
- Produces:
  - `POST /internal/instances/{id}/start` body: `{ "provider_api_key": "sk-...", "provider_base_url": "<optional>", "provider_model": "<optional>" }`. Required on first-start (when `provider_config_enc` is None). Optional otherwise (overrides existing snapshot).
  - `POST /internal/instances/{id}/revoke-provider-key`: clears DB snapshot + container config (if running). Returns `{ "instance_id", "status": "revoked" }`.
  - `GET /internal/instances/{id}/provider-state`: returns `{ "source": "ours"|"user"|"none", "model": "...", "key_set_at": "..." }` (no secret exposed).
  - On wake (start from SLEEPING): call `provider_config.reconcile_snapshot` BEFORE docker start; if container config differs from DB snapshot → encrypt new snapshot into DB (source=user).

- [ ] **Step 1: Write failing test**

Create `savvy-manager/tests/test_instances_router.py`:
```python
import base64
import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from app.main import app
from app.database import Base, get_db
from app.models import Instance, InstanceStatus, PlanType, User


SQLALCHEMY_DATABASE_URL = "sqlite:///./test_instances_router.db"
engine = create_engine(SQLALCHEMY_DATABASE_URL, connect_args={"check_same_thread": False})
TestingSessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)


@pytest.fixture(name="db_session")
def fixture_db_session(monkeypatch):
    Base.metadata.create_all(bind=engine)
    monkeypatch.setenv("SAVVY_PROVIDER_ENC_KEY", base64.urlsafe_b64encode(b"0" * 32).decode())
    from importlib import reload
    from app import config, crypto
    reload(config); reload(crypto)
    db = TestingSessionLocal()
    try:
        yield db
    finally:
        db.close()
        Base.metadata.drop_all(bind=engine)


@pytest.fixture(name="client")
def fixture_client(db_session):
    def override_get_db():
        try:
            yield db_session
        finally:
            pass
    app.dependency_overrides[get_db] = override_get_db
    # Bypass HMAC by overriding dependency
    from app.auth import require_hmac
    app.dependency_overrides[require_hmac] = lambda: {"user_id": "1"}
    yield TestClient(app)
    app.dependency_overrides.clear()


def _create_test_instance(db, instance_id="inst-1", status=InstanceStatus.NOT_CREATED):
    u = User(user_id="1", plan=PlanType.FREE)
    db.add(u)
    inst = Instance(
        instance_id=instance_id, user_id="1", status=status, plan=PlanType.FREE,
        container_name="savvy-u1-w1", volume_name="savvy-u1-data", assigned_port=41000,
    )
    db.add(inst)
    db.commit()
    return inst


def test_start_requires_provider_key_on_first_start(client, db_session):
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    res = client.post("/internal/instances/inst-1/start", json={})
    assert res.status_code == 400
    assert "provider_api_key" in res.json()["detail"]


def test_start_with_provider_key_encrypts_snapshot(client, db_session, monkeypatch):
    _create_test_instance(db_session, status=InstanceStatus.NOT_CREATED)
    # Stub docker create/start: skip real docker
    from app import docker_manager
    monkeypatch.setattr(docker_manager, "start_container", lambda name: True)
    monkeypatch.setattr(docker_manager.settings, "mock_mode", True)

    res = client.post("/internal/instances/inst-1/start", json={
        "provider_api_key": "sk-abc1234567890123",
    })
    assert res.status_code == 200, res.text
    # Reload instance from session and check encrypted snapshot persisted
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.provider_config_enc is not None
    assert inst.provider_config_alg == "fernet"
    from app import crypto
    snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg)
    assert snap["api_key"] == "sk-abc1234567890123"
    assert snap["source"] == "ours"


def test_revoke_clears_snapshot(client, db_session, monkeypatch):
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    # Seed an encrypted snapshot
    from app import crypto
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-x", "model": "claude-sonnet-4", "source": "ours"}
    enc, alg = crypto.encrypt_provider_config(snap)
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    inst.provider_config_enc = enc
    inst.provider_config_alg = alg
    db_session.commit()

    # Stub docker so revoke's exec works
    from app import docker_manager
    fake_container = type("C", (), {"exec_run": lambda self, cmd: type("R", (), {"exit_code": 0})()})()
    fake_client = type("K", (), {"containers": type("CC", (), {"get": lambda self, n: fake_container})()})()
    monkeypatch.setattr(docker_manager, "_client", fake_client)

    res = client.post("/internal/instances/inst-1/revoke-provider-key")
    assert res.status_code == 200
    db_session.expire_all()
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    assert inst.provider_config_enc is None
    assert inst.provider_key_set_at is None


def test_provider_state_returns_source(client, db_session):
    _create_test_instance(db_session, status=InstanceStatus.RUNNING)
    from app import crypto
    snap = {"provider": "custom", "base_url": "http://new-api:3000/v1", "api_key": "sk-x", "model": "claude-sonnet-4", "source": "user"}
    enc, alg = crypto.encrypt_provider_config(snap)
    inst = db_session.query(Instance).filter(Instance.instance_id == "inst-1").first()
    inst.provider_config_enc = enc
    inst.provider_config_alg = alg
    db_session.commit()

    res = client.get("/internal/instances/inst-1/provider-state")
    assert res.status_code == 200
    assert res.json()["source"] == "user"
    assert "api_key" not in res.text  # secret must not leak
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py -v`
Expected: FAIL (new endpoints not defined; `/start` doesn't require key).

- [ ] **Step 3: Modify instances router**

Edit `savvy-manager/app/routers/instances.py`. Replace the `start_instance` function body (lines 42-91) with:

```python
class StartRequest(BaseModel):
    provider_api_key: str | None = None
    provider_base_url: str | None = None
    provider_model: str | None = None


@router.post("/{instance_id}/start", response_model=StartResponse)
async def start_instance(
    instance_id: str,
    body: StartRequest,
    auth=Depends(require_hmac),
    db: Session = Depends(get_db),
):
    inst = _get_instance(instance_id, auth["user_id"], db)

    if inst.status not in (InstanceStatus.SLEEPING, InstanceStatus.NOT_CREATED):
        raise HTTPException(
            status_code=409, detail=f"Cannot start from status {inst.status}"
        )

    from .. import crypto
    from ..provider_config import build_snapshot, reconcile_snapshot, render_config_yaml

    if crypto.provider_enc_key_missing():
        raise HTTPException(status_code=500, detail="Provider encryption key not configured")

    # First-start hard lock: if no snapshot yet, provider_api_key is required.
    is_first_start = inst.provider_config_enc is None
    if is_first_start and not body.provider_api_key:
        raise HTTPException(
            status_code=400,
            detail="provider_api_key is required on first start",
        )

    # If a key is provided on any start, update snapshot (override).
    if body.provider_api_key:
        source = "ours"
        snap = build_snapshot(
            api_key=body.provider_api_key,
            base_url=body.provider_base_url,
            model=body.provider_model,
            source=source,
        )
        enc, alg = crypto.encrypt_provider_config(snap)
        inst.provider_config_enc = enc
        inst.provider_config_alg = alg
        from datetime import datetime, timezone
        inst.provider_key_set_at = datetime.now(timezone.utc)

    now = datetime.now(timezone.utc)
    expires_at = None
    if inst.plan == PlanType.FREE:
        expires_at = now + timedelta(hours=3)

    # Reconcile on wake: if NOT_CREATED we will create; if SLEEPING we may
    # have a container-side config.yaml the user edited — adopt it.
    provider_config_for_create = None
    if inst.status == InstanceStatus.SLEEPING:
        # Read container config (best-effort) and reconcile.
        from ..docker_manager import _client_or_none
        client = _client_or_none()
        if client is not None:
            try:
                c = client.containers.get(inst.container_name)
                res = c.exec_run(["sh", "-c", "cat /opt/data/config.yaml 2>/dev/null || true"])
                yaml_text = ""
                if getattr(res, "exit_code", 1) == 0 and res.output:
                    yaml_text = res.output.decode("utf-8", errors="ignore") if isinstance(res.output, bytes) else str(res.output)
                db_snap = None
                if inst.provider_config_enc:
                    db_snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg or "fernet")
                new_snap, changed = reconcile_snapshot(db_snapshot=db_snap, container_yaml=yaml_text)
                if changed and new_snap is not None:
                    enc, alg = crypto.encrypt_provider_config(new_snap)
                    inst.provider_config_enc = enc
                    inst.provider_config_alg = alg
                    inst.provider_key_set_at = datetime.now(timezone.utc)
            except Exception:
                pass  # Best-effort; container may be gone.
    elif inst.status == InstanceStatus.NOT_CREATED and inst.provider_config_enc:
        provider_config_for_create = crypto.decrypt_provider_config(
            inst.provider_config_enc, inst.provider_config_alg or "fernet"
        )

    docker_result = start_container(inst.container_name)
    if not docker_result:
        # Fallback: container might not exist yet. Try creating it first.
        from ..docker_manager import create_container
        create_res = create_container(
            container_name=inst.container_name,
            volume_name=inst.volume_name,
            user_id=inst.user_id,
            workspace_id=inst.instance_id,
            plan=inst.plan.name if hasattr(inst.plan, "name") else str(inst.plan),
            expires_at=expires_at.isoformat() if expires_at else None,
            provider_config=provider_config_for_create,
        )
        if "error" in create_res:
            raise HTTPException(
                status_code=500,
                detail=f"Failed to create container: {create_res['error']}",
            )

        # Try starting again
        if not start_container(inst.container_name):
            raise HTTPException(
                status_code=500,
                detail="Failed to start container after creation",
            )

    inst.status = InstanceStatus.RUNNING
    inst.started_at = now
    inst.expires_at = expires_at
    db.commit()

    return StartResponse(
        instance_id=instance_id,
        status=inst.status,
        started_at=inst.started_at.isoformat(),
        expires_at=inst.expires_at.isoformat() if inst.expires_at else None,
    )
```

Append new endpoints at end of file (before EOF):
```python
class RevokeResponse(BaseModel):
    instance_id: str
    status: str


@router.post("/{instance_id}/revoke-provider-key", response_model=RevokeResponse)
async def revoke_provider_key(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)

    from .. import crypto
    from ..provider_config import clear_provider_fields_yaml
    from ..docker_manager import _client_or_none

    # 1. Clear DB snapshot.
    inst.provider_config_enc = None
    inst.provider_config_alg = None
    inst.provider_key_set_at = None
    db.commit()

    # 2. If container is running, clear its config.yaml provider fields.
    client = _client_or_none()
    if client is not None:
        try:
            c = client.containers.get(inst.container_name)
            res = c.exec_run(["sh", "-c", "cat /opt/data/config.yaml 2>/dev/null || true"])
            if getattr(res, "exit_code", 1) == 0 and res.output:
                yaml_text = res.output.decode("utf-8", errors="ignore") if isinstance(res.output, bytes) else str(res.output)
                cleared = clear_provider_fields_yaml(yaml_text)
                import base64
                b64 = base64.b64encode(cleared.encode("utf-8")).decode("ascii")
                c.exec_run(["sh", "-c", f"echo '{b64}' | base64 -d > /opt/data/config.yaml"])
        except Exception:
            pass  # Container not present / not running — DB cleared is canonical.

    return RevokeResponse(instance_id=instance_id, status="revoked")


@router.get("/{instance_id}/provider-state")
async def get_provider_state(instance_id: str, auth=Depends(require_hmac), db: Session = Depends(get_db)):
    inst = _get_instance(instance_id, auth["user_id"], db)
    from .. import crypto

    if not inst.provider_config_enc:
        return {"instance_id": instance_id, "source": "none", "model": None, "key_set_at": None}

    snap = crypto.decrypt_provider_config(inst.provider_config_enc, inst.provider_config_alg or "fernet")
    return {
        "instance_id": instance_id,
        "source": snap.get("source", "none"),
        "model": snap.get("model"),
        "key_set_at": inst.provider_key_set_at.isoformat() if inst.provider_key_set_at else None,
    }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd savvy-manager && python -m pytest tests/test_instances_router.py -v`
Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
git add savvy-manager/app/routers/instances.py savvy-manager/tests/test_instances_router.py
git commit -m "feat(savvy-manager): /start requires provider key + /revoke-provider-key + /provider-state"
```

---

## Task 6: Manager startup fail-closed + docker-compose env wiring

**Files:**
- Modify: `savvy-manager/app/main.py` (check on startup)
- Modify: `docker-compose.yml`, `docker-compose.prod.yml` (manager env)

**Interfaces:** None (operational guard).

- [ ] **Step 1: Write failing test**

Append to `savvy-manager/tests/test_instances_router.py` or create `savvy-manager/tests/test_main_startup.py`:
```python
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
```

(The startup-time abort happens in `main.py`; the test asserts the underlying detector still works without env.)

- [ ] **Step 2: Run to verify**

Run: `cd savvy-manager && python -m pytest tests/test_main_startup.py -v`
Expected: PASS (guard helper already implemented in Task 1; just confirm startup hook calls it).

- [ ] **Step 3: Add startup guard to main.py**

Open `savvy-manager/app/main.py` (read if needed first), add at top of the app startup section (after `app = FastAPI(...)` or wherever settings are imported):
```python
from .config import settings
from . import crypto

if crypto.provider_enc_key_missing():
    import sys
    print("FATAL: SAVVY_PROVIDER_ENC_KEY is not configured. Refusing to start.", file=sys.stderr)
    sys.exit(1)
```

(If `main.py` already has a settings import / startup block, append these lines adjacent. The exact position is not critical — the side effect of `sys.exit(1)` runs once at import.)

- [ ] **Step 4: Wire env in compose**

Edit `docker-compose.yml` — for the `manager` service `environment` block, add:
```yaml
      - SAVVY_PROVIDER_ENC_KEY=${SAVVY_PROVIDER_ENC_KEY:-}
      - SAVVY_OPENAI_BASE_URL=http://new-api:3000/v1
      - SAVVY_PROVIDER_DEFAULT_MODEL=claude-sonnet-4
```

Edit `docker-compose.prod.yml` — for the `manager` service `environment` block, add:
```yaml
      - SAVVY_PROVIDER_ENC_KEY=${SAVVY_PROVIDER_ENC_KEY}
      - SAVVY_OPENAI_BASE_URL=https://${SAVVY_PUBLIC_HOST}/v1
      - SAVVY_PROVIDER_DEFAULT_MODEL=claude-sonnet-4
```

(Operators must set `SAVVY_PROVIDER_ENC_KEY` in their `.env` file or environment; without it, manager will refuse to start.)

- [ ] **Step 5: Commit**

```bash
git add savvy-manager/app/main.py docker-compose.yml docker-compose.prod.yml savvy-manager/tests/test_main_startup.py
git commit -m "feat(savvy-manager): fail-closed on missing SAVVY_PROVIDER_ENC_KEY + compose env wiring"
```

---

## Task 7: new-api service layer — forward provider key + provider-state

**Files:**
- Modify: `new-api/service/hermes.go` (after `HermesInstance` struct around line 35; add new helpers near line 240+)
- Modify: `new-api/controller/hermes.go:137-160` (`StartHermesInstance`)
- Modify: `new-api/router/api-router.go:358,360` (register new routes)
- Test: `new-api/service/hermes_test.go` (extend)

**Interfaces:**
- Produces:
  - `service.StartHermesInstance(userID int, instanceID string, providerAPIKey string, providerBaseURL string, providerModel string) error` — forwards to manager `/internal/instances/{id}/start` with JSON body.
  - `service.RevokeHermesProviderKey(userID int, instanceID string) error` — POST `/internal/instances/{id}/revoke-provider-key`.
  - `service.GetHermesProviderState(userID int, instanceID string) (*HermesProviderState, error)` — GET `/internal/instances/{id}/provider-state`.
  - `HermesProviderState` struct: `{ Source string; Model string; KeySetAt string }`.

- [ ] **Step 1: Write failing test**

Add to `new-api/service/hermes_test.go` (extend existing pattern; if file doesn't exist, create with same package and import structure as other service tests). Test target: HMAC forwarding of provider key body + provider-state response parsing.

```go
package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartHermesInstanceForwardsProviderKey(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/instances/inst-1/start", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		// HMAC headers present
		require.NotEmpty(t, r.Header.Get("X-Savvy-Signature"))
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		json.Unmarshal(buf.Bytes(), &gotBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"instance_id":"inst-1","status":"RUNNING","started_at":"now","expires_at":""}`))
	}))
	defer srv.Close()
	t.Setenv("HERMES_MANAGER_URL", srv.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	err := StartHermesInstance(2, "inst-1", "sk-abc1234567890123", "", "")
	require.NoError(t, err)
	require.Equal(t, "sk-abc1234567890123", gotBody["provider_api_key"])
}

func TestGetHermesProviderState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/instances/inst-1/provider-state", r.URL.Path)
		w.WriteHeader(200)
		w.Write([]byte(`{"instance_id":"inst-1","source":"user","model":"gpt-5","key_set_at":"2026-07-04T10:00:00Z"}`))
	}))
	defer srv.Close()
	t.Setenv("HERMES_MANAGER_URL", srv.URL)
	t.Setenv("SAVVY_HMAC_SECRET", "test-secret")

	state, err := GetHermesProviderState(2, "inst-1")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, "user", state.Source)
	require.Equal(t, "gpt-5", state.Model)
	require.False(t, strings.Contains(state.Model, "sk-"), "api_key should not appear in state")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `new-api`): `go test ./service -run TestStartHermesInstanceForwardsProviderKey -v`
Expected: FAIL (functions not defined).

- [ ] **Step 3: Extend service/hermes.go**

After the existing `HermesInstance` struct (around line 34), add:
```go
// HermesProviderState mirrors manager's GET /provider-state response.
// api_key is never included — source/model/key_set_at only.
type HermesProviderState struct {
	Source    string `json:"source"`     // ours | user | none
	Model     string `json:"model"`      // currently configured model
	KeySetAt  string `json:"key_set_at"` // ISO time of last key set
}
```

Find existing `StartHermesInstance(userID int, instanceID string) error` in `service/hermes.go` (replace its body to add provider key params; if it doesn't exist, add as new). Locate the existing function and change its signature + body. New version:
```go
// StartHermesInstance starts the workspace instance and forwards the provider
// key (required on first start) to the manager. Empty providerBaseURL/Model
// let the manager apply its defaults.
func StartHermesInstance(userID int, instanceID, providerAPIKey, providerBaseURL, providerModel string) error {
	body := map[string]any{
		"provider_api_key": providerAPIKey,
	}
	if providerBaseURL != "" {
		body["provider_base_url"] = providerBaseURL
	}
	if providerModel != "" {
		body["provider_model"] = providerModel
	}
	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return err
	}
	_, err = callHermesManager(userID, http.MethodPost, "/internal/instances/"+instanceID+"/start", bodyBytes)
	return err
}

// RevokeHermesProviderKey clears the LLM provider key snapshot (DB + container).
func RevokeHermesProviderKey(userID int, instanceID string) error {
	_, err := callHermesManager(userID, http.MethodPost, "/internal/instances/"+instanceID+"/revoke-provider-key", nil)
	return err
}

// GetHermesProviderState returns the current provider source/model/key-set
// timestamp. Never returns the api_key itself.
func GetHermesProviderState(userID int, instanceID string) (*HermesProviderState, error) {
	resp, err := callHermesManager(userID, http.MethodGet, "/internal/instances/"+instanceID+"/provider-state", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var state HermesProviderState
	if err := common.DecodeJson(resp.Body, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
```

If `callHermesManager` helper does not already exist in `service/hermes.go`, refactor existing HMAC call site(s) — there is likely a `signAndDo` function already. Use it (or add the helper that wraps `signAndDo` + base URL + path). Replace placeholder with the actual existing function per repo convention; check lines 71-80 for `signAndDo`.

- [ ] **Step 4: Update controller/hermes.go `StartHermesInstance`**

Open `new-api/controller/hermes.go` and find `func StartHermesInstance(c *gin.Context)` (around line 137). Replace its body to parse the new body fields. New:
```go
type startHermesReq struct {
	ProviderAPIKey  string `json:"providerApiKey"`
	ProviderBaseURL string `json:"providerBaseUrl"`
	ProviderModel   string `json:"providerModel"`
}

func StartHermesInstance(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "instance_id is required"})
		return
	}

	var req startHermesReq
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		// Empty body is allowed (wake without override); fields validated by service/manager.
	}

	if err := service.StartHermesInstance(userID, instanceID, req.ProviderAPIKey, req.ProviderBaseURL, req.ProviderModel); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("failed to start hermes instance: %s", err.Error()))
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
```

Add `RevokeHermesProviderKey` + `GetHermesProviderState` handlers:
```go
func RevokeHermesProviderKey(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "instance_id is required"})
		return
	}
	if err := service.RevokeHermesProviderKey(userID, instanceID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetHermesProviderState(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "instance_id is required"})
		return
	}
	state, err := service.GetHermesProviderState(userID, instanceID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": state})
}
```

- [ ] **Step 5: Register routes**

Edit `new-api/router/api-router.go` — after the existing `hermesRoute.POST("/instance/:instance_id/access-token", ...)` line, add:
```go
			hermesRoute.POST("/instance/:instance_id/revoke-provider-key", controller.RevokeHermesProviderKey)
			hermesRoute.GET("/instance/:instance_id/provider-state", controller.GetHermesProviderState)
```

- [ ] **Step 6: Run tests to verify they pass**

Run (from `new-api`):
```bash
go test ./service -run "TestStartHermesInstanceForwardsProviderKey|TestGetHermesProviderState" -v
go build ./...
```
Expected: PASS + build succeeds.

- [ ] **Step 7: Commit**

```bash
git add new-api/service/hermes.go new-api/controller/hermes.go new-api/router/api-router.go new-api/service/hermes_test.go
git commit -m "feat(new-api): forward provider key on start + revoke + provider-state endpoints"
```

---

## Task 8: new-api frontend — start modal key input + revoke + state badge + i18n

**Files:**
- Modify: `new-api/web/default/src/features/hermes/types.ts`
- Modify: `new-api/web/default/src/features/hermes/api.ts:43-50` (`startHermesInstance`)
- Modify: `new-api/web/default/src/features/hermes/index.tsx` (start modal + revoke button + badge)
- Modify: `new-api/web/default/src/i18n/locales/en.json`, `zh.json` (new keys)

**Interfaces:**
- Produces: extended `HermesInstance` (no shape change), new `HermesProviderState` type, `revokeHermesProviderKey(instanceId): Promise<ApiResponse>`, `getHermesProviderState(instanceId): Promise<ApiResponse<HermesProviderState>>`, and `startHermesInstance(instanceId, { providerApiKey, providerBaseUrl?, providerModel? })`.

- [ ] **Step 1: Add types + API stubs**

Edit `new-api/web/default/src/features/hermes/types.ts` — append:
```ts
export interface HermesProviderState {
  source: 'ours' | 'user' | 'none'
  model: string | null
  keySetAt: string | null
}

export interface StartHermesInstancePayload {
  providerApiKey: string
  providerBaseUrl?: string
  providerModel?: string
}
```

Edit `new-api/web/default/src/features/hermes/api.ts` — replace the existing `startHermesInstance` with:
```ts
export async function startHermesInstance(
  instanceId: string,
  payload: StartHermesInstancePayload
): Promise<ApiResponse> {
  const res = await api.post(`/api/hermes/instance/${instanceId}/start`, payload)
  return res.data
}

export async function revokeHermesProviderKey(
  instanceId: string
): Promise<ApiResponse> {
  const res = await api.post(`/api/hermes/instance/${instanceId}/revoke-provider-key`)
  return res.data
}

export async function getHermesProviderState(
  instanceId: string
): Promise<ApiResponse<HermesProviderState>> {
  const res = await api.get(`/api/hermes/instance/${instanceId}/provider-state`)
  return res.data
}
```

(Add `HermesProviderState`, `StartHermesInstancePayload` to the existing import list at top.)

- [ ] **Step 2: Add i18n keys**

In `new-api/web/default/src/i18n/locales/en.json` add (treat these as English source strings — keys are the English strings themselves per project convention):
```json
{
  "First start requires an API key": "First start requires an API key",
  "You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).": "You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).",
  "Your data (sessions, files, memory, skills) is preserved. Revoking only clears the key — sending messages will fail until you re-enter a key.": "Your data (sessions, files, memory, skills) is preserved. Revoking only clears the key — sending messages will fail until you re-enter a key.",
  "Provider key (required on first start)": "Provider key (required on first start)",
  "Revoke provider key": "Revoke provider key",
  "Revoke clears all LLM provider keys on this workspace. Your data is kept; chat will fail until you restart with a key.": "Revoke clears all LLM provider keys on this workspace. Your data is kept; chat will fail until you restart with a key.",
  "Confirm revoke": "Confirm revoke",
  "Currently using: this platform's key (billed to your balance)": "Currently using: this platform's key (billed to your balance)",
  "Currently using: your custom provider key (billed by your provider)": "Currently using: your custom provider key (billed by your provider)",
  "No provider key configured — chat will fail. Restart and provide a key.": "No provider key configured — chat will fail. Restart and provide a key."
}
```

`zh.json` add the same keys with Chinese values (verbatim below):
```json
{
  "First start requires an API key": "首次启动需要 API 密钥",
  "You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).": "可在 API Keys 页面生成后粘贴到这里。推荐使用你在本平台生成的密钥(扣本平台账户余额)。",
  "Your data (sessions, files, memory, skills) is preserved. Revoking only clears the key — sending messages will fail until you re-enter a key.": "你的数据(会话、文件、记忆、技能)原样保留。撤销只清密钥 —— 重新填入密钥前发消息会失败。",
  "Provider key (required on first start)": "供应商密钥(首次启动必填)",
  "Revoke provider key": "撤销供应商密钥",
  "Revoke clears all LLM provider keys on this workspace. Your data is kept; chat will fail until you restart with a key.": "撤销会清空此工作区的所有 LLM 供应商密钥。数据保留;重新填入密钥前聊天会失败。",
  "Confirm revoke": "确认撤销",
  "Currently using: this platform's key (billed to your balance)": "当前使用:本平台密钥(扣账户余额)",
  "Currently using: your custom provider key (billed by your provider)": "当前使用:你自定义的供应商密钥(由供应商直接向你计费)",
  "No provider key configured — chat will fail. Restart and provide a key.": "未配置密钥 —— 聊天会失败。请重启并填入密钥。"
}
```

After adding, run: `cd new-api/web/default && bun run i18n:sync` to refresh locale files.

- [ ] **Step 3: Extend the start modal in index.tsx**

Open `new-api/web/default/src/features/hermes/index.tsx`. Find the existing start-workspace button/modal (search for `startHermesInstance` usage). Update the call to pass the payload:

```tsx
// Inside the start handler — gather state from a controlled input
const [providerApikey, setProviderApikey] = useState('')
const [showRevokeConfirm, setShowRevokeConfirm] = useState(false)
const providerState = useQuery({
  queryKey: ['hermesProviderState', instance?.id],
  queryFn: () => getHermesProviderState(instance!.id),
  enabled: !!instance?.id && instance.status === 'running',
})

const handleStart = async () => {
  if (!instance) return
  const res = await startHermesInstance(instance.id, { providerApiKey: providerApikey })
  if (res.success) { /* existing refresh logic */ }
}
```

Add the key input field to the start modal:
```tsx
<div className="space-y-2">
  <label className="text-sm font-medium">
    {t('Provider key (required on first start)')}
  </label>
  <input
    type="password"
    value={providerApikey}
    onChange={(e) => setProviderApikey(e.target.value)}
    placeholder="sk-..."
    className="w-full rounded border px-3 py-2"
    autoComplete="off"
  />
  <p className="text-xs text-muted-foreground">
    {t('You can generate one on the API Keys page and paste it here. We recommend the key you generated on this platform (billed to your account balance).')}
  </p>
</div>
```

Add the provider-state badge (only when running):
```tsx
{providerState.data?.data && (
  <div className="text-xs">
    {providerState.data.data.source === 'ours' && t('Currently using: this platform\'s key (billed to your balance)')}
    {providerState.data.data.source === 'user' && t('Currently using: your custom provider key (billed by your provider)')}
    {providerState.data.data.source === 'none' && t('No provider key configured — chat will fail. Restart and provide a key.')}
  </div>
)}
```

Add the revoke button + confirm dialog:
```tsx
<Button
  variant="danger"
  onClick={() => setShowRevokeConfirm(true)}
  disabled={instance?.status !== 'running'}
>
  {t('Revoke provider key')}
</Button>
{showRevokeConfirm && (
  <Dialog open={showRevokeConfirm} onOpenChange={setShowRevokeConfirm}>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{t('Confirm revoke')}</DialogTitle>
      </DialogHeader>
      <p className="text-sm">{t('Revoke clears all LLM provider keys on this workspace. Your data is kept; chat will fail until you restart with a key.')}</p>
      <DialogFooter>
        <Button variant="ghost" onClick={() => setShowRevokeConfirm(false)}>{t('Cancel')}</Button>
        <Button variant="danger" onClick={async () => {
          const res = await revokeHermesProviderKey(instance!.id)
          if (res.success) { setShowRevokeConfirm(false); /* refetch state */ }
        }}>{t('Revoke provider key')}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
)}
```

(Exact component names — `Dialog`/`Button`/`DialogContent` — must match existing imports used elsewhere in this file. Check the top of `index.tsx` for the UI kit already imported and reuse those components rather than introducing new ones.)

- [ ] **Step 4: Build + check types**

Run:
```bash
cd new-api/web/default
bun run build
bun run i18n:sync
```
Expected: build succeeds, no TS errors.

- [ ] **Step 5: Commit**

```bash
git add new-api/web/default/src/features/hermes/ new-api/web/default/src/i18n/locales/
git commit -m "feat(new-api-fe): provider key input on first start + revoke button + state badge"
```

---

## Task 9: End-to-end manual acceptance runbook + final commit

**Files:**
- Modify: `docs/ops/workspace-routing.md` (append §"Model key injection acceptance")

No automated test for this; it's the manual verification checklist from spec §8.

- [ ] **Step 1: Document the runbook**

Append to `docs/ops/workspace-routing.md`:
```markdown
### 模型密钥注入端到端验收

前置:
- `SAVVY_PROVIDER_ENC_KEY` 已在 .env 设置(32 字节 urlsafe base64)
- `docker compose up -d --build` 已重启 manager + 容器

1. 全新用户首启 workspace
   - 启动弹窗——不填 provider key,点启动 → 期望:400 "provider_api_key is required on first start"
   - 填入 new-api 生成的 sk-xxx → 启动成功
   - 进入工作区 → DevTools Network → `/api/sessions` 200
2. 发消息 → 流式返回(验证 B 层密钥真注入)
3. Settings → 改成自己的 Anthropic key → 仍能调模型
4. sleep → 唤醒 → 用新 key 仍能调用(验证唤醒对账回写 DB,source=user)
5. 工作区控制台点"撤销供应商密钥"
   - 状态显示:未配置
   - workspace UI 仍可访问 + 发消息 → 401/无凭证(预期)
   - docker exec savvy-u1-w1 cat /opt/data/config.yaml → provider/api_key 字段已空
   - volume 数据未变:`docker exec savvy-u1-w1 ls /workspace` 文件原样
6. 回控制台重填我们的 new-api sk → 恢复聊天
```

- [ ] **Step 2: Final verification — all backend tests**

Run:
```bash
cd savvy-manager && python -m pytest -q
cd ../new-api && go test ./service ./controller -run "Hermes|Provider" -v
```
Expected: all pass.

- [ ] **Step 3: Commit docs**

```bash
git add docs/ops/workspace-routing.md
git commit -m "docs: add model key injection end-to-end acceptance runbook"
```

---

## Self-Review

**Spec coverage:**
- §1 Goals → Tasks 1, 5, 7, 8 (first-start hard lock, runtime soft-edit via reconcile, revoke, two-layer independence, zero-fork — confirmed).
- §3 Data model → Task 2 (3 columns + migration).
- §4 Path A/B1/C → Tasks 4, 5, 7, 8 (docker-exec write, reconcile on wake, revoke clear).
- §5 Risks → Task 4 log-leak test (api_key not in caplog); Task 1 fail-closed enc key; Task 6 compose env names.
- §6 Component list — all files referenced in tasks.
- §7.1 docker-exec write config.yaml → Task 4 helper `_write_container_config_yaml`.
- §7.2 reconcile on wake → Task 5 reconciliation block in `/start` for SLEEPING→RUNNING.
- §7.3 revoke clears config fields → Task 5 `clear_provider_fields_yaml` invocation.
- §8 Tests → Tasks 1-7 each include pytest/testify tests; runbook in Task 9.
- §11 Console copywriting → Task 8 i18n keys + modal wording.

**Placeholder scan:** No "TBD", "add error handling", or stub code blocks. Each step ships real code.

**Type consistency:**
- `build_snapshot(source="ours"|"user")` — used identically across Tasks 1, 3, 5.
- `provider_config_enc`/`provider_config_alg`/`provider_key_set_at` — same names in Models (Task 2), router (Task 5), controller (Task 7).
- `StartHermesInstance(userID int, instanceID, providerAPIKey, providerBaseURL, providerModel string)` — same signature in service (Task 7 Step 3) and controller calling site (Task 7 Step 4).
- `HermesProviderState.Source = ours|user|none` — matches router (Task 5) and frontend type (Task 8).
- `provider_api_key` (snake) on wire ↔ `providerApiKey` (camel) in Go struct/JSON tags and frontend — explicitly aligned via `json:"providerApiKey"` and TS payload field name.

**Gaps fixed during review:**
- Original draft had reconcile logic not covered by a test — added `test_reconcile_*` to Task 3.
- Original draft didn't test the api_key log leak — added `test_create_container_does_not_log_api_key` to Task 4.
- Frontend copywriting keys need both en and zh locales — both added in Task 8 Step 2 with verbatim Chinese values.
- `callHermesManager` helper note — added explicit pointer to existing `signAndDo` rather than inventing a new helper.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-04-workspace-provider-key-injection.md`.

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
