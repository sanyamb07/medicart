-- 000014 Country foundation: markets, currencies, per-market product
-- eligibility and pricing, enforced in the database (fail-closed).
-- See docs/COUNTRY_FOUNDATION.md for the strategy, risks and rollback.
--
-- Purely additive: no existing column is altered or dropped, and no existing
-- row in products / business_products / orders / carts / prescriptions /
-- inventory is modified.

-- ---------------------------------------------------------------- currencies

CREATE TABLE currencies (
  code        CHAR(3) PRIMARY KEY CHECK (code ~ '^[A-Z]{3}$'),
  minor_units SMALLINT NOT NULL CHECK (minor_units BETWEEN 0 AND 4),
  symbol      TEXT NOT NULL
);

INSERT INTO currencies (code, minor_units, symbol) VALUES
  ('INR', 2, '₹'), ('USD', 2, '$'), ('GBP', 2, '£'),
  ('AUD', 2, 'A$'), ('NZD', 2, 'NZ$'), ('AED', 2, 'AED');

-- ---------------------------------------------------------------- markets

CREATE TABLE markets (
  code                   CHAR(2) PRIMARY KEY CHECK (code ~ '^[A-Z]{2}$'),
  name                   TEXT NOT NULL,
  default_currency       CHAR(3) NOT NULL REFERENCES currencies(code) ON DELETE RESTRICT,
  default_locale         TEXT NOT NULL,
  supported_locales      TEXT[] NOT NULL,
  -- Whether shelf prices are shown tax-inclusive. Configuration only in this
  -- step; no tax engine reads it yet. To be verified per market.
  tax_display            TEXT NOT NULL CHECK (tax_display IN ('INCLUSIVE','EXCLUSIVE')),
  status                 TEXT NOT NULL DEFAULT 'DISABLED'
                         CHECK (status IN ('LIVE','BROWSE_ONLY','WAITLIST','DISABLED')),
  -- NONE: no legal basis recorded. GRANDFATHERED: operated before this
  -- framework existed (India only). SIGNED_OFF: a named, qualified person
  -- signed the market's regulatory checklist.
  legal_basis            TEXT NOT NULL DEFAULT 'NONE'
                         CHECK (legal_basis IN ('NONE','GRANDFATHERED','SIGNED_OFF')),
  legal_signoff_by       TEXT,
  legal_signoff_at       TIMESTAMPTZ,
  legal_notes            TEXT,
  rx_sales_enabled       BOOLEAN NOT NULL DEFAULT FALSE,
  auto_list_new_products BOOLEAN NOT NULL DEFAULT FALSE,
  is_default             BOOLEAN NOT NULL DEFAULT FALSE,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (code, default_currency),
  CONSTRAINT markets_default_locale_supported CHECK (default_locale = ANY (supported_locales)),
  CONSTRAINT markets_open_needs_legal_basis
    CHECK (status NOT IN ('LIVE','BROWSE_ONLY') OR legal_basis <> 'NONE'),
  CONSTRAINT markets_signoff_recorded
    CHECK (legal_basis <> 'SIGNED_OFF' OR (legal_signoff_by IS NOT NULL AND legal_signoff_at IS NOT NULL)),
  CONSTRAINT markets_rx_needs_signoff
    CHECK (NOT rx_sales_enabled OR (status = 'LIVE' AND legal_basis = 'SIGNED_OFF')),
  CONSTRAINT markets_autolist_only_live
    CHECK (NOT auto_list_new_products OR status = 'LIVE'),
  CONSTRAINT markets_default_is_live
    CHECK (NOT is_default OR status = 'LIVE')
);
CREATE UNIQUE INDEX markets_single_default ON markets (is_default) WHERE is_default;
CREATE TRIGGER markets_updated_at BEFORE UPDATE ON markets
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO markets (code, name, default_currency, default_locale, supported_locales, tax_display,
                     status, legal_basis, legal_notes, rx_sales_enabled, auto_list_new_products, is_default) VALUES
  ('IN', 'India',                'INR', 'en-IN', ARRAY['en-IN'],          'INCLUSIVE', 'LIVE', 'GRANDFATHERED',
   'Existing market before migration 000014. Indian e-pharmacy regulatory checklist not yet signed off; Rx sales stay disabled.',
   FALSE, TRUE, TRUE),
  ('US', 'United States',        'USD', 'en-US', ARRAY['en-US'],          'EXCLUSIVE', 'DISABLED', 'NONE', NULL, FALSE, FALSE, FALSE),
  ('GB', 'United Kingdom',       'GBP', 'en-GB', ARRAY['en-GB'],          'INCLUSIVE', 'DISABLED', 'NONE', NULL, FALSE, FALSE, FALSE),
  ('AU', 'Australia',            'AUD', 'en-AU', ARRAY['en-AU'],          'INCLUSIVE', 'DISABLED', 'NONE', NULL, FALSE, FALSE, FALSE),
  ('NZ', 'New Zealand',          'NZD', 'en-NZ', ARRAY['en-NZ'],          'INCLUSIVE', 'DISABLED', 'NONE', NULL, FALSE, FALSE, FALSE),
  ('AE', 'United Arab Emirates', 'AED', 'en-AE', ARRAY['en-AE','ar-AE'],  'INCLUSIVE', 'DISABLED', 'NONE', NULL, FALSE, FALSE, FALSE);

