# Spec: Hermes Cloud Workspace PRD

## Objective

Launch a credible SaaS product using a white-labeled `new-api` site and console, where users can register and immediately start a private cloud Hermes Workspace. The first release exists to prove the product, support merchant/payment applications, and create the path for later paid always-on runtime and model quota packages.

First release positioning:

> Your personal AI Agent cloud workspace. Register, start Hermes Workspace in the browser, keep your data, and come back anytime.

First release is not positioned as token resale, cheap API proxying, or model arbitrage.

## Brand And Company

- Public brand: Savvy Agent.
- Product name: Hermes Cloud Workspace.
- Company: 粟城科技网络工作室.
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
- As a user, I can register and log in through the main site.
- As a user, I can sign up with email verification, Gmail, or GitHub.
- As a user, I can open a Hermes console from the logged-in `new-api` console.
- As a free user, I can start my Hermes Workspace for up to 3 hours per run.
- As a free user, when the runtime window ends, my container sleeps and my data remains.
- As a user, I can restart a sleeping workspace manually.
- As a paid user, I can keep my workspace always on.
- As an admin, I can see user workspace status and force sleep/stop when needed.

## Public Site Scope

Minimum public pages:

- Home: product value, call to action, login/register.
- Product: what Hermes cloud workspace does.
- Pricing: free plan and paid always-on plan.
- FAQ: runtime limit, data retention, model quota coming later.
- Terms of service.
- Privacy policy.
- Refund and cancellation policy.
- Contact/support page.
- Open source notice: `new-api` attribution, AGPL license notice, source link.
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

## Logged-In Product Scope

`new-api` remains the login, user, and admin shell.

Add a Hermes console entry:

- Sidebar/navigation item: Hermes Workspace.
- User page: current status, remaining free runtime, plan, start/sleep/open buttons, data retention notice.
- Admin page or admin-only controls: user instance list, status, host, start time, expires time, force sleep/stop.

Hermes statuses:

- `NOT_CREATED`
- `CREATING`
- `SLEEPING`
- `STARTING`
- `RUNNING`
- `STOPPING`
- `ERROR`
- `DELETING`

## Plans

| Capability | Free | Paid Always-On |
|---|---|---|
| Hermes Workspace | Yes | Yes |
| Independent container | Yes | Yes |
| Runtime window | 3 hours per start | Always on |
| Auto sleep | Yes | No |
| Data volume | Preserved | Preserved |
| Manual restart | Yes | Usually unnecessary |
| Model quota package | Later | Later |
| Best for | Trial, light use | Long-running use |

Commercial plan display:

- Free: 3-hour runtime per start, auto sleep, persistent storage.
- Starter: 2C2G, 30GB storage, ¥99/month.
- Pro: 4C8G, 80GB storage, coming soon.
- Enterprise: custom agent development, private deployment, custom workspace, contact sales.

Free plan behavior:

- Start creates or starts the user's workspace.
- `hermes-manager` records `started_at` and `expires_at = started_at + 3h`.
- A background scanner sleeps expired free workspaces with `docker stop`.
- Volumes are not deleted.
- User can manually start again and gets a fresh 3-hour window.

Paid behavior:

- Paid entitlement disables automatic free sleep.
- Phase one can use manual admin entitlement.
- Later payment webhook updates entitlement in `new-api`, then syncs it to `hermes-manager`.

## Architecture

```text
Internet
  |
DNS / HTTPS
  |
Reverse proxy
  |-- China host: public website, landing pages, docs, ICP/compliance, marketing content
  |-- Overseas host: new-api app console, hermes-manager, workspace ingress, user containers
```

Service ownership:

- `new-api`: public site, account, login, user/admin console, future payment, future token quota, model gateway.
- `hermes-manager`: Hermes instance records, Docker lifecycle, free runtime window, paid always-on entitlement copy, access tickets, host placement, future Aliyun migration.
- Hermes container: runs `hermes-agent + hermes-workspace`, stores user data in dedicated volumes, defaults model endpoint to our `new-api` gateway.

Do not share databases between `new-api` and `hermes-manager`.

Deployment rationale:

- Public website and compliance pages stay on the China host for ICP and merchant review.
- `new-api`, `hermes-manager`, Workspace runtime, user containers, and model connectivity stay on the overseas host.
- Workspace environments should reach international AI providers without requiring users to configure proxy infrastructure.
- Separating marketing/compliance from runtime reduces coupling.

## Hermes Manager API

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
- HMAC-signed requests or mTLS.
- Include method, path, body hash, timestamp, nonce, request id, and user id in the signature.
- Browser must never call `hermes-manager` directly.

## Workspace Access

Workspace access must go through reverse proxy authentication.

Minimum flow:

1. User logs into `new-api`.
2. User opens Hermes console.
3. `new-api` requests short-lived access token from `hermes-manager`.
4. User opens workspace URL.
5. Reverse proxy validates token, owner, state, and expiry.
6. Reverse proxy routes to the correct Hermes container.

Do not expose raw Docker host ports publicly.

## Tech Stack

- Public site and console: forked `QuantumNous/new-api`.
- License handling: AGPL public fork with attribution and source link.
- Hermes manager: private service, simplest stack acceptable for deployment.
- Runtime: Docker on a single host for MVP.
- Reverse proxy: Nginx.
- Hermes runtime image: existing unified Hermes image from this repo.
- Databases: keep `new-api` DB and `hermes-manager` DB separate.
- Auth methods: email verification, Gmail login, GitHub login. Phone verification is deferred.

## Commands

Build Hermes runtime image:

```powershell
docker build -f Dockerfile.unified -t hermes-unified:saas .
```

Inspect managed containers:

```powershell
docker ps -a --filter "label=hermes.managed=true"
```

Inspect managed volumes:

```powershell
docker volume ls --filter "label=hermes.managed=true"
```

Manual sleep:

```powershell
docker stop hermes-u<user_id>-w<workspace_id>
```

## Project Structure

Expected repo layout after implementation:

```text
savvy/
  new-api/                         # fork/subtree or sibling checkout, public AGPL fork
  hermes-manager/                  # private service
  hermes-agent/
  hermes-workspace/
  Dockerfile.unified
  docs/
```

If `new-api` is kept outside this monorepo, document the checkout path and deployment link in `docs/ops/`.

## Code Style

Keep integration boring:

- `new-api` changes should be UI copy, navigation, a Hermes page, and a small server-side API client.
- Do not put Docker code in `new-api`.
- `hermes-manager` owns all Docker and cloud-provider calls.
- Use server-generated ids, names, tokens, and volume names.
- No user input becomes Docker arguments.
- Keep the first entitlement model to `FREE` and `PAID_RESIDENT`.

## Testing Strategy

Required checks:

- `new-api` route/page check for Hermes console entry.
- Unit test for `hermes-manager` HMAC request verification.
- Unit test for free 3-hour expiry calculation.
- Unit test for Docker command construction.
- Integration smoke test: create user mapping, create instance, start, issue access token, sleep.

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
- Keep `hermes-manager` private and separately deployed.
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
- Expose `hermes-manager` to browsers.
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
- Workspace access is authenticated through proxy.
- Modified `new-api` fork has attribution and source link.
- First release avoids token package sales copy.

## Open Questions

- Brand name, domain, support email, ICP record, company entity.
- Reverse proxy choice.
- First paid always-on price.
- Whether account verification is email-only or phone-required.
- Where the public `new-api` fork will live.
