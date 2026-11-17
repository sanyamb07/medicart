// Package catalog serves products, categories and brands, including search.
//
// Search strategy: Postgres full-text search (weighted tsvector, already
// defined on products.search_vector) ranked by ts_rank, with a pg_trgm
// similarity fallback so a misspelled query ("paracetmol") still finds
// "Paracetamol" instead of returning nothing. No external search service —
// documented in docs/ARCHITECTURE.md as unnecessary at this catalogue size.
package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/platform/money"
)

// Product is the catalogue listing/detail shape returned to clients.
type Product struct {
	ID                   string        `json:"id"`
	SKU                  string        `json:"sku"`
	Slug                 string        `json:"slug"`
	Name                 string        `json:"name"`
	BrandName            *string       `json:"brandName"`
	BrandSlug            *string       `json:"brandSlug"`
	Manufacturer         *string       `json:"manufacturer"`
	GenericName          *string       `json:"genericName"`
	Strength             *string       `json:"strength"`
	DosageForm           *string       `json:"dosageForm"`
	PackSize             *string       `json:"packSize"`
	Description          *string       `json:"description"`
	KeyUses              *string       `json:"keyUses"`
	StorageInfo          *string       `json:"storageInfo"`
	ManufacturerInfo     *string       `json:"manufacturerInfo"`
	MRPPaise             int64         `json:"mrpPaise"`
	PricePaise           int64         `json:"pricePaise"`
	DiscountPercent      int           `json:"discountPercent"`
	RequiresPrescription bool          `json:"requiresPrescription"`
	MaxQtyPerOrder       int           `json:"maxQtyPerOrder"`
	IsDemoData           bool          `json:"isDemoData"`
	IsFeatured           bool          `json:"isFeatured"`
	IsActive             bool          `json:"isActive"`
	InStock              bool          `json:"inStock"`
	AvailableQty         int           `json:"availableQty"`
	ImageURL             *string       `json:"imageUrl"`
	Images               []string      `json:"images,omitempty"`
	Ingredients          []Ingredient  `json:"ingredients,omitempty"`
	Categories           []CategoryRef `json:"categories,omitempty"`
}

type Ingredient struct {
	Name     string  `json:"name"`
	Strength *string `json:"strength"`
}

type CategoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Category struct {
	ID           string  `json:"id"`
	ParentID     *string `json:"parentId"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Icon         *string `json:"icon"`
	SortOrder    int     `json:"sortOrder"`
	ProductCount int     `json:"productCount"`
}

type Brand struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ListFilter narrows a catalogue listing. All fields are optional.
type ListFilter struct {
	Query        string
	CategorySlug string
	BrandSlug    string
	MinPaise     int64
	MaxPaise     int64
	RxOnly       *bool  // nil = either, true = prescription only, false = OTC only
	Sort         string // "relevance" | "price_asc" | "price_desc" | "name_asc" | "newest"
	Page         int
	PageSize     int
	// IncludeInactive is for the admin product list only — the public
	// storefront must never see a deactivated product.
	IncludeInactive bool
	// WithPhoto keeps only products that have a pharmacist-approved genuine
	// pack photo (not the illustration).
	WithPhoto bool
}

// hasPhotoSQL is true when a product has an approved genuine photo that
// customers may see. Listings sort these first so a shelf leads with real
// packs; nothing is hidden by the ordering.
const hasPhotoSQL = `EXISTS (SELECT 1 FROM customer_product_images cpi WHERE cpi.product_id = p.id AND cpi.display_order < 1000000)`

// Repo provides read access to the catalogue.
type Repo struct{ pool *db.Pool }

func NewRepo(pool *db.Pool) *Repo { return &Repo{pool: pool} }

// scanColumns is the shared SELECT list for a product row, used by both List
// and GetBySlug so the two can never drift out of sync in field order.
const productColumns = `
	p.id::text, p.sku, p.slug, p.name,
	b.name, b.slug, b.manufacturer,
	p.generic_name, p.strength, p.dosage_form, p.pack_size,
	p.description, p.key_uses, p.storage_info, p.manufacturer_info,
	p.mrp_paise, p.price_paise, p.requires_prescription, p.max_qty_per_order,
	p.is_demo_data, p.is_featured, p.is_active,
	COALESCE((
		SELECT SUM(ib.qty_on_hand - ib.qty_reserved)
		  FROM inventory_batches ib
		 WHERE ib.product_id = p.id AND ib.is_blocked = FALSE
		   AND ib.expiry_date > CURRENT_DATE
	), 0) AS available_qty,
	(SELECT pi.storage_key FROM customer_product_images pi
	  WHERE pi.product_id = p.id ORDER BY pi.display_order LIMIT 1) AS image_key
`

func scanProduct(row rowScanner) (*Product, error) {
	var p Product
	var availQty int64
	var imageKey *string
	if err := row.Scan(
		&p.ID, &p.SKU, &p.Slug, &p.Name,
		&p.BrandName, &p.BrandSlug, &p.Manufacturer,
		&p.GenericName, &p.Strength, &p.DosageForm, &p.PackSize,
		&p.Description, &p.KeyUses, &p.StorageInfo, &p.ManufacturerInfo,
		&p.MRPPaise, &p.PricePaise, &p.RequiresPrescription, &p.MaxQtyPerOrder,
		&p.IsDemoData, &p.IsFeatured, &p.IsActive,
		&availQty, &imageKey,
	); err != nil {
		return nil, err
	}
	p.DiscountPercent = money.DiscountPercent(money.Paise(p.MRPPaise), money.Paise(p.PricePaise))
	p.AvailableQty = int(availQty)
	p.InStock = availQty > 0
	p.ImageURL = imageKey
	return &p, nil
}

// rowScanner is satisfied by pgx.Row.
type rowScanner interface {
	Scan(dest ...any) error
}

// List returns a filtered, sorted, paginated page of active products.
func (r *Repo) List(ctx context.Context, f ListFilter) ([]*Product, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}

	var (
		where   = []string{"TRUE"}
		args    []any
		argN    = 0
		addArg  = func(v any) string { argN++; args = append(args, v); return fmt.Sprintf("$%d", argN) }
		rankSel = "0 AS rank"
		orderBy = hasPhotoSQL + " DESC, p.is_featured DESC, p.created_at DESC"
	)
	if f.WithPhoto {
		where = append(where, hasPhotoSQL)
	}

	if !f.IncludeInactive {
		where = append(where, "p.is_active = TRUE")
	}

	q := strings.TrimSpace(f.Query)
	if q != "" {
		ph := addArg(q)
		// websearch_to_tsquery handles natural phrases and quoted terms;
		// trigram similarity on name/generic_name catches misspellings that
		// full-text search would miss entirely (different tokens).
		where = append(where, fmt.Sprintf(
			"(p.search_vector @@ websearch_to_tsquery('simple', %s) OR p.name %% %s OR p.generic_name %% %s)",
			ph, ph, ph))
		rankSel = fmt.Sprintf(
			`(ts_rank(p.search_vector, websearch_to_tsquery('simple', %s)) +
			  GREATEST(similarity(p.name, %s), similarity(coalesce(p.generic_name,''), %s))) AS rank`,
			ph, ph, ph)
		if f.Sort == "" || f.Sort == "relevance" {
			orderBy = "rank DESC, " + hasPhotoSQL + " DESC, p.is_featured DESC"
		}
	}
	if f.CategorySlug != "" {
		ph := addArg(f.CategorySlug)
		where = append(where, fmt.Sprintf(
			`p.id IN (SELECT pc.product_id FROM product_categories pc
			           JOIN categories c ON c.id = pc.category_id WHERE c.slug = %s)`, ph))
	}
	if f.BrandSlug != "" {
		ph := addArg(f.BrandSlug)
		where = append(where, fmt.Sprintf("b.slug = %s", ph))
	}
	if f.MinPaise > 0 {
		where = append(where, fmt.Sprintf("p.price_paise >= %s", addArg(f.MinPaise)))
	}
	if f.MaxPaise > 0 {
		where = append(where, fmt.Sprintf("p.price_paise <= %s", addArg(f.MaxPaise)))
	}
	if f.RxOnly != nil {
		where = append(where, fmt.Sprintf("p.requires_prescription = %s", addArg(*f.RxOnly)))
	}

	switch f.Sort {
	case "price_asc":
		orderBy = "p.price_paise ASC"
	case "price_desc":
		orderBy = "p.price_paise DESC"
	case "name_asc":
		orderBy = "p.name ASC"
	case "newest":
		orderBy = "p.created_at DESC"
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := fmt.Sprintf(`
		SELECT count(*) FROM products p
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE %s`, whereSQL)
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	limitPH := addArg(f.PageSize)
	offsetPH := addArg((f.Page - 1) * f.PageSize)
	listSQL := fmt.Sprintf(`
		SELECT %s, %s
		  FROM products p
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE %s
		 ORDER BY %s
		 LIMIT %s OFFSET %s`, productColumns, rankSel, whereSQL, orderBy, limitPH, offsetPH)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	out := make([]*Product, 0, f.PageSize)
	for rows.Next() {
		var rank float64
		p, err := scanProductWithRank(rows, &rank)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// scanProductWithRank scans the product columns plus a trailing rank column
// that List always selects (even when it is the constant 0).
func scanProductWithRank(row rowScanner, rank *float64) (*Product, error) {
	var p Product
	var availQty int64
	var imageKey *string
	if err := row.Scan(
		&p.ID, &p.SKU, &p.Slug, &p.Name,
		&p.BrandName, &p.BrandSlug, &p.Manufacturer,
		&p.GenericName, &p.Strength, &p.DosageForm, &p.PackSize,
		&p.Description, &p.KeyUses, &p.StorageInfo, &p.ManufacturerInfo,
		&p.MRPPaise, &p.PricePaise, &p.RequiresPrescription, &p.MaxQtyPerOrder,
		&p.IsDemoData, &p.IsFeatured, &p.IsActive,
		&availQty, &imageKey, rank,
	); err != nil {
		return nil, err
	}
	p.DiscountPercent = money.DiscountPercent(money.Paise(p.MRPPaise), money.Paise(p.PricePaise))
	p.AvailableQty = int(availQty)
	p.InStock = availQty > 0
	p.ImageURL = imageKey
	return &p, nil
}

// GetBySlug loads one active product with its images, ingredients and categories.
func (r *Repo) GetBySlug(ctx context.Context, slug string) (*Product, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		  FROM products p
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE p.slug = $1 AND p.is_active = TRUE`, productColumns)
	p, err := scanProduct(r.pool.QueryRow(ctx, sql, slug))
	if err != nil {
		return nil, err
	}

	if err := r.attachImages(ctx, p); err != nil {
		return nil, err
	}
	if err := r.attachIngredients(ctx, p); err != nil {
		return nil, err
	}
	if err := r.attachCategories(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// GetByID loads a product by id — used internally by cart/orders pricing,
// where only the fields needed for pricing and stock are populated.
func (r *Repo) GetByID(ctx context.Context, id string) (*Product, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		  FROM products p
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE p.id = $1 AND p.is_active = TRUE`, productColumns)
	return scanProduct(r.pool.QueryRow(ctx, sql, id))
}

// GetByIDAdmin fetches a product regardless of its active flag, so the admin
// edit form can load a deactivated product.
func (r *Repo) GetByIDAdmin(ctx context.Context, id string) (*Product, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		  FROM products p
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE p.id = $1`, productColumns)
	return scanProduct(r.pool.QueryRow(ctx, sql, id))
}

