package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/cart"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/inventory"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
	"github.com/sanyam/medicart/services/api/internal/platform/money"
	"github.com/sanyam/medicart/services/api/internal/shipping"
)

// Order is the full order view returned to customers and staff. Some staff-
// only fields (InternalNotes) are stripped before the customer-facing JSON is
// written — see handlers.go.
type Order struct {
	ID                   string          `json:"id"`
	Reference            string          `json:"reference"`
	Status               string          `json:"status"`
	RecipientName        string          `json:"recipientName"`
	ContactMobile        string          `json:"contactMobile"`
	AddressLine1         string          `json:"addressLine1"`
	AddressLine2         *string         `json:"addressLine2"`
	Landmark             *string         `json:"landmark"`
	City                 string          `json:"city"`
	State                string          `json:"state"`
	PINCode              string          `json:"pinCode"`
	DeliveryNotes        *string         `json:"deliveryNotes"`
	SubtotalPaise        int64           `json:"subtotalPaise"`
	DiscountPaise        int64           `json:"discountPaise"`
	DeliveryFeePaise     int64           `json:"deliveryFeePaise"`
	TaxPaise             int64           `json:"taxPaise"`
	TotalPaise           int64           `json:"totalPaise"`
	RequiresPrescription bool            `json:"requiresPrescription"`
	WhatsAppHandoffAt    *time.Time      `json:"whatsappHandoffAt"`
	WhatsAppConfirmedAt  *time.Time      `json:"whatsappConfirmedAt"`
	CancelledReason      *string         `json:"cancelledReason,omitempty"`
	InternalNotes        *string         `json:"internalNotes,omitempty"`
	PlacedAt             time.Time       `json:"placedAt"`
	Items                []Item          `json:"items"`
	History              []StatusEvent   `json:"history,omitempty"`
	Payment              *PaymentSummary `json:"payment,omitempty"`
}

type Item struct {
	ID             string  `json:"id"`
	ProductID      string  `json:"productId"`
	ProductName    string  `json:"productName"`
	ProductSKU     string  `json:"productSku"`
	BrandName      *string `json:"brandName"`
	Strength       *string `json:"strength"`
	DosageForm     *string `json:"dosageForm"`
	PackSize       *string `json:"packSize"`
	Qty            int     `json:"qty"`
	UnitMRPPaise   int64   `json:"unitMrpPaise"`
	UnitPricePaise int64   `json:"unitPricePaise"`
	LineTotalPaise int64   `json:"lineTotalPaise"`
	RequiresRx     bool    `json:"requiresPrescription"`
}

type StatusEvent struct {
	FromStatus *string   `json:"fromStatus"`
	ToStatus   string    `json:"toStatus"`
	Reason     *string   `json:"reason"`
	ActorType  string    `json:"actorType"`
	CreatedAt  time.Time `json:"createdAt"`
}

type PaymentSummary struct {
	Status      string  `json:"status"`
	AmountPaise int64   `json:"amountPaise"`
	IsTestMode  bool    `json:"isTestMode"`
	PaymentURL  *string `json:"paymentUrl"`
	Method      *string `json:"method"`
}

// CreateInput is the checkout request, already validated at the HTTP layer
// for shape; business validation happens in Create.
type CreateInput struct {
	CartID        string
	RecipientName string
	ContactMobile string
	ContactEmail  string
	AddressLine1  string
	AddressLine2  string
	Landmark      string
	City          string
	State         string
	PINCode       string
	DeliveryNotes string
	// AuthenticatedCustomerID, when set, ties the order to the signed-in
	// customer's existing profile instead of finding-or-creating a guest one
	// by mobile number. Empty for guest checkout.
	AuthenticatedCustomerID string
	// BusinessID is the tenant this order belongs to — always the same
	// business the cart (CartID) itself belongs to; Create verifies this.
	BusinessID string
}

var (
	ErrOrderNotFound = errors.New("orders: not found")
	ErrWrongMobile   = errors.New("orders: mobile number does not match this order")
)

type Service struct {
	pool           *db.Pool
	cartSvc        *cart.Service
	shipSvc        *shipping.Service
	rxSalesEnabled bool
}

func NewService(pool *db.Pool, cartSvc *cart.Service, shipSvc *shipping.Service, rxSalesEnabled bool) *Service {
	return &Service{pool: pool, cartSvc: cartSvc, shipSvc: shipSvc, rxSalesEnabled: rxSalesEnabled}
}

