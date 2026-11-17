-- Multi-tenant foundation: platform owner -> independent pharmacy
-- businesses -> their own customers, catalog selection, pricing, inventory,
-- orders, staff and CRM. See docs/MULTI_TENANT_PLAN.md for the reasoning.
--
-- Tenancy model recap:
--   * businesses is the tenant. Master products/brands/categories stay
--     global; a business "sells" a master product via business_products,
--     which carries its own price/visibility, never touching the master row.
--   * Customer identity (users/customer_profiles) stays global — one phone
--     number is one account platform-wide — but the *relationship* to a
--     business (orders, carts, prescriptions, CRM profile) is scoped.
--   * Staff roles move from the global user_roles table to business_staff,
--     scoped per business. Only CUSTOMER and the new PLATFORM_SUPER_ADMIN
--     role remain meaningful in user_roles.

INSERT INTO roles (code, description) VALUES
  ('PLATFORM_SUPER_ADMIN', 'Manages businesses, platform configuration and platform-wide oversight');

-- ---------------------------------------------------------------- businesses

CREATE TABLE businesses (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug            TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'PENDING'
                  CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','REJECTED')),
  logo_url        TEXT,
  tagline         TEXT,
  support_whatsapp VARCHAR(15),
  -- KYC/license verification is a documented requirement, not a built
  -- workflow this pass: this is a status field a platform admin sets after
  -- reviewing documents exchanged outside the system, not a verified claim.
  license_status  TEXT NOT NULL DEFAULT 'UNVERIFIED'
                  CHECK (license_status IN ('UNVERIFIED','SUBMITTED','VERIFIED')),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER businesses_updated_at BEFORE UPDATE ON businesses
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Platform-level administrators. Separate from business_staff: a platform
-- admin manages businesses, not any one business's day-to-day data.
CREATE TABLE platform_admins (
  user_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  granted_by  UUID REFERENCES users(id) ON DELETE SET NULL
);

-- A user's staff role is scoped to one business (the common case: an
-- employee works for one pharmacy). The same user could hold rows at more
-- than one business, which is why this is a join table rather than a column
-- on users.
CREATE TABLE business_staff (
  business_id  UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_code    TEXT NOT NULL REFERENCES roles(code) ON DELETE RESTRICT,
  is_active    BOOLEAN NOT NULL DEFAULT TRUE,
  granted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  granted_by   UUID REFERENCES users(id) ON DELETE SET NULL,
  PRIMARY KEY (business_id, user_id, role_code)
);
CREATE INDEX business_staff_user_idx ON business_staff(user_id) WHERE is_active;

-- Seed the business that already existed before multi-tenancy: every
-- existing order/cart/batch/prescription backfills to this row, so nothing
-- that worked before this migration breaks.
INSERT INTO businesses (id, slug, name, status, license_status, support_whatsapp)
VALUES ('00000000-0000-0000-0000-000000000001', 'medicart-demo', 'MediCart Demo Pharmacy', 'ACTIVE', 'VERIFIED', NULL);

-- Every user who already held a back-office role becomes business_staff of
-- the demo pharmacy, preserving their existing access exactly.
INSERT INTO business_staff (business_id, user_id, role_code)
SELECT '00000000-0000-0000-0000-000000000001', ur.user_id, ur.role_code
  FROM user_roles ur
 WHERE ur.role_code IN ('ADMIN','INVENTORY_MANAGER','ORDER_MANAGER','PHARMACIST','CUSTOMER_SUPPORT');

-- ---------------------------------------------------------------- catalog selling

-- A business decides to sell a master product by creating this row. Price is
-- the business's own selling price; MRP stays on the master product (a
-- manufacturer ceiling, not something a seller changes). NULL price means
-- "use the master product's price_paise" so listing a product doesn't force
-- re-entering a price that already matches.
CREATE TABLE business_products (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_id    UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  product_id     UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  is_listed      BOOLEAN NOT NULL DEFAULT TRUE,
  price_paise    BIGINT,
  is_visible     BOOLEAN NOT NULL DEFAULT TRUE,
  is_featured    BOOLEAN NOT NULL DEFAULT FALSE,
  min_order_qty  INTEGER NOT NULL DEFAULT 1,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (business_id, product_id),
  CONSTRAINT business_products_price_positive CHECK (price_paise IS NULL OR price_paise > 0),
  CONSTRAINT business_products_min_qty_positive CHECK (min_order_qty >= 1)
);
CREATE INDEX business_products_business_idx ON business_products(business_id) WHERE is_listed AND is_visible;
CREATE TRIGGER business_products_updated_at BEFORE UPDATE ON business_products
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The demo business starts out selling every currently-seeded master
-- product at the master's own price, so the existing storefront keeps
-- working exactly as it did pre-migration.
INSERT INTO business_products (business_id, product_id, is_listed, is_visible, is_featured)
SELECT '00000000-0000-0000-0000-000000000001', p.id, TRUE, TRUE, p.is_featured
  FROM products p;

