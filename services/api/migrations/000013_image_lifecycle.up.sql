-- Image lifecycle, QA evidence and the marketer/manufacturer distinction.
--
-- product_images: verification_status stays the PHARMACIST decision
-- (PENDING / VERIFIED / REJECTED). qa_status is the automated + QA-agent
-- result that comes before it. A row is shown to customers only when the
-- pharmacist set VERIFIED (see customer_product_images); QA can never do it.

ALTER TABLE product_images
  ADD COLUMN width                INTEGER,
  ADD COLUMN height               INTEGER,
  ADD COLUMN format               TEXT,
  -- Where the file came from: manufacturer_catalogue, manufacturer_page,
  -- manufacturer_pdf, staff_upload, illustration.
  ADD COLUMN source_type          TEXT,
  ADD COLUMN qa_status            TEXT CHECK (qa_status IN ('QA_PASSED','QA_FAILED','MANUAL_VERIFICATION_REQUIRED')),
  ADD COLUMN qa_notes             TEXT,
  -- 64-bit difference hash (hex) of the source image: near-duplicates across
  -- different products are caught even when the files differ byte-wise.
  ADD COLUMN phash                TEXT,
  -- The untouched downloaded file, kept next to the optimised WebP.
  ADD COLUMN original_storage_key TEXT,
  -- Why a pharmacist did not approve (shown in admin, used for re-research).
  ADD COLUMN review_action        TEXT CHECK (review_action IN
             ('REJECTED','REQUEST_BETTER_IMAGE','WRONG_MANUFACTURER','WRONG_MEDICINE','MANUAL_VERIFICATION')),
  ADD COLUMN reviewed_at          TIMESTAMPTZ;

UPDATE product_images SET source_type = 'illustration' WHERE is_placeholder;
UPDATE product_images SET qa_status = 'QA_PASSED', source_type = COALESCE(source_type, 'manufacturer_catalogue')
 WHERE NOT is_placeholder AND verification_status = 'PENDING' AND content_sha256 IS NOT NULL;

ALTER TABLE product_enrichment
  -- Company that markets the brand (named on its own catalogue / website).
  ADD COLUMN marketer_name           TEXT,
  ADD COLUMN marketer_source_url     TEXT,
  -- Company that physically manufactures it, only when the pack or an
  -- official document names it. Never inferred from the marketer.
  ADD COLUMN manufacturer_name       TEXT,
  ADD COLUMN manufacturer_evidence   TEXT,
  ADD COLUMN image_research_status   TEXT NOT NULL DEFAULT 'RESEARCH_PENDING' CHECK (image_research_status IN
             ('NOT_FOUND','RESEARCH_PENDING','IMAGE_FOUND','QA_PENDING','QA_PASSED','PHARMACIST_REVIEW_PENDING',
              'APPROVED','REJECTED','MANUAL_VERIFICATION_REQUIRED')),
  ADD COLUMN image_research_note     TEXT,
  -- Catalogue hygiene (demo/test rows, duplicates) separate from medicine review.
  ADD COLUMN catalogue_cleanup_reason TEXT;

-- Everything the pipeline recorded so far came from a company's own
-- catalogue or product page: that establishes the MARKETER.
UPDATE product_enrichment e
   SET marketer_name = regexp_replace(p.manufacturer_info, '\s*\(marketer\)\s*$', ''),
       marketer_source_url = e.manufacturer_source_url
  FROM products p
 WHERE p.id = e.product_id AND p.manufacturer_info IS NOT NULL AND e.edited_at IS NULL;

UPDATE product_enrichment e SET image_research_status = CASE
    WHEN EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = e.product_id AND NOT i.is_placeholder AND i.verification_status = 'VERIFIED') THEN 'APPROVED'
    WHEN EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = e.product_id AND NOT i.is_placeholder AND i.verification_status = 'PENDING') THEN 'PHARMACIST_REVIEW_PENDING'
    ELSE 'RESEARCH_PENDING' END;

CREATE INDEX product_enrichment_image_research_idx ON product_enrichment(image_research_status);
CREATE INDEX product_images_phash_idx ON product_images(phash) WHERE phash IS NOT NULL;
