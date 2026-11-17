package integration_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/markets"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

// Tests for migration 000014: markets, per-market eligibility and pricing,
// enforced in the database. See docs/COUNTRY_FOUNDATION.md.

func sqlStateOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func businessInMarket(t *testing.T, market string) *tenancy.Business {
	t.Helper()
	b := createBusiness(t, "mk-"+market+"-"+randomSuffix(), "Market "+market+" Pharmacy")
	if _, err := pool.Exec(context.Background(), `UPDATE businesses SET market_code = $2 WHERE id = $1`, b.ID, market); err != nil {
		t.Fatalf("set market: %v", err)
	}
	return b
}

// openGBForTest makes GB a LIVE market with a recorded sign-off for the
// duration of one test, then restores it to DISABLED. This is test-only:
// production has no API that changes a market's status.
func openGBForTest(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE markets SET legal_basis = 'SIGNED_OFF', legal_signoff_by = 'integration test',
		legal_signoff_at = now(), status = 'LIVE' WHERE code = 'GB'`); err != nil {
		t.Fatalf("open GB: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE markets SET status = 'DISABLED', legal_basis = 'NONE',
			legal_signoff_by = NULL, legal_signoff_at = NULL WHERE code = 'GB'`); err != nil {
			t.Errorf("restore GB: %v", err)
		}
	})
}

func TestMarketsSeededIndiaOnlyLive(t *testing.T) {
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT code, status, is_default, rx_sales_enabled FROM markets ORDER BY code`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var code, status string
		var def, rx bool
		if err := rows.Scan(&code, &status, &def, &rx); err != nil {
			t.Fatal(err)
		}
		seen[code] = true
		if rx {
			t.Errorf("%s: Rx sales must be disabled in every market", code)
		}
		if code == "IN" && (status != "LIVE" || !def) {
			t.Errorf("IN must be the LIVE default, got status=%s default=%v", status, def)
		}
		if code != "IN" && (status != "DISABLED" || def) {
			t.Errorf("%s must be DISABLED and not default, got %s default=%v", code, status, def)
		}
	}
	for _, c := range []string{"IN", "US", "GB", "AU", "NZ", "AE"} {
		if !seen[c] {
			t.Errorf("market %s not seeded", c)
		}
	}
	b, err := bizRepo.GetByID(ctx, tenancy.DefaultBusinessID)
	if err != nil || b.MarketCode != "IN" {
		t.Fatalf("default business market = %v, %v; want IN", b, err)
	}
	live, err := markets.NewRepo(pool, rec).ListLive(ctx)
	if err != nil || len(live) != 1 || live[0].Code != "IN" || live[0].Currency != "INR" || live[0].LegalBasis != nil {
		t.Fatalf("public live markets = %+v, %v; want only IN with no legal fields", live, err)
	}
}

func TestNewProductAutoListedOnlyInIndia(t *testing.T) {
	id := insertProduct(t, "Auto List 5mg", "T-AUTO-"+randomSuffix(), "auto-list-"+randomSuffix(), 1000, 1200, false)
	var n int
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT count(*), max(status) FROM product_market_listings WHERE product_id = $1`, id).Scan(&n, &status); err != nil {
		t.Fatal(err)
	}
	if n != 1 || status != "ELIGIBLE" {
		t.Fatalf("listings = %d (%s); want exactly one ELIGIBLE (IN)", n, status)
	}
}

func TestMarketConstraintsFailClosed(t *testing.T) {
	ctx := context.Background()
	id := insertProduct(t, "Constraint 5mg", "T-CONS-"+randomSuffix(), "constraint-"+randomSuffix(), 1000, 1200, false)
	cases := []struct{ name, sql string }{
		{"open a market without legal basis", `UPDATE markets SET status = 'LIVE' WHERE code = 'US'`},
		{"browse-only without legal basis", `UPDATE markets SET status = 'BROWSE_ONLY' WHERE code = 'AE'`},
		{"sign-off without signer", `UPDATE markets SET legal_basis = 'SIGNED_OFF' WHERE code = 'US'`},
		{"Rx sales on a grandfathered market", `UPDATE markets SET rx_sales_enabled = TRUE WHERE code = 'IN'`},
		{"default on a disabled market", `UPDATE markets SET is_default = TRUE WHERE code = 'NZ'`},
		{"eligible without basis", `INSERT INTO product_market_listings (product_id, market_code, status, currency) VALUES ('` + id + `', 'AU', 'ELIGIBLE', 'AUD')`},
		{"listing in the wrong currency", `INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency) VALUES ('` + id + `', 'US', 'ELIGIBLE', 'x', 'INR')`},
		{"price above MRP", `INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency, price_minor, mrp_minor) VALUES ('` + id + `', 'NZ', 'ELIGIBLE', 'x', 'NZD', 500, 400)`},
		{"price without MRP", `INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency, price_minor) VALUES ('` + id + `', 'NZ', 'ELIGIBLE', 'x', 'NZD', 500)`},
		{"relaxing a prescription requirement", `UPDATE product_market_listings SET rx_required_override = FALSE WHERE product_id = '` + id + `'`},
	}
	for _, c := range cases {
		if _, err := pool.Exec(ctx, c.sql); err == nil {
			t.Errorf("%s: accepted, want rejection", c.name)
		}
	}
}

