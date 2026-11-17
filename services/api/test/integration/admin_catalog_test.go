package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/inventory"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

func TestAdminProductCreateUpdateDeactivate(t *testing.T) {
	ctx := context.Background()
	sku := "T-ADM-" + randomSuffix()

	created, err := catalogRepo.AdminCreate(ctx, catalog.AdminProductInput{
		SKU: sku, Slug: "t-admin-product-" + randomSuffix(), Name: "Admin Test Product",
		MRPPaise: 10000, PricePaise: 8000, MaxQtyPerOrder: 5, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.IsActive {
		t.Error("new product should be active by default per the input")
	}

	// A public List() must never surface it to a customer once deactivated,
	// but the admin list (IncludeInactive) and GetByIDAdmin always can.
	updated, err := catalogRepo.AdminUpdate(ctx, created.ID, catalog.AdminProductInput{
		SKU: sku, Slug: created.Slug, Name: "Admin Test Product (updated)",
		MRPPaise: 10000, PricePaise: 7500, MaxQtyPerOrder: 5, IsActive: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.IsActive {
		t.Error("expected product to be inactive after update")
	}
	if updated.PricePaise != 7500 {
		t.Errorf("price after update = %d, want 7500", updated.PricePaise)
	}

	// Public GetByID (used by the storefront PDP) must not resolve an
	// inactive product.
	if _, err := catalogRepo.GetByID(ctx, created.ID); err == nil {
		t.Error("expected GetByID to fail for a deactivated product (customer-facing lookup)")
	}
	// But the admin-facing lookup must still see it, for the edit form.
	if _, err := catalogRepo.GetByIDAdmin(ctx, created.ID); err != nil {
		t.Errorf("GetByIDAdmin should still resolve a deactivated product: %v", err)
	}

	if err := catalogRepo.AdminSetActive(ctx, created.ID, true); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	reactivated, err := catalogRepo.GetByIDAdmin(ctx, created.ID)
	if err != nil || !reactivated.IsActive {
		t.Errorf("expected product active again, got %+v, err=%v", reactivated, err)
	}
}

func TestAdminCreateRejectsDuplicateSKU(t *testing.T) {
	ctx := context.Background()
	sku := "T-DUP-" + randomSuffix()
	in := catalog.AdminProductInput{
		SKU: sku, Slug: "t-dup-a-" + randomSuffix(), Name: "Dup A",
		MRPPaise: 1000, PricePaise: 900, MaxQtyPerOrder: 5, IsActive: true,
	}
	if _, err := catalogRepo.AdminCreate(ctx, in); err != nil {
		t.Fatalf("first create: %v", err)
	}
	in.Slug = "t-dup-b-" + randomSuffix() // same SKU, different slug
	if _, err := catalogRepo.AdminCreate(ctx, in); err == nil {
		t.Error("expected a validation error for a duplicate SKU, got none")
	}
}

func TestInventoryReceiveAndBlockBatch(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Batch Admin Test", "T-BATCH-ADM", "t-batch-admin-"+randomSuffix(), 1000, 1200, false)

	batch, err := inventory.Receive(ctx, pool, inventory.ReceiveInput{
		BusinessID: tenancy.DefaultBusinessID, ProductID: productID, BatchNumber: "LOT-" + randomSuffix(),
		ExpiryDate: time.Now().AddDate(1, 0, 0), Qty: 40,
	})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if batch.QtyOnHand != 40 || batch.QtyAvailable != 40 {
		t.Errorf("batch qty = %+v, want on-hand/available 40", batch)
	}

	avail, err := inventory.AvailableQty(ctx, pool, tenancy.DefaultBusinessID, productID)
	if err != nil || avail != 40 {
		t.Fatalf("AvailableQty = %d, %v, want 40", avail, err)
	}

	if err := inventory.SetBlocked(ctx, pool, tenancy.DefaultBusinessID, batch.ID, true, strPtr("QA hold")); err != nil {
		t.Fatalf("block: %v", err)
	}
	avail, err = inventory.AvailableQty(ctx, pool, tenancy.DefaultBusinessID, productID)
	if err != nil || avail != 0 {
		t.Fatalf("AvailableQty after block = %d, %v, want 0 (a blocked batch must not be sellable)", avail, err)
	}

	batches, err := inventory.ListBatchesForProduct(ctx, pool, tenancy.DefaultBusinessID, productID)
	if err != nil || len(batches) != 1 || !batches[0].IsBlocked {
		t.Fatalf("ListBatchesForProduct = %+v, %v", batches, err)
	}

	if err := inventory.SetBlocked(ctx, pool, tenancy.DefaultBusinessID, batch.ID, false, nil); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	avail, err = inventory.AvailableQty(ctx, pool, tenancy.DefaultBusinessID, productID)
	if err != nil || avail != 40 {
		t.Fatalf("AvailableQty after unblock = %d, %v, want 40", avail, err)
	}
}

func TestInventoryReceiveRejectsPastExpiry(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Expired Receive Test", "T-EXP-RCV", "t-exp-rcv-"+randomSuffix(), 1000, 1200, false)
	_, err := inventory.Receive(ctx, pool, inventory.ReceiveInput{
		BusinessID: tenancy.DefaultBusinessID, ProductID: productID, BatchNumber: "LOT-" + randomSuffix(),
		ExpiryDate: time.Now().AddDate(0, 0, -1), Qty: 10,
	})
	if err == nil {
		t.Error("expected an error receiving a batch with a past expiry date")
	}
}

func strPtr(s string) *string { return &s }
