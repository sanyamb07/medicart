-- Record exactly what the enrichment pipeline last wrote into products.*,
-- so a re-run can refresh its own text without ever touching text a person
-- wrote or edited (anything that no longer matches is left alone).
ALTER TABLE product_enrichment
    ADD COLUMN generated_description text,
    ADD COLUMN generated_uses        text,
    ADD COLUMN generated_storage     text,
    -- Set when a person edits any text in the admin screen; re-runs skip it.
    ADD COLUMN edited_at             timestamptz;

UPDATE product_enrichment e
   SET generated_description = p.description,
       generated_uses        = p.key_uses,
       generated_storage     = p.storage_info
  FROM products p
 WHERE p.id = e.product_id
   AND e.last_verified_at IS NULL
   AND e.short_description IS NOT NULL
   AND p.description LIKE e.short_description || '%';
