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
