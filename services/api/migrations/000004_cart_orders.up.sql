-- Carts, prescriptions, orders and the order status ledger.
--
-- Pricing rule enforced by this schema: every order line stores the unit price
-- *as charged at the time of ordering*, copied server-side from products. The
-- browser never supplies a price, and a later price change does not rewrite
-- history.

-- ---------------------------------------------------------------- carts

-- A cart belongs to a signed-in customer, or to an anonymous browser session
-- identified by a random token in a cookie. On login the anonymous cart is
-- merged into the customer's.
CREATE TABLE carts (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id  UUID REFERENCES customer_profiles(id) ON DELETE CASCADE,
  guest_token  TEXT UNIQUE,
  currency     CHAR(3) NOT NULL DEFAULT 'INR',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT carts_owner_present CHECK (customer_id IS NOT NULL OR guest_token IS NOT NULL)
);
-- One active cart per customer.
CREATE UNIQUE INDEX carts_customer_idx ON carts(customer_id) WHERE customer_id IS NOT NULL;
CREATE TRIGGER carts_updated_at BEFORE UPDATE ON carts
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE cart_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cart_id     UUID NOT NULL REFERENCES carts(id)    ON DELETE CASCADE,
  product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  qty         INTEGER NOT NULL,
  added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (cart_id, product_id),
  -- No price column by design: the cart holds intent (product + quantity) and
  -- the server prices it on every read, so a stale cart can never charge a
  -- stale price.
  CONSTRAINT cart_items_qty_positive CHECK (qty > 0 AND qty <= 100)
);
CREATE INDEX cart_items_cart_idx ON cart_items(cart_id);
CREATE TRIGGER cart_items_updated_at BEFORE UPDATE ON cart_items
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------- prescriptions

CREATE TABLE prescriptions (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id   UUID NOT NULL REFERENCES customer_profiles(id) ON DELETE CASCADE,
  -- Private object-storage key. There is no public URL for this object; access
  -- is only ever through a short-lived signed URL issued to an authorised
  -- caller. The key itself is not a secret but is also never shown to anyone
  -- but the owner and a pharmacist.
  storage_key   TEXT NOT NULL,
  original_name TEXT,
  content_type  TEXT NOT NULL,
  size_bytes    BIGINT NOT NULL,
  -- Checksum lets us detect a re-upload of the same file and de-duplicate.
  sha256        TEXT,

  status        TEXT NOT NULL DEFAULT 'PENDING_REVIEW' CHECK (status IN (
    'PENDING_REVIEW', 'APPROVED', 'REJECTED', 'EXPIRED'
  )),
  -- Prescriber details, filled in by the reviewing pharmacist from the image.
  -- Never auto-extracted or guessed.
  doctor_name   TEXT,
  doctor_reg_no TEXT,
  issued_on     DATE,
  valid_until   DATE,

  uploaded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT rx_size_positive CHECK (size_bytes > 0),
  CONSTRAINT rx_valid_dates CHECK (valid_until IS NULL OR issued_on IS NULL OR valid_until >= issued_on)
);
CREATE INDEX prescriptions_customer_idx ON prescriptions(customer_id, uploaded_at DESC);
CREATE INDEX prescriptions_status_idx   ON prescriptions(status, uploaded_at)
  WHERE status = 'PENDING_REVIEW';