// Create validates and persists an order from a priced cart in one
// transaction: stock is reserved FEFO under row locks, so two customers
// racing for the last units cannot both succeed, and the whole order is
// atomic — either every line is reserved and the order recorded, or nothing
// is, with no partial state visible to anyone else.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Order, error) {
	in.ContactMobile = auth.NormaliseMobile(in.ContactMobile)
	if !auth.IsValidMobile(in.ContactMobile) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"contactMobile": "Enter a valid 10-digit mobile number."})
	}
	if !auth.IsValidPIN(in.PINCode) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"pinCode": "Enter a valid 6-digit PIN code."})
	}
	in.RecipientName = strings.TrimSpace(in.RecipientName)
	in.AddressLine1 = strings.TrimSpace(in.AddressLine1)
	in.City = strings.TrimSpace(in.City)
	in.State = strings.TrimSpace(in.State)
	if in.RecipientName == "" || in.AddressLine1 == "" || in.City == "" || in.State == "" {
		return nil, httpx.ErrValidation.WithMessage("Please fill in all required delivery details.")
	}

	area, err := s.shipSvc.Check(ctx, in.PINCode)
	if err != nil {
		return nil, err
	}
	if !area.IsServiceable {
		return nil, httpx.ErrNotServiceable
	}

	summary, err := s.cartSvc.Get(ctx, in.CartID)
	if err != nil {
		return nil, err
	}
	if len(summary.Lines) == 0 {
		return nil, httpx.ErrCartEmpty
	}
	if summary.RequiresRx && !s.rxSalesEnabled {
		return nil, httpx.ErrPrescriptionRequired
	}

	var order *Order
	err = s.pool.InSerializableTx(ctx, func(q db.Querier) error {
		// Re-check every line against live, locked stock inside the
		// transaction — the cart summary above is only a pre-check for a
		// fast user-facing error; this is the authoritative check.
		subtotal := money.Paise(0)
		type lineAlloc struct {
			productID string
			allocs    []inventory.Allocation
		}
		var allocations []lineAlloc
		var items []Item

		for _, l := range summary.Lines {
			if !l.IsActive {
				return httpx.ErrOutOfStock
			}
			allocs, err := inventory.ReserveFEFO(ctx, q, in.BusinessID, l.ProductID, l.Qty)
			if err != nil {
				if errors.Is(err, inventory.ErrInsufficientStock) {
					return httpx.ErrOutOfStock
				}
				return fmt.Errorf("reserve stock: %w", err)
			}
			allocations = append(allocations, lineAlloc{productID: l.ProductID, allocs: allocs})

			lineTotal := l.UnitPricePaise * int64(l.Qty)
			subtotal += money.Paise(lineTotal)
			items = append(items, Item{
				ProductID: l.ProductID, ProductName: l.ProductName, ProductSKU: l.ProductSKU,
				BrandName: l.BrandName, PackSize: l.PackSize, Strength: l.Strength, DosageForm: l.DosageForm,
				Qty: l.Qty, UnitMRPPaise: l.UnitMRPPaise, UnitPricePaise: l.UnitPricePaise,
				LineTotalPaise: lineTotal, RequiresRx: l.RequiresRx,
			})
		}

		deliveryFee := s.shipSvc.DeliveryFee(subtotal, area)
		total := subtotal + deliveryFee

		reference := idgen.OrderReference(time.Now())
		var orderID string
		// A signed-in customer's order is tied to their existing profile; a
		// guest's is found-or-created by mobile number, so orders.customer_id
		// (NOT NULL) is always satisfiable either way.
		var customerID string
		var err error
		if in.AuthenticatedCustomerID != "" {
			customerID = in.AuthenticatedCustomerID
		} else {
			customerID, err = findOrCreateCustomerByMobile(ctx, q, in.ContactMobile)
		}
		if err != nil {
			return err
		}

		err = q.QueryRow(ctx, `
			INSERT INTO orders (
				reference, customer_id, business_id, status, recipient_name, contact_mobile, contact_email,
				address_line1, address_line2, landmark, city, state, pin_code, delivery_notes,
				subtotal_paise, discount_paise, delivery_fee_paise, tax_paise, total_paise,
				requires_prescription
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,0,$16,0,$17,$18)
			RETURNING id::text`,
			reference, customerID, in.BusinessID, StatusAwaitingCustomerConfirmation,
			in.RecipientName, in.ContactMobile, nullIfEmpty(in.ContactEmail),
			in.AddressLine1, nullIfEmpty(in.AddressLine2), nullIfEmpty(in.Landmark),
			in.City, in.State, in.PINCode, nullIfEmpty(in.DeliveryNotes),
			int64(subtotal), int64(deliveryFee), int64(total), summary.RequiresRx,
		).Scan(&orderID)
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}

		if _, err := q.Exec(ctx, `
			INSERT INTO business_customers (business_id, customer_id, order_count, lifetime_value_paise, last_order_at)
			VALUES ($1, $2, 1, $3, now())
			ON CONFLICT (business_id, customer_id) DO UPDATE SET
				order_count = business_customers.order_count + 1,
				lifetime_value_paise = business_customers.lifetime_value_paise + EXCLUDED.lifetime_value_paise,
				last_order_at = now()`,
			in.BusinessID, customerID, int64(total)); err != nil {
			return fmt.Errorf("upsert business customer: %w", err)
		}

		for i, it := range items {
			var itemID string
			err := q.QueryRow(ctx, `
				INSERT INTO order_items (
					order_id, product_id, product_name, product_sku, brand_name, strength, dosage_form, pack_size,
					qty, unit_mrp_paise, unit_price_paise, line_total_paise, requires_prescription
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
				RETURNING id::text`,
				orderID, it.ProductID, it.ProductName, it.ProductSKU, it.BrandName, it.Strength, it.DosageForm, it.PackSize,
				it.Qty, it.UnitMRPPaise, it.UnitPricePaise, it.LineTotalPaise, it.RequiresRx,
			).Scan(&itemID)
			if err != nil {
				return fmt.Errorf("insert order item: %w", err)
			}
			items[i].ID = itemID

			for _, a := range allocations[i].allocs {
				if _, err := q.Exec(ctx, `
					INSERT INTO order_item_allocations (order_item_id, batch_id, qty)
					VALUES ($1, $2, $3)`, itemID, a.BatchID, a.Qty); err != nil {
					return fmt.Errorf("insert allocation: %w", err)
				}
			}
		}

		// Reservation TTL: how long we hold the stock before it should be
		// released if the customer never confirms. No background sweep runs
		// yet in this iteration — see docs/IMPLEMENTATION_PLAN.md.
		if _, err := q.Exec(ctx, `
			INSERT INTO stock_reservations (order_id, expires_at)
			VALUES ($1, now() + INTERVAL '30 minutes')`, orderID); err != nil {
			return fmt.Errorf("insert reservation: %w", err)
		}

		for _, h := range []struct{ from, to, reason, actor string }{
			{"", StatusCreated, "order placed", "customer"},
			{StatusCreated, StatusAwaitingCustomerConfirmation, "awaiting WhatsApp confirmation", "system"},
		} {
			if err := insertHistory(ctx, q, orderID, h.from, h.to, h.reason, h.actor, nil); err != nil {
				return err
			}
		}

		if err := s.cartSvc.Clear(ctx, q, in.CartID); err != nil {
			return fmt.Errorf("clear cart: %w", err)
		}

		order = &Order{
			ID: orderID, Reference: reference, Status: StatusAwaitingCustomerConfirmation,
			RecipientName: in.RecipientName, ContactMobile: in.ContactMobile,
			AddressLine1: in.AddressLine1, City: in.City, State: in.State, PINCode: in.PINCode,
			SubtotalPaise: int64(subtotal), DeliveryFeePaise: int64(deliveryFee), TotalPaise: int64(total),
			RequiresPrescription: summary.RequiresRx, Items: items, PlacedAt: time.Now().UTC(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// findOrCreateCustomerByMobile finds-or-creates the users/customer_profiles
// pair for a checkout mobile number, without issuing a login session. This is
// the guest-checkout identity path described in the package doc comment.
func findOrCreateCustomerByMobile(ctx context.Context, q db.Querier, mobile string) (string, error) {
	var userID string
	err := q.QueryRow(ctx, `SELECT id::text FROM users WHERE mobile = $1`, mobile).Scan(&userID)
	if db.IsNoRows(err) {
		if err := q.QueryRow(ctx, `
			INSERT INTO users (mobile, is_active) VALUES ($1, TRUE)
			RETURNING id::text`, mobile).Scan(&userID); err != nil {
			return "", fmt.Errorf("create guest user: %w", err)
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'CUSTOMER')`, userID); err != nil {
			return "", fmt.Errorf("grant customer role: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("look up user: %w", err)
	}

	var customerID string
	err = q.QueryRow(ctx, `
		INSERT INTO customer_profiles (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING id::text`, userID).Scan(&customerID)
	if err != nil {
		return "", fmt.Errorf("upsert customer profile: %w", err)
	}
	return customerID, nil
}

// InsertHistoryPublic lets other packages (payments, on a verified webhook)
// append an order-status-history row inside their own transaction, so the
// status change and its audit trail commit atomically with the payment
// event that caused it.
func InsertHistoryPublic(ctx context.Context, q db.Querier, orderID, from, to, reason, actorType string, actorID *string) error {
	return insertHistory(ctx, q, orderID, from, to, reason, actorType, actorID)
}

func insertHistory(ctx context.Context, q db.Querier, orderID, from, to, reason, actorType string, actorID *string) error {
	var fromVal any
	if from != "" {
		fromVal = from
	}
	_, err := q.Exec(ctx, `
		INSERT INTO order_status_history (order_id, from_status, to_status, reason, actor_type, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, orderID, fromVal, to, reason, actorType, actorID)
	if err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
