package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

// insertProduct creates a minimal active product, lists it for sale on the
// default business at its own price (so every pre-multi-tenancy test keeps
// working unchanged), and returns its id.
func insertProduct(t *testing.T, name, sku, slug string, pricePaise, mrpPaise int64, requiresRx bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO products (sku, slug, name, mrp_paise, price_paise, requires_prescription,
		                       max_qty_per_order, is_demo_data, is_active, pack_size)
		VALUES ($1, $2, $3, $4, $5, $6, 10, TRUE, TRUE, 'Strip of 10')
		RETURNING id::text`, sku, slug, name, mrpPaise, pricePaise, requiresRx).Scan(&id)
	if err != nil {
		t.Fatalf("insert product: %v", err)
	}
	listProductForBusiness(t, tenancy.DefaultBusinessID, id)
	return id
}

// listProductForBusiness lists an existing product for sale on businessID at
// the master product's own price — the minimal business_products row a
// business-scoped query needs to find it at all.
func listProductForBusiness(t *testing.T, businessID, productID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO business_products (business_id, product_id, is_listed, is_visible)
		VALUES ($1, $2, TRUE, TRUE)
		ON CONFLICT (business_id, product_id) DO UPDATE SET is_listed = TRUE, is_visible = TRUE`,
		businessID, productID); err != nil {
		t.Fatalf("list product for business: %v", err)
	}
}

// insertBatch creates a stock batch for productID on the default business,
// expiring in expiryDays (negative = already expired, for testing exclusion).
func insertBatch(t *testing.T, productID string, qty, expiryDays int) string {
	t.Helper()
	return insertBatchForBusiness(t, tenancy.DefaultBusinessID, productID, qty, expiryDays)
}

// insertBatchForBusiness is insertBatch scoped to an explicit business — used
// by multi-tenant isolation tests that set up a second business's own stock.
func insertBatchForBusiness(t *testing.T, businessID, productID string, qty, expiryDays int) string {
	t.Helper()
	var id string
	expiry := time.Now().AddDate(0, 0, expiryDays)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO inventory_batches (business_id, product_id, batch_number, expiry_date, qty_received, qty_on_hand)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING id::text`, businessID, productID, "BATCH-"+randomSuffix(), expiry, qty).Scan(&id)
	if err != nil {
		t.Fatalf("insert batch: %v", err)
	}
	return id
}

// insertCustomer creates a user + customer_profiles pair and returns the
// customer_profiles id (what everything outside auth keys off of).
func insertCustomer(t *testing.T, mobile string) string {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (mobile, is_active) VALUES ($1, TRUE) RETURNING id::text`, mobile).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO customer_profiles (user_id) VALUES ($1) RETURNING id::text`, userID).Scan(&id); err != nil {
		t.Fatalf("insert customer profile: %v", err)
	}
	return id
}

var batchSeq int

func randomSuffix() string {
	batchSeq++
	return time.Now().Format("150405") + "-" + itoa(batchSeq)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
