#!/bin/sh
# savvy-manager container entrypoint.
#
# Runs DB migrations BEFORE starting the app, so the schema is at head when
# uvicorn serves the first request. A migration failure aborts startup
# (fail-closed) — we never serve against a stale schema, which would surface
# as confusing ORM errors (e.g. "column instances.provider_config_enc does
# not exist").
#
# Honors SAVVY_DATABASE_URL (alembic/env.py overrides the .ini placeholder).
set -e

echo "[entrypoint] running alembic upgrade head..."
alembic upgrade head

echo "[entrypoint] starting uvicorn..."
exec uvicorn app.main:app --host 0.0.0.0 --port 8000
