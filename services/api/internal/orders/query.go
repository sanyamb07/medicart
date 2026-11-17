package orders

import (
	"context"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/db"
)

const orderColumns = `
	o.id::text, o.reference, o.status, o.recipient_name, o.contact_mobile,
	o.address_line1, o.address_line2, o.landmark, o.city, o.state, o.pin_code, o.delivery_notes,
	o.subtotal_paise, o.discount_paise, o.delivery_fee_paise, o.tax_paise, o.total_paise,
	o.requires_prescription, o.whatsapp_handoff_at, o.whatsapp_confirmed_at,
	o.cancelled_reason, o.internal_notes, o.placed_at`

func scanOrder(row interface{ Scan(...any) error }) (*Order, error) {
	var o Order
	if err := row.Scan(
		&o.ID, &o.Reference, &o.Status, &o.RecipientName, &o.ContactMobile,
		&o.AddressLine1, &o.AddressLine2, &o.Landmark, &o.City, &o.State, &o.PINCode, &o.DeliveryNotes,
		&o.SubtotalPaise, &o.DiscountPaise, &o.DeliveryFeePaise, &o.TaxPaise, &o.TotalPaise,
		&o.RequiresPrescription, &o.WhatsAppHandoffAt, &o.WhatsAppConfirmedAt,
		&o.CancelledReason, &o.InternalNotes, &o.PlacedAt,
	); err != nil {
		return nil, err
	}
	return &o, nil
}

// GetByReferenceForCustomer loads an order, authorised by the caller knowing
// both the order reference (an unguessable CSPRNG string) and the contact
// mobile number it was placed with. Neither alone is sufficient — this is the
// whole access-control boundary for customer order tracking without a login
// system, so it is enforced in SQL rather than in application code that could
// be bypassed by a different call path.
func (s *Service) GetByReferenceForCustomer(ctx context.Context, reference, mobile string) (*Order, error) {
	sql := fmt.Sprintf(`SELECT %s FROM orders o WHERE o.reference = $1 AND o.contact_mobile = $2`, orderColumns)
	o, err := scanOrder(s.pool.QueryRow(ctx, sql, reference, mobile))
	if db.IsNoRows(err) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	if err := s.attachItems(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachHistory(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachPayment(ctx, o); err != nil {
		return nil, err
	}
	// Internal notes are staff-only.
	o.InternalNotes = nil
	return o, nil
}

// GetByIDForCustomer loads an order for a signed-in customer, scoped to their
// own customer_id — the authorisation boundary for the account order-history
// view, parallel to the reference+mobile boundary for guest tracking.
func (s *Service) GetByIDForCustomer(ctx context.Context, id, customerID string) (*Order, error) {
	sql := fmt.Sprintf(`SELECT %s FROM orders o WHERE o.id = $1 AND o.customer_id = $2`, orderColumns)
	o, err := scanOrder(s.pool.QueryRow(ctx, sql, id, customerID))
	if db.IsNoRows(err) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	if err := s.attachItems(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachHistory(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachPayment(ctx, o); err != nil {
		return nil, err
	}
	o.InternalNotes = nil
	return o, nil
}

// GetByID loads an order for staff, scoped to their own business — a staff
// member can never load another business's order even by guessing its id.
func (s *Service) GetByID(ctx context.Context, businessID, id string) (*Order, error) {
	sql := fmt.Sprintf(`SELECT %s FROM orders o WHERE o.id = $1 AND o.business_id = $2`, orderColumns)
	o, err := scanOrder(s.pool.QueryRow(ctx, sql, id, businessID))
	if db.IsNoRows(err) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	if err := s.attachItems(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachHistory(ctx, o); err != nil {
		return nil, err
	}
	if err := s.attachPayment(ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Service) attachItems(ctx context.Context, o *Order) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, product_id::text, product_name, product_sku, brand_name,
		       strength, dosage_form, pack_size, qty, unit_mrp_paise, unit_price_paise,
		       line_total_paise, requires_prescription
		  FROM order_items WHERE order_id = $1 ORDER BY created_at`, o.ID)
	if err != nil {
		return fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.ProductID, &it.ProductName, &it.ProductSKU, &it.BrandName,
			&it.Strength, &it.DosageForm, &it.PackSize, &it.Qty, &it.UnitMRPPaise, &it.UnitPricePaise,
			&it.LineTotalPaise, &it.RequiresRx); err != nil {
			return err
		}
		o.Items = append(o.Items, it)
	}
	return rows.Err()
}

func (s *Service) attachHistory(ctx context.Context, o *Order) error {
	rows, err := s.pool.Query(ctx, `
		SELECT from_status, to_status, reason, actor_type, created_at
		  FROM order_status_history WHERE order_id = $1 ORDER BY created_at`, o.ID)
	if err != nil {
		return fmt.Errorf("load order history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h StatusEvent
		if err := rows.Scan(&h.FromStatus, &h.ToStatus, &h.Reason, &h.ActorType, &h.CreatedAt); err != nil {
			return err
		}
		o.History = append(o.History, h)
	}
	return rows.Err()
}

func (s *Service) attachPayment(ctx context.Context, o *Order) error {
	var p PaymentSummary
	err := s.pool.QueryRow(ctx, `
		SELECT status, amount_paise, is_test_mode, payment_url, method
		  FROM payment_attempts WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, o.ID).
		Scan(&p.Status, &p.AmountPaise, &p.IsTestMode, &p.PaymentURL, &p.Method)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load payment: %w", err)
	}
	o.Payment = &p
	return nil
}

// MarkWhatsAppHandoff records that the WhatsApp deep link was opened. This is
// NOT proof the customer sent the message — see internal/whatsapp — it only
// lets the UI show "we're waiting for your WhatsApp confirmation" accurately.
func (s *Service) MarkWhatsAppHandoff(ctx context.Context, orderID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE orders SET whatsapp_handoff_at = now() WHERE id = $1 AND whatsapp_handoff_at IS NULL`, orderID)
	if err != nil {
		return fmt.Errorf("mark handoff: %w", err)
	}
	return nil
}

// BuyAgainSlugs returns slugs of active products the customer has ordered,
// most recently ordered first.
func (s *Service) BuyAgainSlugs(ctx context.Context, customerID string, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.slug
		  FROM order_items oi
		  JOIN orders o ON o.id = oi.order_id
		  JOIN products p ON p.id = oi.product_id
		 WHERE o.customer_id = $1 AND p.is_active
		 GROUP BY p.slug
		 ORDER BY max(o.placed_at) DESC
		 LIMIT $2`, customerID, limit)
	if err != nil {
		return nil, fmt.Errorf("buy again: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}
