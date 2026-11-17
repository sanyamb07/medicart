package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

func TestAssistantProductLookupUsesRealCatalogue(t *testing.T) {
	ctx := context.Background()
	insertProduct(t, "Azithromycin 500mg Tablets", "T-AZI-500", "t-azithromycin-500-"+randomSuffix(), 12000, 15000, true)

	reply, err := assistantSvc.Respond(ctx, tenancy.DefaultBusinessID, "", "do you have azithromycin in stock")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !strings.Contains(reply.Text, "Azithromycin") {
		t.Errorf("reply = %q, want it to mention the matched product", reply.Text)
	}
	if !strings.Contains(reply.Text, "prescription") {
		t.Errorf("reply = %q, want the Rx requirement surfaced for a prescription product", reply.Text)
	}
}

func TestAssistantNeverInventsAnUnknownProduct(t *testing.T) {
	reply, err := assistantSvc.Respond(context.Background(), tenancy.DefaultBusinessID, "", "do you have zzznonexistentmedicine999 in stock")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if strings.Contains(strings.ToLower(reply.Text), "in stock") {
		t.Errorf("reply = %q, must not claim stock for a product that doesn't exist", reply.Text)
	}
}

func TestAssistantServiceabilityUsesRealPolicy(t *testing.T) {
	reply, err := assistantSvc.Respond(context.Background(), tenancy.DefaultBusinessID, "", "do you deliver to 400001")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !strings.Contains(reply.Text, "Mumbai") {
		t.Errorf("reply = %q, want the real serviceable area for the seeded PIN 400001", reply.Text)
	}

	reply, err = assistantSvc.Respond(context.Background(), tenancy.DefaultBusinessID, "", "can you ship to 999999")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !strings.Contains(reply.Text, "don't") {
		t.Errorf("reply = %q, want an honest non-serviceable answer for an unseeded PIN", reply.Text)
	}
}

func TestAssistantOrderStatusRequiresMatchingMobile(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Cetirizine", "T-CET-1", "t-cetirizine-1-"+randomSuffix(), 1500, 2000, false)
	insertBatch(t, productID, 50, 365)
	cartID := newCart(t)
	if err := cartSvc.AddItem(ctx, cartID, productID, 1); err != nil {
		t.Fatalf("add item: %v", err)
	}
	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Assistant Test", ContactMobile: "9876500010",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// Correct reference + correct mobile: answered.
	reply, err := assistantSvc.Respond(ctx, tenancy.DefaultBusinessID, "", order.Reference+" status for 9876500010")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !strings.Contains(reply.Text, order.Reference) {
		t.Errorf("reply = %q, want the order reference echoed back once authorised", reply.Text)
	}

	// Correct reference + wrong mobile: must not leak the status.
	reply, err = assistantSvc.Respond(ctx, tenancy.DefaultBusinessID, "", order.Reference+" status for 9123456700")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if strings.Contains(reply.Text, "currently") {
		t.Errorf("reply = %q, must not reveal order status for a mismatched mobile number", reply.Text)
	}
}

func TestAssistantEscalatesClinicalQuestions(t *testing.T) {
	reply, err := assistantSvc.Respond(context.Background(), tenancy.DefaultBusinessID, "", "what dose of paracetamol is safe for my child")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !reply.Escalate {
		t.Error("expected Escalate=true for a dosage question")
	}
	if strings.Contains(strings.ToLower(reply.Text), "mg") {
		t.Errorf("reply = %q, must not attempt a dosage answer", reply.Text)
	}
}
