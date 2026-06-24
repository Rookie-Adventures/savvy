# Workspace Routing Documentation

## Overview

This document describes how workspace access is routed through the reverse proxy to user containers.

## Architecture

```
User Browser → Nginx Proxy → Token Validation → Container
```

## Token Flow

1. **User requests workspace access** from Hermes console
2. **Backend calls** `POST /internal/instances/{instance_id}/access-token`
3. **Savvy Manager generates** a signed JWT-like token with:
   - `instance_id`
   - `user_id`
   - `iat` (issued at)
   - `exp` (expiration, 30 minutes)
4. **User is redirected** to `/workspace/{user_id}/?token={token}`
5. **Nginx validates** the token via `auth_request` to savvy-manager
6. **If valid**, request is forwarded to the container

## Token Format

```
{base64_payload}.{hmac_signature}
```

Payload contains:
```json
{
  "instance_id": "inst-123",
  "user_id": "user-456",
  "iat": 1234567890,
  "exp": 1234569690
}
```

## Nginx Configuration

The proxy uses `auth_request` module to validate tokens:

```nginx
location /workspace/ {
    auth_request /validate-token;
    auth_request_set $user_id $upstream_http_x_user_id;
    auth_request_set $instance_id $upstream_http_x_instance_id;
    
    proxy_pass http://container;
}
```

## Security

- Tokens expire after 30 minutes
- Tokens are signed with HMAC-SHA256
- Only the instance owner can generate tokens
- Proxy rejects expired or invalid tokens

## Container Routing

In production, containers are routed by:
1. Container name: `savvy-u{user_id}-w1`
2. Docker network: `hermes-network`
3. Container port: typically `3000` or `8080`

## Testing

```bash
# Generate a test token
curl -X POST http://localhost:8000/internal/instances/inst-123/access-token \
  -H "X-Savvy-User-Id: user-456" \
  -H "X-Savvy-Timestamp: $(date +%s)" \
  -H "X-Savvy-Nonce: test" \
  -H "X-Savvy-Signature: {hmac_signature}"

# Access workspace with token
curl -i http://localhost/workspace/user-456/?token={token}
```
