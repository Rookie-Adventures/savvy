# Spec: Hermes Cloud Workspace PRD

## Objective

Launch **Savvy Agent** as a credible SaaS product where users can register and start a private **Hermes Cloud Workspace** in the browser. The first release proves the product and supports merchant/payment applications. Token packages and model quota sales are a later phase.

First-release positioning:

> Your personal AI Agent cloud workspace. Register, start Hermes Workspace in the browser, keep your data, and come back anytime.

Do not position the first release as token resale, cheap API proxying, or model arbitrage.

## Brand And Company

- Public brand: Savvy Agent.
- Product name: Hermes Cloud Workspace.
- Company: 郑州市管城回族区栗橙网络科技工作室(个体工商户); 对外品牌简称 栗橙科技.
- Support email: support@scheng.net.
- ICP: add the final filing text after approval.
- Do not use Hermes as the main brand name. Hermes only names the Workspace product.

## Users

- Individual developers who want to try Hermes without local deployment.
- Small teams evaluating AI agent workspaces.
- Early users needed for merchant/payment approval evidence.
- Internal admins operating containers and handling support.

## Product Stories

- As a visitor, I can understand that the product is a hosted Hermes cloud workspace.
- As a visitor, I can see free and paid plan differences before registering.
- As a user, I can sign up with email verification, Gmail, or GitHub.
- As a user, I can open a Hermes console from the logged-in `new-api` console.
- As a free user, I can start my Hermes Workspace for up to 3 hours per run.
- As a free user, when the runtime window ends, my container sleeps and my data remains.
- As a paid user, I can keep my workspace always on.
- As an admin, I can see workspace status and force sleep/stop when needed.

## Public Site Scope

Minimum public pages:

- Home.
- Product.
- Pricing.
- FAQ.
- Terms of service.
- Privacy policy.
- Refund and cancellation policy.
- Contact/support page.
- Open source notice with `new-api` attribution, AGPL license notice, and source link.
- ICP filing display area.

Main page language should emphasize:

- Cloud Hermes Workspace.
- No local deployment.
- Independent container and persistent data.
- Free 3-hour experience.
- Paid always-on runtime.

Avoid first-release copy about:

- Cheapest token.
- API resale.
- Bypassing official providers.
- Unlimited resources.
- Guaranteed 100% uptime.

## Plans And Resource Limits

MVP container limits are fixed so capacity can be estimated before launch.

| Plan | Runtime | CPU | Memory | PIDs | Storage | Logs | Price |
|---|---|---:|---:|---:|---:|---|---|
| Free | 2 hours per start, then sleep | 0.5 vCPU | 768MB | 128 | 10GB soft quota | 10MB x 3 files | Free |
| Starter | Always on | 2 vCPU | 2GB | 512 | 30GB | 20MB x 5 files | ¥99/month |
| Pro | Coming soon | 4 vCPU | 8GB | 1024 | 80GB | 50MB x 5 files | Coming Soon |
| Enterprise | Custom | Custom | Custom | Custom | Custom | Custom | Contact Sales |

Implementation notes:

- Docker flags must include `--cpus`, `--memory`, `--memory-swap`, `--pids-limit`, and log rotation options.
- Storage is a manager-enforced soft quota unless the runtime host supports filesystem quotas.
- Free plan can be oversubscribed because it sleeps; Starter capacity must be reserved conservatively.

Free plan behavior:

- Start creates or starts the user's workspace.
- `savvy-manager` records `started_at` and `expires_at = started_at + 2h`.
- A background scanner sleeps expired free workspaces with `docker stop`.
- Volumes are not deleted.
- User can manually start again and gets a fresh 2-hour window.

Paid behavior:

- Paid entitlement disables automatic free sleep.
- Phase one can use manual admin entitlement.
- Later payment webhook updates entitlement in `new-api`, then syncs it to `savvy-manager`.

## Architecture

```text
Internet
  |
DNS / HTTPS
  |
Reverse proxy
  |-- China host: public website, landing pages, docs, ICP/compliance, marketing content
  |-- Overseas host: new-api app console, savvy-manager, workspace ingress, user containers
```

Service ownership:

- `new-api`: public site, account, login, user/admin console, future payment, future token quota, model gateway.
- `savvy-manager`: private Hermes manager for instance records, Docker lifecycle, free runtime window, paid entitlement copy, access tickets, host placement, future Aliyun migration.
- Hermes container: runs `hermes-agent + hermes-workspace`, stores user data in dedicated volumes, defaults model endpoint to our `new-api` gateway.

Do not share databases between `new-api` and `savvy-manager`.

