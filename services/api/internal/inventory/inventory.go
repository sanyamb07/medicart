// Package inventory manages stock batches and reservations.
//
// Allocation is FEFO (first-expiry-first-out) and runs inside the caller's
// transaction with row locks, so concurrent checkouts contending for the same
// stock cannot oversell it — the database's own CHECK constraints
// (qty_on_hand >= 0, qty_reserved <= qty_on_hand) are the final backstop even
// if the Go logic here had a bug.
package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/db"
)

var ErrInsufficientStock = errors.New("inventory: insufficient stock")

// Allocation records which batch covered how much of a line.
type Allocation struct {
	BatchID string
	Qty     int
}

// ReserveFEFO locks sellable batches for productID within businessID,
// oldest-expiry-first, and reserves qty units across them, returning the
// allocations made. Must be called with a Querier that is inside a
// transaction — the row lock (SELECT ... FOR UPDATE) is only meaningful
// there. Scoping by businessID means two businesses selling the same master
// product draw from entirely separate stock — one can never sell out the
// other's inventory.
func ReserveFEFO(ctx context.Context, q db.Querier, businessID, productID string, qty int) ([]Allocation, error) {
	if qty <= 0 {
		return nil, fmt.Errorf("inventory: qty must be positive")
	}

	rows, err := q.Query(ctx, `
		SELECT id::text, qty_on_hand, qty_reserved
		  FROM inventory_batches
		 WHERE business_id = $1 AND product_id = $2 AND is_blocked = FALSE AND expiry_date > CURRENT_DATE
		   AND qty_on_hand > qty_reserved
		 ORDER BY expiry_date ASC
		   FOR UPDATE`, businessID, productID)
	if err != nil {
		return nil, fmt.Errorf("lock batches: %w", err)
	}

	type batch struct {
		id        string
		available int
	}
	var batches []batch
	for rows.Next() {
		var id string
		var onHand, reserved int
		if err := rows.Scan(&id, &onHand, &reserved); err != nil {
			rows.Close()
			return nil, err
		}
		batches = append(batches, batch{id: id, available: onHand - reserved})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	remaining := qty
	var allocations []Allocation
	for _, b := range batches {
		if remaining <= 0 {
			break
		}
		take := b.available
		if take > remaining {
			take = remaining
		}
		if take <= 0 {
			continue
		}
		if _, err := q.Exec(ctx, `
			UPDATE inventory_batches SET qty_reserved = qty_reserved + $2 WHERE id = $1`,
			b.id, take); err != nil {
			return nil, fmt.Errorf("reserve batch: %w", err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO inventory_movements (batch_id, product_id, movement_type, qty_delta, qty_reserved_delta, reason)
			VALUES ($1, $2, 'RESERVE', 0, $3, 'order checkout')`,
			b.id, productID, take); err != nil {
			return nil, fmt.Errorf("record reservation movement: %w", err)
		}
		allocations = append(allocations, Allocation{BatchID: b.id, Qty: take})
		remaining -= take
	}

	if remaining > 0 {
		return nil, ErrInsufficientStock
	}
	return allocations, nil
}

// ReleaseAllocations reverses a set of reservations (order cancelled/expired),
// returning the stock to sellable without touching qty_on_hand.
func ReleaseAllocations(ctx context.Context, q db.Querier, productID string, allocations []Allocation, reason string) error {
	for _, a := range allocations {
		if _, err := q.Exec(ctx, `
			UPDATE inventory_batches SET qty_reserved = qty_reserved - $2 WHERE id = $1`,
			a.BatchID, a.Qty); err != nil {
			return fmt.Errorf("release batch: %w", err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO inventory_movements (batch_id, product_id, movement_type, qty_delta, qty_reserved_delta, reason)
			VALUES ($1, $2, 'RELEASE', 0, $3, $4)`,
			a.BatchID, productID, -a.Qty, reason); err != nil {
			return fmt.Errorf("record release movement: %w", err)
		}
	}
	return nil
}

// ConsumeOnDispatch converts a reservation into an actual stock decrement
// (qty_on_hand and qty_reserved both drop). Called when an order moves to
// SHIPPED. Not yet wired into the order flow for this iteration — the mock
// payment flow stops at PAID; dispatch is a manual admin status transition
// that does not currently call this. Documented as pending in the plan.
func ConsumeOnDispatch(ctx context.Context, q db.Querier, productID string, allocations []Allocation, orderID string) error {
	for _, a := range allocations {
		if _, err := q.Exec(ctx, `
			UPDATE inventory_batches
			   SET qty_on_hand = qty_on_hand - $2, qty_reserved = qty_reserved - $2
			 WHERE id = $1`, a.BatchID, a.Qty); err != nil {
			return fmt.Errorf("consume batch: %w", err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO inventory_movements
				(batch_id, product_id, movement_type, qty_delta, qty_reserved_delta, reason, order_id)
			VALUES ($1, $2, 'DISPATCH', $3, $3, 'order dispatched', $4)`,
			a.BatchID, productID, -a.Qty, orderID); err != nil {
			return fmt.Errorf("record dispatch movement: %w", err)
		}
	}
	return nil
}

// AvailableQty sums sellable stock (on_hand - reserved) across non-blocked,
// non-expired batches for a product within one business. Used by cart
// validation before checkout.
func AvailableQty(ctx context.Context, q db.Querier, businessID, productID string) (int, error) {
	var qty int
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(qty_on_hand - qty_reserved), 0)
		  FROM inventory_batches
		 WHERE business_id = $1 AND product_id = $2 AND is_blocked = FALSE AND expiry_date > CURRENT_DATE`,
		businessID, productID).Scan(&qty)
	if err != nil {
		return 0, fmt.Errorf("available qty: %w", err)
	}
	return qty, nil
}
