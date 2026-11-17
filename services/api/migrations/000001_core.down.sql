DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS otp_challenges;
DROP TABLE IF EXISTS sessions;
ALTER TABLE IF EXISTS customer_profiles DROP CONSTRAINT IF EXISTS customer_profiles_default_address_fk;
DROP TABLE IF EXISTS addresses;
DROP TABLE IF EXISTS customer_profiles;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS set_updated_at();
-- Extensions are left in place: other databases on the cluster may use them.
