# Workspace Routing Documentation

## Overview

Workspace access is routed through Nginx to user containers. Raw Docker ports are not exposed publicly.

```text
User Browser -> Nginx -> savvy-manager token validation -> Hermes container
```

## Token Flow

1. User requests workspace access from the Hermes console.
2. `new-api` calls `POST /internal/instances/{instance_id}/access-token`.
3. `savvy-manager` generates a signed workspace access token.
4. User is redirected to `/workspace/{workspace_id}/?token={token}`.
5. Nginx validates the token through `auth_request`.
6. If valid, `savvy-manager` returns the container upstream in `X-Workspace-Upstream`.
7. Nginx proxies the request, including WebSocket traffic, to the container.

## Token Format

```text
{base64_payload}.{hmac_signature}
```

Payload:

```json
{
  "instance_id": "inst-123",
  "user_id": "user-456",
  "iat": 1234567890,
  "exp": 1234569690
}
```

This token is only for browser access to Workspace routes. It is separate from the service-to-service HMAC request signature used by `new-api` when calling `savvy-manager`.

## Nginx Configuration

`savvy-manager` validates the token and returns:

- `X-User-Id`
- `X-Instance-Id`
- `X-Workspace-Upstream`, for example `http://hermes-u123-w456:3000`

```nginx
resolver 127.0.0.11 valid=10s ipv6=off;

map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

location = /_workspace_auth {
    internal;
    proxy_pass http://savvy-manager:8000/internal/workspace/validate;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-URI $request_uri;
    proxy_set_header X-Original-Method $request_method;
}

location /workspace/ {
    auth_request /_workspace_auth;
    auth_request_set $user_id $upstream_http_x_user_id;
    auth_request_set $instance_id $upstream_http_x_instance_id;
    auth_request_set $workspace_upstream $upstream_http_x_workspace_upstream;

    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;

    proxy_pass $workspace_upstream;
}
```

Notes:

- Nginx must run on the same Docker network as workspace containers for Docker DNS names to resolve.
- Variable `proxy_pass` requires Docker DNS resolver `127.0.0.11`.
- WebSocket headers are required for Hermes terminal/session features.
- `savvy-manager` must never return an upstream derived from user input. It must look up the instance owner and container name from its database.

## Container Routing

MVP routing:

- Container name: `hermes-u{user_id}-w{workspace_id}`.
- Docker network: `savvy-net`.
- Container port: `3000`.
- Upstream returned by manager: `http://hermes-u{user_id}-w{workspace_id}:3000`.

Future cross-host routing can return an internal host address instead, but the browser-facing URL should stay stable.

## Security

- Tokens expire after 30 minutes.
- Tokens are signed with HMAC-SHA256.
- Only the instance owner can generate tokens.
- Proxy rejects expired or invalid tokens.
- Workspace containers do not receive platform provider API keys.
- Browser never calls `savvy-manager` directly.

## Testing

```bash
curl -i "http://localhost/workspace/test/?token=bad"
```

Expected: invalid token is rejected.

```bash
curl -i "http://localhost/workspace/test/?token=VALID_TOKEN"
```

Expected: valid token proxies to the matching workspace container.

