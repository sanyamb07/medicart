package orders

import (
	"context"
	"fmt"
	"time"

	"github.com/sanyam/medicart/services/api/internal/audit"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/inventory"
)

// ListFilter narrows an admin order listing.
type ListFilter struct {
	BusinessID string
	Status     string
	Search     string // matches reference or contact mobile
	Page       int
	PageSize   int
}

// ListItem is the row shape for the admin order table.
type ListItem struct {
	ID            string    `json:"id"`
	Reference     string    `json:"reference"`
	Status        string    `json:"status"`
	RecipientName string    `json:"recipientName"`
	ContactMobile string    `json:"contactMobile"`
	TotalPaise    int64     `json:"totalPaise"`
	ItemCount     int       `json:"itemCount"`
	PlacedAt      time.Time `json:"placedAt"`
}

// List returns a page of orders for the admin dashboard.
func (s *Service) List(ctx context.Context, f ListFilter) ([]*ListItem, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 25
	}
	where := "o.business_id = $1"
	args := []any{f.BusinessID}
	n := 1
	if f.Status != "" {
		n++
		where += fmt.Sprintf(" AND o.status = $%d", n)
		args = append(args, f.Status)
	}
	if f.Search != "" {
		n++
		where += fmt.Sprintf(" AND (o.reference ILIKE $%d OR o.contact_mobile = $%d)", n, n)
		args = append(args, "%"+f.Search+"%")
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM orders o WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.reference, o.status, o.recipient_name, o.contact_mobile,
		       o.total_paise, o.placed_at,
		       (SELECT COALESCE(SUM(qty),0) FROM order_items WHERE order_id = o.id)
		  FROM orders o
		 WHERE %s
		 ORDER BY o.placed_at DESC
		 LIMIT $%d OFFSET $%d`, where, n+1, n+2), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	out := make([]*ListItem, 0, f.PageSize)
	for rows.Next() {
		var it ListItem
		if err := rows.Scan(&it.ID, &it.Reference, &it.Status, &it.RecipientName, &it.ContactMobile,
			&it.TotalPaise, &it.PlacedAt, &it.ItemCount); err != nil {
			return nil, 0, err
		}
		out = append(out, &it)
	}
	return out, total, rows.Err()
}

// ListForCustomer returns a signed-in customer's own order history, newest
// first — their self-service alternative to reference+mobile tracking.
func (s *Service) ListForCustomer(ctx context.Context, customerID string, page, pageSize int) ([]*ListItem, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM orders WHERE customer_id = $1`, customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customer orders: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT o.id::text, o.reference, o.status, o.recipient_name, o.contact_mobile,
		       o.total_paise, o.placed_at,
		       (SELECT COALESCE(SUM(qty),0) FROM order_items WHERE order_id = o.id)
		  FROM orders o
		 WHERE o.customer_id = $1
		 ORDER BY o.placed_at DESC
		 LIMIT $2 OFFSET $3`, customerID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list customer orders: %w", err)
	}
	defer rows.Close()

	out := make([]*ListItem, 0, pageSize)
	for rows.Next() {
		var it ListItem
		if err := rows.Scan(&it.ID, &it.Reference, &it.Status, &it.RecipientName, &it.ContactMobile,
			&it.TotalPaise, &it.PlacedAt, &it.ItemCount); err != nil {
			return nil, 0, err
		}
		out = append(out, &it)
	}
	return out, total, rows.Err()
}

// ConfirmWhatsApp records that staff has verified the customer's WhatsApp
// confirmation and the order may proceed to payment. This stands in for the
// "operator confirms availability" step in the brief — see
// docs/IMPLEMENTATION_PLAN.md for why there is no automated inbound WhatsApp
// webhook in this iteration (it requires a Meta Business/WhatsApp Cloud API
// account, which is a credentialed integration this environment does not
// have).
func (s *Service) ConfirmWhatsApp(ctx context.Context, rec *audit.Recorder, businessID, orderID, actorID string) (*Order, error) {
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var status string
		if err := q.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 AND business_id = $2 FOR UPDATE`, orderID, businessID).Scan(&status); err != nil {
			if db.IsNoRows(err) {
				return ErrOrderNotFound
			}
			return fmt.Errorf("lock order: %w", err)
		}
		if !CanTransition(status, StatusAwaitingPayment) {
			return httpx.ErrInvalidTransition.WithMessage(
				fmt.Sprintf("Cannot confirm from status %s.", status))
		}
		if _, err := q.Exec(ctx, `
			UPDATE orders SET status = $2, whatsapp_confirmed_at = now(), whatsapp_confirmed_by = $3
			 WHERE id = $1`, orderID, StatusAwaitingPayment, actorID); err != nil {
			return fmt.Errorf("confirm order: %w", err)
		}
		if err := insertHistory(ctx, q, orderID, status, StatusAwaitingPayment,
			"operator confirmed WhatsApp order", "staff", &actorID); err != nil {
			return err
		}
		return rec.RecordTx(ctx, q, audit.Entry{
			ActorID: &actorID, Action: audit.ActionOrderConfirmed,
			EntityType: "order", EntityID: orderID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, businessID, orderID)
}

