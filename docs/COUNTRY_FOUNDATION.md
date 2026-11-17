# Step 1 — Country Foundation (migration 000014)

Status: see "Verification" at the bottom. That section is filled in only after the commands have actually run.

## Goal

Introduce markets (countries) and per-market product eligibility, with **India as the only, default, LIVE market**, and **no change** to what Indian customers can buy or what they pay.

## Design

| Object | Purpose |
|---|---|
| `currencies` | ISO 4217 code and minor units (INR, USD, GBP, AUD, NZD, AED, all 2 dp) |
| `markets` | One row per country: currency, locales, tax display, `status` (LIVE / BROWSE_ONLY / WAITLIST / DISABLED), `legal_basis`, `rx_sales_enabled`, `auto_list_new_products`, `is_default` |
| `businesses.market_code` | Each pharmacy operates in exactly one market (licences are jurisdictional). Existing businesses are backfilled to `IN` |
| `product_market_listings` | One row per product × market: `status` ELIGIBLE / NOT_ELIGIBLE / UNKNOWN (default **UNKNOWN**), `eligibility_basis`, regional `price_minor`/`mrp_minor` in the market's currency, `rx_required_override` (can only make a product *stricter*), reviewer and time |
| `storefront_listings` (view) | The **single definition of "sellable"**: business listing is listed and visible, product is active, the business's market is LIVE, the product is ELIGIBLE in that market, and a price exists in the market currency |
| `product_sellable()` + triggers on `cart_items` and `order_items` | Database-level enforcement: an ineligible product cannot enter a cart or an order even if application code is wrong. An Rx line cannot enter an order unless the market has `rx_sales_enabled` |

### Rules enforced by constraints (fail-closed)

- A market can be LIVE or BROWSE_ONLY only with a `legal_basis` other than NONE.
- A `SIGNED_OFF` legal basis requires a recorded signer and date.
- `rx_sales_enabled` requires `legal_basis = 'SIGNED_OFF'`.
  - India is `GRANDFATHERED`: it existed before this framework. Its Indian e-pharmacy checklist is still open, so Rx stays off, exactly as today. The old `FEATURE_RX_SALES_ENABLED=false` setting still applies as well.
- Exactly one default market, and it must be LIVE.
- A listing can be ELIGIBLE only with a written `eligibility_basis`.
- A listing's currency must equal its market's currency (composite foreign key).
- Regional price and MRP are set together, price ≤ MRP.
- A NULL regional price means "inherit the master product price". This is only honoured when the market currency equals the product's currency (INR). Otherwise the product has no price there and is **not sellable**.
- New products are auto-listed only in markets with `auto_list_new_products = TRUE`, only India today, and only when the currencies match. This preserves today's admin "create product → it sells in India" workflow. Every other market starts UNKNOWN, so the product is not sellable there.

### Prices

Money stays in BIGINT minor units. Existing `*_paise` columns are **not renamed or changed**. A business's `price_paise` override is now read as "minor units of that business's market currency". For India that is identical to today.

Regional prices are set explicitly. There is no FX-derived selling price.

## Affected tables

| Table | Change | Existing data touched? |
|---|---|---|
| `currencies`, `markets`, `product_market_listings` | New | No |
| `businesses` | `ADD COLUMN market_code CHAR(2) NOT NULL DEFAULT 'IN'` | Values added only; no existing column changed |
| `products` | New AFTER INSERT trigger (auto-list) | No |
| `cart_items`, `order_items` | New BEFORE INSERT triggers (guards) | No (existing rows are never re-validated) |
| `products`, `business_products`, `orders`, `order_items`, `users`, `prescriptions`, `inventory_*`, `payments` | **Unchanged** | No |

Backfill: one `IN` listing per existing product.
- ELIGIBLE (basis: `GRANDFATHERED: sold in India before migration 000014`) for every product,
- except the 79 rows classed `not_approved_for_human_use`, which become NOT_ELIGIBLE. They are already inactive, so nothing a customer sees changes, but they can no longer be resold by simply re-activating them.

## Application changes

- Storefront catalogue, product page, cart (add / set quantity / re-price) and the business catalogue read through `storefront_listings` instead of hand-written `bp.is_listed … AND p.is_active` predicates.
- `GET /api/v1/markets`: public list of LIVE markets (code, currency, locales, default).
- The storefront business payload now includes `marketCode`.
- Platform admin:
  - `GET /api/v1/platform/markets`
  - `GET /api/v1/platform/products/:id/markets`
  - `PUT /api/v1/platform/products/:id/markets/:code`, which sets eligibility, basis and regional price. It is audit-logged and validated. It cannot change a market's status or switch on Rx sales: those require a legal sign-off and are deliberately not exposed through the API.

