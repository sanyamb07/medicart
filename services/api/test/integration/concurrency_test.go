package integration_test

import (
	"context"
	"sync"
	"testing"

	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

// TestNoOversellUnderConcurrentCheckout is the test that actually justifies
// using a real database instead of mocks: five units of stock, two customers
// each trying to buy three, at the same instant. Without row locking both
// could read "5 available" and both succeed, overselling by one. With FEFO
// reservation under SELECT ... FOR UPDATE inside a serializable transaction
// (internal/inventory.ReserveFEFO via Pool.InSerializableTx), exactly one
// must succeed and the other must see out_of_stock.
func TestNoOversellUnderConcurrentCheckout(t *testing.T) {
	ctx := context.Background()
	productID := insertProduct(t, "Test Contested Stock", "T-RACE-1", "t-race-1", 2000, 2500, false)
	insertBatch(t, productID, 5, 365) // only 5 sellable units total

	const attempts = 2
	const qtyEach = 3 // 2 × 3 = 6 demanded against 5 available: at most one can win

	results := make([]error, attempts)
	var wg sync.WaitGroup
	wg.Add(attempts)

	for i := 0; i < attempts; i++ {
		// Cart creation happens before the goroutine starts: t.Fatalf must only
		// ever be called from the goroutine running the test function itself,
		// never from a spawned one, so no testing-package call happens inside
		// the goroutine below — only plain error values, collected afterwards.
		cartID, _, cartErr := cartSvc.GetOrCreateGuestCart(ctx, tenancy.DefaultBusinessID, "")
		if cartErr != nil {
			t.Fatalf("create cart %d: %v", i, cartErr)
		}
		if err := cartSvc.AddItem(ctx, cartID, productID, qtyEach); err != nil {
			t.Fatalf("add item for cart %d: %v", i, err)
		}

		go func(i int, cartID string) {
			defer wg.Done()
			_, err := orderSvc.Create(ctx, orders.CreateInput{
				CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "Race Test", ContactMobile: "98765432" + itoa(20+i),
				AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
			})
			results[i] = err
		}(i, cartID)
	}
	wg.Wait()

	succeeded, failed := 0, 0
	for _, err := range results {
		if err == nil {
			succeeded++
		} else {
			failed++
		}
	}

	if succeeded != 1 {
		t.Errorf("expected exactly 1 of %d concurrent checkouts to succeed (5 units, 3 each demanded), got %d succeeded, %d failed: %v",
			attempts, succeeded, failed, results)
	}

	// The database must reflect reality regardless of what the Go layer
	// reported: reserved can never exceed on_hand (this is also a DB CHECK
	// constraint, so a violation here would mean the constraint itself failed).
	var onHand, reserved int
	if err := pool.QueryRow(ctx,
		`SELECT qty_on_hand, qty_reserved FROM inventory_batches WHERE product_id = $1`, productID).
		Scan(&onHand, &reserved); err != nil {
		t.Fatalf("read batch: %v", err)
	}
	if reserved > onHand {
		t.Fatalf("oversold: reserved=%d > on_hand=%d", reserved, onHand)
	}
	if reserved != qtyEach {
		t.Errorf("expected exactly %d reserved (one winning order), got %d", qtyEach, reserved)
	}
}
