-- Affiliate marketing: external promoters earn a commission on orders they
-- refer, restricted to non-prescription items only (see the package doc
-- comment on internal/affiliate for why Rx items are deliberately excluded —
-- paying a commission to induce prescription-drug sales is a different, and
-- here unreviewed, regulatory question from ordinary e-commerce affiliate
-- marketing). The program is platform-wide, not per-business: an affiliate
-- promotes OneTrueDose as a whole, not any one pharmacy on it.

INSERT INTO roles (code, description) VALUES
  ('AFFILIATE', 'External promoter who earns commission on non-prescription order referrals');

-- Singleton settings row. A flat, admin-configurable rate applied to every
-- new referral going forward — not a per-affiliate negotiated rate (keeps
-- the program simple to reason about and explain to a prospective affiliate).
-- The id CHECK enforces exactly one row.
CREATE TABLE affiliate_settings (
  id                   SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  commission_rate_bps  INTEGER NOT NULL DEFAULT 500
                       CHECK (commission_rate_bps >= 0 AND commission_rate_bps <= 5000),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by           UUID REFERENCES users(id) ON DELETE SET NULL
);
INSERT INTO affiliate_settings (id, commission_rate_bps) VALUES (1, 500); -- 5%

CREATE TABLE affiliates (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  -- Short, unguessable, used in the referral link (?ref=CODE) and never
  -- reused once issued, even if the affiliate is later rejected/suspended.
  code            TEXT NOT NULL UNIQUE,
  full_name       TEXT NOT NULL,
  email           CITEXT NOT NULL UNIQUE,
  phone           VARCHAR(15),
  -- Free-text payout instructions (UPI ID / bank details), admin-readable
  -- only, used manually when marking a payout batch paid — there is no
  -- payout API integration in this iteration, see internal/affiliate's
  -- package doc.
  payout_details  TEXT,
  status          TEXT NOT NULL DEFAULT 'PENDING'
                  CHECK (status IN ('PENDING','ACTIVE','SUSPENDED','REJECTED')),
  rejected_reason TEXT,
  approved_at     TIMESTAMPTZ,
  approved_by     UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT affiliates_code_format CHECK (code ~ '^[0-9A-Z]{6,10}$')
);
CREATE INDEX affiliates_status_idx ON affiliates(status, created_at DESC);
CREATE TRIGGER affiliates_updated_at BEFORE UPDATE ON affiliates
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One referral row per order (an order is attributed to at most one
-- affiliate — first-touch, whichever code was stored client-side at
-- checkout). eligible_subtotal_paise is the sum of that order's
-- non-prescription line totals only; commission_rate_bps is frozen at the
-- rate in effect when the order was placed, same reasoning as order_items
-- freezing unit prices — a later rate change must not rewrite history.
CREATE TABLE affiliate_referrals (
  id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  affiliate_id            UUID NOT NULL REFERENCES affiliates(id) ON DELETE CASCADE,
  order_id                UUID NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
  business_id             UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  code_used               TEXT NOT NULL,
  eligible_subtotal_paise BIGINT NOT NULL,
  commission_rate_bps     INTEGER NOT NULL,
  commission_paise        BIGINT NOT NULL,
  status                  TEXT NOT NULL DEFAULT 'PENDING'
                          CHECK (status IN ('PENDING','CONFIRMED','VOID','PAID')),
  confirmed_at            TIMESTAMPTZ,
  voided_at               TIMESTAMPTZ,
  paid_at                 TIMESTAMPTZ,
  paid_by                 UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT affiliate_referrals_amounts_non_negative
    CHECK (eligible_subtotal_paise >= 0 AND commission_paise >= 0)
);
CREATE INDEX affiliate_referrals_affiliate_idx ON affiliate_referrals(affiliate_id, status, created_at DESC);
CREATE INDEX affiliate_referrals_status_idx    ON affiliate_referrals(status, created_at DESC);
