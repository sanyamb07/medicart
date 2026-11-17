-- Medicine information enrichment.
--
-- Two layers, deliberately separate:
--   * drug_monographs caches what an authoritative source says about one
--     ACTIVE INGREDIENT by a given route (one fetch serves every brand and
--     strength containing it).
--   * product_enrichment holds the per-product result plus its workflow
--     status, so the pipeline is resumable and an admin can see exactly
--     which products still need a human.
-- Nothing here overwrites an existing products column: the pipeline only
-- fills products.description / key_uses / storage_info / strength /
-- pack_size when they are NULL.

CREATE TABLE drug_monographs (
  ingredient_key     TEXT NOT NULL,          -- normalised name, e.g. 'AMLODIPINE'
  route_group        TEXT NOT NULL,          -- 'ORAL','TOPICAL','OPHTHALMIC','INHALATION','INJECTION','ANY'
  status             TEXT NOT NULL CHECK (status IN ('found','not_found','error')),
  matched_name       TEXT,
  pharm_class        TEXT,
  indications        TEXT,
  mechanism          TEXT,
  warnings           TEXT,
  boxed_warning      TEXT,
  adverse_reactions  TEXT,
  contraindications  TEXT,
  storage            TEXT,
  source_name        TEXT,
  source_url         TEXT,
  attempts           INTEGER NOT NULL DEFAULT 0,
  last_error         TEXT,
  fetched_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (ingredient_key, route_group)
);

CREATE TABLE product_enrichment (
  product_id           UUID PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  status               TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
                         'pending','processing','partially_completed','completed','needs_manual_review','failed')),
  therapeutic_category TEXT,
  short_description    TEXT,
  mechanism_of_action  TEXT,
  precautions          TEXT,
  side_effects         TEXT,
  warnings             TEXT,
  contraindications    TEXT,
  seo_title            TEXT,
  meta_description     TEXT,
  info_source          TEXT,
  info_source_url      TEXT,
  -- 'placeholder' = generic illustration, 'needs_review' = an image was
  -- supplied but nobody confirmed it is this exact brand/strength/form,
  -- 'verified' = a human confirmed it.
  image_status         TEXT NOT NULL DEFAULT 'placeholder'
                       CHECK (image_status IN ('placeholder','needs_review','verified')),
  missing_fields       TEXT[] NOT NULL DEFAULT '{}',
  unmatched_ingredients TEXT[] NOT NULL DEFAULT '{}',
  attempts             INTEGER NOT NULL DEFAULT 0,
  last_error           TEXT,
  -- Set only by a human in the admin screen; the pipeline never sets these.
  last_verified_at     TIMESTAMPTZ,
  verified_by          UUID REFERENCES users(id) ON DELETE SET NULL,
  processed_at         TIMESTAMPTZ,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX product_enrichment_status_idx ON product_enrichment(status);
CREATE TRIGGER product_enrichment_updated_at BEFORE UPDATE ON product_enrichment
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE product_images
  ADD COLUMN source_name    TEXT,
  ADD COLUMN source_url     TEXT,
  ADD COLUMN is_placeholder BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN verified_at    TIMESTAMPTZ;
UPDATE product_images SET is_placeholder = TRUE, source_name = 'MediCart illustration'
 WHERE storage_key LIKE '/img/%';

INSERT INTO product_enrichment (product_id) SELECT id FROM products ON CONFLICT DO NOTHING;
