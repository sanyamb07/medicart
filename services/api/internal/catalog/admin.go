package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// AdminProductInput is the writable subset of a product — pricing,
// identity, and the fields a pharmacy operator actually maintains. Fields
// that need provenance (search_vector, timestamps, availability) are never
// settable directly.
type AdminProductInput struct {
	SKU                  string
	Slug                 string
	Name                 string
	BrandID              *string
	GenericName          *string
	Strength             *string
	DosageForm           *string
	PackSize             *string
	Description          *string
	MRPPaise             int64
	PricePaise           int64
	RequiresPrescription bool
	MaxQtyPerOrder       int
	IsActive             bool
	IsFeatured           bool
}

func (in AdminProductInput) validate() error {
	fields := map[string]string{}
	if strings.TrimSpace(in.SKU) == "" {
		fields["sku"] = "SKU is required."
	}
	if strings.TrimSpace(in.Slug) == "" {
		fields["slug"] = "Slug is required."
	}
	if strings.TrimSpace(in.Name) == "" {
		fields["name"] = "Name is required."
	}
	if in.MRPPaise <= 0 {
		fields["mrpPaise"] = "MRP must be greater than zero."
	}
	if in.PricePaise <= 0 {
		fields["pricePaise"] = "Price must be greater than zero."
	}
	if in.PricePaise > in.MRPPaise {
		fields["pricePaise"] = "Price cannot exceed MRP."
	}
	if in.MaxQtyPerOrder <= 0 {
		fields["maxQtyPerOrder"] = "Max quantity per order must be at least 1."
	}
	if len(fields) > 0 {
		return httpx.ErrValidation.WithFields(fields)
	}
	return nil
}

// AdminCreate inserts a new product. New products are never demo data by
// default — an admin creating one through this form means it's real
// catalogue content, not seeded fixtures.
func (r *Repo) AdminCreate(ctx context.Context, in AdminProductInput) (*Product, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO products (sku, slug, name, brand_id, generic_name, strength, dosage_form, pack_size,
		                       description, mrp_paise, price_paise, requires_prescription, max_qty_per_order,
		                       is_active, is_featured, is_demo_data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15, FALSE)
		RETURNING id::text`,
		in.SKU, in.Slug, in.Name, in.BrandID, in.GenericName, in.Strength, in.DosageForm, in.PackSize,
		in.Description, in.MRPPaise, in.PricePaise, in.RequiresPrescription, in.MaxQtyPerOrder,
		in.IsActive, in.IsFeatured,
	).Scan(&id)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, httpx.ErrValidation.WithMessage("A product with that SKU or slug already exists.")
		}
		return nil, fmt.Errorf("create product: %w", err)
	}
	return r.GetByIDAdmin(ctx, id)
}

// AdminUpdate replaces a product's writable fields.
func (r *Repo) AdminUpdate(ctx context.Context, id string, in AdminProductInput) (*Product, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE products SET
			sku = $2, slug = $3, name = $4, brand_id = $5, generic_name = $6, strength = $7,
			dosage_form = $8, pack_size = $9, description = $10, mrp_paise = $11, price_paise = $12,
			requires_prescription = $13, max_qty_per_order = $14, is_active = $15, is_featured = $16
		WHERE id = $1`,
		id, in.SKU, in.Slug, in.Name, in.BrandID, in.GenericName, in.Strength,
		in.DosageForm, in.PackSize, in.Description, in.MRPPaise, in.PricePaise,
		in.RequiresPrescription, in.MaxQtyPerOrder, in.IsActive, in.IsFeatured,
	)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, httpx.ErrValidation.WithMessage("A product with that SKU or slug already exists.")
		}
		return nil, fmt.Errorf("update product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	return r.GetByIDAdmin(ctx, id)
}

// AdminSetActive activates or deactivates a product without deleting it —
// stock history, past orders, and the catalogue row all stay intact.
func (r *Repo) AdminSetActive(ctx context.Context, id string, active bool) error {
	tag, err := r.pool.Exec(ctx, `UPDATE products SET is_active = $2 WHERE id = $1`, id, active)
	if err != nil {
		return fmt.Errorf("set product active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}
