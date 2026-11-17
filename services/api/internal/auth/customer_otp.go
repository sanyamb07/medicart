// Customer self-service login by mobile OTP.
//
// This supersedes the earlier package-doc note that customer login was out of
// scope — it's now implemented: a customer authenticates with a one-time code
// sent to their mobile number, using the same opaque-session mechanism as
// staff (Service.CreateSession / Authenticate), just granted the CUSTOMER
// role instead of a back-office one.
package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
)

const (
	otpLength      = 6
	otpTTL         = 10 * time.Minute
	otpMaxAttempts = 5
)

// StartOTPResult reports the outcome of requesting a code.
type StartOTPResult struct {
	ExpiresIn int `json:"expiresInSeconds"`
	// DevCode is populated ONLY when devOTPEcho is set (non-production, no SMS
	// provider configured) — see NewService. Never present in production.
	DevCode      string `json:"devCode,omitempty"`
	DeliveryNote string `json:"deliveryNote"`
}

// StartOTP issues a one-time code for a mobile number. The response is
// identical whether or not the number already has an account — revealing
// that would let anyone enumerate the customer base.
func (s *Service) StartOTP(ctx context.Context, mobile string) (*StartOTPResult, error) {
	mobile = NormaliseMobile(mobile)
	if !IsValidMobile(mobile) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{
			"mobile": "Enter a valid 10-digit Indian mobile number.",
		})
	}

	code := idgen.NumericOTP(otpLength)
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash otp: %w", err)
	}

	expires := time.Now().Add(otpTTL)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO otp_challenges (mobile, code_hash, max_attempts, expires_at)
		VALUES ($1, $2, $3, $4)`, mobile, string(hash), otpMaxAttempts, expires); err != nil {
		return nil, fmt.Errorf("store otp: %w", err)
	}

	res := &StartOTPResult{ExpiresIn: int(otpTTL.Seconds())}
	if s.devOTPEcho {
		res.DevCode = code
		res.DeliveryNote = "No SMS provider is configured, so the code is shown here for local development."
	} else {
		res.DeliveryNote = "We've sent a code to your mobile number."
	}
	return res, nil
}

// VerifyOTP checks a code and returns the (possibly newly created) customer
// user. Only the newest unconsumed challenge for the number counts, so an
// attacker cannot keep an old code alive by requesting a new one.
func (s *Service) VerifyOTP(ctx context.Context, mobile, code string) (*User, error) {
	mobile = NormaliseMobile(mobile)
	code = strings.TrimSpace(code)
	if !IsValidMobile(mobile) || len(code) != otpLength {
		return nil, httpx.ErrUnauthorized.WithMessage("That code is not valid.")
	}

	var userID string
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var (
			challengeID string
			codeHash    string
			attempts    int
			maxAttempts int
			expiresAt   time.Time
		)
		// FOR UPDATE serialises concurrent attempts on the same challenge.
		err := q.QueryRow(ctx, `
			SELECT id::text, code_hash, attempts, max_attempts, expires_at
			  FROM otp_challenges
			 WHERE mobile = $1 AND consumed_at IS NULL
			 ORDER BY created_at DESC
			 LIMIT 1
			   FOR UPDATE`, mobile).
			Scan(&challengeID, &codeHash, &attempts, &maxAttempts, &expiresAt)
		if db.IsNoRows(err) {
			return httpx.ErrUnauthorized.WithMessage("Request a new code and try again.")
		}
		if err != nil {
			return fmt.Errorf("load otp: %w", err)
		}
		if time.Now().After(expiresAt) {
			return httpx.ErrUnauthorized.WithMessage("That code has expired. Request a new one.")
		}
		if attempts >= maxAttempts {
			return httpx.ErrUnauthorized.WithMessage("Too many incorrect attempts. Request a new code.")
		}

		if bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(code)) != nil {
			if _, uerr := q.Exec(ctx,
				`UPDATE otp_challenges SET attempts = attempts + 1 WHERE id = $1`, challengeID); uerr != nil {
				return fmt.Errorf("record otp attempt: %w", uerr)
			}
			remaining := maxAttempts - attempts - 1
			if remaining <= 0 {
				return httpx.ErrUnauthorized.WithMessage("Too many incorrect attempts. Request a new code.")
			}
			return httpx.ErrUnauthorized.WithMessage(fmt.Sprintf("That code is incorrect. %d attempt(s) left.", remaining))
		}

		if _, err := q.Exec(ctx,
			`UPDATE otp_challenges SET consumed_at = now() WHERE id = $1`, challengeID); err != nil {
			return fmt.Errorf("consume otp: %w", err)
		}

		uid, err := upsertCustomerByMobile(ctx, q, mobile)
		if err != nil {
			return err
		}
		userID = uid
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.loadUser(ctx, userID)
}

// upsertCustomerByMobile finds or creates the user + customer_profile pair
// for a mobile number, granting the CUSTOMER role on first sight. Shared by
// OTP login and guest checkout (internal/orders calls the equivalent logic
// for guest checkout) — kept here as the canonical version for the login path.
func upsertCustomerByMobile(ctx context.Context, q db.Querier, mobile string) (userID string, err error) {
	err = q.QueryRow(ctx, `SELECT id::text FROM users WHERE mobile = $1`, mobile).Scan(&userID)
	switch {
	case db.IsNoRows(err):
		if err := q.QueryRow(ctx, `
			INSERT INTO users (mobile, is_active, last_login_at)
			VALUES ($1, TRUE, now())
			RETURNING id::text`, mobile).Scan(&userID); err != nil {
			return "", fmt.Errorf("create user: %w", err)
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'CUSTOMER')`, userID); err != nil {
			return "", fmt.Errorf("grant customer role: %w", err)
		}
	case err != nil:
		return "", fmt.Errorf("look up user: %w", err)
	default:
		if _, err := q.Exec(ctx,
			`UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
			return "", fmt.Errorf("record login: %w", err)
		}
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO customer_profiles (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return "", fmt.Errorf("create customer profile: %w", err)
	}
	return userID, nil
}
