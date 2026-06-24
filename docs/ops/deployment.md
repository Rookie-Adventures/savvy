# Savvy Agent Deployment Guide

## Prerequisites

- Docker and Docker Compose.
- Domain name with DNS configured.
- SSL certificates for production HTTPS.

## Services

| Service | Port | Description |
|---|---:|---|
| nginx | 80, 443 | Reverse proxy |
| new-api | 3000 | Main app, account console, future gateway |
| savvy-manager | 8000 | Private Hermes workspace manager |
| newapi-db | 5432 | PostgreSQL for new-api |
| manager-db | 5433 | PostgreSQL for savvy-manager |
| redis | 6379 | Optional new-api cache/rate-limit shared state |

## Redis Role

Redis is configured only for `new-api` when `REDIS_CONN_STRING` is set. It is used as shared runtime state for cache/rate-limit style features supported by `new-api`.

`savvy-manager` does not use Redis in the MVP. Its source of truth is `manager-db`; instance expiry and status reconciliation must work without Redis.

For a smaller single-node local stack, Redis can be removed only if `REDIS_CONN_STRING` is unset and `new-api` is verified to start without it.

## Workspace Resource Limits

These values are the MVP defaults used by `savvy-manager` when starting containers.

| Plan | Docker limits | Storage | Sleep |
|---|---|---:|---|
| Free | `--cpus=0.5 --memory=768m --memory-swap=768m --pids-limit=128 --log-opt max-size=10m --log-opt max-file=3` | 10GB soft quota | after 3 hours |
| Starter | `--cpus=2 --memory=2g --memory-swap=2g --pids-limit=512 --log-opt max-size=20m --log-opt max-file=5` | 30GB | no auto sleep |
| Pro | `--cpus=4 --memory=8g --memory-swap=8g --pids-limit=1024 --log-opt max-size=50m --log-opt max-file=5` | 80GB | coming soon |

Storage quota is soft unless the host filesystem supports project quotas. At minimum, alert when a workspace volume crosses 80% of its plan storage.

## Quick Start

```bash
docker compose up -d
```

Access: `http://localhost`

## Production Start

```bash
docker compose -f docker-compose.prod.yml up -d
```

Before production, change all placeholder secrets:

- `SAVVY_HMAC_SECRET`
- `SESSION_SECRET`
- PostgreSQL passwords
- Redis password
- OAuth secrets
- payment/provider secrets

## Backup

### Database Backup

```bash
docker exec newapi-db pg_dump -U newapi new-api > newapi-backup.sql
docker exec manager-db pg_dump -U savvy savvy_manager > manager-backup.sql
```

### Database Restore

```bash
docker exec -i newapi-db psql -U newapi new-api < newapi-backup.sql
docker exec -i manager-db psql -U savvy savvy_manager < manager-backup.sql
```

### Workspace Volume Backup

Cold backup is the MVP-supported backup mode. Stop the workspace first, archive the Docker volume, then start it again if needed.

```bash
docker stop hermes-uUSER-wWORKSPACE

docker run --rm \
  -v hermes_uUSER_wWORKSPACE_data:/data:ro \
  -v /backup:/backup \
  alpine sh -c "cd /data && tar czf /backup/hermes_uUSER_wWORKSPACE_data_$(date +%Y%m%d_%H%M).tgz ."
```

Hot backup of a running workspace is not guaranteed in the MVP. For paid always-on users, schedule a maintenance window or use filesystem-level snapshots after the runtime host supports them.

Backups must be copied off-host. A single overseas host plus local-only backups is not disaster recovery.

### Workspace Volume Restore

```bash
docker volume create hermes_uUSER_wWORKSPACE_data

docker run --rm \
  -v hermes_uUSER_wWORKSPACE_data:/data \
  -v /backup:/backup \
  alpine sh -c "cd /data && tar xzf /backup/hermes_uUSER_wWORKSPACE_data_YYYYMMDD_HHMM.tgz"
```

## Monitoring

Minimum checks:

- Public site HTTPS.
- `new-api` health.
- `savvy-manager` health.
- PostgreSQL health.
- Redis health if enabled.
- Docker daemon health.
- Running workspace count.
- Free workspace expiry scanner.
- Disk usage and inode usage.
- Latest database backup time.
- Latest workspace volume backup time.
- Nginx 4xx/5xx rate.

Must alert:

- Public site unavailable for 2 minutes.
- `savvy-manager` unavailable for 2 minutes.
- Database unavailable.
- Disk above 80%; urgent above 90%.
- Backup missing for 24 hours.
- Free expiry scanner stopped.
- Batch workspace container exits.

## Single-Host Failure Plan

The MVP accepts one overseas runtime host as a deliberate simplification. It must still have a manual recovery path:

1. Alert if `new-api`, `savvy-manager`, Docker, database, or disk is unhealthy for more than 2 minutes.
2. Keep daily off-host backups of both databases and workspace volumes.
3. Keep the current Docker image tag and environment variables documented.
4. If the overseas host is lost, provision a replacement host, restore DBs and workspace volumes, then repoint DNS/Nginx.

No automatic HA is promised in MVP. Add multi-host scheduling only after paid usage justifies it.

## Rescue Commands

```bash
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs -f
docker ps -a --filter "label=hermes.managed=true"
docker inspect hermes-uUSER-wWORKSPACE
docker logs --tail=200 hermes-uUSER-wWORKSPACE
docker stop hermes-uUSER-wWORKSPACE
docker start hermes-uUSER-wWORKSPACE
docker volume ls | grep hermes_uUSER_wWORKSPACE
df -h
docker system df
docker image prune
```

Do not run `docker system prune --volumes` unless every target volume has been verified as disposable.

