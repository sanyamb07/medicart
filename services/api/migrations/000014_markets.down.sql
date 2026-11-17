-- Reverts 000014. Drops only objects 000014 created; no pre-existing column
-- or row is touched. Regional listing data is lost: export
-- product_market_listings first if it holds anything entered after 000014.
DROP TRIGGER IF EXISTS order_items_market_guard ON order_items;
DROP FUNCTION IF EXISTS order_items_market_guard();
DROP TRIGGER IF EXISTS cart_items_market_guard ON cart_items;
DROP FUNCTION IF EXISTS cart_items_market_guard();
DROP FUNCTION IF EXISTS product_sellable(UUID, UUID);
DROP VIEW IF EXISTS storefront_listings;
DROP TRIGGER IF EXISTS products_auto_list_markets ON products;
DROP FUNCTION IF EXISTS products_auto_list_markets();
DROP TABLE IF EXISTS product_market_listings;
DROP INDEX IF EXISTS businesses_market_idx;
ALTER TABLE businesses DROP COLUMN IF EXISTS market_code;
DROP TABLE IF EXISTS markets;
DROP TABLE IF EXISTS currencies;
