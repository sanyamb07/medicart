# MediCart — Implementation Plan

**Status legend**

| Marker | Meaning |
|---|---|
| ✅ Complete | Implemented and verified by an executed test/command |
| 🟡 Partial | Implemented but incomplete or not fully verified |
| ⬜ Not implemented | No code yet |
| 🔑 Blocked: credentials | Code exists; needs a real API key/account to run live |
| ⚖️ Blocked: external approval | Needs legal/regulatory/business sign-off |
| ❓ Not yet verified | Code exists, not executed |

Last updated: 2026-10-25 (admin product/inventory management pass)

---

## 0. Environment (verified on this machine)

| Item | Value | Notes |
|---|---|---|
| OS | Ubuntu 20.04.6 LTS, kernel 5.15 | |
| CPU / RAM | 12 cores / 14 GiB | plenty for local dev |
| Disk free | 183 GB | |
| Docker | 24.0.6, Compose v2.21.0 | works without `sudo` ✅ |
| `sudo` | **requires a password** | so no `apt install`; toolchains installed user-locally |
| Go (system) | 1.19.9 at `/usr/local/go` | too old — left untouched |
| Go (installed for this project) | **1.24.8** at `~/.local/toolchains/go1.24.8` | installed from the official tarball |
| Node (system/nvm) | was 16.16.0 | too old for Vite 6 |
| Node (installed) | **22.23.4** via nvm, set as nvm default | |
| Port 5432 | **BUSY** (pre-existing PostgreSQL) | MediCart's Postgres is published on **55432** |
| Ports 5173 / 8080 / 8081 | free | web / api / adminer |

Nothing pre-existing in the home directory was modified or removed. `~/.bashrc` was **not**
edited; see `docs/LOCAL_DEVELOPMENT.md` for the one line to add if you want `go` on your PATH
permanently.

---

## 1. Architecture summary

A **modular monolith**: one Go binary, one Postgres database, one static React bundle.

```
Browser ──► React SPA (static hosting)
               │ JSON/REST + httpOnly cookie session
               ▼
         Go API (single binary, Gin)
          ├── PostgreSQL (catalog, orders, inventory, ledger)
          ├── Object storage (prescriptions = private, product images = public)
          ├── Payment provider  (interface → razorpay | mock)
          ├── WhatsApp          (interface → cloud API | wa.me link only)
          └── AI provider       (interface → anthropic | mock)
```

Every external integration sits behind a Go interface with a **mock implementation**, so the
whole platform runs locally with zero paid credentials.

### Deliberate deviations from the brief (with rationale)

1. **pgx/v5 + a hand-written repository layer instead of sqlc.**
   A large share of the queries here are *dynamically shaped* — catalogue search with
   optional category/brand/price filters, four sort orders, admin list filters, cursor
   pagination. sqlc handles dynamic SQL poorly (you end up with `sqlc.narg` soup or
   hand-written queries anyway), and mixing two data-access styles is worse for a single
   maintainer than picking one. We keep type safety by scanning into explicit structs in a
   `internal/*/repo.go` per module and never passing interpolated user input into SQL
   (parameterised everywhere). Documented in `docs/ARCHITECTURE.md`.
2. **golang-migrate used as a library with `embed.FS`**, not the CLI. The migrations ship
   inside the binary, so `migrate` is one `go run ./cmd/migrate up` with no extra tool to
   install, and production migration runs need no separate artifact.
3. **No Redis.** Cart lives in Postgres (it must survive, and it is read far less than it is
   written); rate limiting is an in-process token bucket, which is correct for a
   single-instance deployment and documented as the thing to revisit at >1 instance.

---

## 2. Phase plan and live status

This table is updated only after a phase's commands have actually been executed.

