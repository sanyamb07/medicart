-- Catalogue: brands, categories, products, images, identifiers, ingredients.
--
-- Search design: a generated tsvector column (weighted name > brand/generic >
-- description) handles word search, and pg_trgm GIN indexes on name and
-- generic name handle misspellings. No external search cluster — Postgres
-- covers this catalogue size comfortably and adds no operational cost.

CREATE TABLE brands (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,
  -- Manufacturer/marketer, shown on the product page for traceability.
  manufacturer  TEXT,
  logo_url      TEXT,
  is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT brands_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$')
);
CREATE TRIGGER brands_updated_at BEFORE UPDATE ON brands
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE categories (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  parent_id    UUID REFERENCES categories(id) ON DELETE SET NULL,
  name         TEXT NOT NULL,
  slug         TEXT NOT NULL UNIQUE,
  description  TEXT,
  -- Local asset path (e.g. '/img/categories/pain-relief.svg'), never a remote
  -- URL that could disappear.
  icon         TEXT,
  sort_order   INTEGER     NOT NULL DEFAULT 100,
  is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT categories_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT categories_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX categories_parent_idx ON categories(parent_id, sort_order);
CREATE TRIGGER categories_updated_at BEFORE UPDATE ON categories
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Dosage forms kept as an enum-like table so admins can extend it without a
-- migration, while products still reference a controlled vocabulary.
CREATE TABLE dosage_forms (
  code       TEXT PRIMARY KEY,           -- 'TABLET', 'SYRUP'
  label      TEXT NOT NULL,              -- 'Tablet', 'Syrup'
  sort_order INTEGER NOT NULL DEFAULT 100
);
INSERT INTO dosage_forms (code, label, sort_order) VALUES
  ('TABLET','Tablet',10), ('CAPSULE','Capsule',20), ('SYRUP','Syrup',30),
  ('SUSPENSION','Suspension',40), ('INJECTION','Injection',50),
  ('CREAM','Cream',60), ('OINTMENT','Ointment',70), ('GEL','Gel',80),
  ('DROPS','Drops',90), ('INHALER','Inhaler',100), ('SPRAY','Spray',110),
  ('POWDER','Powder',120), ('SACHET','Sachet',130), ('SOLUTION','Solution',140),
  ('LOTION','Lotion',150), ('DEVICE','Device',160), ('OTHER','Other',900);

CREATE TABLE products (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sku               TEXT NOT NULL UNIQUE,
  slug              TEXT NOT NULL UNIQUE,
  name              TEXT NOT NULL,
  brand_id          UUID REFERENCES brands(id) ON DELETE SET NULL,

  -- Clinical descriptors. NULL means "not verified for this product" and the
  -- UI omits the field entirely rather than guessing — we must never invent a
  -- composition, a strength, or a storage instruction.
  generic_name      TEXT,
  strength          TEXT,                 -- '500 mg', '5 mg/ml'
  dosage_form       TEXT REFERENCES dosage_forms(code) ON DELETE SET NULL,
  pack_size         TEXT,                 -- 'Strip of 15 tablets'
  pack_units        INTEGER,              -- 15, for per-unit price display
  description       TEXT,
  key_uses          TEXT,
  storage_info      TEXT,
  manufacturer_info TEXT,
  country_of_origin TEXT,

  -- Commerce. Money is paise; price_paise is what the customer pays and is the
  -- only figure the server ever uses for totals.
  mrp_paise         BIGINT NOT NULL,
  price_paise       BIGINT NOT NULL,
  currency          CHAR(3) NOT NULL DEFAULT 'INR',
  tax_rate_bps      INTEGER NOT NULL DEFAULT 0,  -- basis points, e.g. 500 = 5%

  -- Regulatory / fulfilment
  requires_prescription BOOLEAN NOT NULL DEFAULT FALSE,
  is_controlled_substance BOOLEAN NOT NULL DEFAULT FALSE,
  max_qty_per_order INTEGER NOT NULL DEFAULT 10,

  -- Every seeded product is demonstration data. The storefront labels anything
  -- flagged here so a demo catalogue is never mistaken for real stock.
  is_demo_data      BOOLEAN NOT NULL DEFAULT FALSE,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  is_featured       BOOLEAN NOT NULL DEFAULT FALSE,

  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT products_slug_format   CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT products_mrp_positive  CHECK (mrp_paise > 0),
  CONSTRAINT products_price_positive CHECK (price_paise > 0),
  -- Selling above MRP is not permitted; this makes it impossible to save.
  CONSTRAINT products_price_lte_mrp CHECK (price_paise <= mrp_paise),
  CONSTRAINT products_currency_inr  CHECK (currency = 'INR'),
  CONSTRAINT products_tax_bounds    CHECK (tax_rate_bps >= 0 AND tax_rate_bps <= 10000),
  CONSTRAINT products_qty_positive  CHECK (max_qty_per_order >= 1),
  CONSTRAINT products_pack_units    CHECK (pack_units IS NULL OR pack_units > 0),
  -- A controlled substance is prescription-only by definition.
  CONSTRAINT products_controlled_requires_rx
    CHECK (NOT is_controlled_substance OR requires_prescription)
);

CREATE TRIGGER products_updated_at BEFORE UPDATE ON products
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Weighted search vector, maintained by Postgres so it can never drift from
-- the row. 'simple' rather than 'english': medicine names are proper nouns and
-- stemming them ("Dolo" -> "Dolo") adds nothing while risking bad matches.
ALTER TABLE products ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('simple', coalesce(name, '')),         'A') ||
    setweight(to_tsvector('simple', coalesce(generic_name, '')),  'B') ||
    setweight(to_tsvector('simple', coalesce(strength, '')),      'C') ||
    setweight(to_tsvector('simple', coalesce(pack_size, '')),     'D') ||
    setweight(to_tsvector('simple', coalesce(key_uses, '')),      'D')
  ) STORED;

