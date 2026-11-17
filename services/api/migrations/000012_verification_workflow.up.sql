-- Verification workflow and image provenance.
--
-- Product level (product_enrichment.verification_status):
--   VERIFIED            a pharmacist checked the information AND a real image.
--                       Only ever set from the admin screen, never by a script.
--   PARTIALLY_VERIFIED  some facts come from a cited authoritative source, but
--                       something (image, manufacturer, a section) is missing.
--   NEEDS_REVIEW        information or image must be checked by a person;
--                       review_reasons says why.
--
-- Image level (product_images.verification_status):
--   PENDING   a candidate with a recorded source, not yet confirmed to show
--             this exact brand, strength and form. NOT shown to customers.
--   VERIFIED  a person confirmed it. Shown to customers.
--   REJECTED  a person (or QA) found it wrong. Never shown; kept for the record
--             so the same wrong image is not attached again.
-- Placeholder illustrations are not product photos; they are shown only when
-- no VERIFIED photo exists.

ALTER TABLE product_images
  ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'PENDING'
             CHECK (verification_status IN ('PENDING','VERIFIED','REJECTED')),
  ADD COLUMN verification_notes  TEXT,
  ADD COLUMN verified_by         UUID REFERENCES users(id) ON DELETE SET NULL,
  -- Page the image was found on (source_url is the image file itself).
  ADD COLUMN source_page_url     TEXT,
  -- What the researcher could read on the pack, for the reviewer to compare.
  ADD COLUMN visible_pack_text   TEXT,
  -- Detects one file being attached to several different products.
  ADD COLUMN content_sha256      TEXT;

UPDATE product_images SET verification_status = 'VERIFIED'
 WHERE verified_at IS NOT NULL AND NOT is_placeholder;

CREATE INDEX product_images_sha_idx ON product_images(content_sha256) WHERE content_sha256 IS NOT NULL;

-- The one definition of "an image a customer may see". Every storefront
-- query reads images through this view, so a pending or rejected image can
-- never leak onto a product card by a query forgetting the filter.
CREATE VIEW customer_product_images AS
SELECT id, product_id, storage_key, alt_text,
       -- A verified photo always sorts ahead of the placeholder illustration.
       (CASE WHEN is_placeholder THEN 1000000 ELSE 0 END) + sort_order AS display_order
  FROM product_images
 WHERE verification_status = 'VERIFIED' OR is_placeholder;

ALTER TABLE product_enrichment
  ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'NEEDS_REVIEW'
             CHECK (verification_status IN ('VERIFIED','PARTIALLY_VERIFIED','NEEDS_REVIEW')),
  ADD COLUMN review_reasons          TEXT[] NOT NULL DEFAULT '{}',
  -- The supplier list row exactly as received (sr, name, generic, price), so
  -- the original is always recoverable whatever is edited later.
  ADD COLUMN source_row              JSONB,
  ADD COLUMN regulatory_group        TEXT,
  ADD COLUMN manufacturer_source_url TEXT,
  ADD COLUMN official_product_url    TEXT,
  -- True when the listed composition is suspected wrong: the pipeline then
  -- must not describe the product from that composition.
  ADD COLUMN hold_generated_text     BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE product_enrichment SET verification_status = CASE
    WHEN last_verified_at IS NOT NULL AND image_status = 'verified' THEN 'VERIFIED'
    WHEN status IN ('completed','partially_completed') THEN 'PARTIALLY_VERIFIED'
    ELSE 'NEEDS_REVIEW' END;

CREATE INDEX product_enrichment_verification_idx ON product_enrichment(verification_status);
