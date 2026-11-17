# OneTrueDose — Global Readiness Audit (Phase 1)

Date: 2026-11-17 · Scope: the whole `medicart` repository (code name MediCart, brand OneTrueDose).
No code was changed for this audit; I read the existing files, and the figures come from earlier work in this repository.

---

## 0. Executive summary

1. **The engineering base is sound and worth keeping.**
   - The app is a modular monolith: one Go API (Gin + pgx), one PostgreSQL 16 database and a React 19 SPA.
   - Money is stored as integers, migrations are embedded, and RBAC, multi-tenancy, a prescription workflow, an image-verification workflow, mock payment and AI drivers, CI and Dockerfiles are already in place.
   - **Recommendation: keep the modular monolith.** Do not split it into microservices.
2. **The product is built for India in many places.**
   - Prices and fees are in INR (`*_paise` columns, the `formatPaise` function with Indian digit grouping, a `₹` symbol).
   - Delivery serviceability is checked by Indian PIN code (`serviceable_pin_codes`).
   - Login is by Indian mobile OTP, payments are designed for Razorpay, and WhatsApp is the support channel.
   - Going international is a cross-cutting change, not a theming change.
3. **Most of the risk is legal, not technical.**
   - The catalogue is 3,300 **Indian-market brands**: Indian manufacturers and marketers, with prices set for India.
   - In every target market, prescription medicines generally have to be authorised by the local regulator and supplied through a locally licensed pharmacy. That means FDA approval plus state pharmacy licences (US), an MHRA authorisation plus GPhC registration (UK), ARTG listing with the TGA (AU), Medsafe consent (NZ), and MOHAP/DHA registration (UAE).
   - Shipping these Indian brands direct to consumers in those countries is very likely **not permissible** for most products.
   - This needs confirmation from qualified regulatory counsel in each country. This audit is not legal advice.
4. **So the right architecture is "market-gated, fail-closed, partner-fulfilled".**
   - Every product has one global identity, plus a separate listing per market.
   - A product can be sold in a market only when that market's legal eligibility is recorded and evidenced. If eligibility is unknown, the product cannot be bought there.
   - Sales in a regulated market go through locally licensed pharmacy partners. The existing tenancy model (`businesses`, `business_products`) already fits this.
5. **Launch order recommendation:**
   - **India** stays the live market, since all the current logic and data are Indian.
   - Build the global foundation next.
   - Each new country opens **only** after its legal checklist is signed off and a licensed partner is in place. Until then it can run as browse-only/information mode, or a waitlist.

---

## 1. Existing architecture assessment

```
Browser (React 19 SPA, Vite, Tailwind, react-query)
   │  REST/JSON, httpOnly session cookies, X-Business-Slug header (tenant)
   ▼
Go API — single binary (Gin, pgx/v5)          cmd/: api, migrate, seed, enrich, catalog-sync
   ├─ internal/: auth, catalog, cart, orders, payments, prescriptions, inventory,
   │             shipping, enrichment, assistant, ai, analytics, crm, enquiries,
   │             notifications, whatsapp, tenancy, affiliate, admin, audit, storage
   ├─ PostgreSQL 16 (13 migrations; FTS + trigram search; FEFO inventory)
   ├─ Object storage: local disk | S3-compatible (R2 planned)
   ├─ Payments: interface → mock (Razorpay adapter NOT implemented)
   ├─ WhatsApp: wa.me link mode (Cloud API not implemented)
   └─ AI: interface → rule-based mock (LLM driver NOT implemented)
```

