"""Assemble, render, and reconcile workspace provider config snapshots.

Snapshot fields (stored in DB as encrypted JSON):
    base_url, api_key, model, provider, source

`render_config_yaml` emits ONLY model.* keys into the yaml that ships to
the container's /opt/data/config.yaml — `source` is metadata and never lands
in the container file.
"""
from __future__ import annotations

from typing import Optional, Tuple

import requests
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
    # model MUST be provided by the caller — start_instance probes new-api
    # /v1_models and passes the result; users never pick a model. No hardcoded
    # fallback: a None here is a programming error, not "use a default" (the old
    # default claude-sonnet-4 silently shipped a non-existent channel → 503).
    if not model:
        raise ValueError("model is required (probe new-api /v1/models first)")
    return {
        "provider": "custom",
        "base_url": base_url or settings.openai_base_url,
        "api_key": api_key,
        "model": model,
        "source": source,
    }


def probe_default_model(*, api_key: str, base_url: Optional[str]) -> str:
    """Ask new-api which models this key can use; return the first one.

    Users do not pick a model — the key decides. We probe /v1/models with the
    key and take data[0].id as model.default. Probe failure is fatal: we refuse
    to ship a guess that may not be a real channel (the bug that started this).
    """
    url = (base_url or settings.openai_base_url).rstrip("/")
    if not url.endswith("/models"):
        url = url + "/models"
    resp = requests.get(
        url,
        headers={"Authorization": f"Bearer {api_key}"},
        timeout=10,
    )
    resp.raise_for_status()
    data = resp.json().get("data", [])
    if not data:
        raise ValueError(f"new-api /v1/models returned no models for this key")
    return data[0]["id"]


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
    # A container config with provider=='auto' is the image's untouched template
    # default (openrouter / no real key). It is NOT a user edit — treating it as
    # one would clobber the DB key snapshot on every wake-start, so the user's
    # configured provider never reaches the container. Skip reconcile for the
    # template case and let the DB snapshot drive the write-back.
    if container_fields is not None and container_fields.get("provider") == "auto":
        return db_snapshot, False
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


def merge_provider_into_yaml(original_yaml: str, snapshot: dict) -> str:
    """Replace ONLY the `model:` section in an existing config.yaml with the
    provider snapshot, preserving every other section (terminal/browser/...).

    Why not just write `render_config_yaml` over the file: that emits only the
    `model:` block — overwriting with it would trash terminal/browser/agent
    sections. On wake-start we have a live container whose config.yaml already
    has the full template (or a user-edited copy), so we must merge, not clobber.

    Parse-failure / non-dict fallback: return `render_config_yaml(snapshot)`
    (model-only). This matches create_container's behavior on a fresh
    container where the template hasn't materialized yet — safer than
    preserving a broken file."""
    if not original_yaml:
        return render_config_yaml(snapshot)
    try:
        doc = yaml.safe_load(original_yaml)
    except yaml.YAMLError:
        return render_config_yaml(snapshot)
    if not isinstance(doc, dict):
        return render_config_yaml(snapshot)
    doc["model"] = {
        "provider": "custom",
        "default": snapshot["model"],
        "base_url": snapshot["base_url"],
        "api_key": snapshot["api_key"],
        "api_mode": "chat_completions",
    }
    return yaml.safe_dump(doc, sort_keys=False, allow_unicode=True)


def clear_provider_fields_yaml(yaml_text: str) -> str:
    """Strip provider-bearing lines from model: section. Other sections
    and other keys under model: (e.g. context_length) are preserved.

    Parse-failure / non-dict branch: return the original ``yaml_text``
    UNCHANGED. We cannot safely clear what we cannot parse, and preserving
    the user's data (incl. non-`model` sections they edited) takes priority
    over aggressive clearing. Callers (revoke_provider_key) detect this
    case (the returned text equals the input / is non-empty after strip of
    an empty clear) and skip the write-back so the container config is not
    truncated to empty."""
    if not yaml_text:
        return ""
    try:
        doc = yaml.safe_load(yaml_text)
    except yaml.YAMLError:
        # Parse failure: return original unchanged — do NOT destroy user data.
        return yaml_text
    if not isinstance(doc, dict):
        # Non-dict doc (scalar/list): return original unchanged — nothing to
        # safely clear; preserve user's file.
        return yaml_text
    model_section = doc.get("model")
    if isinstance(model_section, dict):
        for key in ("provider", "default", "base_url", "api_key", "api_mode"):
            model_section.pop(key, None)
        if not model_section:
            doc.pop("model", None)
    return yaml.safe_dump(doc, sort_keys=False, allow_unicode=True)
