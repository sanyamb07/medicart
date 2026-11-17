-- Minimal, privacy-conscious product analytics.
--
-- Deliberately narrow: an event name, a small flat JSON of non-sensitive
-- properties (product ids/slugs, prices, counts — never names typed by a
-- user, never prescription or medical content), and a per-browser anonymous
-- id. No cross-site tracking, no third-party script, no PII column to leak.
CREATE TABLE analytics_events (
  id           BIGSERIAL PRIMARY KEY,
  event_name   TEXT NOT NULL,
  -- Anonymous per-browser id (a random token kept in localStorage), NOT a
  -- customer id — this table answers "how do people use the site", not "what
  -- did this named person do", so it must not double as a customer profile.
  anon_id      TEXT,
  properties   JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX analytics_events_name_idx ON analytics_events(event_name, created_at DESC);
-- Events are useful in aggregate only; nothing here is read back per-user, so
-- there is deliberately no index on anon_id.
