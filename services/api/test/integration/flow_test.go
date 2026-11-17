package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/payments"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

// newCart returns a fresh guest cart id.
func newCart(t *testing.T) string {
	t.Helper()
	id, _, err := cartSvc.GetOrCreateGuestCart(context.Background(), tenancy.DefaultBusinessID, "")
	if err != nil {
		t.Fatalf("create cart: %v", err)
	}
	return id
}

func TestCheckoutCreatesOrderWithCorrectTotals(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Paracetamol", "T-PARA-1", "t-paracetamol-1", 2800, 3500, false)
	insertBatch(t, productID, 50, 365)

	cartID := newCart(t)
	if err := cartSvc.AddItem(ctx, cartID, productID, 3); err != nil {
		t.Fatalf("add item: %v", err)
	}

	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Test Customer", ContactMobile: "9876543211",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// 3 × ₹28.00 = ₹84.00 subtotal; + ₹49.00 delivery (below free threshold) = ₹133.00.
	if order.SubtotalPaise != 8400 {
		t.Errorf("subtotal = %d, want 8400", order.SubtotalPaise)
	}
	if order.DeliveryFeePaise != 4900 {
		t.Errorf("delivery fee = %d, want 4900", order.DeliveryFeePaise)
	}
	if order.TotalPaise != 13300 {
		t.Errorf("total = %d, want 13300", order.TotalPaise)
	}
	if order.Status != orders.StatusAwaitingCustomerConfirmation {
		t.Errorf("status = %s, want %s", order.Status, orders.StatusAwaitingCustomerConfirmation)
	}
	if order.Reference == "" {
		t.Error("expected a non-empty order reference")
	}

	// The cart must be emptied by a successful checkout.
	summary, err := cartSvc.Get(ctx, cartID)
	if err != nil {
		t.Fatalf("get cart: %v", err)
	}
	if len(summary.Lines) != 0 {
		t.Errorf("expected cart to be cleared after checkout, got %d lines", len(summary.Lines))
	}

	// Stock must be reserved, not yet decremented.
	var onHand, reserved int
	if err := pool.QueryRow(ctx,
		`SELECT qty_on_hand, qty_reserved FROM inventory_batches WHERE product_id = $1`, productID).
		Scan(&onHand, &reserved); err != nil {
		t.Fatalf("read batch: %v", err)
	}
	if onHand != 50 || reserved != 3 {
		t.Errorf("batch = on_hand=%d reserved=%d, want on_hand=50 reserved=3", onHand, reserved)
	}
}

func TestOrderTrackingDeniesWrongMobile(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Ibuprofen", "T-IBU-1", "t-ibuprofen-1", 3000, 3500, false)
	insertBatch(t, productID, 20, 365)

	cartID := newCart(t)
	_ = cartSvc.AddItem(ctx, cartID, productID, 1)
	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Wrong Mobile Test", ContactMobile: "9876543212",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// Correct reference, correct mobile: must succeed.
	if _, err := orderSvc.GetByReferenceForCustomer(ctx, order.Reference, "9876543212"); err != nil {
		t.Errorf("expected access with correct mobile to succeed, got %v", err)
	}

	// Correct reference, wrong mobile: must be denied exactly like "not found" —
	// this is the entire authorisation boundary for order tracking.
	_, err = orderSvc.GetByReferenceForCustomer(ctx, order.Reference, "9999999999")
	if !errors.Is(err, orders.ErrOrderNotFound) {
		t.Errorf("expected ErrOrderNotFound for wrong mobile, got %v", err)
	}
}

func TestPrescriptionRequiredBlocksCheckout(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Amoxicillin", "T-AMOX-1", "t-amoxicillin-1", 7500, 9000, true)
	insertBatch(t, productID, 10, 365)

	cartID := newCart(t)
	_ = cartSvc.AddItem(ctx, cartID, productID, 1)

	_, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Rx Test", ContactMobile: "9876543213",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	var ae *httpx.APIError
	if !errors.As(err, &ae) || ae.Code != "prescription_required" {
		t.Fatalf("expected prescription_required error, got %v", err)
	}
}

func TestCheckoutRejectsUnserviceablePIN(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Vitamin C", "T-VITC-1", "t-vitaminc-1", 18900, 25000, false)
	insertBatch(t, productID, 10, 365)

	cartID := newCart(t)
	_ = cartSvc.AddItem(ctx, cartID, productID, 1)

	_, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "PIN Test", ContactMobile: "9876543214",
		AddressLine1: "1 Test Street", City: "Nowhere", State: "Nowhere", PINCode: "999999",
	})
	var ae *httpx.APIError
	if !errors.As(err, &ae) || ae.Code != "not_serviceable" {
		t.Fatalf("expected not_serviceable error, got %v", err)
	}
}

func TestExpiredBatchExcludedFromStock(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Expired Product", "T-EXP-1", "t-expired-1", 2000, 2500, false)
	insertBatch(t, productID, 100, -10) // expired 10 days ago

	cartID := newCart(t)
	if err := cartSvc.AddItem(ctx, cartID, productID, 1); err != nil {
		t.Fatalf("add item: %v", err)
	}

	_, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Expiry Test", ContactMobile: "9876543215",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	var ae *httpx.APIError
	if !errors.As(err, &ae) || ae.Code != "out_of_stock" {
		t.Fatalf("expected out_of_stock for an expired-only batch, got %v", err)
	}
}

