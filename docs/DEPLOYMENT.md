# Deployment — OneTrueDose production plan

**Status: planned, not provisioned.** Nothing in this document has been
executed. No domain is registered, no Hetzner account exists, no Cloudflare
Pages/R2 project exists, and no secret below has a real value yet. Every
step here is written so that running it is the *first* time it happens —
read it fully before starting, especially the order dependencies (DNS before
Caddy, R2 buckets before `.env`, etc.).

This plan was chosen after comparing shared hosting, PaaS (Render/Railway),
serverless, a single VPS, and a hybrid — see the chat history for the full
comparison and reasoning. Summary of why: the backend is one stateless Go
binary with no queues, sockets, or GPU need; a single small VPS running
Postgres alongside it costs less than any managed-Postgres PaaS once you
price in the database, and the existing `docker-compose.yml` already proves
this exact stack runs correctly together.

## Architecture

```
                          ┌─────────────────────────┐
  Browser  ───HTTPS──────▶│   Cloudflare (edge)      │
                          │  • Pages: static SPA     │
                          │  • Proxy: api.*.com      │
                          │  • R2: file storage      │
                          └───────────┬──────────────┘
                                      │ HTTPS (Full strict)
                                      ▼
                          ┌─────────────────────────┐
                          │   Hetzner VPS            │
                          │  ┌───────────────────┐  │
                          │  │ Caddy (TLS term.)  │  │
                          │  └─────────┬──────────┘  │
                          │            ▼              │
                          │  ┌───────────────────┐  │
                          │  │ api (Go binary)    │  │
                          │  └─────────┬──────────┘  │
                          │            ▼              │
                          │  ┌───────────────────┐  │
                          │  │ db (Postgres)      │  │
                          │  └───────────────────┘  │
                          └─────────────────────────┘
                                      │
                                nightly pg_dump
                                      ▼
                          ┌─────────────────────────┐
                          │  Cloudflare R2           │
                          │  (separate backups       │
                          │   bucket, zero egress)   │
                          └─────────────────────────┘
```

No `web` container runs in production — the frontend is a static build
deployed straight to Cloudflare Pages. `infra/deployment/docker-compose.prod.yml`
on the VPS only runs `db`, `api`, and `caddy`.

## Domain & DNS (onetruedose.com)

| Record | Type | Points to | Purpose |
|---|---|---|---|
| `onetruedose.com` | CNAME (Pages auto-manages) | Cloudflare Pages project | Storefront + `/admin` + `/platform` (one SPA, client-side routed — see "Routing decision" below) |
| `www.onetruedose.com` | CNAME | `onetruedose.com` | Redirect to apex (Cloudflare Pages handles this automatically once both are added as custom domains) |
| `api.onetruedose.com` | A | VPS public IP | Backend API, proxied (orange-clouded) through Cloudflare |

**Routing decision — one SPA, not three subdomains.** The original brief
suggested separate `admin.`/`platform.` subdomains. We're not doing that:
the frontend is already one React app with `/admin` and `/platform` as
client-side routes, built and deployed once. Subdomains would mean either
three separate Pages deployments of the same code or a Cloudflare Worker to
route between them — real added complexity for zero functional benefit,
since access control is already enforced server-side (role/session checks),
not by which URL someone typed. `api.` stays a real subdomain because it's
a genuinely different service (the Go binary on the VPS, not a Pages
deployment).

**Known limitation, carried forward honestly:** business storefronts
(`greenleaf`, `medicart-demo`, ...) are currently selected via a client-side
header (`X-Business-Slug`, stored in `localStorage`) rather than a URL path
or subdomain — see `docs/MULTI_TENANT_PLAN.md` §1. This means **two people
cannot yet share a link to a specific business's storefront** — the URL is
always just `onetruedose.com` regardless of which business is selected in
that browser. Fixing this (e.g. `onetruedose.com/store/greenleaf/...`) is a
frontend routing change, not a deployment one, and is not part of this plan.

## What production config changes (no code change, just `.env`)

