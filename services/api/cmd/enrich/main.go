// Command enrich runs the medicine-information pipeline over the catalogue.
// Resumable: stop it at any time and run it again; finished products are
// skipped and cached source lookups are not repeated.
//
//	go run ./cmd/enrich              # everything still pending
//	go run ./cmd/enrich -limit 200   # at most 200 products
//	go run ./cmd/enrich -requeue-all # reprocess everything not yet verified or edited by a person
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sanyam/medicart/services/api/internal/config"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/enrichment"
)

func main() {
	limit := flag.Int("limit", 100000, "maximum products to process")
	batch := flag.Int("batch", 25, "products per batch")
	requeue := flag.Bool("requeue-all", false, "reset every product to pending first (keeps the source cache)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "enrich:", err)
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, db.Config{URL: cfg.DatabaseURL})
	if err != nil {
		fmt.Fprintln(os.Stderr, "enrich: connect:", err)
		os.Exit(1)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `INSERT INTO product_enrichment (product_id) SELECT id FROM products ON CONFLICT DO NOTHING`); err != nil {
		fmt.Fprintln(os.Stderr, "enrich:", err)
		os.Exit(1)
	}
	if *requeue {
		if _, err := pool.Exec(ctx, `UPDATE product_enrichment SET status='pending', attempts=0 WHERE last_verified_at IS NULL AND edited_at IS NULL`); err != nil {
			fmt.Fprintln(os.Stderr, "enrich:", err)
			os.Exit(1)
		}
	}

	svc := enrichment.NewService(pool, os.Getenv("OPENFDA_API_KEY"))
	start := time.Now()
	res, err := svc.Run(ctx, *limit, *batch, func(r enrichment.RunResult) {
		fmt.Printf("[%s] processed=%d completed=%d partial=%d needs_review=%d failed=%d\n",
			time.Since(start).Round(time.Second), r.Processed, r.Completed, r.Partial, r.NeedsReview, r.Failed)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "enrich:", err)
		os.Exit(1)
	}
	fmt.Printf("done: %+v in %s\n", res, time.Since(start).Round(time.Second))
	if res.RateLimited {
		fmt.Println("stopped early: the source's request quota was reached. Run again later, or set OPENFDA_API_KEY (free) for a higher quota.")
	}
}