Deployment rationale:

- Public website and compliance pages stay on the China host for ICP and merchant review.
- `new-api`, `savvy-manager`, Workspace runtime, user containers, and model connectivity stay on the overseas host.
- Workspace environments should reach international AI providers without requiring users to configure proxy infrastructure.
- Separating marketing/compliance from runtime reduces coupling.

## Redis Role

- Redis is optional shared runtime state for `new-api` when `REDIS_CONN_STRING` is configured.
- Redis may be used by `new-api` for cache/rate-limit style features.
- `savvy-manager` must not depend on Redis in the MVP.
- `savvy-manager` uses its database as the source of truth for instance state and free runtime windows.
- If Redis is removed from local deployment, unset `REDIS_CONN_STRING` and verify `new-api` starts in single-node mode.

## Savvy Manager API

Private API called only by `new-api` backend:

- `POST /internal/users/upsert`
- `GET /internal/users/{user_id}/instance`
- `POST /internal/users/{user_id}/instance`
- `POST /internal/instances/{instance_id}/start`
- `POST /internal/instances/{instance_id}/sleep`
- `POST /internal/instances/{instance_id}/stop`
- `POST /internal/instances/{instance_id}/access-token`
- `PATCH /internal/users/{user_id}/entitlement`

Authentication:

- Internal network only.
- HMAC-signed service-to-service requests for MVP.
- Include method, path, body hash, timestamp, nonce, request id, and user id in the signature.
- Browser must never call `savvy-manager` directly.
- mTLS can be added later if manager calls cross host boundaries.

Token boundary:

- Service API authentication uses HMAC request signatures between `new-api` and `savvy-manager`.
- Workspace browser access uses a separate short-lived token: `{base64_payload}.{hmac_signature}`.
- These two mechanisms use different secrets and are validated in different places.

## Workspace Access

Workspace access must go through Nginx authentication.

Minimum flow:

1. User logs into `new-api`.
2. User opens Hermes console.
3. `new-api` requests a short-lived access token from `savvy-manager`.
4. User opens workspace URL.
5. Nginx validates the token through `auth_request`.
6. `savvy-manager` returns the workspace upstream.
7. Nginx routes to the correct Hermes container with WebSocket support.

Do not expose raw Docker host ports publicly.

## Tech Stack

- Public site and console: forked `QuantumNous/new-api`.
- License handling: AGPL public fork with attribution and source link.
- Manager service: private `savvy-manager`.
- Runtime: Docker on a single overseas host for MVP.
- Reverse proxy: Nginx.
- Hermes runtime image: existing unified Hermes image from this repo.
- Databases: keep `new-api` DB and `savvy-manager` DB separate.
- Auth methods: email verification, Gmail login, GitHub login. Phone verification is deferred.

## Testing Strategy

Required checks:

- `new-api` route/page check for Hermes console entry.
- Unit test for `savvy-manager` HMAC request verification.
- Unit test for workspace access token signing/validation.
- Unit test for free 3-hour expiry calculation.
- Unit test for Docker command construction and resource limits.
- Integration smoke test: create user mapping, create instance, start, issue access token, open through Nginx, sleep.

Manual smoke:

- Register user.
- Open Hermes page.
- Create/start workspace.
- Open workspace through proxy.
- Sleep workspace.
- Restart workspace and verify data still exists.

## Boundaries

Always:

- Keep modified `new-api` source public if deployed publicly.
- Keep required attribution and source links visible.
- Keep `savvy-manager` private and separately deployed.
- Verify workspace ownership on every action.
- Preserve volumes on sleep.
- Force platform quota packages through our `new-api` gateway.

Ask first:

- Adding Kubernetes.
- Adding team workspaces.
- Adding token package sales to first release.
- Changing Hermes agent/workspace internals.
- Direct cloud migration automation.

Never:

- Mount Docker socket into `new-api`.
- Expose `savvy-manager` to browsers.
- Expose user containers by raw public ports.
- Put provider keys, payment keys, cloud keys, or manager secrets in the public fork.
- Promise unlimited runtime, unlimited resources, or guaranteed no data loss.

## Success Criteria

- Public site looks like a real Hermes cloud workspace product.
- User can register and reach a Hermes console.
- User can start a private Hermes Workspace.
- Free workspace sleeps after 3 hours and preserves data.
- User can restart a sleeping workspace.
- Admin can inspect and force stop/sleep instances.
- Workspace access is authenticated through Nginx and supports WebSocket.
- Modified `new-api` fork has attribution and source link.
- First release avoids token package sales copy.

