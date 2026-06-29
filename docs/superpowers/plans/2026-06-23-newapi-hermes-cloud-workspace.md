# New API Hermes Cloud Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Launch a white-labeled `new-api` site with a Hermes Workspace console backed by a private `hermes-manager`.

**Architecture:** `new-api` owns public site, login, user console, admin console, and future gateway/payment. `hermes-manager` owns Docker lifecycle, runtime windows, volumes, access tickets, and future Aliyun runtime migration. They communicate through a small private API.

**Tech Stack:** `new-api` fork, private manager service, Docker, reverse proxy, existing Hermes unified image.

## Global Constraints

- Do not build a separate main-site user system.
- Do not put Docker control code in `new-api`.
- Do not put private manager secrets or cloud keys in the public fork.
- Do not sell or market token packages in phase one.
- Free workspaces run for 3 hours per start, then sleep with data preserved.
- Modified `new-api` must keep AGPL attribution and source link.
- Workspace access must go through authenticated reverse proxy, not raw public ports.
- Public brand is Savvy Agent; product name is Hermes Cloud Workspace.
- Company is 粟城科技网络工作室; support email is support@scheng.net.
- Reverse proxy is Nginx.
- MVP auth supports email verification, Gmail login, and GitHub login.

---

## File Structure

- Modify: `new-api` fork
  - White-label copy, logo, footer, navigation.
  - Add Hermes console page and backend API client.
- Create: `hermes-manager/`
  - Private service for user mapping, instance lifecycle, Docker calls, free runtime scanner, access token issuance.
- Modify: deployment files
  - Add `new-api`, `hermes-manager`, reverse proxy, and Docker runtime network.
- Modify: docs
  - Add ops runbook, deployment variables, open-source notice, merchant-review checklist.

---

### Task 1: Fork and White-Label New API

**Files:**
- Modify: `new-api` branding assets and site copy.
- Modify: `new-api` footer/license/source links.
- Modify: `new-api` navigation.

**Interfaces:**
- Produces: public site that presents Hermes Cloud Workspace as the product.

- [ ] Replace product name, logo, homepage headline, and CTA.
- [ ] Use Savvy Agent as the public brand and Hermes Cloud Workspace as the product name.
- [ ] Add company name 粟城科技网络工作室 and support email support@scheng.net where appropriate.
- [ ] Add footer links: Terms, Privacy, Refund, Contact, Open Source.
- [ ] Preserve `new-api` attribution and source link.
- [ ] Remove first-release emphasis on token resale or cheap API proxying.
- [ ] Verify public pages render after build.

Verify:

```powershell
cd new-api
pnpm build
```

Expected: build succeeds and pages show the new brand.

---

### Task 2: Add Public Trust Pages

**Files:**
- Modify/Create: `new-api` public pages for product, pricing, FAQ, terms, privacy, refund, contact, open source notice.

**Interfaces:**
- Produces: pages needed for a credible SaaS and later merchant/payment application.

- [ ] Add product page: cloud Hermes Workspace, no local deployment, independent runtime, data preserved.
- [ ] Add pricing page: Free and Paid Always-On.
- [ ] Show plans: Free, Starter 2C2G 30GB ¥99/month, Pro 4C8G 80GB Coming Soon, Enterprise Contact Sales.
- [ ] Add FAQ: 3-hour free runtime, sleep behavior, data retention, model quota later.
- [ ] Add terms placeholder.
- [ ] Add privacy placeholder.
- [ ] Add refund/cancellation placeholder.
- [ ] Add contact/support page with company/support placeholders.
- [ ] Add open-source notice naming `new-api` and linking source.

Verify:

```powershell
cd new-api
pnpm build
```

Expected: all routes/pages compile.

---

### Task 3: Hermes Manager API Skeleton

**Files:**
- Create: `hermes-manager/`
- Create: `hermes-manager` app entrypoint.
- Create: `hermes-manager` database module.
- Create: `hermes-manager` auth/signature module.
- Create: `hermes-manager` tests.

**Interfaces:**
- Produces: private API with health check and HMAC verification.

- [ ] Add `/health`.
- [ ] Add HMAC request verification.
- [ ] Add user mapping table.
- [ ] Add instance table with status values.
- [ ] Add tests for HMAC valid, invalid, stale timestamp, and replay nonce.

Verify:

```powershell
cd hermes-manager
python -m pytest tests -q
```

Expected: tests pass.

---

### Task 3.5: Configure MVP Login Methods

**Files:**
- Modify: `new-api` auth/settings configuration.
- Modify: deployment environment documentation.

**Interfaces:**
- Produces email verification, Gmail login, and GitHub login.

- [ ] Enable email verification.
- [ ] Configure Gmail via custom OIDC/OAuth provider.
- [ ] Configure GitHub OAuth login.
- [ ] Keep phone verification disabled for MVP.
- [ ] Document callback URLs and provider secrets.

Verify:

```powershell
cd new-api
pnpm build
```

Expected: login/register page shows email, Gmail, and GitHub paths.

---

### Task 4: Hermes Manager Instance Lifecycle

**Files:**
- Modify: `hermes-manager` instance API.
- Modify: `hermes-manager` Docker runtime module.
- Modify: `hermes-manager` tests.

**Interfaces:**
- Produces:
  - `POST /internal/users/upsert`
  - `GET /internal/users/{user_id}/instance`
  - `POST /internal/users/{user_id}/instance`
  - `POST /internal/instances/{instance_id}/start`
  - `POST /internal/instances/{instance_id}/sleep`
  - `POST /internal/instances/{instance_id}/stop`

