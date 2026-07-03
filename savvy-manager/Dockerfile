FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ ./app/
# Alembic migrations — entrypoint runs `alembic upgrade head` before uvicorn
# so the schema is at head on first request. Without these, a fresh/rebuilt
# manager against an existing DB (e.g. dev/prod that predates a model change)
# would hit UndefinedColumn errors. env.py reads settings.database_url
# (SAVVY_DATABASE_URL env, compose-wired) — no .ini URL editing needed.
COPY alembic.ini ./
COPY alembic/ ./alembic/
COPY entrypoint.sh ./
RUN chmod +x entrypoint.sh

EXPOSE 8000

ENTRYPOINT ["./entrypoint.sh"]