| Phase | Scope | Status |
|---|---|---|
| 0 | Inspect environment, plan | ✅ Complete |
| 1 | Foundation: Go+Gin skeleton, config, logging, health, Postgres, migrations, Docker Compose, Vite app, CI | ✅ Complete — `docker compose --profile full up --build` runs db+api+web together and was verified end to end (healthchecks, CORS, a real API call through the browser-facing origin); `.github/workflows/ci.yml` runs gofmt/vet/build/test for the backend and lint/typecheck/build for the frontend on every push/PR (not yet exercised by an actual GitHub Actions run — verified by running the equivalent commands locally) |
| 2 | Catalogue: categories, brands, products, FTS+trigram search, PDP, home page, responsive shell | ✅ Complete — verified search (incl. misspelling), PDP, category grid |
| 3 | Cart + checkout: server-authoritative pricing, stock validation, order creation in a tx | ✅ Complete — verified (integration tests + live run), FEFO stock reservation under row locks, no-oversell race test passes |
| 4 | WhatsApp handoff + order status page + admin order management | ✅ Complete (link-mode only; Cloud API not implemented, see §5) |
| 5 | Payments: provider abstraction, mock adapter, webhooks, idempotency, customer self-service checkout page | ✅ Complete (mock driver only; Razorpay adapter not implemented, see §5) |
| 6 | Inventory batches, expiry, reservations, prescription upload/review | ✅ Complete — expired-batch exclusion verified by test. Prescription upload (`internal/prescriptions`) is now wired end to end: customer upload (sniffed + hashed + stored privately), pharmacist/admin review queue, signed-URL viewing with ownership/role authorisation, all covered by integration tests and a live browser check. This is still independent of checkout — it does not unblock Rx sales; `FEATURE_RX_SALES_ENABLED` is untouched. |
| 7 | AI assistant | 🟡 Implemented as the declared "mock" driver: `internal/assistant` is deterministic, rule-based intent matching (no LLM, no API key) that calls the real catalogue/shipping/order-tracking services — never invents a price, stock level, or order status — and escalates any clinical/dosage question instead of answering it. A floating chat widget is live on the storefront. An LLM-backed driver (`AI_DRIVER=anthropic`) is **not implemented** — that needs the tool-authorisation and prompt-injection safeguards the original brief describes, which is real future work, not a small addition. |
| 8 | Admin dashboard: orders, payments, prescriptions, products, inventory, basic auth/RBAC | 🟡 Order, payment, prescription-review, and product/inventory management complete and verified (create/edit/deactivate a product, receive a stock batch, block/unblock a batch — all live-tested in the browser). Customer list and sales/inventory reports still not built. |
| 9 | Tests + security pass | 🟡 43 Go tests passing (unit + integration incl. concurrency, prescription authorisation boundaries, assistant authorisation/non-fabrication checks, and admin product/inventory operations); no frontend/Playwright test suite committed (one-off scripts used for manual verification, not checked in) |
| 10 | Deployment configuration + provider comparison + costs | 🟡 `docs/DEPLOYMENT.md` written: Dockerfiles for both services exist and build correctly, a low-cost provider shortlist is documented, and the production config checklist is written — but nothing has actually been deployed; there are no live URLs and no deploy step in CI (deliberately — see the doc for why) |
| 11 | Handover | 🟡 This document; no separate HANDOVER.md yet |