- [ ] Make instance creation idempotent by `user_id`.
- [ ] Generate container and volume names server-side.
- [ ] Add Docker labels: `hermes.managed=true`, user id, workspace id, plan, expires at.
- [ ] Start container with CPU, memory, pids, log, and no-privilege limits.
- [ ] Sleep with `docker stop`, never delete volume.
- [ ] Add tests for command construction and ownership checks.

Verify:

```powershell
cd hermes-manager
python -m pytest tests -q
```

Expected: tests pass.

---

### Task 5: Free Runtime Scanner

**Files:**
- Modify: `hermes-manager` scanner/scheduler.
- Modify: `hermes-manager` tests.

**Interfaces:**
- Produces: background task that sleeps expired free workspaces.

- [ ] On start, set `started_at` and `expires_at = started_at + 3h` for free users.
- [ ] Scan running free instances every minute.
- [ ] Sleep expired instances with `docker stop`.
- [ ] Preserve data volume.
- [ ] Store status as `SLEEPING`.
- [ ] Add tests for expiry math and paid-user exemption.

Verify:

```powershell
cd hermes-manager
python -m pytest tests -q
```

Expected: tests pass.

---

### Task 6: Workspace Access Tickets and Proxy Contract

**Files:**
- Modify: `hermes-manager` access token endpoint.
- Modify: reverse proxy config.
- Add: ops documentation for workspace routing.

**Interfaces:**
- Produces: `POST /internal/instances/{instance_id}/access-token`.

- [ ] Generate short-lived access token for the owner only.
- [ ] Include instance id, user id, expiry, and signature.
- [ ] Configure proxy route pattern for workspace access.
- [ ] Proxy must reject expired or invalid tickets.
- [ ] Proxy must route only to the matching container.

Verify:

```powershell
curl -i https://workspace.example.com/u/test
```

Expected: unauthenticated or invalid ticket is rejected.

---

### Task 7: New API Hermes Console Integration

**Files:**
- Modify: `new-api` backend to call `hermes-manager`.
- Modify: `new-api` frontend to add Hermes console page.

**Interfaces:**
- Consumes `hermes-manager` API.
- Produces user-facing Hermes console.

- [x] Add server-side manager client with HMAC signing.
- [x] Add Hermes console route/page.
- [x] Show status, plan, remaining runtime, last error.
- [x] Add Start, Sleep, Restart, Open Workspace actions.
- [x] Make Open Workspace request a fresh access ticket before redirect.
- [x] Add admin-only instance list or link to minimal manager admin endpoint.

Verify:

```powershell
cd new-api
pnpm build
```

Expected: build succeeds and Hermes page appears for logged-in users.

---

### Task 8: Runtime Image and Default Gateway Configuration

**Files:**
- Modify: `Dockerfile.unified` only if required.
- Modify: `hermes-manager` container environment builder.
- Add: docs for required environment variables.

**Interfaces:**
- Produces Hermes container that points model calls to our `new-api` gateway by default.

- [x] Build `hermes-unified:saas`.
- [x] Start container with per-user data and workspace volumes.
- [x] Set default OpenAI-compatible base URL to `new-api`.
- [x] Do not inject upstream provider API keys into user containers.
- [x] Keep user-supplied keys optional and separate from platform packages.

Verify:

```powershell
docker build -f Dockerfile.unified -t hermes-unified:saas .
```

Expected: image builds.

---

### Task 9: Single-Host Deployment

**Files:**
- Create/Modify: deployment compose or scripts.
- Create: reverse proxy config.
- Create: ops runbook.

**Interfaces:**
- Produces single-host MVP deployment.

- [x] Deploy China host for public website, landing pages, docs, ICP/compliance, and marketing content.
- [x] Deploy overseas host for `new-api`, `hermes-manager`, workspace ingress, and user containers.
- [x] Configure Nginx on both hosts.
- [x] Build or pull Hermes runtime image.
- [x] Configure internal-only manager access.
- [x] Configure workspace ingress.
- [x] Add manual rescue commands for list, inspect, logs, stop, start, backup.

Verify:

```powershell
docker ps
```

Expected: `new-api`, `hermes-manager`, proxy, and test workspace container can run.

---

### Task 10: End-to-End Launch Smoke

**Files:**
- Create: `docs/ops/hermes-cloud-workspace-runbook.md`
- Create: `docs/ops/merchant-application-checklist.md`

**Interfaces:**
- Produces launch checklist and proof path.

- [x] Register a test user.
- [x] Open Hermes console.
- [x] Create/start workspace.
- [x] Open Workspace through proxy.
- [x] Create a test file inside Workspace.
- [x] Sleep workspace.
- [x] Restart workspace.
- [x] Confirm test file remains.
- [x] Confirm free expiry scanner sleeps after 3 hours in a shortened test mode.
- [x] Confirm source/AGPL links are visible.
- [x] Confirm public trust pages exist.

Verify:

```powershell
docker ps -a --filter "label=hermes.managed=true"
```

Expected: user workspace can move through running and sleeping states without data loss.

---

## Incremental Delivery Order

1. White-label and public trust pages.
2. Private `hermes-manager` skeleton.
3. Docker lifecycle and free sleep.
4. Workspace proxy/access tickets.
5. `new-api` Hermes console integration.
6. Single-host deployment.
7. End-to-end smoke and merchant checklist.

Do not add token packages, payment providers, or Aliyun migration until this product loop works.

## Self-Review

- Spec coverage: public site, user registration, Hermes console, manager API, free 3-hour sleep, data retention, proxy access, AGPL boundary, and launch checklist are covered.
- Placeholder scan: no unresolved placeholders are required to execute the first phase.
- Scope check: no separate main-site auth system, no Kubernetes, no token package sales in phase one.
