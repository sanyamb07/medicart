// Package integration_test exercises the critical money- and stock-affecting
// paths against a real, throwaway PostgreSQL database — the thing unit tests
// with mocked repositories cannot catch: a wrong SQL type, a missing FK, a
// race two goroutines can actually hit under row locks.
//
// It targets its own database ("medicart_test" by default, on the same
// Postgres server docker-compose starts for development) so it never touches
// development data, and (re)creates it fresh on every run.
//
// If no Postgres is reachable, every test in this package is skipped rather
// than failed — `make test-api` always starts one first, but a bare `go test
// ./...` without Docker running should not look like a code failure.
package integration_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sanyam/medicart/services/api/internal/affiliate"
	"github.com/sanyam/medicart/services/api/internal/assistant"
	"github.com/sanyam/medicart/services/api/internal/audit"
	authpkg "github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/cart"
	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/crm"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/enquiries"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/payments"
	"github.com/sanyam/medicart/services/api/internal/prescriptions"
	"github.com/sanyam/medicart/services/api/internal/shipping"
	"github.com/sanyam/medicart/services/api/internal/storage"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

const (
	adminURLDefault = "postgres://medicart:medicart_dev_pw@localhost:55432/postgres?sslmode=disable"
	testDBName      = "medicart_test"
)

var (
	pool            *db.Pool
	cartSvc         *cart.Service
	shipSvc         *shipping.Service
	orderSvc        *orders.Service
	paymentSvc      *payments.Service
	prescriptionSvc *prescriptions.Service
	assistantSvc    *assistant.Service
	catalogRepo     *catalog.Repo
	bizRepo         *tenancy.Repo
	crmRepo         *crm.Repo
	enquiryRepo     *enquiries.Repo
	rec             *audit.Recorder

	testWebhookSecret = []byte("test-only-webhook-secret-0000000000000000")
	staffUserID       string
)

func adminURL() string {
	if v := os.Getenv("MEDICART_TEST_ADMIN_DATABASE_URL"); v != "" {
		return v
	}
	return adminURLDefault
}

func testURL() string {
	if v := os.Getenv("MEDICART_TEST_DATABASE_URL"); v != "" {
		return v
	}
	return fmt.Sprintf("postgres://medicart:medicart_dev_pw@localhost:55432/%s?sslmode=disable", testDBName)
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminURL())
	if err != nil {
		fmt.Fprintln(os.Stderr, "skip: cannot reach postgres:", err)
		os.Exit(0)
	}
	if err := admin.Ping(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "skip: postgres not reachable:", err)
		os.Exit(0)
	}

	// Recreate the test database from scratch every run, so tests never
	// depend on, or leak into, state from a previous run.
	if _, err := admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, testDBName)); err != nil {
		fmt.Fprintln(os.Stderr, "drop test db:", err)
		os.Exit(1)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, testDBName)); err != nil {
		fmt.Fprintln(os.Stderr, "create test db:", err)
		os.Exit(1)
	}
	admin.Close()

	if _, _, err := db.MigrateUp(testURL()); err != nil {
		fmt.Fprintln(os.Stderr, "migrate test db:", err)
		os.Exit(1)
	}

	p, err := db.Connect(ctx, db.Config{URL: testURL()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect test db:", err)
		os.Exit(1)
	}
	pool = p

	rec = audit.New(pool)
	cartSvc = cart.NewService(pool)
	shipSvc = shipping.NewService(pool, 4900, 49900)
	orderSvc = orders.NewService(pool, cartSvc, shipSvc, false) // FEATURE_RX_SALES_ENABLED=false
	authSvc := authpkg.NewService(pool, time.Hour, true)
	affiliateSvc := affiliate.NewService(pool, authSvc)
	paymentSvc = payments.NewService(pool, rec, "mock", testWebhookSecret, "http://localhost:5173", affiliateSvc)

	store, err := storage.NewLocal(os.TempDir()+"/medicart-test-storage", "http://localhost:8080/files", testWebhookSecret)
	if err != nil {
		fmt.Fprintln(os.Stderr, "init test storage:", err)
		os.Exit(1)
	}
	prescriptionSvc = prescriptions.NewService(pool, store, 10<<20, 10*time.Minute)

	catalogRepo = catalog.NewRepo(pool)
	bizRepo = tenancy.NewRepo(pool)
	crmRepo = crm.NewRepo(pool)
	enquiryRepo = enquiries.NewRepo(pool)
	assistantSvc = assistant.NewService(catalogRepo, shipSvc, orderSvc, crmRepo, enquiryRepo)

	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, is_active) VALUES ('teststaff@example.com', 'x', TRUE)
		RETURNING id::text`).Scan(&staffUserID); err != nil {
		fmt.Fprintln(os.Stderr, "seed staff user:", err)
		os.Exit(1)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO serviceable_pin_codes (pin_code, city, state, is_serviceable, eta_min_days, eta_max_days)
		VALUES ('400001', 'Mumbai', 'Maharashtra', TRUE, 1, 2)`); err != nil {
		fmt.Fprintln(os.Stderr, "seed pin code:", err)
		os.Exit(1)
	}

	code := m.Run()
	pool.Close()
	os.Exit(code)
}
