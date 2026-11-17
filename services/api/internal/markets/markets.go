// Package markets holds country configuration and per-market product
// eligibility (migration 000014, docs/COUNTRY_FOUNDATION.md).
//
// What is deliberately NOT here: changing a market's status, legal basis or
// Rx setting. Opening a country requires a recorded legal sign-off, and the
// database constraints refuse those changes without one; there is no API
// that flips them.
package markets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sanyam/medicart/services/api/internal/audit"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

const DefaultCode = "IN"

type Market struct {
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	Currency         string   `json:"currency"`
	CurrencyMinor    int      `json:"currencyMinorUnits"`
	CurrencySymbol   string   `json:"currencySymbol"`
	DefaultLocale    string   `json:"defaultLocale"`
	SupportedLocales []string `json:"supportedLocales"`
	TaxDisplay       string   `json:"taxDisplay"`
	Status           string   `json:"status"`
	IsDefault        bool     `json:"isDefault"`
	// Admin-only fields; nil in the public listing.
	LegalBasis     *string    `json:"legalBasis,omitempty"`
	LegalSignoffBy *string    `json:"legalSignoffBy,omitempty"`
	LegalSignoffAt *time.Time `json:"legalSignoffAt,omitempty"`
	LegalNotes     *string    `json:"legalNotes,omitempty"`
	RxSalesEnabled *bool      `json:"rxSalesEnabled,omitempty"`
}

type Listing struct {
	MarketCode         string     `json:"marketCode"`
	MarketStatus       string     `json:"marketStatus"`
	Currency           string     `json:"currency"`
	Status             string     `json:"status"` // ELIGIBLE | NOT_ELIGIBLE | UNKNOWN (no row = UNKNOWN)
	EligibilityBasis   *string    `json:"eligibilityBasis"`
	PriceMinor         *int64     `json:"priceMinor"` // nil = inherits master price when currencies match
	MRPMinor           *int64     `json:"mrpMinor"`
	RxRequiredOverride *bool      `json:"rxRequiredOverride"`
	ReviewedBy         *string    `json:"reviewedBy"`
	ReviewedAt         *time.Time `json:"reviewedAt"`
}

type Repo struct {
	pool *db.Pool
	rec  *audit.Recorder
}

func NewRepo(pool *db.Pool, rec *audit.Recorder) *Repo { return &Repo{pool: pool, rec: rec} }

// ListLive returns markets customers can shop in, default first.
func (r *Repo) ListLive(ctx context.Context) ([]*Market, error) {
	return r.list(ctx, false, "WHERE m.status = 'LIVE'")
}

// ListAll returns every configured market with its legal fields (admin).
func (r *Repo) ListAll(ctx context.Context) ([]*Market, error) {
	return r.list(ctx, true, "")
}

