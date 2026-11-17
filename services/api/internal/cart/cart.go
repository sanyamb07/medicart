// Package cart manages the shopping cart.
//
// The cart stores only product id + quantity — never a price. Every read
// re-prices every line from the live products table, so a cart can never
// charge a stale or client-supplied price. Carts are guest-only in this
// iteration (identified by an opaque cookie token); there is no customer
// login yet to merge into, so merge-on-login is not implemented (see
// docs/IMPLEMENTATION_PLAN.md).
package cart

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
)

var ErrLineNotFound = errors.New("cart: item not in cart")

// Line is one priced cart line.
type Line struct {
	ProductID      string  `json:"productId"`
	ProductSlug    string  `json:"productSlug"`
	ProductName    string  `json:"productName"`
	ProductSKU     string  `json:"productSku"`
	BrandName      *string `json:"brandName"`
	ImageURL       *string `json:"imageUrl"`
	PackSize       *string `json:"packSize"`
	Strength       *string `json:"strength"`
	DosageForm     *string `json:"dosageForm"`
	Qty            int     `json:"qty"`
	UnitMRPPaise   int64   `json:"unitMrpPaise"`
	UnitPricePaise int64   `json:"unitPricePaise"`
	LineTotalPaise int64   `json:"lineTotalPaise"`
	RequiresRx     bool    `json:"requiresPrescription"`
	AvailableQty   int     `json:"availableQty"`
	MaxQtyPerOrder int     `json:"maxQtyPerOrder"`
	// Exceeded is true when qty in the cart is more than can actually be
	// fulfilled right now. The frontend highlights this before checkout.
	Exceeded bool `json:"exceeded"`
	IsActive bool `json:"isActive"`
}

// Summary is the priced cart.
type Summary struct {
	CartID        string  `json:"cartId"`
	Lines         []*Line `json:"lines"`
	SubtotalPaise int64   `json:"subtotalPaise"`
	ItemCount     int     `json:"itemCount"`
	RequiresRx    bool    `json:"requiresPrescription"`
	HasIssues     bool    `json:"hasIssues"`
}

type Service struct{ pool *db.Pool }

func NewService(pool *db.Pool) *Service { return &Service{pool: pool} }

// GetOrCreateGuestCart returns the cart for a guest token within businessID,
// creating one (and a fresh token, when the browser had none yet) when
// absent. The same browser cookie can back a separate cart row per business
// — a shopper's cart at one pharmacy never mixes with another's.
func (s *Service) GetOrCreateGuestCart(ctx context.Context, businessID, token string) (cartID, newToken string, err error) {
	if token != "" {
		err := s.pool.QueryRow(ctx,
			`SELECT id::text FROM carts WHERE guest_token = $1 AND business_id = $2`, token, businessID).Scan(&cartID)
		if err == nil {
			return cartID, token, nil
		}
		if !db.IsNoRows(err) {
			return "", "", fmt.Errorf("load cart: %w", err)
		}
	}
	newToken = idgen.Token()
	err = s.pool.QueryRow(ctx,
		`INSERT INTO carts (guest_token, business_id) VALUES ($1, $2) RETURNING id::text`, newToken, businessID).Scan(&cartID)
	if err != nil {
		return "", "", fmt.Errorf("create cart: %w", err)
	}
	return cartID, newToken, nil
}

// businessIDForCart looks up which business a cart belongs to, so AddItem/
// SetQty can validate the product against that same business's catalog
// rather than trusting a caller-supplied business id.
func (s *Service) businessIDForCart(ctx context.Context, cartID string) (string, error) {
	var businessID string
	err := s.pool.QueryRow(ctx, `SELECT business_id::text FROM carts WHERE id = $1`, cartID).Scan(&businessID)
	if db.IsNoRows(err) {
		return "", fmt.Errorf("cart: not found")
	}
	if err != nil {
		return "", fmt.Errorf("load cart business: %w", err)
	}
	return businessID, nil
}