-- ---------------------------------------------------------------- businesses → market

-- A pharmacy is licensed in one jurisdiction, so a business operates in
-- exactly one market. Every existing business is Indian.
ALTER TABLE businesses
  ADD COLUMN market_code CHAR(2) NOT NULL DEFAULT 'IN' REFERENCES markets(code) ON DELETE RESTRICT;
CREATE INDEX businesses_market_idx ON businesses(market_code);

-- ---------------------------------------------------------------- per-market listings

CREATE TABLE product_market_listings (
  product_id           UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  market_code          CHAR(2) NOT NULL,
  status               TEXT NOT NULL DEFAULT 'UNKNOWN'
                       CHECK (status IN ('ELIGIBLE','NOT_ELIGIBLE','UNKNOWN')),
  eligibility_basis    TEXT,
  currency             CHAR(3) NOT NULL,
  -- NULL pair = inherit the master product's price/MRP, honoured only when
  -- currency equals the product's currency (see storefront_listings).
  price_minor          BIGINT,
  mrp_minor            BIGINT,
  -- Can only make a product stricter than its master classification.
  rx_required_override BOOLEAN CHECK (rx_required_override IS NULL OR rx_required_override),
  reviewed_by          UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_at          TIMESTAMPTZ,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

  PRIMARY KEY (product_id, market_code),
  FOREIGN KEY (market_code, currency) REFERENCES markets(code, default_currency) ON DELETE RESTRICT,
  CONSTRAINT pml_eligible_needs_basis
    CHECK (status <> 'ELIGIBLE' OR length(btrim(coalesce(eligibility_basis, ''))) > 0),
  CONSTRAINT pml_price_pair CHECK ((price_minor IS NULL) = (mrp_minor IS NULL)),
  CONSTRAINT pml_price_positive CHECK (price_minor IS NULL OR (price_minor > 0 AND mrp_minor > 0)),
  CONSTRAINT pml_price_lte_mrp CHECK (price_minor IS NULL OR price_minor <= mrp_minor)
);
CREATE INDEX pml_market_status_idx ON product_market_listings(market_code, status);
CREATE TRIGGER pml_updated_at BEFORE UPDATE ON product_market_listings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Backfill India from what is sold today. Products the supplier list marks
-- as not approved for human use are already inactive; they become explicitly
-- NOT_ELIGIBLE so re-activating them can never put them back on sale.
INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency)
SELECT p.id, 'IN',
       CASE WHEN e.regulatory_group = 'not_approved_for_human_use' THEN 'NOT_ELIGIBLE' ELSE 'ELIGIBLE' END,
       CASE WHEN e.regulatory_group = 'not_approved_for_human_use'
            THEN 'Supplier list: not approved for human use'
            ELSE 'GRANDFATHERED: sold in India before migration 000014' END,
       'INR'
  FROM products p
  LEFT JOIN product_enrichment e ON e.product_id = p.id;

