-- Payments: attempts, provider events, refunds.
--
-- Core rules this schema enforces:
--   * MediCart never stores card numbers, CVVs or UPI PINs. Only provider
--     references and a coarse method label are kept.
--   * An order is marked PAID only from a verified provider signal, recorded
--     here first. A browser redirect is never sufficient.
--   * Webhook deliveries are idempotent: provider_event_id is unique, so a
--     redelivery collides instead of double-crediting.

CREATE TABLE payment_attempts (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id      UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,

  provider      TEXT NOT NULL,   -- 'mock' | 'razorpay'
  -- True when this attempt was created by a simulator. Surfaced in the UI and
  -- in reports so a test payment can never be mistaken for real money.
  is_test_mode  BOOLEAN NOT NULL DEFAULT FALSE,

  -- Amount is copied from the order inside the same transaction that creates
  -- the attempt, so a client cannot influence what is charged.
  amount_paise  BIGINT NOT NULL,
  currency      CHAR(3) NOT NULL DEFAULT 'INR',

  status        TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN (
    'CREATED',    -- we asked the provider for an order/link
    'PENDING',    -- customer is paying
    'SUCCEEDED',
    'FAILED',
    'CANCELLED',
    'EXPIRED'
  )),

  -- Provider references. provider_order_id is the gateway's order handle;
  -- provider_payment_id appears once a payment is actually attempted.
  provider_order_id   TEXT,
  provider_payment_id TEXT,
  -- Hosted checkout / payment link the customer is sent to.
  payment_url   TEXT,
  -- Coarse method only: 'upi', 'card', 'netbanking'. Never instrument details.
  method        TEXT,
  failure_code     TEXT,
  failure_reason   TEXT,

  expires_at    TIMESTAMPTZ,
  succeeded_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT payment_amount_positive CHECK (amount_paise > 0),
  CONSTRAINT payment_currency_inr CHECK (currency = 'INR'),
  CONSTRAINT payment_succeeded_has_reference
    CHECK (status <> 'SUCCEEDED' OR provider_payment_id IS NOT NULL)
);
CREATE INDEX payment_attempts_order_idx ON payment_attempts(order_id, created_at DESC);
CREATE INDEX payment_attempts_status_idx ON payment_attempts(status, created_at DESC);
-- A provider payment id must map to exactly one attempt.
CREATE UNIQUE INDEX payment_attempts_provider_payment_idx
  ON payment_attempts(provider, provider_payment_id)
  WHERE provider_payment_id IS NOT NULL;
CREATE UNIQUE INDEX payment_attempts_provider_order_idx
  ON payment_attempts(provider, provider_order_id)
  WHERE provider_order_id IS NOT NULL;
CREATE TRIGGER payment_attempts_updated_at BEFORE UPDATE ON payment_attempts
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Raw, append-only record of every provider signal we accepted. This is the
-- audit trail for reconciliation and the mechanism for webhook idempotency.
CREATE TABLE payment_events (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_attempt_id UUID REFERENCES payment_attempts(id) ON DELETE SET NULL,
  provider           TEXT NOT NULL,
  -- The provider's own event id. UNIQUE across provider: a redelivered
  -- webhook violates this and is recognised as a duplicate rather than
  -- processed twice.
  provider_event_id  TEXT NOT NULL,
  event_type         TEXT NOT NULL,     -- 'payment.captured', 'payment.failed'
  -- Whether the HMAC signature verified. Unverified payloads are rejected
  -- before reaching here; the column exists so a failure is still recorded.
  signature_verified BOOLEAN NOT NULL DEFAULT FALSE,
  -- Provider payload with secrets stripped by the adapter before insert.
  payload            JSONB NOT NULL DEFAULT '{}'::jsonb,
  processed_at       TIMESTAMPTZ,
  processing_error   TEXT,
  received_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (provider, provider_event_id)
);
CREATE INDEX payment_events_attempt_idx ON payment_events(payment_attempt_id, received_at DESC);
CREATE INDEX payment_events_type_idx    ON payment_events(event_type, received_at DESC);
CREATE INDEX payment_events_unprocessed_idx ON payment_events(received_at)
  WHERE processed_at IS NULL;

CREATE TABLE refunds (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id           UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
  payment_attempt_id UUID NOT NULL REFERENCES payment_attempts(id) ON DELETE RESTRICT,

  amount_paise       BIGINT NOT NULL,
  currency           CHAR(3) NOT NULL DEFAULT 'INR',
  reason             TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'PENDING'
                     CHECK (status IN ('PENDING','PROCESSING','COMPLETED','FAILED')),
  provider_refund_id TEXT,
  -- Who authorised it. Refunds are never automatic.
  initiated_by       UUID REFERENCES users(id) ON DELETE SET NULL,
  completed_at       TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT refund_amount_positive CHECK (amount_paise > 0),
  CONSTRAINT refund_currency_inr CHECK (currency = 'INR')
);
CREATE INDEX refunds_order_idx ON refunds(order_id, created_at DESC);
CREATE INDEX refunds_status_idx ON refunds(status);
CREATE UNIQUE INDEX refunds_provider_idx ON refunds(provider_refund_id)
  WHERE provider_refund_id IS NOT NULL;
CREATE TRIGGER refunds_updated_at BEFORE UPDATE ON refunds
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