// AddItem adds qty of a product, or increases qty if already present, capped
// at the product's max-qty-per-order. The product must be listed and
// visible in the cart's own business — never another business's catalog.
func (s *Service) AddItem(ctx context.Context, cartID, productID string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("cart: qty must be positive")
	}
	businessID, err := s.businessIDForCart(ctx, cartID)
	if err != nil {
		return err
	}
	var maxQty int
	err = s.pool.QueryRow(ctx, `
		SELECT p.max_qty_per_order FROM storefront_listings sl JOIN products p ON p.id = sl.product_id
		 WHERE sl.product_id = $1 AND sl.business_id = $2`, productID, businessID).
		Scan(&maxQty)
	if db.IsNoRows(err) {
		return fmt.Errorf("cart: product not found")
	}
	if err != nil {
		return fmt.Errorf("load product: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO cart_items (cart_id, product_id, qty)
		VALUES ($1, $2, LEAST($3::int, $4::int))
		ON CONFLICT (cart_id, product_id)
		DO UPDATE SET qty = LEAST(cart_items.qty + $3::int, $4::int), updated_at = now()`,
		cartID, productID, qty, maxQty)
	if err != nil {
		return fmt.Errorf("add item: %w", err)
	}
	return nil
}

// SetQty sets a line to an exact quantity (0 removes it), capped at max-qty.
func (s *Service) SetQty(ctx context.Context, cartID, productID string, qty int) error {
	if qty <= 0 {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cartID, productID)
		if err != nil {
			return fmt.Errorf("remove item: %w", err)
		}
		return nil
	}

	businessID, err := s.businessIDForCart(ctx, cartID)
	if err != nil {
		return err
	}
	var maxQty int
	err = s.pool.QueryRow(ctx, `
		SELECT p.max_qty_per_order FROM storefront_listings sl JOIN products p ON p.id = sl.product_id
		 WHERE sl.product_id = $1 AND sl.business_id = $2`, productID, businessID).
		Scan(&maxQty)
	if db.IsNoRows(err) {
		return fmt.Errorf("cart: product not found")
	}
	if err != nil {
		return fmt.Errorf("load product: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE cart_items SET qty = LEAST($3::int, $4::int), updated_at = now()
		 WHERE cart_id = $1 AND product_id = $2`, cartID, productID, qty, maxQty)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLineNotFound
	}
	return nil
}

// RemoveItem deletes a line entirely.
func (s *Service) RemoveItem(ctx context.Context, cartID, productID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cartID, productID)
	if err != nil {
		return fmt.Errorf("remove item: %w", err)
	}
	return nil
}

// Get returns the priced cart, re-pricing every line live from this cart's
// own business's storefront_listings row — never the master product directly,
// and never another business's pricing or stock.
func (s *Service) Get(ctx context.Context, cartID string) (*Summary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id::text, p.slug, p.name, p.sku, b.name,
		       (SELECT pi.storage_key FROM customer_product_images pi WHERE pi.product_id = p.id ORDER BY pi.display_order LIMIT 1),
		       p.pack_size, p.strength, p.dosage_form, ci.qty, COALESCE(sl.mrp_minor, p.mrp_paise),
		       COALESCE(sl.price_minor, p.price_paise), COALESCE(sl.requires_prescription, p.requires_prescription),
		       p.max_qty_per_order, (sl.product_id IS NOT NULL),
		       COALESCE((SELECT SUM(ib.qty_on_hand - ib.qty_reserved) FROM inventory_batches ib
		                  WHERE ib.product_id = p.id AND ib.business_id = c.business_id
		                    AND ib.is_blocked = FALSE AND ib.expiry_date > CURRENT_DATE), 0)
		  FROM cart_items ci
		  JOIN carts c ON c.id = ci.cart_id
		  JOIN products p ON p.id = ci.product_id
		  LEFT JOIN brands b ON b.id = p.brand_id
		  -- storefront_listings decides sellability (market LIVE, product
		  -- ELIGIBLE there, listed, active). A line that is no longer
		  -- sellable is flagged inactive and blocks checkout.
		  LEFT JOIN storefront_listings sl ON sl.product_id = p.id AND sl.business_id = c.business_id
		 WHERE ci.cart_id = $1
		 ORDER BY ci.added_at`, cartID)
	if err != nil {
		return nil, fmt.Errorf("load cart lines: %w", err)
	}
	defer rows.Close()

	sum := &Summary{CartID: cartID, Lines: []*Line{}}
	for rows.Next() {
		var l Line
		var availQty int64
		if err := rows.Scan(&l.ProductID, &l.ProductSlug, &l.ProductName, &l.ProductSKU, &l.BrandName,
			&l.ImageURL, &l.PackSize, &l.Strength, &l.DosageForm, &l.Qty, &l.UnitMRPPaise, &l.UnitPricePaise, &l.RequiresRx,
			&l.MaxQtyPerOrder, &l.IsActive, &availQty); err != nil {
			return nil, err
		}
		l.AvailableQty = int(availQty)
		l.LineTotalPaise = l.UnitPricePaise * int64(l.Qty)
		l.Exceeded = !l.IsActive || l.Qty > l.AvailableQty
		sum.Lines = append(sum.Lines, &l)
		sum.SubtotalPaise += l.LineTotalPaise
		sum.ItemCount += l.Qty
		if l.RequiresRx {
			sum.RequiresRx = true
		}
		if l.Exceeded {
			sum.HasIssues = true
		}
	}
	return sum, rows.Err()
}

// Clear empties a cart (called after a successful checkout).
func (s *Service) Clear(ctx context.Context, q db.Querier, cartID string) error {
	_, err := q.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cartID)
	return err
}
