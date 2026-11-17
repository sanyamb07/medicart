-- 000015 Fail-closed currency guard. orders.currency still defaults to INR
-- (000004), so an order for a business in a non-INR market would record
-- foreign minor units labelled INR. No such market can be LIVE today; this
-- makes that mistake impossible rather than relying on memory. Checkout must
-- write the market currency before any non-INR market opens (Step E).
CREATE OR REPLACE FUNCTION order_items_market_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  biz UUID;
  order_currency CHAR(3);
  rx  BOOLEAN;
  rx_ok BOOLEAN;
  listing_currency CHAR(3);
BEGIN
  SELECT o.business_id, o.currency INTO biz, order_currency FROM orders o WHERE o.id = NEW.order_id;
  SELECT sl.requires_prescription, m.rx_sales_enabled, sl.currency INTO rx, rx_ok, listing_currency
    FROM storefront_listings sl JOIN markets m ON m.code = sl.market_code
   WHERE sl.business_id = biz AND sl.product_id = NEW.product_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'product % is not sellable in this market', NEW.product_id
      USING ERRCODE = 'MK001';
  END IF;
  IF (rx OR NEW.requires_prescription) AND NOT rx_ok THEN
    RAISE EXCEPTION 'prescription sales are not enabled in this market'
      USING ERRCODE = 'MK002';
  END IF;
  IF order_currency <> listing_currency THEN
    RAISE EXCEPTION 'order currency % does not match market currency %', order_currency, listing_currency
      USING ERRCODE = 'MK003';
  END IF;
  RETURN NEW;
END $$;
