-- MediCart core schema: extensions, identity, roles, addresses, audit.
--
-- Conventions used by every migration:
--   * Primary keys are UUIDs generated in the database.
--   * Money is BIGINT minor units (paise) plus an explicit currency column.
--   * Timestamps are TIMESTAMPTZ, written as UTC (now() returns UTC here
--     because the server runs in UTC; values are stored absolute regardless).
--   * Soft deletion is avoided except where history matters (products use an
--     is_active flag because orders reference them forever).

-- pg_trgm powers fuzzy/misspelled product search alongside full-text search.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- citext gives case-insensitive uniqueness for emails without lower() indexes.
CREATE EXTENSION IF NOT EXISTS citext;

-- Shared trigger to maintain updated_at without the application remembering.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------- identity

CREATE TABLE users (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Customers sign in with a mobile number (OTP); staff sign in with email +
  -- password. Either identifier may be null, but at least one is required.
  mobile          VARCHAR(15) UNIQUE,
  email           CITEXT UNIQUE,
  password_hash   TEXT,
  full_name       TEXT,
  is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
  last_login_at   TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT users_identifier_present CHECK (mobile IS NOT NULL OR email IS NOT NULL),
  -- Indian mobile numbers: 10 digits starting 6-9. Stored without country code.
  CONSTRAINT users_mobile_format CHECK (mobile IS NULL OR mobile ~ '^[6-9][0-9]{9}$'),
  -- A password hash without an email would be unusable; keep the pair honest.
  CONSTRAINT users_password_needs_email CHECK (password_hash IS NULL OR email IS NOT NULL)
);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE roles (
  code        TEXT PRIMARY KEY,
  description TEXT NOT NULL
);

CREATE TABLE user_roles (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_code  TEXT NOT NULL REFERENCES roles(code) ON DELETE RESTRICT,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
  PRIMARY KEY (user_id, role_code)
);
CREATE INDEX user_roles_role_idx ON user_roles(role_code);

INSERT INTO roles (code, description) VALUES
  ('CUSTOMER',          'Shops on the storefront and manages their own orders'),
  ('ADMIN',             'Full back-office access including user and role management'),
  ('INVENTORY_MANAGER', 'Manages products, stock, batches and expiry'),
  ('ORDER_MANAGER',     'Confirms, fulfils, cancels and refunds orders'),
  ('PHARMACIST',        'Reviews and approves or rejects prescriptions'),
  ('CUSTOMER_SUPPORT',  'Answers customer enquiries; limited customer data access');

-- Customer-specific profile data, split from users so staff accounts carry none.
CREATE TABLE customer_profiles (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  default_address_id     UUID,  -- FK added after addresses exists
  marketing_opt_in       BOOLEAN     NOT NULL DEFAULT FALSE,
  whatsapp_opt_in        BOOLEAN     NOT NULL DEFAULT FALSE,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER customer_profiles_updated_at BEFORE UPDATE ON customer_profiles
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE addresses (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id    UUID NOT NULL REFERENCES customer_profiles(id) ON DELETE CASCADE,
  label          TEXT,                       -- "Home", "Office"
  recipient_name TEXT        NOT NULL,
  mobile         VARCHAR(15) NOT NULL,
  line1          TEXT        NOT NULL,
  line2          TEXT,
  landmark       TEXT,
  city           TEXT        NOT NULL,
  state          TEXT        NOT NULL,
  pin_code       VARCHAR(6)  NOT NULL,
  is_deleted     BOOLEAN     NOT NULL DEFAULT FALSE,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT addresses_pin_format    CHECK (pin_code ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT addresses_mobile_format CHECK (mobile ~ '^[6-9][0-9]{9}$')
);
CREATE INDEX addresses_customer_idx ON addresses(customer_id) WHERE is_deleted = FALSE;
CREATE TRIGGER addresses_updated_at BEFORE UPDATE ON addresses
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE customer_profiles
  ADD CONSTRAINT customer_profiles_default_address_fk
  FOREIGN KEY (default_address_id) REFERENCES addresses(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------- sessions

-- Opaque server-side sessions rather than JWTs: they can be revoked instantly
-- (logout, suspected compromise, role change) which a stateless token cannot.
-- Only the SHA-256 of the token is stored, so a database leak does not hand
-- over live sessions.
CREATE TABLE sessions (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash    TEXT        NOT NULL UNIQUE,
  user_agent    TEXT,
  ip_address    INET,
  expires_at    TIMESTAMPTZ NOT NULL,
  revoked_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx    ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at) WHERE revoked_at IS NULL;

-- One-time codes for passwordless customer login. Only the hash is stored.
CREATE TABLE otp_challenges (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  mobile       VARCHAR(15) NOT NULL,
  code_hash    TEXT        NOT NULL,
  attempts     SMALLINT    NOT NULL DEFAULT 0,
  max_attempts SMALLINT    NOT NULL DEFAULT 5,
  consumed_at  TIMESTAMPTZ,
  expires_at   TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT otp_mobile_format CHECK (mobile ~ '^[6-9][0-9]{9}$'),
  CONSTRAINT otp_attempts_bound CHECK (attempts >= 0 AND attempts <= max_attempts)
);
CREATE INDEX otp_mobile_idx ON otp_challenges(mobile, created_at DESC);

-- ---------------------------------------------------------------- audit

-- Append-only trail for sensitive operations. There is deliberately no UPDATE
-- or DELETE path in the application for this table.
CREATE TABLE audit_logs (
  id           BIGSERIAL PRIMARY KEY,
  actor_id     UUID REFERENCES users(id) ON DELETE SET NULL,
  actor_role   TEXT,
  action       TEXT NOT NULL,          -- 'order.cancel', 'prescription.approve'
  entity_type  TEXT NOT NULL,          -- 'order', 'product', 'prescription'
  entity_id    TEXT NOT NULL,
  -- Summary of what changed. Must never contain prescription images,
  -- credentials, payment secrets or full personal data — the writers redact.
  metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
  ip_address   INET,
  request_id   TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_entity_idx ON audit_logs(entity_type, entity_id, created_at DESC);
CREATE INDEX audit_actor_idx  ON audit_logs(actor_id, created_at DESC);
CREATE INDEX audit_action_idx ON audit_logs(action, created_at DESC);

-- ---------------------------------------------------------------- idempotency

-- Guards against duplicate side effects when a client retries: checkout
-- double-submits and payment webhook redeliveries both land here.
CREATE TABLE idempotency_keys (
  key           TEXT PRIMARY KEY,
  scope         TEXT        NOT NULL,   -- 'checkout', 'payment_webhook'
  user_id       UUID REFERENCES users(id) ON DELETE SET NULL,
  request_hash  TEXT,                   -- detects key reuse with a different body
  response_body JSONB,                  -- replayed verbatim on retry
  status        TEXT        NOT NULL DEFAULT 'IN_PROGRESS'
                CHECK (status IN ('IN_PROGRESS','COMPLETED','FAILED')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at  TIMESTAMPTZ
);
CREATE INDEX idempotency_created_idx ON idempotency_keys(created_at);