func (r *Repo) list(ctx context.Context, admin bool, where string) ([]*Market, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.code, m.name, m.default_currency, c.minor_units, c.symbol, m.default_locale,
		       m.supported_locales, m.tax_display, m.status, m.is_default,
		       m.legal_basis, m.legal_signoff_by, m.legal_signoff_at, m.legal_notes, m.rx_sales_enabled
		  FROM markets m JOIN currencies c ON c.code = m.default_currency `+where+`
		 ORDER BY m.is_default DESC, m.name`)
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	defer rows.Close()
	out := []*Market{}
	for rows.Next() {
		var m Market
		var basis string
		var by, notes *string
		var at *time.Time
		var rx bool
		if err := rows.Scan(&m.Code, &m.Name, &m.Currency, &m.CurrencyMinor, &m.CurrencySymbol, &m.DefaultLocale,
			&m.SupportedLocales, &m.TaxDisplay, &m.Status, &m.IsDefault, &basis, &by, &at, &notes, &rx); err != nil {
			return nil, err
		}
		if admin {
			m.LegalBasis, m.LegalSignoffBy, m.LegalSignoffAt, m.LegalNotes, m.RxSalesEnabled = &basis, by, at, notes, &rx
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// ProductListings returns one entry per configured market for productID.
// A market with no listing row is reported as UNKNOWN (not sellable).
func (r *Repo) ProductListings(ctx context.Context, productID string) ([]*Listing, error) {
	if _, err := uuid.Parse(productID); err != nil {
		return nil, httpx.ErrNotFound.WithMessage("Product not found.")
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("product exists: %w", err)
	}
	if !exists {
		return nil, httpx.ErrNotFound.WithMessage("Product not found.")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.code, m.status, m.default_currency, COALESCE(l.status, 'UNKNOWN'), l.eligibility_basis,
		       l.price_minor, l.mrp_minor, l.rx_required_override, l.reviewed_by::text, l.reviewed_at
		  FROM markets m
		  LEFT JOIN product_market_listings l ON l.market_code = m.code AND l.product_id = $1
		 ORDER BY m.is_default DESC, m.name`, productID)
	if err != nil {
		return nil, fmt.Errorf("product listings: %w", err)
	}
	defer rows.Close()
	out := []*Listing{}
	for rows.Next() {
		var l Listing
		if err := rows.Scan(&l.MarketCode, &l.MarketStatus, &l.Currency, &l.Status, &l.EligibilityBasis,
			&l.PriceMinor, &l.MRPMinor, &l.RxRequiredOverride, &l.ReviewedBy, &l.ReviewedAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

type ListingInput struct {
	Status             string `json:"status"`
	EligibilityBasis   string `json:"eligibilityBasis"`
	PriceMinor         *int64 `json:"priceMinor"`
	MRPMinor           *int64 `json:"mrpMinor"`
	RxRequiredOverride *bool  `json:"rxRequiredOverride"`
}

// SetListing records a reviewer's eligibility decision and regional price
// for one product in one market, atomically with its audit entry. The
// currency is always the market's own; it cannot be supplied.
func (r *Repo) SetListing(ctx context.Context, actorID, productID, marketCode string, in ListingInput) (*Listing, error) {
	if _, err := uuid.Parse(productID); err != nil {
		return nil, httpx.ErrNotFound.WithMessage("Product not found.")
	}
	marketCode = strings.ToUpper(strings.TrimSpace(marketCode))
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))
	in.EligibilityBasis = strings.TrimSpace(in.EligibilityBasis)
	switch in.Status {
	case "ELIGIBLE", "NOT_ELIGIBLE", "UNKNOWN":
	default:
		return nil, httpx.ErrValidation.WithMessage("Status must be ELIGIBLE, NOT_ELIGIBLE or UNKNOWN.")
	}
	if in.Status == "ELIGIBLE" && in.EligibilityBasis == "" {
		return nil, httpx.ErrValidation.WithMessage("Record the basis for eligibility (e.g. the authorisation reference) before marking a product eligible.")
	}
	if (in.PriceMinor == nil) != (in.MRPMinor == nil) {
		return nil, httpx.ErrValidation.WithMessage("Set the regional price and MRP together, or neither.")
	}
	if in.PriceMinor != nil && (*in.PriceMinor <= 0 || *in.MRPMinor <= 0 || *in.PriceMinor > *in.MRPMinor) {
		return nil, httpx.ErrValidation.WithMessage("Regional price must be positive and not above MRP.")
	}
	if in.RxRequiredOverride != nil && !*in.RxRequiredOverride {
		return nil, httpx.ErrValidation.WithMessage("A market can only make a product stricter (prescription required), never less strict.")
	}

	err := r.pool.InTx(ctx, func(q db.Querier) error {
		var currency string
		if err := q.QueryRow(ctx, `SELECT default_currency FROM markets WHERE code = $1`, marketCode).Scan(&currency); err != nil {
			if db.IsNoRows(err) {
				return httpx.ErrNotFound.WithMessage("Unknown market.")
			}
			return err
		}
		var productCurrency string
		if err := q.QueryRow(ctx, `SELECT currency FROM products WHERE id = $1`, productID).Scan(&productCurrency); err != nil {
			if db.IsNoRows(err) {
				return httpx.ErrNotFound.WithMessage("Product not found.")
			}
			return err
		}
		if in.Status == "ELIGIBLE" && in.PriceMinor == nil && currency != productCurrency {
			return httpx.ErrValidation.WithMessage("Set a regional price in " + currency + " — this market cannot inherit the " + productCurrency + " master price.")
		}
		var before []byte
		_ = q.QueryRow(ctx, `SELECT to_jsonb(l) FROM product_market_listings l WHERE product_id = $1 AND market_code = $2`,
			productID, marketCode).Scan(&before)
		if _, err := q.Exec(ctx, `
			INSERT INTO product_market_listings (product_id, market_code, status, eligibility_basis, currency,
			                                     price_minor, mrp_minor, rx_required_override, reviewed_by, reviewed_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, now())
			ON CONFLICT (product_id, market_code) DO UPDATE SET
			  status = EXCLUDED.status, eligibility_basis = EXCLUDED.eligibility_basis,
			  price_minor = EXCLUDED.price_minor, mrp_minor = EXCLUDED.mrp_minor,
			  rx_required_override = EXCLUDED.rx_required_override,
			  reviewed_by = EXCLUDED.reviewed_by, reviewed_at = now()`,
			productID, marketCode, in.Status, in.EligibilityBasis, currency,
			in.PriceMinor, in.MRPMinor, in.RxRequiredOverride, actorID); err != nil {
			if db.IsCheckViolation(err) {
				return httpx.ErrValidation.WithMessage("That listing is not allowed (" + db.ConstraintName(err) + ").")
			}
			return fmt.Errorf("upsert listing: %w", err)
		}
		return r.rec.RecordTx(ctx, q, audit.Entry{
			ActorID: &actorID, ActorRole: httpx.RolePlatformSuperAdmin, Action: audit.ActionMarketListingSet,
			EntityType: "product", EntityID: productID,
			Metadata: map[string]any{
				"market": marketCode, "status": in.Status, "basis": in.EligibilityBasis,
				"priceMinor": in.PriceMinor, "mrpMinor": in.MRPMinor, "rxRequiredOverride": in.RxRequiredOverride,
				"before": jsonOrNil(before),
			},
		})
	})
	if err != nil {
		return nil, err
	}
	ls, err := r.ProductListings(ctx, productID)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		if l.MarketCode == marketCode {
			return l, nil
		}
	}
	return nil, httpx.ErrNotFound
}

func jsonOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
