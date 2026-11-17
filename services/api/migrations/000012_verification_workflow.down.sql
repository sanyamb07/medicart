DROP INDEX IF EXISTS product_enrichment_verification_idx;
ALTER TABLE product_enrichment
  DROP COLUMN IF EXISTS hold_generated_text,
  DROP COLUMN IF EXISTS official_product_url,
  DROP COLUMN IF EXISTS manufacturer_source_url,
  DROP COLUMN IF EXISTS regulatory_group,
  DROP COLUMN IF EXISTS source_row,
  DROP COLUMN IF EXISTS review_reasons,
  DROP COLUMN IF EXISTS verification_status;

DROP VIEW IF EXISTS customer_product_images;
DROP INDEX IF EXISTS product_images_sha_idx;
ALTER TABLE product_images
  DROP COLUMN IF EXISTS content_sha256,
  DROP COLUMN IF EXISTS visible_pack_text,
  DROP COLUMN IF EXISTS source_page_url,
  DROP COLUMN IF EXISTS verified_by,
  DROP COLUMN IF EXISTS verification_notes,
  DROP COLUMN IF EXISTS verification_status;
