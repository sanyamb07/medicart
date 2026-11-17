# MediCart multi-tenant platform — architecture & implementation checklist

This document records the decisions behind turning MediCart from a single
pharmacy storefront into a multi-tenant B2B2C platform (platform owner →
independent pharmacy businesses → their own customers), and tracks exactly
what is implemented versus designed-but-not-built. Update it whenever the
scope of a phase changes — it is the source of truth for what "done" means
here, not a one-time planning artifact.

## 1. Tenancy model

- **Tenant = `businesses` row.** One business is one independently operated
  pharmacy on the platform (its own catalog selection, pricing, inventory,
  orders, customers, staff, branding).
- **Customer identity stays global, business relationship is scoped.** A
  phone number is one `users`/`customer_profiles` row platform-wide (a real
  person has one account whether they shop at one business or five — this
  mirrors how a marketplace account works). What's scoped per business is the
  *relationship*: orders, carts, prescriptions tied to a specific business,
  and a new `business_customers` row (CRM profile: lead status, lifetime
  value, notes) created the first time that customer interacts with that
  business. This avoids duplicating identity rows per tenant while still
  giving each business an isolated customer list and CRM view.
- **Staff roles are business-scoped**, not global. A new `business_staff`
  table (business_id, user_id, role_code) replaces the old assumption that
  `user_roles` roles like ADMIN/PHARMACIST mean "admin of the one store."
  The existing global `user_roles` table is kept for exactly two roles that
  are genuinely platform-wide: `CUSTOMER` (unchanged) and a new
  `PLATFORM_SUPER_ADMIN`.
- **Master catalog is global, selling is per-business.** `products` (and
  brands/categories/dosage forms) stay the single source of truth for what a
  medicine *is*. A new `business_products` row is how a business decides to
  *sell* a master product, with its own price, visibility, featured flag and
  minimum order quantity. Changing ABC Pharmacy's price never touches XYZ
  Pharmacy's `business_products` row for the same master product.
- **Tenant resolution: a business-selection header, not path prefixes or
  subdomains.** Locally there is one host (`localhost:5173`); production
  would resolve a tenant from the `Host` header (business.medicart.com). We
  simulate that with an `X-Business-Slug` request header the frontend
  attaches to every storefront call, set from a small client-side "current
  storefront" context (localStorage-backed) that a `/businesses` picker page
  lets the shopper change. This keeps the existing route surface
  (`/`, `/products`, `/cart`, ...) working unchanged for the default
  business, while every request is genuinely resolved to a tenant server-side
  — nothing is trusted from the frontend beyond "which business," and every
  repository method that touches tenant data takes an explicit `businessID`.
  Platform-admin and business-admin APIs resolve the business from the staff
  session's `business_staff` membership (business admin) or require none
  (platform admin), not from the header.
- **No unnecessary microservices.** Everything stays in the one Go module,
  one Postgres database, with tenant_id columns and server-side checks on
  every query — matching the existing architecture's grain.

## 2. What "done" means for isolation (tested, not assumed)

- A business's catalog, pricing, inventory, orders, customers and enquiries
  are only ever queried with an explicit `business_id = $1` predicate from
  resolved, authenticated context — never from a client-supplied ID without
  a matching `business_staff` row proving the caller belongs to that
  business (or a platform-admin override).
- Integration tests cover: Business A cannot read Business B's
  `business_products`/orders/customers/enquiries; a business admin session
  cannot call platform-admin routes; a platform-admin session has no implicit
  business-admin rights (it manages businesses, not their day-to-day data).

## 3. Phased checklist

Legend: `[x]` done and verified (tests pass, exercised through the API/UI),
`[~]` partially done, `[ ]` designed but not built yet.

### Phase 3 — Multi-tenant foundation
- [x] `businesses` table (slug, name, status: PENDING/ACTIVE/SUSPENDED, branding fields, created_at)
- [x] `platform_admins` table + `PLATFORM_SUPER_ADMIN` seed role
- [x] `business_staff` table (business_id, user_id, role_code)
- [x] Backfill: existing seeded staff users become `business_staff` of the
      pre-existing storefront, now named and slugged as a real business
      (`medicart-demo`)
- [x] Second seeded business (`greenleaf`) with its own staff login and
      independently-priced catalog subset, to make isolation real and
      testable rather than theoretical
- [x] `ResolveBusiness` middleware (header-based for storefront; session-based
      for business admin) + `RequireBusinessRole` middleware
- [x] Platform admin API: list/create/approve/suspend businesses
- [x] Platform admin login + minimal dashboard UI

### Phase 4 — Catalog, pricing, inventory, orders as tenant data
- [x] `business_products` (selection + price + visibility + featured + min qty)
- [x] Storefront product listing/search/detail reads from `business_products`
      joined to master `products`, scoped by resolved business
