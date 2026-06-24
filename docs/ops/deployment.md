# Savvy Agent Deployment Guide

## Prerequisites

- Docker and Docker Compose
- Domain name with DNS configured
- SSL certificates (optional, for HTTPS)

## Quick Start (Local Development)

```bash
# Start infrastructure
docker compose up -d

# Start new-api locally
cd new-api
go run main.go --log-dir ./logs
```

Access: http://localhost

## Production Deployment

### 1. Configure Environment

Edit `docker-compose.prod.yml` and update:

```yaml
environment:
  - SAVVY_HMAC_SECRET=your-secure-random-secret
  - SESSION_SECRET=your-session-secret
```

### 2. Configure SSL (Optional)

Place your SSL certificates in `deploy/ssl/`:
- `cert.pem`
- `key.pem`

### 3. Start Services

```bash
docker compose -f docker-compose.prod.yml up -d
```

### 4. Initialize Database

The database will be automatically initialized on first start.

### 5. Configure Authentication

1. Access admin panel at `http://your-domain.com`
2. Go to System Settings → Authentication
3. Configure email, GitHub, and Gmail login (see `new-api/setting/mvp-auth-config.md`)

## Services

| Service | Port | Description |
|---------|------|-------------|
| nginx | 80, 443 | Reverse proxy |
| new-api | 3000 | Main application |
| savvy-manager | 8000 | Workspace manager |
| newapi-db | 5432 | PostgreSQL (new-api) |
| manager-db | 5433 | PostgreSQL (manager) |
| redis | 6379 | Cache |

## Backup

### Database Backup

```bash
# Backup new-api database
docker exec newapi-db pg_dump -U newapi new-api > newapi-backup.sql

# Backup manager database
docker exec manager-db pg_dump -U savvy savvy_manager > manager-backup.sql
```

### Restore

```bash
# Restore new-api database
docker exec -i newapi-db psql -U newapi new-api < newapi-backup.sql

# Restore manager database
docker exec -i manager-db psql -U savvy savvy_manager < manager-backup.sql
```

## Monitoring

### Check Service Status

```bash
docker compose -f docker-compose.prod.yml ps
```

### View Logs

```bash
# All services
docker compose -f docker-compose.prod.yml logs -f

# Specific service
docker compose -f docker-compose.prod.yml logs -f new-api
```

## Troubleshooting

### Container won't start

1. Check logs: `docker compose logs <service>`
2. Verify environment variables
3. Check database connectivity

### Workspace access issues

1. Verify token validation: `curl -H "X-Token: <token>" http://localhost:8000/internal/validate-workspace-token`
2. Check nginx logs for proxy errors

### Database connection errors

1. Verify database is running: `docker compose ps`
2. Check connection string matches environment variables