| Area | Status | Verdict |
|---|---|---|
| Modular monolith, one DB | Works | **Keep**: right for the current scale |
| Money as BIGINT minor units, plus a `currency` column | Works (INR) | **Keep**; generalise from paise to minor units |
| Multi-tenant businesses and `business_products` | Works (header-based tenant) | **Keep**; reuse it for licensed pharmacy partners per market |
| Catalogue search (FTS + trigram, typo tolerance) | Works | **Keep**; add market filtering and synonyms |
| Cart/checkout with server-side pricing, row-lock reservation | Works, tested (no-oversell race test) | **Keep** |
| Prescriptions: private storage, signed URLs, review queue | Works; Rx sales disabled by flag | **Keep**; make it per market |
| Image verification and provenance (000012/000013) | Works; 303 verified photos | **Keep** |
| Payments | Mock only | **Extend** with a Stripe/Adyen adapter (and Razorpay for India) |
| Shipping | India PIN table only | **Replace** with market-aware zones and rules |
| Assistant | Rule-based, safe escalation | **Keep** the safety layer; add an LLM driver later |
| Deployment | Documented, not provisioned | **Do**: VPS + Cloudflare, as planned |

## 2. Technology stack assessment

| Layer | Current | Assessment |
|---|---|---|
| Backend | Go 1.24, Gin, pgx/v5, golang-migrate (embedded) | Excellent fit: fast, single static binary, cheap to host |
| DB | PostgreSQL 16 | Correct. It covers FTS, JSONB, row-level locking, partitioning later. No other DB is needed |
| Cache/queue | None (in-process rate limit) | Fine for one instance. Add **Redis** only when there is more than one API instance or a real job queue |
| Frontend | React 19, Vite, Tailwind, react-query, lucide | Good. Gap: it is a **pure SPA**, which is weak for SEO (see §8) |
| i18n | None; strings hard-coded in English; `₹`/en-IN in `lib/money.ts` | Must build |
| Tests | Go unit + integration tests against real Postgres; frontend: lint/tsc/build only | Backend good; frontend E2E missing (Playwright used ad hoc, not committed) |
| CI/CD | `ci.yml` (gofmt, vet, `go test -race`, eslint, tsc, build); `deploy.yml` | Good. Never run on GitHub until now. Check the first Actions run on the new repo |

## 3. Frontend and backend assessment

**Implemented and working:**
- Customer pages: home, listing, PDP, compare, search autocomplete, cart, checkout (mock pay), order tracking, account with buy-again and prescriptions, wishlist.
- Back office: admin (orders, products, inventory, prescriptions), platform admin (businesses, medicines and image review), affiliate pages.

**Partial:**
- The customer list and sales reports are missing.
- The assistant is rule-based.
- The demo banner is still on.
- `MockCheckout` is the only payment path.

**Missing for global:**
- Country selector and market context.
- i18n and RTL.
- Currency formatting per locale.
- International address forms (ZIP, postcode, emirate).
- Phone input in E.164 for any country (OTP is India-oriented).
- Email login (most Western customers expect email).
- Tax display (VAT/GST inclusive vs US sales tax exclusive).
- Per-market legal pages, cookie consent, and SSR/prerender for SEO.

**Backend gaps:**
- No `markets` concept.
- Price lives on `business_products` in INR.
- Shipping is keyed by PIN.
- No tax engine.
- No consent records or DSAR (data subject access request) endpoints.
- No MFA for staff.
- No malware scanning of uploads.

## 4. Database assessment

**Strengths:**
- Normalised core (products, brands, categories, dosage forms, inventory batches, orders, payments, audit).
- Integer money, UTC timestamps, parameterised SQL.
- Enrichment and provenance tables, and the customer-safe image view.

**Gaps for global:**
1. **No market dimension.** `products` mixes global identity with India-only facts (INR MRP, Indian schedule, Indian marketer).
2. **Composition is free text** (`generic_name`). Ingredients are not normalised, so cross-market equivalence is unreliable. This showed up in the "Metformin Hcl / Hydrochloride / Metformin" case.
3. **`*_paise` column names** encode a currency. Keep them for backward compatibility. Add `currency` to every money-bearing row, and treat values as minor units of that currency.
4. **No regulatory authorisation table** (per product × market × regulator, with an evidence URL and expiry).
5. **No consent, retention or erasure tables**, and no field-level encryption for prescription metadata.

