package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// Batch is the admin-facing view of one received lot of a product.
type Batch struct {
	ID            string    `json:"id"`
	ProductID     string    `json:"productId"`
	BatchNumber   string    `json:"batchNumber"`
	ExpiryDate    time.Time `json:"expiryDate"`
	QtyReceived   int       `json:"qtyReceived"`
	QtyOnHand     int       `json:"qtyOnHand"`
	QtyReserved   int       `json:"qtyReserved"`
	QtyAvailable  int       `json:"qtyAvailable"`
	IsBlocked     bool      `json:"isBlocked"`
	BlockedReason *string   `json:"blockedReason"`
	IsExpired     bool      `json:"isExpired"`
	ReceivedAt    time.Time `json:"receivedAt"`
}

// ListBatchesForProduct returns every batch for a product within one
// business, soonest-expiry first — the order an operator needs to see to
// spot what's about to lapse. A business never sees another business's
// batches of the same master product.
func ListBatchesForProduct(ctx context.Context, pool *db.Pool, businessID, productID string) ([]Batch, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, product_id::text, batch_number, expiry_date,
		       qty_received, qty_on_hand, qty_reserved, is_blocked, blocked_reason, received_at,
		       expiry_date <= CURRENT_DATE AS is_expired
		  FROM inventory_batches
		 WHERE business_id = $1 AND product_id = $2
		 ORDER BY expiry_date ASC`, businessID, productID)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer rows.Close()

	out := []Batch{}
	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.ID, &b.ProductID, &b.BatchNumber, &b.ExpiryDate,
			&b.QtyReceived, &b.QtyOnHand, &b.QtyReserved, &b.IsBlocked, &b.BlockedReason, &b.ReceivedAt,
			&b.IsExpired); err != nil {
			return nil, fmt.Errorf("scan batch: %w", err)
		}
		b.QtyAvailable = b.QtyOnHand - b.QtyReserved
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListExpiringSoon returns one business's non-blocked batches expiring
// within `days` days (including already-expired ones), across all its
// products — the admin "what needs attention" view.
func ListExpiringSoon(ctx context.Context, pool *db.Pool, businessID string, days int) ([]Batch, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, product_id::text, batch_number, expiry_date,
		       qty_received, qty_on_hand, qty_reserved, is_blocked, blocked_reason, received_at,
		       expiry_date <= CURRENT_DATE AS is_expired
		  FROM inventory_batches
		 WHERE business_id = $1 AND is_blocked = FALSE AND qty_on_hand > 0
		   AND expiry_date <= CURRENT_DATE + make_interval(days => $2)
		 ORDER BY expiry_date ASC`, businessID, days)
	if err != nil {
		return nil, fmt.Errorf("list expiring batches: %w", err)
	}
	defer rows.Close()

	out := []Batch{}
	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.ID, &b.ProductID, &b.BatchNumber, &b.ExpiryDate,
			&b.QtyReceived, &b.QtyOnHand, &b.QtyReserved, &b.IsBlocked, &b.BlockedReason, &b.ReceivedAt,
			&b.IsExpired); err != nil {
			return nil, fmt.Errorf("scan batch: %w", err)
		}
		b.QtyAvailable = b.QtyOnHand - b.QtyReserved
		out = append(out, b)
	}
	return out, rows.Err()
}

// ReceiveInput is a new stock delivery being recorded.
type ReceiveInput struct {
	BusinessID  string
	ProductID   string
	BatchNumber string
	ExpiryDate  time.Time
	Qty         int
	CostPaise   *int64
	Supplier    *string
	InvoiceRef  *string
}

// Receive records a new batch of stock for one business. Expiry is required
// and never inferred — see the schema comment on inventory_batches.
func Receive(ctx context.Context, pool *db.Pool, in ReceiveInput) (*Batch, error) {
	if in.Qty <= 0 {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"qty": "Quantity received must be greater than zero."})
	}
	if in.BatchNumber == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"batchNumber": "A batch/lot number is required."})
	}
	if !in.ExpiryDate.After(time.Now()) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"expiryDate": "Expiry date must be in the future."})
	}

	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO inventory_batches (business_id, product_id, batch_number, expiry_date, qty_received, qty_on_hand, cost_paise, supplier_name, invoice_ref)
		VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8)
		RETURNING id::text`,
		in.BusinessID, in.ProductID, in.BatchNumber, in.ExpiryDate, in.Qty, in.CostPaise, in.Supplier, in.InvoiceRef,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("receive batch: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inventory_movements (batch_id, product_id, movement_type, qty_delta, qty_reserved_delta, reason)
		VALUES ($1, $2, 'RECEIPT', $3, 0, 'stock received')`, id, in.ProductID, in.Qty); err != nil {
		return nil, fmt.Errorf("record receive movement: %w", err)
	}

	batches, err := ListBatchesForProduct(ctx, pool, in.BusinessID, in.ProductID)
	if err != nil {
		return nil, err
	}
	for _, b := range batches {
		if b.ID == id {
			return &b, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// SetBlocked quarantines or unquarantines a batch (recall, QA hold) without
// touching its quantities — ReserveFEFO already skips any blocked batch.
// Scoped to businessID so a business can never block/unblock another
// business's batch, even by guessing its id.
func SetBlocked(ctx context.Context, pool *db.Pool, businessID, batchID string, blocked bool, reason *string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE inventory_batches SET is_blocked = $3, blocked_reason = $4 WHERE id = $1 AND business_id = $2`,
		batchID, businessID, blocked, reason)
	if err != nil {
		return fmt.Errorf("set batch blocked: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}
