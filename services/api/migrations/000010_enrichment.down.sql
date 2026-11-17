ALTER TABLE product_images DROP COLUMN source_name, DROP COLUMN source_url, DROP COLUMN is_placeholder, DROP COLUMN verified_at;
DROP TABLE IF EXISTS product_enrichment;
DROP TABLE IF EXISTS drug_monographs;
