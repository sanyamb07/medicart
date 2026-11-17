DROP TABLE IF EXISTS enquiries;
DROP TABLE IF EXISTS business_customers;

ALTER TABLE chat_sessions DROP COLUMN IF EXISTS business_id;

ALTER TABLE prescriptions DROP COLUMN IF EXISTS business_id;

DROP INDEX IF EXISTS carts_guest_business_idx;
DROP INDEX IF EXISTS carts_customer_business_idx;
ALTER TABLE carts ADD CONSTRAINT carts_guest_token_key UNIQUE (guest_token);
CREATE UNIQUE INDEX carts_customer_idx ON carts(customer_id) WHERE customer_id IS NOT NULL;
ALTER TABLE carts DROP COLUMN IF EXISTS business_id;

ALTER TABLE orders DROP COLUMN IF EXISTS business_id;

ALTER TABLE inventory_batches DROP CONSTRAINT IF EXISTS inventory_batches_business_product_batch_key;
ALTER TABLE inventory_batches ADD CONSTRAINT inventory_batches_product_id_batch_number_key UNIQUE (product_id, batch_number);
ALTER TABLE inventory_batches DROP COLUMN IF EXISTS business_id;

DROP TABLE IF EXISTS business_products;
DROP TABLE IF EXISTS business_staff;
DROP TABLE IF EXISTS platform_admins;
DROP TABLE IF EXISTS businesses;

DELETE FROM roles WHERE code = 'PLATFORM_SUPER_ADMIN';