CREATE TRIGGER prescriptions_updated_at BEFORE UPDATE ON prescriptions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Each review decision is a row, so the full review history survives even if a
-- prescription is re-reviewed. Only a PHARMACIST may write here.
CREATE TABLE prescription_reviews (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  prescription_id UUID NOT NULL REFERENCES prescriptions(id) ON DELETE CASCADE,
  reviewer_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  decision        TEXT NOT NULL CHECK (decision IN ('APPROVED','REJECTED','MORE_INFO_NEEDED')),
  -- Shown to the customer, so it must be written for them.
  notes           TEXT,
  reviewed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX rx_reviews_prescription_idx ON prescription_reviews(prescription_id, reviewed_at DESC);
CREATE INDEX rx_reviews_reviewer_idx     ON prescription_reviews(reviewer_id, reviewed_at DESC);

-- ---------------------------------------------------------------- orders

CREATE TABLE orders (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Short, non-sequential, human-readable. Part of the tracking authorisation
  -- check, hence generated from a CSPRNG rather than a counter.
  reference       TEXT NOT NULL UNIQUE,
  customer_id     UUID NOT NULL REFERENCES customer_profiles(id) ON DELETE RESTRICT,

  status          TEXT NOT NULL CHECK (status IN (
    'CREATED',
    'AWAITING_CUSTOMER_CONFIRMATION',
    'AWAITING_PRESCRIPTION_REVIEW',
    'AWAITING_PAYMENT',
    'PAYMENT_PENDING',
    'PAID',
    'PROCESSING',
    'READY_FOR_DISPATCH',
    'SHIPPED',
    'DELIVERED',
    'CANCELLED',
    'REFUND_PENDING',
    'REFUNDED'
  )),

  -- Delivery address is COPIED onto the order, not referenced: editing a saved
  -- address must not retroactively change where a past order went.
  recipient_name  TEXT NOT NULL,
  contact_mobile  VARCHAR(15) NOT NULL,
  contact_email   CITEXT,
  address_line1   TEXT NOT NULL,
  address_line2   TEXT,
  landmark        TEXT,
  city            TEXT NOT NULL,
  state           TEXT NOT NULL,
  pin_code        VARCHAR(6) NOT NULL,
  delivery_notes  TEXT,

  -- Money, all server-computed. subtotal = sum of line totals.
  subtotal_paise      BIGINT NOT NULL,
  discount_paise      BIGINT NOT NULL DEFAULT 0,
  delivery_fee_paise  BIGINT NOT NULL DEFAULT 0,
  tax_paise           BIGINT NOT NULL DEFAULT 0,
  total_paise         BIGINT NOT NULL,
  currency            CHAR(3) NOT NULL DEFAULT 'INR',

  -- Set when any line requires a prescription.
  requires_prescription BOOLEAN NOT NULL DEFAULT FALSE,
  prescription_id       UUID REFERENCES prescriptions(id) ON DELETE SET NULL,

  -- WhatsApp handoff bookkeeping. handoff_opened_at records that we generated
  -- the link and the customer clicked it; it is explicitly NOT proof that a
  -- message was sent, which is why confirmation is a separate timestamp set
  -- only by an authorised operator or a verified inbound webhook.
  whatsapp_handoff_at    TIMESTAMPTZ,
  whatsapp_confirmed_at  TIMESTAMPTZ,
  whatsapp_confirmed_by  UUID REFERENCES users(id) ON DELETE SET NULL,

  cancelled_reason  TEXT,
  internal_notes    TEXT,

  placed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT orders_pin_format      CHECK (pin_code ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT orders_mobile_format   CHECK (contact_mobile ~ '^[6-9][0-9]{9}$'),
  CONSTRAINT orders_currency_inr    CHECK (currency = 'INR'),
  CONSTRAINT orders_amounts_non_negative CHECK (
    subtotal_paise >= 0 AND discount_paise >= 0 AND
    delivery_fee_paise >= 0 AND tax_paise >= 0 AND total_paise >= 0),
  -- The total must always equal its parts. A pricing bug therefore aborts the
  -- transaction instead of persisting an order that cannot be reconciled.
  CONSTRAINT orders_total_consistent CHECK (
    total_paise = subtotal_paise - discount_paise + delivery_fee_paise + tax_paise),
  CONSTRAINT orders_discount_within_subtotal CHECK (discount_paise <= subtotal_paise),
  CONSTRAINT orders_cancelled_has_reason CHECK (status <> 'CANCELLED' OR cancelled_reason IS NOT NULL)
);
CREATE INDEX orders_customer_idx ON orders(customer_id, placed_at DESC);
CREATE INDEX orders_status_idx   ON orders(status, placed_at DESC);
CREATE INDEX orders_placed_idx   ON orders(placed_at DESC);
CREATE INDEX orders_pin_idx      ON orders(pin_code);
CREATE TRIGGER orders_updated_at BEFORE UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Now that orders exists, close the loop from the inventory ledger.
ALTER TABLE inventory_movements
  ADD CONSTRAINT movements_order_fk
  FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE SET NULL;

CREATE TABLE order_items (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id      UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id    UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,

  -- Denormalised product descriptors, frozen at order time. The product row
  -- may be renamed, repriced or deactivated later; the invoice must not change.
  product_name  TEXT NOT NULL,
  product_sku   TEXT NOT NULL,
  brand_name    TEXT,
  strength      TEXT,
  dosage_form   TEXT,
  pack_size     TEXT,

  qty                INTEGER NOT NULL,
  unit_mrp_paise     BIGINT NOT NULL,
  unit_price_paise   BIGINT NOT NULL,
  line_total_paise   BIGINT NOT NULL,
  tax_rate_bps       INTEGER NOT NULL DEFAULT 0,
  currency           CHAR(3) NOT NULL DEFAULT 'INR',

  requires_prescription BOOLEAN NOT NULL DEFAULT FALSE,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (order_id, product_id),
  CONSTRAINT order_items_qty_positive CHECK (qty > 0),
  CONSTRAINT order_items_prices_positive CHECK (unit_price_paise > 0 AND unit_mrp_paise > 0),
  CONSTRAINT order_items_price_lte_mrp CHECK (unit_price_paise <= unit_mrp_paise),
  -- Arithmetic guard: the line total must be the unit price times quantity.
  CONSTRAINT order_items_line_total_consistent
    CHECK (line_total_paise = unit_price_paise * qty)
);
CREATE INDEX order_items_order_idx   ON order_items(order_id);
CREATE INDEX order_items_product_idx ON order_items(product_id);

-- Which batch satisfied which line. Needed to answer "what is the expiry of
-- what I was sent?" truthfully, and for recalls.
CREATE TABLE order_item_allocations (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
  batch_id      UUID NOT NULL REFERENCES inventory_batches(id) ON DELETE RESTRICT,
  qty           INTEGER NOT NULL,
  released_at   TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT allocation_qty_positive CHECK (qty > 0)
);
CREATE INDEX allocations_item_idx  ON order_item_allocations(order_item_id);
CREATE INDEX allocations_batch_idx ON order_item_allocations(batch_id);

-- Append-only status ledger. Every transition is recorded with who caused it.
CREATE TABLE order_status_history (
  id          BIGSERIAL PRIMARY KEY,
  order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  from_status TEXT,                  -- null for the initial CREATED row
  to_status   TEXT NOT NULL,
  reason      TEXT,
  -- 'customer' | 'staff' | 'system' | 'payment_webhook' | 'whatsapp_webhook'
  actor_type  TEXT NOT NULL,
  actor_id    UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_status_history_order_idx ON order_status_history(order_id, created_at);

-- Stock reservations that expire. A reservation holds inventory while the
-- customer completes WhatsApp confirmation and payment; if they abandon the
-- flow, a sweep releases it so the stock is sellable again.
CREATE TABLE stock_reservations (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'ACTIVE'
              CHECK (status IN ('ACTIVE','RELEASED','CONSUMED')),
  expires_at  TIMESTAMPTZ NOT NULL,
  released_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One active reservation per order.
CREATE UNIQUE INDEX stock_reservations_active_order_idx
  ON stock_reservations(order_id) WHERE status = 'ACTIVE';
CREATE INDEX stock_reservations_expiry_idx
  ON stock_reservations(expires_at) WHERE status = 'ACTIVE';

CREATE TABLE shipments (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  carrier         TEXT,
  tracking_number TEXT,
  -- Carrier tracking page. Shown only when actually known.
  tracking_url    TEXT,
  status          TEXT NOT NULL DEFAULT 'PENDING'
                  CHECK (status IN ('PENDING','DISPATCHED','IN_TRANSIT','DELIVERED','RETURNED','LOST')),
  dispatched_at   TIMESTAMPTZ,
  delivered_at    TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX shipments_order_idx ON shipments(order_id);
CREATE TRIGGER shipments_updated_at BEFORE UPDATE ON shipments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