**Proposed additions (additive migrations only; no data is deleted):**

```
markets(code PK 'IN','US','GB','AU','NZ','AE', name, default_currency, default_locale,
        locales[], tax_mode('INCLUSIVE'|'EXCLUSIVE'), status('LIVE'|'BROWSE_ONLY'|'WAITLIST'|'DISABLED'),
        rx_sales_enabled bool DEFAULT false, legal_signoff_by, legal_signoff_at, config jsonb)
currencies(code PK, minor_units, symbol)
fx_rates(base, quote, rate NUMERIC(18,8), source, fetched_at)        -- display/reference only
ingredients(id, inn_name, synonyms[])  +  product_ingredients(product_id, ingredient_id, strength_value NUMERIC, strength_unit)
product_market_listings(product_id, market_code, business_id NULL, local_brand_name,
        status('ELIGIBLE'|'NOT_ELIGIBLE'|'UNKNOWN') DEFAULT 'UNKNOWN',
        rx_classification, restricted_category, warnings jsonb,
        price_minor BIGINT, currency, tax_class, shipping_class, updated_by, updated_at)
regulatory_authorisations(product_id, market_code, regulator, authorisation_ref,
        evidence_url, verified_by, verified_at, expires_at)
consents(user_id, purpose, granted bool, policy_version, at)          -- marketing, analytics, health-data
data_requests(user_id, kind('EXPORT'|'DELETE'), status, requested_at, completed_at)
```

**Fail-closed rule (enforced in SQL, not only in the UI).** A product can be bought in market M only when all of these hold:
- `markets.status='LIVE'`;
- a listing exists with `status='ELIGIBLE'` and a price in M's currency;
- if the product is Rx, `markets.rx_sales_enabled` is true and a valid authorisation exists.

India would be seeded as `LIVE`, with listings created from current data so behaviour is unchanged. All other markets would be seeded as `DISABLED` or `BROWSE_ONLY`.

**Regional prices are set, never converted.** FX rates are used only for an optional "≈" reference, never as the selling price.

## 5. Security assessment

| Control | Current | Gap / action | Priority |
|---|---|---|---|
| Sessions | httpOnly cookies, staff and customer sessions separate, prod rejects default secret | Add SameSite=Lax/Strict review and session rotation on privilege change | High |
| Staff MFA | None | **TOTP MFA for all staff/pharmacist/platform roles** | **Immediate** before any launch |
| Rate limiting | In-process token bucket | OK for 1 instance; move to Redis or Cloudflare rules when scaling | Medium |
| SQL injection | Parameterised everywhere | Keep; add `gosec` in CI | Low effort |
| Uploads | MIME sniffing, sha256, private bucket, signed URLs | Add **ClamAV scan** (worker) and size/page limits for PDFs | High before real Rx |
| Encryption | TLS planned (Caddy/Cloudflare); disk-level at rest | Encrypted backups (age/GPG); field encryption for prescription notes | High |
| Audit log | `internal/audit` exists | Cover every admin write, plus price/eligibility/regulatory changes | High |
| Dependencies | None scanned | `govulncheck`, `npm audit`, Dependabot | Low effort |
| Secrets | `.env`, gitignored; checked before the GitHub push | Production: Docker secrets or Doppler/1Password; never in compose files | High |
| Privacy | Analytics already avoids PII and free text | Consent management, DSAR export/delete, retention jobs, cookie banner (UK/EU) | High per market |

**No compliance claim may be made in the UI** (HIPAA, GDPR, "licensed", etc.) until evidence exists. The current trust strip already follows this.

## 6. International readiness (by market)