Copy `.env.production.example` → `.env` on the VPS and fill in every
`___FILL_IN___`. The defaults already reflect the decisions above:
`CORS_ALLOWED_ORIGINS`, `COOKIE_DOMAIN`, `PAYMENT_CALLBACK_BASE_URL` all
point at `onetruedose.com`; `STORAGE_DRIVER=s3` with an R2 endpoint;
`PAYMENT_DRIVER` and `AI_DRIVER` deliberately **stay on their mock/local
values** — see "What is NOT production-ready yet" below.

One real code change was needed and is already made:
`router.TrustedPlatform = gin.PlatformCloudflare` in `cmd/api/main.go`, so
the rate limiter and audit logs see the real client IP (from Cloudflare's
tamper-proof `CF-Connecting-IP` header) instead of Caddy's container IP.
This only works if the VPS firewall also rejects traffic that didn't come
through Cloudflare — see the firewall step below.

## Provisioning runbook (in order)

### 1. Domain
Register `onetruedose.com` (confirmed name/TLD) at a registrar of your
choice — not done by this plan. Point its nameservers at Cloudflare once you
add the zone there (step 3).

### 2. Hetzner VPS
Create a Cloud server — cheapest current 2 vCPU / 4 GB tier (the exact plan
name has changed recently; verify at hetzner.com/cloud before ordering).
Ubuntu 24.04 LTS image. Add your SSH public key at creation time; disable
password auth.

```bash
# On the fresh VPS, as root:
apt update && apt upgrade -y
apt install -y docker.io docker-compose-plugin ufw
# Firewall: SSH from your own IP only, HTTP/S from Cloudflare only (so
# gin.PlatformCloudflare above can't be bypassed by someone connecting
# directly and forging CF-Connecting-IP themselves).
ufw default deny incoming
ufw allow from <YOUR_ADMIN_IP> to any port 22
for ip in $(curl -s https://www.cloudflare.com/ips-v4); do ufw allow from "$ip" to any port 80,443 proto tcp; done
for ip in $(curl -s https://www.cloudflare.com/ips-v6); do ufw allow from "$ip" to any port 80,443 proto tcp; done
ufw enable

# Non-root deploy user, for the CI SSH key — never deploy as root.
adduser --disabled-password deploy
usermod -aG docker deploy
mkdir -p /home/deploy/.ssh && cp /root/.ssh/authorized_keys /home/deploy/.ssh/ # or add a separate CI-only key
chown -R deploy:deploy /home/deploy/.ssh
mkdir -p /opt/medicart && chown deploy:deploy /opt/medicart
```

### 3. Cloudflare
- Add the `onetruedose.com` zone to Cloudflare, update nameservers at the
  registrar.
- DNS: add the `api` A record (step above), proxied (orange cloud).
- SSL/TLS mode: **Full (strict)** — requires Caddy's real cert (handled
  automatically once Caddy is running, see step 5).
- Create two **R2** buckets: `onetruedose-prescriptions` (private) and
  `onetruedose-backups` (private). Generate an R2 API token scoped to just
  these two buckets.
- Create a **Cloudflare Pages** project named `onetruedose`, connected to
  this GitHub repo, build output directory `apps/web/dist` (the deploy
  workflow pushes builds directly via `wrangler`, so the Pages dashboard's
  own Git integration can be left disabled to avoid a double-deploy).

### 4. Secrets
In the GitHub repo → Settings → Secrets and variables → Actions, add:
`VPS_HOST`, `VPS_SSH_USER` (`deploy`), `VPS_SSH_KEY` (the CI key's private
half), `CLOUDFLARE_API_TOKEN` (Pages:Edit scope only), `CLOUDFLARE_ACCOUNT_ID`.