// UpdateStatus performs a staff-initiated status change, rejecting anything
// not in the legal transition table.
func (s *Service) UpdateStatus(ctx context.Context, rec *audit.Recorder, businessID, orderID, newStatus, reason, actorID string) (*Order, error) {
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var status string
		if err := q.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 AND business_id = $2 FOR UPDATE`, orderID, businessID).Scan(&status); err != nil {
			if db.IsNoRows(err) {
				return ErrOrderNotFound
			}
			return fmt.Errorf("lock order: %w", err)
		}
		if !CanTransition(status, newStatus) {
			return httpx.ErrInvalidTransition.WithMessage(
				fmt.Sprintf("Cannot move from %s to %s.", status, newStatus))
		}
		if _, err := q.Exec(ctx, `UPDATE orders SET status = $2 WHERE id = $1`, orderID, newStatus); err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		if err := insertHistory(ctx, q, orderID, status, newStatus, reason, "staff", &actorID); err != nil {
			return err
		}
		return rec.RecordTx(ctx, q, audit.Entry{
			ActorID: &actorID, Action: audit.ActionOrderStatusChanged,
			EntityType: "order", EntityID: orderID,
			Metadata: map[string]any{"from": status, "to": newStatus, "reason": reason},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, businessID, orderID)
}

// Cancel cancels an order and releases any reserved stock. actorType is
// "customer" or "staff"; reason is required and stored for support
// visibility. businessID is empty for a customer-initiated cancellation
// (authorised instead by GetByReferenceForCustomer/GetByIDForCustomer having
// already resolved the order) and required for a staff cancellation.
func (s *Service) Cancel(ctx context.Context, rec *audit.Recorder, businessID, orderID, reason, actorType string, actorID *string) (*Order, error) {
	var orderBusinessID string
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var status string
		lockSQL, lockArgs := `SELECT status, business_id::text FROM orders WHERE id = $1 FOR UPDATE`, []any{orderID}
		if businessID != "" {
			lockSQL, lockArgs = `SELECT status, business_id::text FROM orders WHERE id = $1 AND business_id = $2 FOR UPDATE`, []any{orderID, businessID}
		}
		if err := q.QueryRow(ctx, lockSQL, lockArgs...).Scan(&status, &orderBusinessID); err != nil {
			if db.IsNoRows(err) {
				return ErrOrderNotFound
			}
			return fmt.Errorf("lock order: %w", err)
		}
		if actorType == "customer" && !CanCustomerCancel(status) {
			return httpx.ErrInvalidTransition.WithMessage(
				"This order can no longer be cancelled online. Please contact support.")
		}
		if !CanTransition(status, StatusCancelled) {
			return httpx.ErrInvalidTransition.WithMessage(
				fmt.Sprintf("Cannot cancel an order in status %s.", status))
		}

		if _, err := q.Exec(ctx, `
			UPDATE orders SET status = $2, cancelled_reason = $3 WHERE id = $1`,
			orderID, StatusCancelled, reason); err != nil {
			return fmt.Errorf("cancel order: %w", err)
		}
		if err := insertHistory(ctx, q, orderID, status, StatusCancelled, reason, actorType, actorID); err != nil {
			return err
		}

		// Release any active stock reservation back to sellable.
		rows, err := q.Query(ctx, `
			SELECT oi.product_id::text, oia.batch_id::text, oia.qty
			  FROM order_items oi
			  JOIN order_item_allocations oia ON oia.order_item_id = oi.id
			 WHERE oi.order_id = $1 AND oia.released_at IS NULL`, orderID)
		if err != nil {
			return fmt.Errorf("load allocations: %w", err)
		}
		type row struct {
			productID, batchID string
			qty                int
		}
		var allocs []row
		for rows.Next() {
			var rr row
			if err := rows.Scan(&rr.productID, &rr.batchID, &rr.qty); err != nil {
				rows.Close()
				return err
			}
			allocs = append(allocs, rr)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		byProduct := map[string][]inventory.Allocation{}
		for _, a := range allocs {
			byProduct[a.productID] = append(byProduct[a.productID], inventory.Allocation{BatchID: a.batchID, Qty: a.qty})
		}
		for productID, prodAllocs := range byProduct {
			if err := inventory.ReleaseAllocations(ctx, q, productID, prodAllocs, "order cancelled: "+reason); err != nil {
				return err
			}
		}
		if _, err := q.Exec(ctx, `
			UPDATE order_item_allocations SET released_at = now()
			 WHERE order_item_id IN (SELECT id FROM order_items WHERE order_id = $1)
			   AND released_at IS NULL`, orderID); err != nil {
			return fmt.Errorf("mark allocations released: %w", err)
		}
		if _, err := q.Exec(ctx, `
			UPDATE stock_reservations SET status = 'RELEASED', released_at = now()
			 WHERE order_id = $1 AND status = 'ACTIVE'`, orderID); err != nil {
			return fmt.Errorf("release reservation: %w", err)
		}

		return rec.RecordTx(ctx, q, audit.Entry{
			ActorID: actorID, Action: audit.ActionOrderCancelled,
			EntityType: "order", EntityID: orderID, Metadata: map[string]any{"reason": reason, "actor_type": actorType},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, orderBusinessID, orderID)
}

// AddInternalNote appends a staff note (overwrite-free: it's the current note
// field, documented here as a simple single field rather than a thread —
// enough for this iteration's admin dashboard). Scoped to businessID so a
// staff member can only annotate their own business's orders.
func (s *Service) AddInternalNote(ctx context.Context, rec *audit.Recorder, businessID, orderID, note, actorID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE orders SET internal_notes = $2 WHERE id = $1 AND business_id = $3`, orderID, note, businessID)
	if err != nil {
		return fmt.Errorf("add note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotFound
	}
	rec.Record(ctx, audit.Entry{ActorID: &actorID, Action: audit.ActionOrderNoteAdded, EntityType: "order", EntityID: orderID})
	return nil
}