func TestDisabledMarketBusinessCannotSellAnything(t *testing.T) {
	ctx := context.Background()
	us := businessInMarket(t, "US")
	slug := "iso-us-" + randomSuffix()
	id := insertProduct(t, "Isolation 5mg", "T-ISO-"+randomSuffix(), slug, 1000, 1200, false)
	listProductForBusiness(t, us.ID, id)
	insertBatchForBusiness(t, us.ID, id, 20, 365)

	list, total, err := catalogRepo.ListForBusiness(ctx, us.ID, catalog.ListFilter{})
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("US storefront lists %d products (err %v); want none", total, err)
	}
	if _, err := catalogRepo.GetBySlugForBusiness(ctx, us.ID, slug); !db.IsNoRows(err) {
		t.Fatalf("US product page: %v; want not found", err)
	}
	cartID, _, err := cartSvc.GetOrCreateGuestCart(ctx, us.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cartSvc.AddItem(ctx, cartID, id, 1); err == nil {
		t.Fatal("cart service added a product in a disabled market")
	}
	// Database guard, bypassing the application entirely.
	_, err = pool.Exec(ctx, `INSERT INTO cart_items (cart_id, product_id, qty) VALUES ($1, $2, 1)`, cartID, id)
	if sqlStateOf(err) != "MK001" {
		t.Fatalf("direct cart insert: %v; want MK001", err)
	}

	// The same product is still sold by the Indian business, at its INR price.
	p, err := catalogRepo.GetBySlugForBusiness(ctx, tenancy.DefaultBusinessID, slug)
	if err != nil || p.PricePaise != 1000 || p.MRPPaise != 1200 {
		t.Fatalf("India product: %+v, %v", p, err)
	}
}

