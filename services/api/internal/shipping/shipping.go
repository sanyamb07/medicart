// Package shipping checks PIN code serviceability and computes the delivery
// fee. Both are config/data driven — no hard-coded guarantee about delivery
// time is made anywhere in this package.
package shipping

import (
	"context"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/platform/money"
)

type Area struct {
	PINCode          string `json:"pinCode"`
	City             string `json:"city"`
	State            string `json:"state"`
	IsServiceable    bool   `json:"isServiceable"`
	ETAMinDays       *int   `json:"etaMinDays"`
	ETAMaxDays       *int   `json:"etaMaxDays"`
	CODAvailable     bool   `json:"codAvailable"`
	DeliveryFeePaise int64  `json:"deliveryFeePaise"`
}

type Service struct {
	pool              *db.Pool
	defaultFeePaise   int64
	freeAboveFeePaise int64
}

func NewService(pool *db.Pool, defaultFeePaise, freeAboveFeePaise int64) *Service {
	return &Service{pool: pool, defaultFeePaise: defaultFeePaise, freeAboveFeePaise: freeAboveFeePaise}
}

// Check reports serviceability for a PIN code. A PIN absent from the table is
// reported as not serviceable — we never guess coverage.
func (s *Service) Check(ctx context.Context, pin string) (*Area, error) {
	var (
		a           Area
		feeOverride *int64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT pin_code, city, state, is_serviceable, eta_min_days, eta_max_days,
		       cod_available, delivery_fee_paise
		  FROM serviceable_pin_codes WHERE pin_code = $1`, pin).
		Scan(&a.PINCode, &a.City, &a.State, &a.IsServiceable, &a.ETAMinDays, &a.ETAMaxDays,
			&a.CODAvailable, &feeOverride)
	if db.IsNoRows(err) {
		return &Area{PINCode: pin, IsServiceable: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check pin code: %w", err)
	}
	a.DeliveryFeePaise = s.defaultFeePaise
	if feeOverride != nil {
		a.DeliveryFeePaise = *feeOverride
	}
	return &a, nil
}

// DeliveryFee computes the fee for an order subtotal and PIN area, applying
// the free-delivery threshold.
func (s *Service) DeliveryFee(subtotal money.Paise, area *Area) money.Paise {
	if int64(subtotal) >= s.freeAboveFeePaise {
		return 0
	}
	return money.Paise(area.DeliveryFeePaise)
}