-- New products are listed automatically only in markets configured for it
-- (India today), and only when the currencies match so the inherited master
-- price is meaningful. Every other market starts with no row = not sellable.
CREATE FUNCTION products_auto_list_markets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency)
  SELECT NEW.id, m.code, 'ELIGIBLE', 'AUTO_LISTED: created while ' || m.code || ' auto-listing was enabled', m.default_currency
    FROM markets m
   WHERE m.auto_list_new_products AND m.default_currency = NEW.currency
  ON CONFLICT DO NOTHING;
  RETURN NEW;
END $$;
CREATE TRIGGER products_auto_list_markets AFTER INSERT ON products
  FOR EACH ROW EXECUTE FUNCTION products_auto_list_markets();

-- ---------------------------------------------------------------- the definition of "sellable"

-- One row per (business, product) a customer can actually buy, with the
-- effective price in the business's market currency. Every storefront,
-- cart and order path reads this view; nothing else decides sellability.
CREATE VIEW storefront_listings AS
SELECT bp.business_id,
       bp.product_id,
       bp.is_featured,
       bp.min_order_qty,
       biz.market_code,
       pml.currency,
       COALESCE(bp.price_paise, pml.price_minor,
                CASE WHEN pml.currency = p.currency THEN p.price_paise END) AS price_minor,
       COALESCE(pml.mrp_minor,
                CASE WHEN pml.currency = p.currency THEN p.mrp_paise END)   AS mrp_minor,
       (p.requires_prescription OR COALESCE(pml.rx_required_override, FALSE)) AS requires_prescription
  FROM business_products bp
  JOIN businesses biz ON biz.id = bp.business_id
  JOIN markets m      ON m.code = biz.market_code AND m.status = 'LIVE'
  JOIN product_market_listings pml
                      ON pml.product_id = bp.product_id AND pml.market_code = biz.market_code
                     AND pml.status = 'ELIGIBLE'
  JOIN products p     ON p.id = bp.product_id AND p.is_active
 WHERE bp.is_listed AND bp.is_visible
   AND COALESCE(bp.price_paise, pml.price_minor,
                CASE WHEN pml.currency = p.currency THEN p.price_paise END) IS NOT NULL
   AND COALESCE(pml.mrp_minor,
                CASE WHEN pml.currency = p.currency THEN p.mrp_paise END) IS NOT NULL;

-- ---------------------------------------------------------------- database-level guards

CREATE FUNCTION product_sellable(p_business UUID, p_product UUID) RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM storefront_listings
                  WHERE business_id = p_business AND product_id = p_product)
$$;

-- New cart lines only. Existing lines are never re-validated here (the cart
-- re-prices and flags them on read instead).
CREATE FUNCTION cart_items_market_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE biz UUID;
BEGIN
  SELECT business_id INTO biz FROM carts WHERE id = NEW.cart_id;
  IF NOT product_sellable(biz, NEW.product_id) THEN
    RAISE EXCEPTION 'product % is not sellable in this market', NEW.product_id
      USING ERRCODE = 'MK001';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER cart_items_market_guard BEFORE INSERT OR UPDATE OF product_id ON cart_items
  FOR EACH ROW EXECUTE FUNCTION cart_items_market_guard();

CREATE FUNCTION order_items_market_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  biz UUID;
  rx  BOOLEAN;
  rx_ok BOOLEAN;
BEGIN
  SELECT o.business_id INTO biz FROM orders o WHERE o.id = NEW.order_id;
  SELECT sl.requires_prescription, m.rx_sales_enabled INTO rx, rx_ok
    FROM storefront_listings sl JOIN markets m ON m.code = sl.market_code
   WHERE sl.business_id = biz AND sl.product_id = NEW.product_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'product % is not sellable in this market', NEW.product_id
      USING ERRCODE = 'MK001';
  END IF;
  IF (rx OR NEW.requires_prescription) AND NOT rx_ok THEN
    RAISE EXCEPTION 'prescription sales are not enabled in this market'
      USING ERRCODE = 'MK002';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER order_items_market_guard BEFORE INSERT ON order_items
  FOR EACH ROW EXECUTE FUNCTION order_items_market_guard();
