package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// businessProductColumns mirrors productColumns but reads price, visibility
// and featured status from business_products — a business's own storefront
// never prices or lists a product from the master row directly. The master
// MRP stays authoritative (a manufacturer ceiling, not something a seller
// changes).
const businessProductColumns = `
	p.id::text, p.sku, p.slug, p.name,
	b.name, b.slug, b.manufacturer,
	p.generic_name, p.strength, p.dosage_form, p.pack_size,
	p.description, p.key_uses, p.storage_info, p.manufacturer_info,
	p.mrp_paise, COALESCE(bp.price_paise, p.price_paise), p.requires_prescription,
	p.max_qty_per_order,
	p.is_demo_data, bp.is_featured, (p.is_active AND bp.is_visible),
	COALESCE((
		SELECT SUM(ib.qty_on_hand - ib.qty_reserved)
		  FROM inventory_batches ib
		 WHERE ib.product_id = p.id AND ib.business_id = bp.business_id
		   AND ib.is_blocked = FALSE AND ib.expiry_date > CURRENT_DATE
	), 0) AS available_qty,
	(SELECT pi.storage_key FROM customer_product_images pi
	  WHERE pi.product_id = p.id ORDER BY pi.display_order LIMIT 1) AS image_key
`

// ListForBusiness is the storefront listing, scoped to one business's own
// catalog selection and pricing. A product this business never listed, or
// has unlisted, never appears here regardless of the master product's own
// is_active flag.
func (r *Repo) ListForBusiness(ctx context.Context, businessID string, f ListFilter) ([]*Product, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}

	var (
		where   = []string{"bp.business_id = $1", "bp.is_listed = TRUE", "p.is_active = TRUE", "bp.is_visible = TRUE"}
		args    = []any{businessID}
		argN    = 1
		addArg  = func(v any) string { argN++; args = append(args, v); return fmt.Sprintf("$%d", argN) }
		rankSel = "0 AS rank"
		orderBy = hasPhotoSQL + " DESC, bp.is_featured DESC, p.created_at DESC"
	)
	if f.WithPhoto {
		where = append(where, hasPhotoSQL)
	}

	q := strings.TrimSpace(f.Query)
	if q != "" {
		ph := addArg(q)
		where = append(where, fmt.Sprintf(
			"(p.search_vector @@ websearch_to_tsquery('simple', %s) OR p.name %% %s OR p.generic_name %% %s)",
			ph, ph, ph))
		rankSel = fmt.Sprintf(
			`(ts_rank(p.search_vector, websearch_to_tsquery('simple', %s)) +
			  GREATEST(similarity(p.name, %s), similarity(coalesce(p.generic_name,''), %s))) AS rank`,
			ph, ph, ph)
		if f.Sort == "" || f.Sort == "relevance" {
			orderBy = "rank DESC, " + hasPhotoSQL + " DESC, bp.is_featured DESC"
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
	if f.RxOnly != nil {
		where = append(where, fmt.Sprintf("p.requires_prescription = %s", addArg(*f.RxOnly)))
	}
	priceExpr := "COALESCE(bp.price_paise, p.price_paise)"
	if f.MinPaise > 0 {
		where = append(where, fmt.Sprintf("%s >= %s", priceExpr, addArg(f.MinPaise)))
	}
	if f.MaxPaise > 0 {
		where = append(where, fmt.Sprintf("%s <= %s", priceExpr, addArg(f.MaxPaise)))
	}

	switch f.Sort {
	case "price_asc":
		orderBy = priceExpr + " ASC"
	case "price_desc":
		orderBy = priceExpr + " DESC"
	case "name_asc":
		orderBy = "p.name ASC"
	case "newest":
		orderBy = "p.created_at DESC"
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := fmt.Sprintf(`
		SELECT count(*) FROM business_products bp
		JOIN products p ON p.id = bp.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE %s`, whereSQL)
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count business products: %w", err)
	}

	limitPH := addArg(f.PageSize)
	offsetPH := addArg((f.Page - 1) * f.PageSize)
	listSQL := fmt.Sprintf(`
		SELECT %s, %s
		  FROM business_products bp
		  JOIN products p ON p.id = bp.product_id
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE %s
		 ORDER BY %s
		 LIMIT %s OFFSET %s`, businessProductColumns, rankSel, whereSQL, orderBy, limitPH, offsetPH)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list business products: %w", err)
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

// GetBySlugForBusiness loads one product as this business sells it — 404s
// when the business never listed it, even if the master product exists and
// is active (another business's catalog is invisible from here).
func (r *Repo) GetBySlugForBusiness(ctx context.Context, businessID, slug string) (*Product, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		  FROM business_products bp
		  JOIN products p ON p.id = bp.product_id
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE p.slug = $2 AND bp.business_id = $1
		   AND bp.is_listed = TRUE AND bp.is_visible = TRUE AND p.is_active = TRUE`, businessProductColumns)
	p, err := scanProduct(r.pool.QueryRow(ctx, sql, businessID, slug))
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

// GetByIDForBusiness is GetBySlugForBusiness's id-keyed twin, used by
// cart/order pricing so a line is always re-priced from this business's own
// business_products row, never the master product directly.
func (r *Repo) GetByIDForBusiness(ctx context.Context, businessID, productID string) (*Product, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		  FROM business_products bp
		  JOIN products p ON p.id = bp.product_id
		  LEFT JOIN brands b ON b.id = p.brand_id
		 WHERE p.id = $2 AND bp.business_id = $1
		   AND bp.is_listed = TRUE AND bp.is_visible = TRUE AND p.is_active = TRUE`, businessProductColumns)
	return scanProduct(r.pool.QueryRow(ctx, sql, businessID, productID))
}

// ---------------------------------------------------------------- business catalog management

// CatalogRow is one master product plus this business's selection state —
// the shape the "pick what to sell" admin screen renders, in one query
// rather than the admin fetching the master catalog and the selection
// separately.
type CatalogRow struct {
	ProductID   string  `json:"productId"`
	SKU         string  `json:"sku"`
	Name        string  `json:"name"`
	BrandName   *string `json:"brandName"`
	MRPPaise    int64   `json:"mrpPaise"`
	MasterPrice int64   `json:"masterPricePaise"`
	IsListed    bool    `json:"isListed"`
	PricePaise  *int64  `json:"pricePaise"` // null = uses master price
	IsVisible   bool    `json:"isVisible"`
	IsFeatured  bool    `json:"isFeatured"`
	MinOrderQty int     `json:"minOrderQty"`
	RequiresRx  bool    `json:"requiresPrescription"`
}

// ListCatalogForBusiness returns every active master product with this
// business's selection state (unselected products show IsListed = false and
// null pricing fields), so the admin screen can toggle any master product on
// without a separate "browse master catalog" step.
func (r *Repo) ListCatalogForBusiness(ctx context.Context, businessID string, query string) ([]*CatalogRow, error) {
	where := "p.is_active = TRUE"
	args := []any{businessID}
	if q := strings.TrimSpace(query); q != "" {
		where += " AND (p.name ILIKE $2 OR p.sku ILIKE $2)"
		args = append(args, "%"+q+"%")
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT p.id::text, p.sku, p.name, b.name, p.mrp_paise, p.price_paise, p.requires_prescription,
		       COALESCE(bp.is_listed, FALSE), bp.price_paise, COALESCE(bp.is_visible, TRUE),
		       COALESCE(bp.is_featured, FALSE), COALESCE(bp.min_order_qty, 1)
		  FROM products p
		  LEFT JOIN brands b ON b.id = p.brand_id
		  LEFT JOIN business_products bp ON bp.product_id = p.id AND bp.business_id = $1
		 WHERE %s
		 ORDER BY bp.is_listed DESC NULLS LAST, p.name`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list catalog for business: %w", err)
	}
	defer rows.Close()

	out := []*CatalogRow{}
	for rows.Next() {
		var row CatalogRow
		if err := rows.Scan(&row.ProductID, &row.SKU, &row.Name, &row.BrandName, &row.MRPPaise, &row.MasterPrice,
			&row.RequiresRx, &row.IsListed, &row.PricePaise, &row.IsVisible, &row.IsFeatured, &row.MinOrderQty); err != nil {
			return nil, err
		}
		out = append(out, &row)
	}
	return out, rows.Err()
}

// ListingInput is a business's decision about one master product.
type ListingInput struct {
	IsListed    bool
	PricePaise  *int64 // nil = use the master product's price
	IsVisible   bool
	IsFeatured  bool
	MinOrderQty int
}

// SetListing creates or updates a business's business_products row for a
// master product — the only way a business's catalog or pricing changes,
// and it never touches the master product row.
func (r *Repo) SetListing(ctx context.Context, businessID, productID string, in ListingInput) error {
	if in.PricePaise != nil && *in.PricePaise <= 0 {
		return httpx.ErrValidation.WithFields(map[string]string{"pricePaise": "Price must be greater than zero."})
	}
	if in.MinOrderQty < 1 {
		in.MinOrderQty = 1
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return fmt.Errorf("check product exists: %w", err)
	}
	if !exists {
		return httpx.ErrNotFound.WithMessage("Unknown product.")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO business_products (business_id, product_id, is_listed, price_paise, is_visible, is_featured, min_order_qty)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (business_id, product_id) DO UPDATE SET
			is_listed = EXCLUDED.is_listed, price_paise = EXCLUDED.price_paise,
			is_visible = EXCLUDED.is_visible, is_featured = EXCLUDED.is_featured,
			min_order_qty = EXCLUDED.min_order_qty`,
		businessID, productID, in.IsListed, in.PricePaise, in.IsVisible, in.IsFeatured, in.MinOrderQty)
	if err != nil {
		return fmt.Errorf("set business listing: %w", err)
	}
	return nil
}
