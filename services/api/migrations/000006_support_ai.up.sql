-- Customer support, FAQs, AI chat sessions and notifications.
--
-- Privacy stance baked into this schema: AI chat transcripts carry a retention
-- deadline (`retain_until`) and are purged by a sweep, and the assistant's
-- answers are grounded in the faqs / shipping_policies tables so it has an
-- approved source to cite rather than inventing policy.

CREATE TABLE faqs (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category    TEXT NOT NULL,       -- 'ordering','payment','delivery','prescription'
  question    TEXT NOT NULL,
  answer      TEXT NOT NULL,
  sort_order  INTEGER NOT NULL DEFAULT 100,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  -- Only FAQs marked approved are exposed to the AI assistant as grounding.
  is_ai_approved BOOLEAN NOT NULL DEFAULT FALSE,
  updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX faqs_category_idx ON faqs(category, sort_order) WHERE is_active;
CREATE INDEX faqs_ai_idx ON faqs(is_ai_approved) WHERE is_active AND is_ai_approved;
CREATE INDEX faqs_search_idx ON faqs USING GIN (question gin_trgm_ops);
CREATE TRIGGER faqs_updated_at BEFORE UPDATE ON faqs
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE support_requests (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference    TEXT NOT NULL UNIQUE,
  customer_id  UUID REFERENCES customer_profiles(id) ON DELETE SET NULL,
  order_id     UUID REFERENCES orders(id) ON DELETE SET NULL,

  category     TEXT NOT NULL CHECK (category IN (
    'ORDER_ISSUE','PAYMENT_ISSUE','DELIVERY_ISSUE','PRODUCT_QUERY',
    'PRESCRIPTION_QUERY','PHARMACIST_CONSULT','REFUND_REQUEST','OTHER')),
  subject      TEXT NOT NULL,
  message      TEXT NOT NULL,
  -- Contact details captured so a guest can raise a request too.
  contact_name   TEXT,
  contact_mobile VARCHAR(15),
  contact_email  CITEXT,

  status       TEXT NOT NULL DEFAULT 'OPEN'
               CHECK (status IN ('OPEN','IN_PROGRESS','AWAITING_CUSTOMER','RESOLVED','CLOSED')),
  priority     TEXT NOT NULL DEFAULT 'NORMAL'
               CHECK (priority IN ('LOW','NORMAL','HIGH','URGENT')),
  -- Set when the AI assistant escalated rather than a human raising it.
  escalated_from_ai BOOLEAN NOT NULL DEFAULT FALSE,
  -- Clinical questions are routed to a pharmacist and must never be answered
  -- by the assistant.
  needs_pharmacist  BOOLEAN NOT NULL DEFAULT FALSE,

  assigned_to  UUID REFERENCES users(id) ON DELETE SET NULL,
  resolution   TEXT,
  resolved_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX support_customer_idx ON support_requests(customer_id, created_at DESC);
CREATE INDEX support_status_idx   ON support_requests(status, priority, created_at);
CREATE INDEX support_assigned_idx ON support_requests(assigned_to) WHERE status <> 'CLOSED';
CREATE TRIGGER support_requests_updated_at BEFORE UPDATE ON support_requests
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------- AI chat

CREATE TABLE chat_sessions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Null for an anonymous visitor; the browser holds an opaque session token.
  customer_id  UUID REFERENCES customer_profiles(id) ON DELETE SET NULL,
  guest_token  TEXT,
  -- Which provider answered, so a mock-mode transcript is identifiable.
  provider     TEXT NOT NULL DEFAULT 'mock',
  model        TEXT,
  -- Retention deadline; a sweep deletes sessions past it along with messages.
  retain_until TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '30 days'),
  message_count INTEGER NOT NULL DEFAULT 0,
  escalated_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT chat_session_owner CHECK (customer_id IS NOT NULL OR guest_token IS NOT NULL)
);
CREATE INDEX chat_sessions_customer_idx ON chat_sessions(customer_id, created_at DESC);
CREATE INDEX chat_sessions_guest_idx    ON chat_sessions(guest_token) WHERE guest_token IS NOT NULL;
CREATE INDEX chat_sessions_retention_idx ON chat_sessions(retain_until);
CREATE TRIGGER chat_sessions_updated_at BEFORE UPDATE ON chat_sessions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE chat_messages (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id  UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
  role        TEXT NOT NULL CHECK (role IN ('user','assistant','tool','system')),
  content     TEXT NOT NULL,
  -- For role='tool': which controlled function ran and with what arguments.
  -- Recorded so every claim the assistant made can be traced to live data.
  tool_name   TEXT,
  tool_input  JSONB,
  tool_output JSONB,
  -- Set when the provider call failed, for the admin "AI failures" review.
  error       TEXT,
  -- Customer feedback on the answer: 1 helpful, -1 not helpful.
  feedback    SMALLINT CHECK (feedback IS NULL OR feedback IN (-1, 1)),
  feedback_note TEXT,
  tokens_in   INTEGER,
  tokens_out  INTEGER,
  latency_ms  INTEGER,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_messages_session_idx ON chat_messages(session_id, created_at);
CREATE INDEX chat_messages_errors_idx  ON chat_messages(created_at DESC) WHERE error IS NOT NULL;
CREATE INDEX chat_messages_feedback_idx ON chat_messages(feedback, created_at DESC)
  WHERE feedback IS NOT NULL;

-- Runtime-editable AI settings, so an admin can change the system prompt,
-- escalation keywords or disclaimers without a deploy.
CREATE TABLE ai_settings (
  key         TEXT PRIMARY KEY,
  value       TEXT NOT NULL,
  description TEXT,
  updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- notifications

-- Outbox for customer notifications. Rows are written in the same transaction
-- as the state change that caused them, then delivered by a worker — so a
-- delivery failure can be retried without losing the notification, and a
-- provider outage never rolls back an order.
CREATE TABLE notifications (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id   UUID REFERENCES customer_profiles(id) ON DELETE CASCADE,
  order_id      UUID REFERENCES orders(id) ON DELETE CASCADE,
  channel       TEXT NOT NULL CHECK (channel IN ('WHATSAPP','EMAIL','SMS','IN_APP')),
  template      TEXT NOT NULL,
  -- Template variables only — no free-form personal or medical detail.
  payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
  recipient     TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','SENT','FAILED','SKIPPED')),
  attempts      SMALLINT NOT NULL DEFAULT 0,
  last_error    TEXT,
  provider_ref  TEXT,
  sent_at       TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notifications_pending_idx ON notifications(created_at)
  WHERE status = 'PENDING';
CREATE INDEX notifications_order_idx ON notifications(order_id, created_at DESC);
CREATE TRIGGER notifications_updated_at BEFORE UPDATE ON notifications
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