func TestIneligibleProductBlockedInIndiaEvenIfActiveAndListed(t *testing.T) {
	ctx := context.Background()
	slug := "not-human-" + randomSuffix()
	id := insertProduct(t, "Not For Humans 5mg", "T-NH-"+randomSuffix(), slug, 1000, 1200, false)
	insertBatch(t, id, 20, 365)
	cartID := newCart(t)
	if err := cartSvc.AddItem(ctx, cartID, id, 1); err != nil {
		t.Fatalf("add while eligible: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE product_market_listings SET status = 'NOT_ELIGIBLE', eligibility_basis = 'test' WHERE product_id = $1 AND market_code = 'IN'`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogRepo.GetBySlugForBusiness(ctx, tenancy.DefaultBusinessID, slug); !db.IsNoRows(err) {
		t.Fatalf("ineligible product still visible: %v", err)
	}
	// The line already in the cart is flagged, and checkout refuses it.
	sum, err := cartSvc.Get(ctx, cartID)
	if err != nil || len(sum.Lines) != 1 || sum.Lines[0].IsActive || !sum.HasIssues {
		t.Fatalf("cart line not flagged: %+v %v", sum, err)
	}
	_, err = orderSvc.Create(ctx, orders.CreateInput{
		CartID: cartID, BusinessID: tenancy.DefaultBusinessID, RecipientName: "X", ContactMobile: "9876500001",
		AddressLine1: "1 Test Street", City: "Mumbai", State: "Maharashtra", PINCode: "400001",
	})
	var ae *httpx.APIError
	if !errors.As(err, &ae) || ae.Code != "out_of_stock" {
		t.Fatalf("checkout of an ineligible product: %v; want refusal", err)
	}
}

func TestOrderItemGuardsBlockIneligibleAndRx(t *testing.T) {
	ctx := context.Background()
	otc := insertProduct(t, "Guard OTC", "T-GO-"+randomSuffix(), "guard-otc-"+randomSuffix(), 1000, 1200, false)
	rx := insertProduct(t, "Guard Rx", "T-GR-"+randomSuffix(), "guard-rx-"+randomSuffix(), 1000, 1200, true)
	customer := insertCustomer(t, "98765"+randomDigits(5))
	var orderID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO orders (reference, customer_id, business_id, status, recipient_name, contact_mobile,
		                    address_line1, city, state, pin_code, subtotal_paise, total_paise)
		VALUES ($1, $2, $3, 'CREATED', 'X', '9876500002', '1', 'Mumbai', 'MH', '400001', 0, 0)
		RETURNING id::text`, "T-"+randomSuffix(), customer, tenancy.DefaultBusinessID).Scan(&orderID); err != nil {
		t.Fatalf("order: %v", err)
	}
	ins := func(pid string, rxFlag bool) error {
		_, err := pool.Exec(ctx, `INSERT INTO order_items (order_id, product_id, product_name, product_sku, qty,
			unit_mrp_paise, unit_price_paise, line_total_paise, requires_prescription)
			VALUES ($1, $2, 'x', 'x', 1, 1200, 1000, 1000, $3)`, orderID, pid, rxFlag)
		return err
	}
	if err := ins(otc, false); err != nil {
		t.Fatalf("eligible OTC line rejected: %v", err)
	}
	if err := ins(rx, false); sqlStateOf(err) != "MK002" {
		t.Fatalf("Rx line (snapshot flag lying) = %v; want MK002", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE product_market_listings SET status = 'UNKNOWN' WHERE product_id = $1`, otc); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := ins(otc, false); sqlStateOf(err) != "MK001" {
		t.Fatalf("UNKNOWN-eligibility line = %v; want MK001", err)
	}
	// An order recorded in a currency other than its market's is refused.
	if _, err := pool.Exec(ctx, `UPDATE product_market_listings SET status = 'ELIGIBLE' WHERE product_id = $1`, otc); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE orders DROP CONSTRAINT orders_currency_inr`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `UPDATE orders SET currency = 'INR' WHERE id = $1`, orderID)
		if _, err := pool.Exec(context.Background(), `ALTER TABLE orders ADD CONSTRAINT orders_currency_inr CHECK (currency = 'INR')`); err != nil {
			t.Errorf("restore constraint: %v", err)
		}
	}()
	if _, err := pool.Exec(ctx, `UPDATE orders SET currency = 'GBP' WHERE id = $1`, orderID); err != nil {
		t.Fatal(err)
	}
	if err := ins(otc, false); sqlStateOf(err) != "MK003" {
		t.Fatalf("currency-mismatched line = %v; want MK003", err)
	}
}

func TestRegionalPricingIsIndependentPerMarket(t *testing.T) {
	ctx := context.Background()
	openGBForTest(t)
	gb := businessInMarket(t, "GB")
	slug := "regional-" + randomSuffix()
	id := insertProduct(t, "Regional 5mg", "T-REG-"+randomSuffix(), slug, 2800, 3500, false)
	listProductForBusiness(t, gb.ID, id)
	insertBatchForBusiness(t, gb.ID, id, 20, 365)
	repo := markets.NewRepo(pool, rec)

	// Live GB market, but no GB listing yet → UNKNOWN → not sellable.
	if _, err := catalogRepo.GetBySlugForBusiness(ctx, gb.ID, slug); !db.IsNoRows(err) {
		t.Fatalf("product without a GB listing is visible: %v", err)
	}
	// Eligible without a GBP price must be refused (cannot inherit INR).
	if _, err := repo.SetListing(ctx, staffUserID, id, "GB", markets.ListingInput{Status: "ELIGIBLE", EligibilityBasis: "test MA ref"}); err == nil {
		t.Fatal("GB listing without a GBP price was accepted")
	}
	// And even if written directly, the view keeps it unsellable.
	if _, err := pool.Exec(ctx, `INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency)
		VALUES ($1, 'GB', 'ELIGIBLE', 'direct', 'GBP')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogRepo.GetBySlugForBusiness(ctx, gb.ID, slug); !db.IsNoRows(err) {
		t.Fatalf("GB listing with no GBP price is sellable: %v", err)
	}

	price, mrp := int64(499), int64(599)
	l, err := repo.SetListing(ctx, staffUserID, id, "gb", markets.ListingInput{Status: "ELIGIBLE", EligibilityBasis: "test MA ref", PriceMinor: &price, MRPMinor: &mrp})
	if err != nil || l.Currency != "GBP" || *l.PriceMinor != 499 {
		t.Fatalf("set GB listing: %+v %v", l, err)
	}
	p, err := catalogRepo.GetBySlugForBusiness(ctx, gb.ID, slug)
	if err != nil || p.PricePaise != 499 || p.MRPPaise != 599 {
		t.Fatalf("GB price: %+v %v; want 499/599", p, err)
	}
	inP, err := catalogRepo.GetBySlugForBusiness(ctx, tenancy.DefaultBusinessID, slug)
	if err != nil || inP.PricePaise != 2800 || inP.MRPPaise != 3500 {
		t.Fatalf("India price changed: %+v %v; want 2800/3500", inP, err)
	}
	// Business override stays per business.
	if _, err := pool.Exec(ctx, `UPDATE business_products SET price_paise = 450 WHERE business_id = $1 AND product_id = $2`, gb.ID, id); err != nil {
		t.Fatal(err)
	}
	if p, _ := catalogRepo.GetBySlugForBusiness(ctx, gb.ID, slug); p == nil || p.PricePaise != 450 {
		t.Fatalf("GB business override not applied: %+v", p)
	}
	// A GB cart prices in GBP minor units.
	cartID, _, err := cartSvc.GetOrCreateGuestCart(ctx, gb.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cartSvc.AddItem(ctx, cartID, id, 2); err != nil {
		t.Fatalf("GB add: %v", err)
	}
	if sum, err := cartSvc.Get(ctx, cartID); err != nil || sum.SubtotalPaise != 900 {
		t.Fatalf("GB cart subtotal: %+v %v; want 900", sum, err)
	}
	// The change is audited.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'market.listing_set' AND entity_id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows = %d, %v; want 1", n, err)
	}
}

