// Package money handles currency amounts as integer minor units.
//
// Every amount in MediCart is an int64 count of the currency's minor unit
// (paise for INR). Floating-point types are never used for money: they cannot
// represent 0.10 exactly, and repeated addition of cents accumulates error that
// shows up as orders whose totals are off by a paisa.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Paise is an amount in the minor unit of the currency.
type Paise int64

// CurrencyINR is the only currency the platform is configured for today.
// The column exists on every money-bearing table so adding another is a data
// change rather than a schema change.
const CurrencyINR = "INR"

var ErrNegative = errors.New("money: amount must not be negative")

// FromRupees converts whole rupees to paise.
func FromRupees(rupees int64) Paise { return Paise(rupees * 100) }

// Rupees returns the whole-rupee part. Use only for display.
func (p Paise) Rupees() int64 { return int64(p) / 100 }

// Int64 returns the raw minor-unit value, for storage.
func (p Paise) Int64() int64 { return int64(p) }

// Add returns p+q.
func (p Paise) Add(q Paise) Paise { return p + q }

// Sub returns p-q, clamped at zero. Discounts must never produce a negative
// payable amount, so the clamp is the desired behaviour rather than a bug.
func (p Paise) Sub(q Paise) Paise {
	if q > p {
		return 0
	}
	return p - q
}

// MulQty multiplies a unit price by an item quantity.
func (p Paise) MulQty(qty int) Paise { return p * Paise(qty) }

// PercentOff returns the discount amount for a whole-number percentage,
// rounded half-up so a 10% discount on ₹10.05 is ₹1.01 rather than ₹1.00.
// Rounding happens once, here, rather than being spread across call sites.
func (p Paise) PercentOff(percent int) Paise {
	if percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return p
	}
	n := int64(p) * int64(percent)
	return Paise((n + 50) / 100)
}

// DiscountPercent reports the integer percentage saved when selling at `p`
// against an MRP of `mrp`, truncated toward zero. Returns 0 when there is no
// genuine saving, so the UI never renders a "0% off" or negative badge.
func DiscountPercent(mrp, p Paise) int {
	if mrp <= 0 || p >= mrp {
		return 0
	}
	return int(((int64(mrp) - int64(p)) * 100) / int64(mrp))
}

// String renders the amount as a plain decimal ("1234.50"), without a symbol
// or grouping. Presentation/localisation is the frontend's job.
func (p Paise) String() string {
	neg := p < 0
	v := int64(p)
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", v/100, v%100)
	if neg {
		return "-" + s
	}
	return s
}

// Format renders the amount for human-facing text such as the WhatsApp message,
// using Indian digit grouping (₹1,23,456.00).
func (p Paise) Format() string {
	v := int64(p)
	neg := v < 0
	if neg {
		v = -v
	}
	whole := strconv.FormatInt(v/100, 10)
	frac := fmt.Sprintf("%02d", v%100)

	var out string
	if len(whole) <= 3 {
		out = whole
	} else {
		// Last three digits, then groups of two (the Indian lakh/crore system).
		head, tail := whole[:len(whole)-3], whole[len(whole)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		out = strings.Join(groups, ",") + "," + tail
	}
	if neg {
		return "-" + out + "." + frac
	}
	return out + "." + frac
}

// ParseRupees reads a decimal rupee string ("1234.50", "1234", "1,234.5") into
// paise. Used by admin forms, which are the only place a human types a price.
func ParseRupees(s string) (Paise, error) {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), "₹", ""))
	if s == "" {
		return 0, errors.New("money: empty amount")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: invalid amount %q", s)
	}
	var f int64
	if hasFrac {
		switch len(frac) {
		case 0:
			f = 0
		case 1:
			f, err = strconv.ParseInt(frac, 10, 64)
			f *= 10
		case 2:
			f, err = strconv.ParseInt(frac, 10, 64)
		default:
			return 0, errors.New("money: more than two decimal places")
		}
		if err != nil {
			return 0, fmt.Errorf("money: invalid fraction %q", frac)
		}
	}
	total := w*100 + f
	if neg {
		total = -total
	}
	if total < 0 {
		return 0, ErrNegative
	}
	return Paise(total), nil
}