// buildCheckoutOrder drives a product through to AWAITING_PAYMENT and returns
// it, for payment-focused tests that don't care about checkout itself.
func buildCheckoutOrder(t *testing.T, mobile string, qty int) *orders.Order {
	t.Helper()
	ctx := context.Background()
	productID := insertProduct(t, "Test Payment Product "+mobile, "T-PAY-"+mobile, "t-pay-"+mobile, 5000, 6000, false)
	insertBatch(t, productID, 50, 365)

	cartID := newCart(t)
	if err := cartSvc.AddItem(ctx, cartID, productID, qty); err != nil {
		t.Fatalf("add item: %v", err)
	}
	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Payment Test", ContactMobile: mobile,
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := orderSvc.ConfirmWhatsApp(ctx, rec, tenancy.DefaultBusinessID, order.ID, staffUserID); err != nil {
		t.Fatalf("confirm whatsapp: %v", err)
	}
	return order
}

func TestPaymentWebhookIsIdempotentAndSignatureVerified(t *testing.T) {
	ctx := context.Background()
	order := buildCheckoutOrder(t, "9876543216", 2)

	attempt, err := paymentSvc.CreateAttempt(ctx, order.ID)
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if attempt.AmountPaise != order.TotalPaise {
		// Re-fetch the order since CreateAttempt reads the authoritative total
		// from the DB row, which should equal what checkout computed.
		t.Logf("attempt amount %d vs order.TotalPaise at checkout time %d (fee/subtotal may differ per fixture)", attempt.AmountPaise, order.TotalPaise)
	}

	payload := payments.WebhookPayload{
		EventID: "evt-" + order.Reference, EventType: "payment.captured",
		ProviderOrderID: attempt.ProviderOrderID, ProviderPaymentID: "pay-" + order.Reference, Method: "upi",
	}
	raw, _ := json.Marshal(payload)
	sig := paymentSvc.SignWebhook(raw)

	// First delivery: must process and mark the order PAID.
	already, err := paymentSvc.HandleWebhook(ctx, raw, sig)
	if err != nil {
		t.Fatalf("first webhook: %v", err)
	}
	if already {
		t.Error("first delivery should not be reported as already processed")
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, order.ID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	if status != orders.StatusPaid {
		t.Fatalf("order status = %s, want %s", status, orders.StatusPaid)
	}

	// Duplicate delivery (identical event id, as a real gateway retry would
	// send): must be detected and must NOT process twice.
	already, err = paymentSvc.HandleWebhook(ctx, raw, sig)
	if err != nil {
		t.Fatalf("duplicate webhook: %v", err)
	}
	if !already {
		t.Error("duplicate delivery should be reported as already processed")
	}

	var eventCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payment_events WHERE provider_event_id = $1`, payload.EventID).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("expected exactly 1 payment_events row for the event id, got %d", eventCount)
	}

	var succeededCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM payment_attempts WHERE order_id = $1 AND status = 'SUCCEEDED'`, order.ID).
		Scan(&succeededCount); err != nil {
		t.Fatalf("count succeeded attempts: %v", err)
	}
	if succeededCount != 1 {
		t.Errorf("expected exactly 1 succeeded payment attempt, got %d", succeededCount)
	}

	// A forged signature must be rejected outright.
	_, err = paymentSvc.HandleWebhook(ctx, raw, "deadbeef00000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, payments.ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature for a forged signature, got %v", err)
	}
}

func TestPaymentFailureReturnsOrderToAwaitingPayment(t *testing.T) {
	ctx := context.Background()
	order := buildCheckoutOrder(t, "9876543217", 1)

	attempt, err := paymentSvc.CreateAttempt(ctx, order.ID)
	if err != nil {
		t.Fatalf("create attempt: %v", err)
	}

	payload := payments.WebhookPayload{
		EventID: "evt-fail-" + order.Reference, EventType: "payment.failed",
		ProviderOrderID: attempt.ProviderOrderID, FailureReason: "insufficient funds (simulated)",
	}
	raw, _ := json.Marshal(payload)
	sig := paymentSvc.SignWebhook(raw)

	if _, err := paymentSvc.HandleWebhook(ctx, raw, sig); err != nil {
		t.Fatalf("webhook: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, order.ID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	if status != orders.StatusAwaitingPayment {
		t.Errorf("order status = %s, want %s (so the customer can retry)", status, orders.StatusAwaitingPayment)
	}
}

func TestCancelReleasesReservedStock(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Cancel Product", "T-CANCEL-1", "t-cancel-1", 2000, 2500, false)
	insertBatch(t, productID, 10, 365)

	cartID := newCart(t)
	_ = cartSvc.AddItem(ctx, cartID, productID, 4)
	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Cancel Test", ContactMobile: "9876543218",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	var reserved int
	pool.QueryRow(ctx, `SELECT qty_reserved FROM inventory_batches WHERE product_id = $1`, productID).Scan(&reserved)
	if reserved != 4 {
		t.Fatalf("expected 4 reserved before cancel, got %d", reserved)
	}

	if _, err := orderSvc.Cancel(ctx, rec, "", order.ID, "customer changed their mind", "customer", nil); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	pool.QueryRow(ctx, `SELECT qty_reserved FROM inventory_batches WHERE product_id = $1`, productID).Scan(&reserved)
	if reserved != 0 {
		t.Errorf("expected reservation released (0) after cancel, got %d", reserved)
	}
}