func TestReactivatingNotApprovedProductStaysUnsellable(t *testing.T) {
	ctx := context.Background()
	slug := "react-" + randomSuffix()
	id := insertProduct(t, "Reactivate 5mg", "T-REA-"+randomSuffix(), slug, 1000, 1200, false)
	if _, err := pool.Exec(ctx, `UPDATE products SET is_active = FALSE WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE product_market_listings SET status = 'NOT_ELIGIBLE', eligibility_basis = 'Supplier list: not approved for human use' WHERE product_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET is_active = TRUE WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogRepo.GetBySlugForBusiness(ctx, tenancy.DefaultBusinessID, slug); !db.IsNoRows(err) {
		t.Fatalf("re-activated NOT_ELIGIBLE product is on sale: %v", err)
	}
}

func TestMarketListingAPIValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := markets.NewHandlers(markets.NewRepo(pool, rec))
	h.Register(r.Group("/public"))
	admin := r.Group("/admin", func(c *gin.Context) {
		c.Set(httpx.CtxUserID, staffUserID)
		c.Set(httpx.CtxUserRoles, []string{httpx.RolePlatformSuperAdmin})
	})
	h.RegisterPlatformAdmin(admin)
	id := insertProduct(t, "API 5mg", "T-API-"+randomSuffix(), "api-"+randomSuffix(), 1000, 1200, false)

	if code, body := call(t, r, http.MethodGet, "/public/markets", nil); code != 200 || len(body["data"].([]any)) != 1 {
		t.Fatalf("public markets: %d %v", code, body)
	}
	code, body := call(t, r, http.MethodGet, "/admin/products/"+id+"/markets", nil)
	if code != 200 || len(body["data"].([]any)) != 6 {
		t.Fatalf("listings: %d %v", code, body)
	}
	for _, it := range body["data"].([]any) {
		m := it.(map[string]any)
		want := "UNKNOWN"
		if m["marketCode"] == "IN" {
			want = "ELIGIBLE"
		}
		if m["status"] != want {
			t.Errorf("%v status %v, want %s", m["marketCode"], m["status"], want)
		}
	}
	bad := []map[string]any{
		{"status": "ELIGIBLE"},                                             // no basis
		{"status": "MAYBE", "eligibilityBasis": "x"},                       // bad status
		{"status": "ELIGIBLE", "eligibilityBasis": "x", "priceMinor": 100}, // price without MRP
		{"status": "ELIGIBLE", "eligibilityBasis": "x", "rxRequiredOverride": false},
	}
	for _, b := range bad {
		if code, _ := call(t, r, http.MethodPut, "/admin/products/"+id+"/markets/AU", b); code != http.StatusUnprocessableEntity {
			t.Errorf("%v accepted with %d", b, code)
		}
	}
	if code, _ := call(t, r, http.MethodPut, "/admin/products/not-a-uuid/markets/AU", map[string]any{"status": "UNKNOWN"}); code != 404 {
		t.Errorf("bad product id = %d, want 404", code)
	}
	if code, _ := call(t, r, http.MethodPut, "/admin/products/"+id+"/markets/ZZ", map[string]any{"status": "UNKNOWN"}); code != 404 {
		t.Errorf("unknown market = %d, want 404", code)
	}
	// Making a product prescription-only in one market is allowed.
	if code, body := call(t, r, http.MethodPut, "/admin/products/"+id+"/markets/IN",
		map[string]any{"status": "ELIGIBLE", "eligibilityBasis": "review", "rxRequiredOverride": true}); code != 200 {
		t.Fatalf("stricter override: %d %v", code, body)
	}
	var rxNow bool
	_ = pool.QueryRow(context.Background(), `SELECT requires_prescription FROM storefront_listings WHERE product_id = $1 AND business_id = $2`, id, tenancy.DefaultBusinessID).Scan(&rxNow)
	if !rxNow {
		t.Error("market Rx override not reflected in storefront_listings")
	}
}