| Market | Technical readiness | Legal position (to be verified by counsel) | Realistic first mode |
|---|---|---|---|
| **India (IN)** | Live data, INR, PIN, OTP | Existing model; still needs the Indian e-pharmacy rules sign-off already flagged in the repo | LIVE (OTC; Rx behind flag) |
| **US** | None | Rx needs FDA-approved products and a pharmacy licence in each shipping state (plus NABP/.pharmacy for credibility); importing unapproved drugs is generally prohibited; state privacy laws; HIPAA applies if acting as a covered entity | Browse-only/waitlist; future partner model |
| **UK (GB)** | None | GPhC-registered pharmacy, MHRA distance-selling logo, UK marketing authorisation for products; UK GDPR | Waitlist; partner model |
| **Australia** | None | ARTG-registered goods; advertising restrictions on Rx (no consumer advertising of prescription medicines); personal importation is a narrow individual exemption, **not a business model** | Waitlist |
| **New Zealand** | None | Medsafe consent; Pharmacy Council licensing; Privacy Act 2020 | Waitlist |
| **UAE** | None | MOHAP/DHA registration and licensed pharmacy; import permits; PDPL | Waitlist; partner model |

**Implication:** the Indian Rx catalogue **cannot** be the international catalogue. Each market needs its own eligible catalogue, sourced from locally authorised products via local partners. OTC and wellness ranges are easier but are still regulated (TGA listing, MHRA GSL/P, and so on).

## 7. Missing capabilities (ranked)

1. Market and country model with fail-closed eligibility (DB + API + middleware).
2. i18n framework, locale-aware money/date/number formatting, RTL.
3. Market-aware shipping zones and rules; tax engine (inclusive/exclusive, VAT/GST rates; US sales tax via a provider such as Stripe Tax).
4. Real payment adapter (Stripe or Adyen internationally; Razorpay for India) with webhooks and refunds.
5. Email + password/passkey auth and international phone (E.164); staff MFA.
6. Consent, DSAR and retention.
7. SEO: per-market URLs, hreflang, prerender/SSR, sitemaps, Product/Breadcrumb schema.
8. Committed Playwright E2E suite and accessibility checks (axe).
9. Observability: structured logs → Loki/Better Stack, Sentry, uptime checks, backup restore drills.
10. LLM assistant driver with tool authorisation and prompt-injection controls.

## 8. Recommended architecture

**Modular monolith, market-aware.** One Go binary, with new `markets`, `pricing`, `tax` and `compliance` modules beside the existing ones. Market context is resolved by middleware in this order:
1. URL prefix (`/us/`, `/uk/` …);
2. stored preference cookie;
3. IP country (Cloudflare `CF-IPCountry`), used only to *suggest*, never to force;

then it is placed in the request context. Every catalogue, cart and checkout query takes the market and joins `product_market_listings`.

**URL strategy: subfolders** (`onetruedose.com/us/…`, `/uk/`, `/au/`, `/nz/`, `/ae/`, `/in/`).
- One domain builds authority, and it is the cheapest option: one certificate, one Cloudflare zone.
- hreflang works cleanly between subfolders.
- ccTLDs only make sense later if a market demands local presence.

**Frontend SEO.** Keep the SPA for the app shell, cart and account. Prerender public catalogue, category and info pages per market at build/ISR time. A Go-side HTML renderer or a Vite SSG step is enough; **no Next.js rewrite** is needed now.

**Microservices: no.** At under about 100k orders/month, a monolith on one VPS with managed backups is cheaper, simpler and faster to change. Split out only for a measurable reason, for example search moving to Meilisearch/Typesense when Postgres FTS p95 exceeds about 100 ms at catalogue scale, or background workers once there are queues.

```
Cloudflare (DNS, CDN, WAF, geo header, R2 images/backups, Pages for prerendered/static)
        │
   Caddy (TLS) ── Go API (market middleware → modules) ── PostgreSQL (+ PITR backups)
                       │                                   └─ (later) read replica
                       ├─ Worker goroutines / later Redis-backed queue (emails, ClamAV, FX, sitemap)
                       └─ Adapters: Stripe/Adyen · Razorpay · Postmark/SES · carrier APIs · LLM
```