### 5. First deploy (manual, once — after this, pushes to `main` deploy automatically)
```bash
# On the VPS, as `deploy`:
cd /opt/medicart
git clone <this repo> .
cp .env.production.example .env
vim .env   # fill in every ___FILL_IN___
docker compose -f infra/deployment/docker-compose.prod.yml --env-file .env up -d
docker compose -f infra/deployment/docker-compose.prod.yml logs -f api   # confirm migrations ran, server listening
curl -fsS https://api.onetruedose.com/healthz   # once DNS + Caddy cert have both settled
```
Then run `go run ./cmd/seed -admin-only` once (with `BOOTSTRAP_ADMIN_EMAIL`/
`PASSWORD` set in `.env`) to create the first real admin account, then
**remove those two lines from `.env`** and restart the `api` container.

### 6. Backups
```bash
# On the VPS:
pip install awscli --break-system-packages   # or apt install awscli
aws configure set aws_access_key_id ___ --profile r2
aws configure set aws_secret_access_key ___ --profile r2
export AWS_PROFILE=r2
crontab -e
# 0 3 * * * R2_BACKUP_BUCKET=onetruedose-backups R2_ENDPOINT=https://<account>.r2.cloudflarestorage.com /opt/medicart/infra/scripts/backup-db.sh >> /var/log/medicart-backup.log 2>&1
```
**Test the restore path before you need it**: `aws s3 cp` the newest dump
back down, `gunzip`, `psql` it into a throwaway database, and diff a few
tables against production. A backup nobody has ever restored is not a
backup.

## Rollback

```bash
# On the VPS:
cd /opt/medicart
export API_IMAGE=$(cat .last-deployed-image)   # the one before the bad deploy — check deploy.yml's history in GitHub Actions if this file is missing
docker compose -f infra/deployment/docker-compose.prod.yml up -d api
```
Database migrations only ever add (no destructive migration exists in this
project's history), so rolling back the API image does not require a
matching DB rollback in practice — but always check the migration history
for the commit range between the two images before assuming that.

## What is NOT production-ready yet (do not advertise as working)

- **Payments**: `PAYMENT_DRIVER=mock` — no real money moves. A Razorpay (or
  other gateway) integration is unimplemented; `internal/payments` is
  already behind a driver interface, so adding one is a new driver, not a
  rewrite, but it is not done.
- **AI assistant**: `AI_DRIVER=mock` — the deployed assistant is the
  existing deterministic, rule-based mock (see `internal/assistant`), not
  an LLM. It costs $0 in production as configured. If/when a real
  Anthropic-backed driver is built: use `claude-haiku-5-5`
  ($0.10/$0.50 per 1M input/output tokens — current pricing, verify at
  anthropic.com/pricing before relying on it) and keep `AI_MAX_TOKENS`/
  `AI_RATE_LIMIT_PER_HOUR` as hard per-business ceilings, not just defaults.
- **Prescription sales**: `FEATURE_RX_SALES_ENABLED=false` — stays false
  until a qualified pharmacy-regulatory review signs off. This is a legal
  decision, not a config flag to flip for convenience.
- **WhatsApp**: `WHATSAPP_DRIVER=link` — generates `wa.me` deep links only,
  no Business Cloud API integration.
- **Shareable per-business storefront URLs**: see "Known limitation" above.

## Monthly cost (steady state, confirm each figure before paying)

| Item | Estimate | Source confidence |
|---|---|---|
| Hetzner VPS (2 vCPU/4GB) | ~$5–6/mo | Third-party trackers disagreed on exact current plan name/price — verify at hetzner.com/cloud |
| Cloudflare Pages | $0 | Free tier, static sites |
| Cloudflare R2 (prescriptions + backups, early volume) | ~$0–1/mo | $0.015/GB-month, zero egress, 10GB free |
| Domain (.com, amortized) | ~$1.70/mo | Namecheap's own Nov-2026 notice: $19.98/yr renewal |
| AI | $0 (mock driver deployed) | N/A until a real driver ships |
| Monitoring (UptimeRobot free) | $0 | Confirm its free tier covers commercial use before relying on it — conflicting reports found |
| CI/CD (GitHub Actions free tier) | $0 | 2,000 min/mo on a private repo |
| **Total** | **≈ $7–9/month** | |