CREATE INDEX products_search_idx       ON products USING GIN (search_vector);
CREATE INDEX products_name_trgm_idx    ON products USING GIN (name gin_trgm_ops);
CREATE INDEX products_generic_trgm_idx ON products USING GIN (generic_name gin_trgm_ops);
CREATE INDEX products_active_idx       ON products(is_active) WHERE is_active = TRUE;
CREATE INDEX products_brand_idx        ON products(brand_id) WHERE is_active = TRUE;
CREATE INDEX products_featured_idx     ON products(is_featured) WHERE is_active AND is_featured;
CREATE INDEX products_price_idx        ON products(price_paise) WHERE is_active = TRUE;
CREATE INDEX products_rx_idx           ON products(requires_prescription) WHERE is_active = TRUE;

-- Many-to-many: a product can sit under several categories.
CREATE TABLE product_categories (
  product_id  UUID NOT NULL REFERENCES products(id)   ON DELETE CASCADE,
  category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  is_primary  BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (product_id, category_id)
);
CREATE INDEX product_categories_category_idx ON product_categories(category_id);
-- At most one primary category per product.
CREATE UNIQUE INDEX product_categories_one_primary_idx
  ON product_categories(product_id) WHERE is_primary;

CREATE TABLE product_images (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  -- Storage key, resolved through the storage driver. Local assets shipped
  -- with the app use a '/img/...' path; uploads use an object-storage key.
  storage_key TEXT NOT NULL,
  alt_text    TEXT NOT NULL,
  sort_order  INTEGER NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX product_images_product_idx ON product_images(product_id, sort_order);

-- Barcodes / regulatory identifiers. Kept in its own table because a product
-- can carry several (EAN, HSN, manufacturer code).
CREATE TABLE product_identifiers (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  id_type     TEXT NOT NULL CHECK (id_type IN ('EAN','UPC','HSN','GTIN','MFR_CODE','OTHER')),
  id_value    TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (id_type, id_value)
);
CREATE INDEX product_identifiers_product_idx ON product_identifiers(product_id);

-- Active ingredients. Only populated where the composition is verified; an
-- empty set renders as "composition not listed", never as a guess.
CREATE TABLE product_ingredients (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id    UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  ingredient    TEXT NOT NULL,
  strength      TEXT,
  sort_order    INTEGER NOT NULL DEFAULT 0,
  -- Set only when a pharmacist has confirmed the entry against the label.
  verified_at   TIMESTAMPTZ,
  verified_by   UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (product_id, ingredient)
);
CREATE INDEX product_ingredients_product_idx   ON product_ingredients(product_id, sort_order);
CREATE INDEX product_ingredients_name_trgm_idx ON product_ingredients USING GIN (ingredient gin_trgm_ops);