### Added beyond the original phase list, in response to later requests
- **Customer authentication** — mobile + OTP login (`internal/auth/customer_otp.go`), separate session cookie from staff, dev-mode OTP echo when no SMS provider is configured.
- **Self-service payment** — the customer, not just an admin, can open a mock gateway checkout (`/pay/mock/:id`, method selection UI) and complete payment themselves; order tracking polls and prompts "Choose payment method" once an order is payable.
- **Minimal privacy-conscious analytics** — `analytics_events` table + closed event vocabulary (`internal/analytics`), batched client-side (`src/lib/analytics.ts`); no PII, no free text.
- **Search autocomplete** — debounced suggestion dropdown in the header, keyboard-navigable.
- **Design pass** — lucide-react icons replacing emoji throughout, compacted hero, consistent product-card grid, trust strip (claims limited to what's actually true of this demo).
- **Docker Compose full-stack** — `services/api/Dockerfile` (multi-stage, static binary, non-root) and `apps/web/Dockerfile` (nginx + SPA fallback) added; `docker compose --profile full up --build` brings up db+api+web together, verified healthy with a real cross-origin API call.
- **CI workflow** — `.github/workflows/ci.yml`: backend job runs against a real throwaway Postgres service container (gofmt, vet, build, `go test -race`); frontend job runs ESLint, `tsc`, and the production build.
- **Prescription upload and pharmacist review** — `internal/prescriptions` wires the previously-unused `internal/storage` package into real endpoints: customer upload (`POST /me/prescriptions`, sniffed/hashed/stored privately), a pharmacist/admin review queue (`/admin/prescriptions/pending`, approve/reject with notes), and signed-URL viewing authorised to the owner or staff only. Frontend: an upload widget on `/account` and a review screen at `/admin/prescriptions`. This is independent of checkout — `FEATURE_RX_SALES_ENABLED` is untouched and Rx sales remain blocked.
- **AI assistant (mock driver)** — `internal/assistant`: deterministic keyword-based intent matching that calls the real catalogue/shipping/order-tracking services for product availability, pricing, PIN serviceability, and authorised order-status lookups, and explicitly escalates (never answers) dosage/interaction/diagnosis questions. A floating chat widget is live on the storefront (hidden on `/admin`). Covered by integration tests including a cross-mobile-number authorisation-boundary test and a "never claims stock for a nonexistent product" test.
- **`docs/DEPLOYMENT.md`** — low-cost provider shortlist, production config checklist, and an explicit statement that nothing has actually been deployed yet.
- **Admin product + inventory management** — `internal/catalog/admin.go` and `admin_handlers.go` (create/update/deactivate a product, admin list that includes inactive products, `GetByIDAdmin` for the edit form) and `internal/inventory/admin.go` and `handlers.go` (receive a stock batch with a required future expiry date, list batches per product soonest-expiry-first, block/unblock a batch for a QA hold or recall, list everything expiring within N days across the catalogue). Frontend: `/admin/products` (searchable list with Rx/status badges), `/admin/products/new` and `/admin/products/:id` (one form, create and edit), with a stock-batches table and a "Receive stock" form on the edit page. Verified with 6 new integration tests (including rejecting a duplicate SKU and rejecting a past expiry date) and a full live-browser run: create a product → it redirects into edit mode with real saved data → receive a batch → the batch table shows it as "Sellable" with real on-hand/available quantities.

---

## 3. Data model

See `docs/DATABASE.md` for the full annotated schema.

Money is stored as **`BIGINT` minor units (paise)** everywhere, with an explicit
`currency CHAR(3)` column defaulting to `INR`. No floating point touches money in Go or SQL.
All timestamps are `TIMESTAMPTZ` and stored in UTC.

---

## 4. Order state machine

```
CREATED
  └─► AWAITING_CUSTOMER_CONFIRMATION ──► AWAITING_PRESCRIPTION_REVIEW ──┐
                 │                                    │                │
                 │                                    ▼                │
                 └──────────────────────────────► AWAITING_PAYMENT ◄────┘
                                                      │
                                                      ▼
                                              PAYMENT_PENDING
                                                      │
                                   ┌──────────────────┼──────────────┐
                                   ▼                  ▼              ▼
                                 PAID            (failed → AWAITING_PAYMENT)
                                   │
                                   ▼
                              PROCESSING ──► READY_FOR_DISPATCH ──► SHIPPED ──► DELIVERED
                                   │
                                   └──► CANCELLED ──► REFUND_PENDING ──► REFUNDED
```

Transitions are validated in `internal/orders/state.go`. The frontend can never set a status;
it only calls intent endpoints (`/confirm-whatsapp`, `/cancel`) which are themselves
authorisation-checked, and staff transitions require a role.

---

## 5. Known limitations (carried into handover)

- Razorpay, WhatsApp Cloud API and Anthropic are implemented but run in **mock mode** until
  you add credentials. No integration has been exercised against a live provider from here.
- The regulatory checklist (`docs/REGULATORY_CHECKLIST.md`) lists open questions requiring a
  qualified professional. `FEATURE_RX_SALES_ENABLED` defaults to **false**; prescription-only
  medicines cannot be ordered until it is deliberately turned on.
- Rate limiting is per-process; horizontal scaling needs a shared store.
- All catalogue content is clearly-labelled **demonstration data** and is rendered with a
  persistent "demo data" banner.
- No Playwright/Vitest suite is checked into the repo — verification was the Go test suite
  (34 tests, unit + integration against a real throwaway Postgres) plus manual/scripted
  browser runs against the live dev servers and the dockerized stack, not a repeatable
  `make test-e2e` target. `.github/workflows/ci.yml` exists and was verified by running its
  steps locally, but has not yet been exercised by an actual GitHub Actions run (no remote
  repository was pushed to from this session).
- Admin UI covers orders, payments, prescription review, and product/inventory management
  (create/edit/deactivate products, receive and block/unblock stock batches). Still missing:
  brand/category CRUD (brands and categories are seeded, not editable from the UI), a customer
  list, and sales/inventory reports (an `expiring-soon` query exists in the backend —
  `internal/inventory.ListExpiringSoon` — but has no admin screen surfacing it yet).
- AI assistant: implemented as a deterministic rule-based "mock" driver (see §2 above), not an
  LLM. `chat_sessions`/`chat_messages`/`faqs`/`ai_settings` tables from the original migration
  set still exist unused — the mock driver answers from live service calls, not from a stored
  conversation or FAQ table, so nothing writes to them yet.
- Prescription upload and pharmacist review are now implemented end to end (see §2), but this
  is deliberately decoupled from checkout: the Rx-block rule at checkout
  (`FEATURE_RX_SALES_ENABLED=false`) is untouched, so uploading and even approving a
  prescription still does not let a prescription-required item be purchased. Wiring an approved
  prescription to a specific order (rather than just a customer's upload history) is not built.
- Nothing has been deployed anywhere. `docs/DEPLOYMENT.md` documents Dockerfiles, a provider
  shortlist, and the config checklist, but there is no live URL, no provisioned database, and
  no deploy workflow — see that document for why a deploy step in CI was deliberately not added.
- `docs/ARCHITECTURE.md`, `docs/DATABASE.md`, `docs/API.md`, `docs/SECURITY.md`,
  `docs/WHATSAPP_INTEGRATION.md`, `docs/PAYMENT_INTEGRATION.md`, `docs/AI_ASSISTANT.md`,
  `docs/OPERATIONS.md`, `docs/TESTING.md`, and `docs/REGULATORY_CHECKLIST.md` are referenced
  from the original brief and, in places, from this document, but were never written — only
  `docs/IMPLEMENTATION_PLAN.md` and `docs/DEPLOYMENT.md` exist. Treat any reference to the
  others elsewhere in this file as aspirational, not a working link.
- Design pass was targeted, not exhaustive: icons, hero sizing, product-card consistency, a
  search autocomplete, and an honest trust strip are done. Not done: a written design-token
  file, PLP breadcrumbs/filter sidebar beyond sort, per-category icon artwork (all categories
  currently share one icon), a mobile bottom-nav, and a line-by-line audit against every
  bullet of the pasted design/CRO briefs — those were triaged to the highest-impact items given
  the scope involved, not implemented point-for-point.
- Customer accounts: OTP login works and ties new orders to the signed-in profile, with an
  order-history page (`/account`). There's no profile editing, saved-address book, or
  reorder-to-cart action (reorder was deliberately left out — see the CRO brief's own warning
  against re-adding medicines, especially Rx ones, without re-verification).