func (r *Repo) attachImages(ctx context.Context, p *Product) error {
	rows, err := r.pool.Query(ctx,
		`SELECT storage_key FROM customer_product_images WHERE product_id = $1 ORDER BY display_order`, p.ID)
	if err != nil {
		return fmt.Errorf("load images: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		p.Images = append(p.Images, key)
	}
	return rows.Err()
}

func (r *Repo) attachIngredients(ctx context.Context, p *Product) error {
	rows, err := r.pool.Query(ctx,
		`SELECT ingredient, strength FROM product_ingredients WHERE product_id = $1 ORDER BY sort_order`, p.ID)
	if err != nil {
		return fmt.Errorf("load ingredients: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ing Ingredient
		if err := rows.Scan(&ing.Name, &ing.Strength); err != nil {
			return err
		}
		p.Ingredients = append(p.Ingredients, ing)
	}
	return rows.Err()
}

func (r *Repo) attachCategories(ctx context.Context, p *Product) error {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, c.name, c.slug
		  FROM product_categories pc JOIN categories c ON c.id = pc.category_id
		 WHERE pc.product_id = $1 ORDER BY pc.is_primary DESC, c.sort_order`, p.ID)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cr CategoryRef
		if err := rows.Scan(&cr.ID, &cr.Name, &cr.Slug); err != nil {
			return err
		}
		p.Categories = append(p.Categories, cr)
	}
	return rows.Err()
}

// ListCategories returns active top-level and child categories with a live
// product count, for the homepage "shop by category" section and filters.
func (r *Repo) ListCategories(ctx context.Context) ([]*Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, c.parent_id::text, c.name, c.slug, c.icon, c.sort_order,
		       (SELECT count(*) FROM product_categories pc
		          JOIN products p ON p.id = pc.product_id
		         WHERE pc.category_id = c.id AND p.is_active) AS product_count
		  FROM categories c
		 WHERE c.is_active = TRUE
		 ORDER BY c.sort_order, c.name`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	out := []*Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.Slug, &c.Icon, &c.SortOrder, &c.ProductCount); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ListBrands returns active brands.
func (r *Repo) ListBrands(ctx context.Context) ([]*Brand, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, slug FROM brands WHERE is_active = TRUE ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list brands: %w", err)
	}
	defer rows.Close()

	out := []*Brand{}
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.Name, &b.Slug); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}