## 9. Implementation roadmap and costs

Costs are approximate (USD/month, late 2026 list prices). Development effort assumes one senior engineer working with me.

| # | Phase | Why | Immediate? | Effort | Infra cost |
|---|---|---|---|---|---|
| A | **Market foundation**: `markets`, `currencies`, `product_market_listings` (fail-closed), market middleware, India seeded as LIVE with current data, tests | Everything else depends on it; enforces "never sell in unsupported countries" | **Yes** | 1–2 wk | $0 |
| B | **i18n + money**: i18next, extract strings, `Intl.NumberFormat` currency per market, country selector (suggest from IP, persist choice), RTL scaffold, `/xx/` routes | Native feel per market | **Yes** | 2 wk | $0 |
| C | **Security hardening**: staff TOTP MFA, audit coverage, govulncheck/npm audit/gosec in CI, encrypted backups | Required before any real data | **Yes** | 1 wk | ~$0–5 |
| D | **Deploy MVP** (India): Hetzner CX32 + Cloudflare + R2 + Sentry + uptime | Get real feedback | Yes | 3–5 days | ~$25–45 |
| E | Shipping zones and tax engine, international addresses and phones, email auth | Needed for any non-IN market | Before first new market | 2–3 wk | Stripe Tax ~0.5%/txn |
| F | Payments: Razorpay (IN) live; Stripe/Adyen adapter (verify healthcare MCC eligibility per market first) | Real revenue | Before launch | 2 wk | % fees only |
| G | Privacy: consent, cookie banner, DSAR export/delete, retention jobs | UK GDPR / PDPL / Privacy Acts | Before EU/UK/AE launch | 1–2 wk | $0 |
| H | SEO: prerendered catalogue per market, hreflang, sitemaps, schema | Organic growth | After A/B | 2 wk | $0 |
| I | E2E Playwright suite (journeys and negative cases from the brief §19) + axe | Prevents regressions | Alongside B | 1 wk | CI minutes ~$0 |
| J | AI: LLM driver behind the existing safety layer (tool allow-list, no clinical answers, eval set) | Differentiator | After F | 2–3 wk | ~$50–300 API usage |
| K | Partner/market onboarding per country (legal sign-off, partner pharmacy, eligible catalogue) | Legal launch | Per market | Weeks–months, mostly non-engineering | Legal: significant, outside engineering |

**Deployment tiers:**
1. **Minimum viable production** (~$25–60/mo): one VPS (4 vCPU/8 GB), Postgres on the same box with nightly encrypted dumps plus WAL archiving to R2, Cloudflare free/pro, Sentry free, Postmark ~$15.
2. **Growing international** (~$250–600/mo): two app VPSs behind a load balancer, managed Postgres with PITR and a read replica (DigitalOcean/AWS RDS), Redis, Meilisearch, Cloudflare Pro, paid Sentry/log tier.
3. **Large-scale global** ($3k+/mo): managed Kubernetes or ECS in two regions, regional DB residency where law requires (e.g. UAE health data), dedicated search cluster, a data warehouse for analytics.

## 10. Priority-wise execution plan

1. **Now:** Phase A (market foundation, fail-closed) → B (i18n/currency/country selector) → C (MFA and security) → I (E2E tests). All are additive, so India keeps working unchanged.
2. **Next:** D (deploy India), F (Razorpay live), H (SEO).
3. **Per new market, only after legal sign-off:** E, G, a payment provider eligibility check, a partner pharmacy onboarded as a `business`, an eligible catalogue loaded into `product_market_listings`, then `markets.status = 'LIVE'`.

## Decisions needed from the business (not engineering)

- Confirm India remains a live market (assumed yes; all current data is Indian).
- Choose the first international market, and engage regulatory counsel there.
- Identify licensed partner pharmacies per market.
- Budget for tier 1 hosting.
