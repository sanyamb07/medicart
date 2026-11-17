// Package idgen creates the identifiers MediCart shows to people.
//
// Internal primary keys are UUIDs. Customer-facing references (order numbers,
// support ticket numbers) are short, unambiguous, and generated from a
// cryptographically secure source so they cannot be enumerated by guessing the
// next value — an order reference is part of the order-tracking authorisation
// check, so predictability would be a real access-control weakness.
package idgen

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

// crockford is Crockford base32 minus I, L, O, U — no character pair that a
// customer could misread when reading a reference over the phone.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomString(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(crockford)))
	for i := range b {
		// crypto/rand only fails if the OS entropy source is broken, in which
		// case there is nothing sensible to fall back to.
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("idgen: no system entropy: " + err.Error())
		}
		b[i] = crockford[v.Int64()]
	}
	return string(b)
}

// OrderReference returns e.g. "MC-26A3-7KQF9D2M".
// The middle group is a year/month stamp, which makes references roughly
// sortable for support staff without leaking a sequence.
func OrderReference(now time.Time) string {
	u := now.UTC()
	return fmt.Sprintf("MC-%02d%s-%s", u.Year()%100, string(crockford[u.Month()]), randomString(8))
}

// SupportReference returns e.g. "SR-4KD92MXQ".
func SupportReference() string { return "SR-" + randomString(8) }

// BatchNumberSuffix helps admins name a batch when the supplier gave none.
func BatchNumberSuffix() string { return randomString(6) }

// AffiliateCode returns an 8-character referral code, e.g. "7KQF9D2M" — same
// alphabet as other customer-facing identifiers, so it is never misread when
// read aloud or typed by hand.
func AffiliateCode() string { return randomString(8) }

// Token returns a 256-bit opaque secret, hex encoded, for session cookies and
// signed-URL grants.
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("idgen: no system entropy: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}

// NumericOTP returns a zero-padded n-digit one-time code.
func NumericOTP(n int) string {
	limit := big.NewInt(1)
	for range n {
		limit.Mul(limit, big.NewInt(10))
	}
	v, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic("idgen: no system entropy: " + err.Error())
	}
	return fmt.Sprintf("%0*d", n, v)
}