-- ---------------------------------------------------------------- tenant-owned data

ALTER TABLE inventory_batches ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE CASCADE;
UPDATE inventory_batches SET business_id = '00000000-0000-0000-0000-000000000001' WHERE business_id IS NULL;
ALTER TABLE inventory_batches ALTER COLUMN business_id SET NOT NULL;
ALTER TABLE inventory_batches DROP CONSTRAINT inventory_batches_product_id_batch_number_key;
ALTER TABLE inventory_batches ADD CONSTRAINT inventory_batches_business_product_batch_key
  UNIQUE (business_id, product_id, batch_number);
CREATE INDEX inventory_batches_business_idx ON inventory_batches(business_id, product_id);

ALTER TABLE orders ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE RESTRICT;
UPDATE orders SET business_id = '00000000-0000-0000-0000-000000000001' WHERE business_id IS NULL;
ALTER TABLE orders ALTER COLUMN business_id SET NOT NULL;
CREATE INDEX orders_business_idx ON orders(business_id, placed_at DESC);

ALTER TABLE carts ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE CASCADE;
UPDATE carts SET business_id = '00000000-0000-0000-0000-000000000001' WHERE business_id IS NULL;
ALTER TABLE carts ALTER COLUMN business_id SET NOT NULL;
DROP INDEX carts_customer_idx;
ALTER TABLE carts DROP CONSTRAINT carts_guest_token_key;
CREATE UNIQUE INDEX carts_customer_business_idx ON carts(customer_id, business_id) WHERE customer_id IS NOT NULL;
CREATE UNIQUE INDEX carts_guest_business_idx ON carts(guest_token, business_id) WHERE guest_token IS NOT NULL;

ALTER TABLE prescriptions ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE CASCADE;
UPDATE prescriptions SET business_id = '00000000-0000-0000-0000-000000000001' WHERE business_id IS NULL;
ALTER TABLE prescriptions ALTER COLUMN business_id SET NOT NULL;
CREATE INDEX prescriptions_business_idx ON prescriptions(business_id, status);

ALTER TABLE chat_sessions ADD COLUMN business_id UUID REFERENCES businesses(id) ON DELETE CASCADE;
UPDATE chat_sessions SET business_id = '00000000-0000-0000-0000-000000000001' WHERE business_id IS NULL;
ALTER TABLE chat_sessions ALTER COLUMN business_id SET NOT NULL;
CREATE INDEX chat_sessions_business_idx ON chat_sessions(business_id, created_at DESC);

-- ---------------------------------------------------------------- CRM

-- A business's view of one customer. Created the first time that customer
-- interacts with the business (chat, cart, or order) — never duplicating the
-- global identity, just the business-specific relationship.
CREATE TABLE business_customers (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_id        UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  customer_id        UUID NOT NULL REFERENCES customer_profiles(id) ON DELETE CASCADE,
  lead_status        TEXT NOT NULL DEFAULT 'NEW'
                     CHECK (lead_status IN ('NEW','CONTACTED','INTERESTED','AWAITING_RESPONSE','CONVERTED','LOST')),
  notes              TEXT,
  order_count        INTEGER NOT NULL DEFAULT 0,
  lifetime_value_paise BIGINT NOT NULL DEFAULT 0,
  first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_order_at      TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (business_id, customer_id)
);
CREATE INDEX business_customers_business_idx ON business_customers(business_id, lead_status, updated_at DESC);
CREATE TRIGGER business_customers_updated_at BEFORE UPDATE ON business_customers
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A recorded customer enquiry/lead — created automatically when the AI
-- assistant detects product, price or purchase intent, so interest that
-- doesn't convert to an order is never silently lost.
CREATE TABLE enquiries (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_id        UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  customer_id        UUID REFERENCES customer_profiles(id) ON DELETE SET NULL,
  chat_session_id    UUID REFERENCES chat_sessions(id) ON DELETE SET NULL,
  channel            TEXT NOT NULL DEFAULT 'AI_CHAT' CHECK (channel IN ('AI_CHAT','OTHER')),
  products_discussed TEXT[] NOT NULL DEFAULT '{}',
  search_queries     TEXT[] NOT NULL DEFAULT '{}',
  intent             TEXT,
  status             TEXT NOT NULL DEFAULT 'NEW'
                     CHECK (status IN ('NEW','CONTACTED','INTERESTED','AWAITING_RESPONSE','CONVERTED','LOST')),
  contact_name       TEXT,
  contact_mobile     VARCHAR(15),
  contact_email      CITEXT,
  converted_order_id UUID REFERENCES orders(id) ON DELETE SET NULL,
  follow_up_at       TIMESTAMPTZ,
  notes              TEXT,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX enquiries_business_idx ON enquiries(business_id, status, updated_at DESC);
CREATE INDEX enquiries_session_idx ON enquiries(chat_session_id);
CREATE TRIGGER enquiries_updated_at BEFORE UPDATE ON enquiries
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