- [x] Business admin "Catalog" screen: browse master catalog, toggle listed,
      set price/visibility (separate from the platform's master-product editor)
- [x] `orders`, `carts`, `inventory_batches`, `prescriptions` gain `business_id`,
      backfilled to `medicart-demo` for existing rows, required on all new rows
- [x] Existing admin order/inventory/prescription-review screens scoped by
      the signed-in staff member's business rather than a hardcoded single
      tenant
- [ ] Per-customer-group price lists (hospital/wholesale/corporate pricing) —
      designed (see §5) but not implemented this pass

### Verified end-to-end (2026-11-11)
- [x] Live curl + Playwright pass across two real seeded businesses
      (`medicart-demo`, `greenleaf`): independent catalog/pricing/inventory,
      isolated orders, isolated CRM, isolated AI-assistant answers — see
      §6 for two real bugs this surfaced and fixed
- [x] Platform admin: login, business list/create/approve/suspend, lifecycle
      (PENDING → ACTIVE → SUSPENDED resolves/stops resolving as a storefront)
- [x] Business admin: Catalog (pick & price), Customers (CRM), Enquiries
      (AI leads) screens, all role-gated and business-scoped
- [x] Storefront switcher (`/businesses` picker, header indicator) tested
      switching a live browser session between two businesses mid-session

### Phase 5 — CRM and AI assistant as tenant features
- [x] `business_customers` (CRM profile: lead_status, notes, lifetime value,
      order count, first/last seen) created on first interaction
- [x] Business admin "Customers" screen (CRM list + lead status + notes)
- [x] `chat_sessions`/`chat_messages` (already existed, unused) wired up for
      real: every assistant turn is now logged, scoped by business
- [x] `enquiries` table + automatic capture when the assistant detects
      product/price/purchase intent, scoped by business
- [x] Business admin "Enquiries" screen (lead list, status, follow-up)
- [x] Assistant scoped per business: product/price/stock answers come from
      that business's `business_products`, never another tenant's
- [ ] LLM-backed assistant provider — still mock/rule-based only, clearly
      labelled as such in the UI (unchanged from before this phase)

### Not attempted this pass (designed only, noted so nothing is silently dropped)
- [ ] Coupon/promotion engine
- [ ] Loyalty points / referral program
- [ ] GST/tax line items
- [ ] Subscription billing for businesses, custom domains, white-label theming
- [ ] KYC/pharmacy license document verification workflow (beyond a status field)
- [ ] Notification delivery workers (the `notifications` outbox table exists,
      unwired, same as before this phase)
- [ ] Abandoned-cart recovery, frequently-bought-together, recommendation engine

## 4. Data model additions (exact shape)

See `services/api/migrations/000008_multi_tenant.up.sql` for the authoritative
definition. Summary:

```
businesses(id, slug, name, status, logo_url, support_whatsapp, created_at, updated_at)
platform_admins(user_id PK)
business_staff(business_id, user_id, role_code, PK(business_id,user_id,role_code))
business_products(id, business_id, product_id, is_listed, price_paise, is_visible,
                   is_featured, min_order_qty, UNIQUE(business_id, product_id))
business_customers(id, business_id, customer_id, lead_status, notes,
                    order_count, lifetime_value_paise, first_seen_at, last_order_at,
                    UNIQUE(business_id, customer_id))
enquiries(id, business_id, customer_id NULL, chat_session_id NULL, channel,
          products_discussed, search_queries, intent, status, contact_name,
          contact_mobile, contact_email, converted_order_id NULL, follow_up_at,
          notes, created_at, updated_at)

orders.business_id, carts.business_id, inventory_batches.business_id,
prescriptions.business_id, chat_sessions.business_id  — all NOT NULL,
backfilled to the medicart-demo business id.
```

## 6. Bugs found during end-to-end verification (fixed)

Building the storefront switcher surfaced two real, pre-existing-shaped bugs
that unit/integration tests couldn't catch (both needed a real browser):

- **CORS misconfiguration**: `internal/httpx.CORS()`'s allowed-headers list
  didn't include `X-Business-Slug`, the new custom header the frontend
  attaches to every storefront call. A real browser's CORS preflight then
  silently blocked *every* request carrying it — including staff login —
  the instant a shopper picked a non-default storefront. curl testing never
  caught this because curl doesn't enforce CORS. Fixed by adding the header
  to `Access-Control-Allow-Headers`.
- **Rate-limit bucket too broad**: `/api/v1/auth/me` and `/api/v1/staff/me`
  (fired on every single page load by the header's session check) shared the
  same 10/min-per-IP bucket as login and OTP issuance. Ordinary browsing —
  not even attempting to log in — could exhaust the budget before a real
  login attempt happened. Fixed by splitting `auth.Handlers.Register` (the
  credential-checking routes, still rate-limited) from a new
  `RegisterSessionReads` (the cheap, idempotent "am I signed in" reads,
  mounted with no rate limit).

## 5. Customer-group pricing (designed, not yet implemented)

A `business_product_prices(business_id, product_id, customer_group, price_paise)`
table with `business_customers.customer_group` (RETAIL/HOSPITAL/CLINIC/
WHOLESALE/CORPORATE, default RETAIL) would let price resolution check for a
group-specific override before falling back to `business_products.price_paise`
— without duplicating product rows per group. Left for a follow-up pass.
