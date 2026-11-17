DROP INDEX IF EXISTS product_images_phash_idx;
DROP INDEX IF EXISTS product_enrichment_image_research_idx;
ALTER TABLE product_enrichment
  DROP COLUMN IF EXISTS catalogue_cleanup_reason,
  DROP COLUMN IF EXISTS image_research_note,
  DROP COLUMN IF EXISTS image_research_status,
  DROP COLUMN IF EXISTS manufacturer_evidence,
  DROP COLUMN IF EXISTS manufacturer_name,
  DROP COLUMN IF EXISTS marketer_source_url,
  DROP COLUMN IF EXISTS marketer_name;
ALTER TABLE product_images
  DROP COLUMN IF EXISTS reviewed_at,
  DROP COLUMN IF EXISTS review_action,
  DROP COLUMN IF EXISTS original_storage_key,
  DROP COLUMN IF EXISTS phash,
  DROP COLUMN IF EXISTS qa_notes,
  DROP COLUMN IF EXISTS qa_status,
  DROP COLUMN IF EXISTS source_type,
  DROP COLUMN IF EXISTS format,
  DROP COLUMN IF EXISTS height,
  DROP COLUMN IF EXISTS width;
