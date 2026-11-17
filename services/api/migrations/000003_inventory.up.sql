-- Inventory with batch and expiry tracking.
--
-- Stock is held per batch, not per product, because expiry is a property of a
-- batch and an expired batch must never be sold. Product-level availability is
-- derived from the sum of sellable batches.
--
-- Three quantities per batch:
--   qty_received  - immutable record of what arrived
--   qty_on_hand   - physically present
--   qty_reserved  - committed to unpaid orders (held, not yet shipped)
-- Sellable = qty_on_hand - qty_reserved, for non-expired, active batches.

CREATE TABLE inventory_batches (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id      UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  batch_number    TEXT NOT NULL,
  -- Expiry is a real date from the pack. It is never inferred or defaulted;
  -- a batch cannot be created without it.
  expiry_date     DATE NOT NULL,
  manufactured_on DATE,

  qty_received    INTEGER NOT NULL,
  qty_on_hand     INTEGER NOT NULL,
  qty_reserved    INTEGER NOT NULL DEFAULT 0,

  -- Purchase cost, for margin reporting. Separate from the selling price.
  cost_paise      BIGINT,
  currency        CHAR(3) NOT NULL DEFAULT 'INR',

  supplier_name   TEXT,
  invoice_ref     TEXT,
  -- Set when a batch is quarantined/recalled: it stops being sellable without
  -- destroying the stock record.
  is_blocked      BOOLEAN NOT NULL DEFAULT FALSE,
  blocked_reason  TEXT,

  received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (product_id, batch_number),
  CONSTRAINT batch_qty_received_positive CHECK (qty_received > 0),
  -- The database itself forbids negative stock and over-reservation, so a bug
  -- in application logic becomes a failed transaction rather than oversold
  -- inventory.
  CONSTRAINT batch_qty_on_hand_non_negative CHECK (qty_on_hand >= 0),
  CONSTRAINT batch_qty_reserved_non_negative CHECK (qty_reserved >= 0),
  CONSTRAINT batch_reserved_lte_on_hand CHECK (qty_reserved <= qty_on_hand),
  CONSTRAINT batch_cost_non_negative CHECK (cost_paise IS NULL OR cost_paise >= 0),
  CONSTRAINT batch_blocked_has_reason
    CHECK (NOT is_blocked OR blocked_reason IS NOT NULL),
  CONSTRAINT batch_mfg_before_expiry
    CHECK (manufactured_on IS NULL OR manufactured_on < expiry_date)
);

CREATE TRIGGER inventory_batches_updated_at BEFORE UPDATE ON inventory_batches
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Allocation order is FEFO (first-expiry-first-out): this index serves both
-- the sellable-stock sum and the pick order.
CREATE INDEX batches_product_expiry_idx
  ON inventory_batches(product_id, expiry_date)
  WHERE is_blocked = FALSE AND qty_on_hand > 0;
CREATE INDEX batches_expiry_idx ON inventory_batches(expiry_date)
  WHERE is_blocked = FALSE AND qty_on_hand > 0;

-- Append-only ledger of every stock change. Never updated or deleted: the
-- current qty_on_hand must always be reproducible by replaying this table.
CREATE TABLE inventory_movements (
  id            BIGSERIAL PRIMARY KEY,
  batch_id      UUID NOT NULL REFERENCES inventory_batches(id) ON DELETE RESTRICT,
  product_id    UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  movement_type TEXT NOT NULL CHECK (movement_type IN (
    'RECEIPT',        -- new stock arrived
    'RESERVE',        -- held for an order
    'RELEASE',        -- reservation expired or order cancelled
    'DISPATCH',       -- shipped; leaves qty_on_hand
    'RETURN',         -- customer return back into stock
    'ADJUSTMENT',     -- manual correction (count, damage)
    'EXPIRY_WRITEOFF' -- removed because expired
  )),
  -- Signed delta applied to qty_on_hand (0 for pure reservation moves).
  qty_delta        INTEGER NOT NULL,
  -- Signed delta applied to qty_reserved.
  qty_reserved_delta INTEGER NOT NULL DEFAULT 0,
  reason        TEXT,
  order_id      UUID,       -- FK added in the orders migration
  actor_id      UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT movement_has_effect CHECK (qty_delta <> 0 OR qty_reserved_delta <> 0)
);
CREATE INDEX movements_batch_idx   ON inventory_movements(batch_id, created_at DESC);
CREATE INDEX movements_product_idx ON inventory_movements(product_id, created_at DESC);
CREATE INDEX movements_order_idx   ON inventory_movements(order_id) WHERE order_id IS NOT NULL;

-- Serviceable delivery areas. An order cannot be placed to a PIN code absent
-- from this table (or present with is_serviceable = false).
CREATE TABLE serviceable_pin_codes (
  pin_code          VARCHAR(6) PRIMARY KEY,
  city              TEXT NOT NULL,
  state             TEXT NOT NULL,
  is_serviceable    BOOLEAN NOT NULL DEFAULT TRUE,
  -- Delivery estimate in days, shown as a range. Null means "we cannot give an
  -- estimate" and the UI says exactly that rather than inventing one.
  eta_min_days      SMALLINT,
  eta_max_days      SMALLINT,
  -- Per-area override of the global delivery fee; null falls back to config.
  delivery_fee_paise BIGINT,
  cod_available     BOOLEAN NOT NULL DEFAULT FALSE,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT pin_format CHECK (pin_code ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT pin_eta_order CHECK (
    eta_min_days IS NULL OR eta_max_days IS NULL OR eta_min_days <= eta_max_days),
  CONSTRAINT pin_fee_non_negative CHECK (delivery_fee_paise IS NULL OR delivery_fee_paise >= 0)
);
CREATE INDEX pin_codes_serviceable_idx ON serviceable_pin_codes(is_serviceable);
CREATE TRIGGER pin_codes_updated_at BEFORE UPDATE ON serviceable_pin_codes
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Editable policy text (shipping, returns, refunds, prescriptions). Served to
-- the storefront help centre and to the AI assistant, so there is exactly one
-- source of truth and the assistant cannot invent a policy.
CREATE TABLE shipping_policies (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_key  TEXT NOT NULL UNIQUE,   -- 'delivery', 'returns', 'refunds'
  title       TEXT NOT NULL,
  body        TEXT NOT NULL,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER shipping_policies_updated_at BEFORE UPDATE ON shipping_policies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