## Backward-compatibility risks and mitigations

| Risk | Mitigation |
|---|---|
| A product that sells today disappears | Backfill makes every currently-sellable product ELIGIBLE in IN, with NULL price (inherits the master price). Verified by diffing the full sellable set (product × business × price × MRP × Rx) before and after |
| Admin creates a product and it doesn't appear | Auto-list trigger for IN; covered by test |
| Admin price edits stop applying | IN listings inherit the master price (NULL regional price), so edits still flow through |
| Existing carts or orders break | Guards apply to new inserts only; existing rows are never re-validated |
| Test fixtures insert products directly | Trigger auto-lists them in IN, so fixtures keep working |
| Performance | The view adds two indexed PK joins; listing query timing checked after migration |

## Backup and recovery (verified before migrating)

- `data/enrichment/pre_000014_markets_backup.dump` (pg_dump custom format, sha256 in the `.sha256` file).
- It was restored into a scratch database, `medicart_restore_check`. Row counts for products, business_products, orders, order_items, users, prescriptions, inventory_batches and product_images, the sum of all product prices, and the schema version all matched the live database exactly.

## Rollback

1. **Preferred (schema only):** `go run ./cmd/migrate down 1`. This runs `000014_markets.down.sql`, which drops the triggers, functions, view, listings, `businesses.market_code`, markets and currencies. No pre-existing column is altered, so the pre-000014 data is untouched.
   - Regional listing data entered after the migration is lost on rollback. Export it first: `\copy product_market_listings TO 'listings.csv' CSV HEADER`.
2. The application must be rolled back to the previous commit together with the schema, because the new code reads `storefront_listings`.
3. **Disaster:** `pg_restore --clean --if-exists -d medicart pre_000014_markets_backup.dump`. Note that this also discards any orders placed after the backup.

## Follow-up: migration 000015 (order currency guard)

`orders.currency` still defaults to `INR` (from 000004). An order for a business in a non-INR market would therefore record foreign minor units labelled INR. No such market can be LIVE today, but 000015 extends the `order_items` guard to refuse a line whose order currency differs from the market currency (SQLSTATE `MK003`). Checkout maps any `MK00x` guard refusal to a 409 with a plain message instead of a 500. Its down migration restores the 000014 guard.

## Verification (executed 2026-11-17)

| Check | Result |
|---|---|
| Backup + test restore into a scratch DB; row counts and price sum compared | Identical |
| Rehearsal on the restored copy: up → diff → down → up (000014 and 000015) | Clean both ways |
| Sellable set (business × product × price × MRP × Rx), before vs after, **rehearsal and live** | Byte-identical, 3,236 rows |
| Live row counts after migration (products 3,312, orders 7, order_items 7, users 13, prescriptions 3, batches 3,317, price sum) | Unchanged |
| `go vet`, `go test -race ./...` | All pass; 37 integration tests (28 existing + 9 new) |
| HTTP end-to-end against the migrated copy (port 8090): markets, listing, cart, guest checkout, tracking, Rx checkout refused, OTP login, /auth/me, my orders, buy-again, prescription upload/list/signed URL, logout | 18/18 pass |
| Browser smoke (live, desktop 1366 + mobile 390): home, search, compare, cart, product page | ₹ prices render, related products load, 0 JS errors, 0 API 5xx |

New tests (`test/integration/markets_test.go`):
- India is the only LIVE/default market and Rx is off everywhere;
- new products are auto-listed only in IN;
- ten fail-closed constraint cases;
- a business in a disabled market can sell nothing (catalogue, product page, cart service, and a raw SQL cart insert all refused);
- a NOT_ELIGIBLE product is hidden, its existing cart line is flagged, and checkout refuses it;
- the order-item guards refuse UNKNOWN eligibility (MK001), Rx (MK002) and currency mismatch (MK003);
- regional GBP pricing is independent of the INR price, the business override still applies, and a GB cart prices in GBP;
- re-activating a not-approved product keeps it unsellable;
- listing API validation and audit logging.

The scratch database was dropped after testing because it held a copy of customer data. Backups kept: `data/enrichment/pre_000014_markets_backup.dump` and `pre_000014_markets_final.dump` (gitignored).
