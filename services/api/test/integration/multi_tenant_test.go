package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/crm"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

// createBusiness registers and immediately activates a test business (the
// platform-admin approval step collapsed into one call, since these tests
// are about what an ACTIVE business can and cannot see).
func createBusiness(t *testing.T, slug, name string) *tenancy.Business {
	t.Helper()
	ctx := context.Background()
	b, err := bizRepo.Create(ctx, tenancy.CreateInput{Slug: slug, Name: name})
	if err != nil {
		t.Fatalf("create business %s: %v", slug, err)
	}
	if _, err := bizRepo.SetStatus(ctx, b.ID, tenancy.StatusActive); err != nil {
		t.Fatalf("activate business %s: %v", slug, err)
	}
	b.Status = tenancy.StatusActive
	return b
}

func TestPlatformAdminBusinessLifecycle(t *testing.T) {
	ctx := context.Background()
	b, err := bizRepo.Create(ctx, tenancy.CreateInput{Slug: "lifecycle-test-" + randomSuffix(), Name: "Lifecycle Test Pharmacy"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != tenancy.StatusPending {
		t.Errorf("new business status = %s, want PENDING", b.Status)
	}
	// A pending business must not resolve as a storefront.
	if _, err := bizRepo.ResolveBySlug(ctx, b.Slug); err == nil {
		t.Error("expected a PENDING business to not resolve as an active storefront")
	}

	approved, err := bizRepo.SetStatus(ctx, b.ID, tenancy.StatusActive)
	if err != nil || approved.Status != tenancy.StatusActive {
		t.Fatalf("approve: %+v, %v", approved, err)
	}
	if _, err := bizRepo.ResolveBySlug(ctx, b.Slug); err != nil {
		t.Errorf("expected an ACTIVE business to resolve as a storefront: %v", err)
	}

	suspended, err := bizRepo.SetStatus(ctx, b.ID, tenancy.StatusSuspended)
	if err != nil || suspended.Status != tenancy.StatusSuspended {
		t.Fatalf("suspend: %+v, %v", suspended, err)
	}
	if _, err := bizRepo.ResolveBySlug(ctx, b.Slug); err == nil {
		t.Error("expected a SUSPENDED business to stop resolving as a storefront")
	}
}

func TestBusinessCatalogAndPricingAreIsolated(t *testing.T) {
	ctx := context.Background()
	a := createBusiness(t, "iso-a-"+randomSuffix(), "Isolation Pharmacy A")
	b := createBusiness(t, "iso-b-"+randomSuffix(), "Isolation Pharmacy B")

	// One master product, listed by both businesses at different prices.
	productID := insertProduct(t, "Isolation Test Tablets", "T-ISO-"+randomSuffix(), "t-iso-"+randomSuffix(), 10000, 12000, false)
	// insertProduct already listed it for the default business; relist for A
	// and B explicitly at their own prices, and unlist it from the default
	// business so this test's business-scoped queries are unambiguous.
	if err := catalogRepo.SetListing(ctx, a.ID, productID, catalog.ListingInput{IsListed: true, IsVisible: true}); err != nil {
		t.Fatalf("list for A: %v", err)
	}
	cheaperPrice := int64(9000)
	if err := catalogRepo.SetListing(ctx, b.ID, productID, catalog.ListingInput{IsListed: true, IsVisible: true, PricePaise: &cheaperPrice}); err != nil {
		t.Fatalf("list for B: %v", err)
	}

	gotA, err := catalogRepo.GetByIDForBusiness(ctx, a.ID, productID)
	if err != nil {
		t.Fatalf("get for A: %v", err)
	}
	if gotA.PricePaise != 10000 {
		t.Errorf("business A price = %d, want 10000 (master price, no override)", gotA.PricePaise)
	}
	gotB, err := catalogRepo.GetByIDForBusiness(ctx, b.ID, productID)
	if err != nil {
		t.Fatalf("get for B: %v", err)
	}
	if gotB.PricePaise != 9000 {
		t.Errorf("business B price = %d, want 9000 (its own override)", gotB.PricePaise)
	}

	// A business that never listed the product must not be able to see it at all.
	c := createBusiness(t, "iso-c-"+randomSuffix(), "Isolation Pharmacy C (never listed it)")
	if _, err := catalogRepo.GetByIDForBusiness(ctx, c.ID, productID); err == nil {
		t.Error("expected business C, which never listed this product, to not find it")
	}

	// Each business's own inventory is separate even for the same master
	// product: stock received for A must not be sellable by B.
	insertBatchForBusiness(t, a.ID, productID, 20, 365)
	availA, err := availableQtyFor(ctx, a.ID, productID)
	if err != nil || availA != 20 {
		t.Errorf("business A available = %d, %v, want 20", availA, err)
	}
	availB, err := availableQtyFor(ctx, b.ID, productID)
	if err != nil || availB != 0 {
		t.Errorf("business B available = %d, %v, want 0 (A's stock must not leak to B)", availB, err)
	}
}

func TestOrdersAreIsolatedPerBusiness(t *testing.T) {
	ctx := context.Background()
	a := createBusiness(t, "iso-orders-a-"+randomSuffix(), "Order Isolation A")
	b := createBusiness(t, "iso-orders-b-"+randomSuffix(), "Order Isolation B")

	productA := insertProduct(t, "Order Iso Product A", "T-OIA-"+randomSuffix(), "t-oia-"+randomSuffix(), 5000, 6000, false)
	if err := catalogRepo.SetListing(ctx, a.ID, productA, catalog.ListingInput{IsListed: true, IsVisible: true}); err != nil {
		t.Fatalf("list for A: %v", err)
	}
	insertBatchForBusiness(t, a.ID, productA, 10, 365)

	cartID, _, err := cartSvc.GetOrCreateGuestCart(ctx, a.ID, "")
	if err != nil {
		t.Fatalf("create cart: %v", err)
	}
	if err := cartSvc.AddItem(ctx, cartID, productA, 1); err != nil {
		t.Fatalf("add item: %v", err)
	}
	order, err := orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: a.ID, RecipientName: "Iso Test", ContactMobile: "9876501234",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// Business A sees its own order.
	itemsA, totalA, err := orderSvc.List(ctx, orders.ListFilter{BusinessID: a.ID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list for A: %v", err)
	}
	foundInA := false
	for _, it := range itemsA {
		if it.ID == order.ID {
			foundInA = true
		}
	}
	if !foundInA || totalA == 0 {
		t.Error("expected business A's order list to include the order it just created")
	}

	// Business B's admin view must not see it, by listing or by direct id lookup.
	itemsB, _, err := orderSvc.List(ctx, orders.ListFilter{BusinessID: b.ID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list for B: %v", err)
	}
	for _, it := range itemsB {
		if it.ID == order.ID {
			t.Error("business B's order list must not include business A's order")
		}
	}
	if _, err := orderSvc.GetByID(ctx, b.ID, order.ID); err != orders.ErrOrderNotFound {
		t.Errorf("GetByID from business B = %v, want ErrOrderNotFound", err)
	}
	// Business B staff must not be able to cancel business A's order either.
	if _, err := orderSvc.Cancel(ctx, rec, b.ID, order.ID, "attempted cross-tenant cancel", "staff", &staffUserID); err != orders.ErrOrderNotFound {
		t.Errorf("Cancel from business B = %v, want ErrOrderNotFound", err)
	}
}

func TestCRMIsIsolatedPerBusiness(t *testing.T) {
	ctx := context.Background()
	a := createBusiness(t, "iso-crm-a-"+randomSuffix(), "CRM Isolation A")
	b := createBusiness(t, "iso-crm-b-"+randomSuffix(), "CRM Isolation B")
	customerID := insertCustomer(t, "9876509"+randomDigits(3))

	if err := crmRepo.EnsureCustomer(ctx, a.ID, customerID); err != nil {
		t.Fatalf("ensure customer for A: %v", err)
	}
	rowsA, err := crmRepo.List(ctx, a.ID, "")
	if err != nil {
		t.Fatalf("list for A: %v", err)
	}
	found := false
	for _, r := range rowsA {
		if r.CustomerID == customerID {
			found = true
		}
	}
	if !found {
		t.Error("expected business A's CRM list to include the customer it just saw")
	}

	rowsB, err := crmRepo.List(ctx, b.ID, "")
	if err != nil {
		t.Fatalf("list for B: %v", err)
	}
	for _, r := range rowsB {
		if r.CustomerID == customerID {
			t.Error("business B's CRM list must not include a customer who only interacted with business A")
		}
	}

	// Business B can never update a CRM row it doesn't own.
	if _, err := crmRepo.Update(ctx, b.ID, customerID, crm.UpdateInput{LeadStatus: "CONTACTED"}); err == nil {
		t.Error("expected business B updating business A's customer relationship to fail")
	}
}

func TestAssistantNeverLeaksAnotherBusinessesCatalog(t *testing.T) {
	ctx := context.Background()
	a := createBusiness(t, "iso-ai-a-"+randomSuffix(), "AI Isolation A")
	b := createBusiness(t, "iso-ai-b-"+randomSuffix(), "AI Isolation B")

	unique := "Zxquinolone" + randomSuffix()
	productID := insertProduct(t, unique+" Tablets", "T-AIISO-"+randomSuffix(), "t-aiiso-"+randomSuffix(), 4000, 5000, false)
	if err := catalogRepo.SetListing(ctx, a.ID, productID, catalog.ListingInput{IsListed: true, IsVisible: true}); err != nil {
		t.Fatalf("list for A: %v", err)
	}
	// Deliberately NOT listed for B.

	replyA, err := assistantSvc.Respond(ctx, a.ID, "", "do you have "+unique+" in stock")
	if err != nil {
		t.Fatalf("respond for A: %v", err)
	}
	if !strings.Contains(replyA.Text, unique) {
		t.Errorf("business A reply = %q, want it to find the product it lists", replyA.Text)
	}

	replyB, err := assistantSvc.Respond(ctx, b.ID, "", "do you have "+unique+" in stock")
	if err != nil {
		t.Fatalf("respond for B: %v", err)
	}
	if strings.Contains(replyB.Text, unique) {
		t.Errorf("business B reply = %q, must not surface a product only business A lists", replyB.Text)
	}
}

func availableQtyFor(ctx context.Context, businessID, productID string) (int, error) {
	var qty int
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(qty_on_hand - qty_reserved), 0)
		  FROM inventory_batches
		 WHERE business_id = $1 AND product_id = $2 AND is_blocked = FALSE AND expiry_date > CURRENT_DATE`,
		businessID, productID).Scan(&qty)
	return qty, err
}

func randomDigits(n int) string {
	batchSeq++
	s := itoa(batchSeq)
	for len(s) < n {
		s = "0" + s
	}
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}
