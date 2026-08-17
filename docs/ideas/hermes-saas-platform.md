# Hermes Cloud Workspace

## Problem Statement

How might we launch a credible SaaS product quickly, using the approved domain, where a user can register and immediately get a private cloud Hermes Workspace without installing Docker or configuring Hermes locally?

## Recommended Direction

Use `new-api` as the app console, account system, admin console, and future model gateway. White-label its frontend as **Savvy Agent**, keep the required AGPL attribution and source links, and add a small **Hermes Cloud Workspace** console entry after login.

Keep Hermes runtime control in a separate private service named `hermes-manager`. `new-api` only sends user-scoped intents such as create, start, sleep, status, and access. `hermes-manager` owns Docker, free runtime windows, volumes, paid always-on status, and later Aliyun migration.

The first product story is not token resale. It is a hosted Hermes cloud workspace:

- Register.
- Create or start a private Hermes Workspace.
- Use it for 3 hours on the free plan.
- Let it sleep automatically while preserving data.
- Restart manually when needed.
- Upgrade later for always-on runtime.

Token packages and model quota are phase two. They should enter naturally when the user needs model calls through our `new-api` gateway.

## Key Assumptions to Validate

- [ ] A hosted Hermes Workspace is enough of a product to support merchant/payment application review. Validate with a complete home page, pricing page, workspace console, support/contact page, terms, privacy policy, and product screenshots.
- [ ] Free users accept a 3-hour runtime window if their data is preserved. Validate by showing remaining time and restart behavior clearly.
- [ ] `new-api` white-labeling plus a Hermes console is faster than building a new main site. Validate by forking `new-api` and making the smallest branded navigation/page changes.
- [ ] A private `hermes-manager` can stay outside the public AGPL fork. Validate by keeping it as a separate service, repo, database, and deployment.
- [ ] One Docker host is enough for the MVP. Validate by running multiple user containers with CPU, memory, log, and disk limits.

## MVP Scope

MVP includes:

- White-labeled `new-api` public site.
- Registration and login through `new-api`.
- User console entry for Hermes Workspace.
- Hermes console page showing instance status, remaining free runtime, start/sleep/open actions, and data retention notice.
- Admin view or minimal admin API for listing Hermes instances and forcing sleep/stop.
- Private `hermes-manager` API for user upsert, instance create/status/start/sleep, and short-lived access link generation.
- Per-user Hermes container and per-user Docker volume.
- Free plan: 3 hours per start, then `docker stop`, volume preserved.
- Paid plan placeholder: always-on entitlement, initially manual/admin controlled.
- Workspace access through reverse proxy, not raw exposed Docker ports.
- Public open-source notice for the modified `new-api` fork.

## Not Doing (and Why)

- Token package sales in phase one: payment review and user messaging are cleaner if the product is a cloud workspace first.
- Direct Docker control inside `new-api`: keeps AGPL and infrastructure boundaries clean.
- Kubernetes: single-host Docker is enough to validate the product.
- Team workspaces: one user, one workspace first.
- Complex billing: paid always-on can be manual before payment integration is approved.
- Deep `new-api` gateway changes: keep upstream merge pain low.
- Migrating `new-api` to per-user Aliyun servers: only Hermes runtime migrates later.

## Open Questions

- ICP record text will be added after approval.
- Exact domain mapping is still open, but the deployment is hybrid: China host for public website/marketing/compliance content, overseas host for `new-api`, `hermes-manager`, Workspace runtime, user containers, and model connectivity.

## Confirmed Inputs

- Brand: Savvy Agent.
- Product: Hermes Cloud Workspace.
- Company: 郑州市管城回族区栗橙网络科技工作室(个体工商户); 对外品牌简称 栗橙科技.
- Support: support@scheng.net.
- Authentication: email verification, Gmail login, and GitHub login for MVP. Phone verification is not required for initial launch.
- Reverse proxy: Nginx.
- Free plan: 3-hour runtime per start, auto sleep, persistent storage.
- Starter: 2C2G, 30GB storage, ¥99/month.
- Pro: 4C8G, 80GB storage, coming soon.
- Enterprise: custom agent development, private deployment, custom workspace, contact sales.
