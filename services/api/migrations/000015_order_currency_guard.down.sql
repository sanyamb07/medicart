-- Restores the 000014 version of the guard (without the currency check).
CREATE OR REPLACE FUNCTION order_items_market_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  biz UUID;
  rx  BOOLEAN;
  rx_ok BOOLEAN;
BEGIN
  SELECT o.business_id INTO biz FROM orders o WHERE o.id = NEW.order_id;
  SELECT sl.requires_prescription, m.rx_sales_enabled INTO rx, rx_ok
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
  RETURN NEW;
END $$;
