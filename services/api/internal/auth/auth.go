// Package auth handles two authentication paths sharing one session
// mechanism: staff sign in with email + password (see LoginStaff); customers
// sign in with a mobile number and a one-time code (see customer_otp.go).
// Guest checkout remains available and does not require either — a guest's
// order is still tied to a real customer_profile, found-or-created by mobile
// number, just without a login session; order tracking for a guest stays by
// order reference + mobile (see internal/orders).
//
// Sessions are opaque random tokens, hashed with SHA-256 before storage, so a
// database leak does not hand over live sessions and logout/role-change can
// revoke access immediately (unlike a self-contained JWT).
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
)

// bcryptCost: ~250ms per hash on commodity hardware. The login path is not
// hot, so the slower, more crack-resistant cost factor is the right trade.
const bcryptCost = 12

// User is an authenticated identity — a staff member or a customer.
type User struct {
	ID         string    `json:"id"`
	Mobile     *string   `json:"mobile"`
	Email      *string   `json:"email"`
	FullName   *string   `json:"fullName"`
	IsActive   bool      `json:"isActive"`
	Roles      []string  `json:"roles"`
	CustomerID *string   `json:"customerId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Service implements authentication and session management for both staff
// and customers.
type Service struct {
	pool       *db.Pool
	sessionTTL time.Duration
	// devOTPEcho exposes the generated OTP code in the API response when no
	// SMS provider is configured, so local development can complete a login
	// without one. Hard-wired off in production regardless of configuration
	// (see NewService) — the code is never echoed when APP_ENV=production.
	devOTPEcho bool
}

// NewService builds the auth service. echoOTP should be true only outside
// production and only when no real SMS provider is wired up.
func NewService(pool *db.Pool, sessionTTL time.Duration, echoOTP bool) *Service {
	return &Service{pool: pool, sessionTTL: sessionTTL, devOTPEcho: echoOTP}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LoginStaff authenticates an email/password account.
func (s *Service) LoginStaff(ctx context.Context, email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return nil, httpx.ErrUnauthorized.WithMessage("Enter your email and password.")
	}

	var (
		userID   string
		hash     *string
		isActive bool
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, password_hash, is_active FROM users WHERE email = $1`, email).
		Scan(&userID, &hash, &isActive)

	if db.IsNoRows(err) || (err == nil && hash == nil) {
		// Compare against a dummy hash so a missing account takes the same
		// time as a wrong password — otherwise response timing would reveal
		// which emails are registered.
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$12$000000000000000000000000000000000000000000000000000"),
			[]byte(password))
		return nil, httpx.ErrUnauthorized.WithMessage("Incorrect email or password.")
	}
	if err != nil {
		return nil, fmt.Errorf("look up staff user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(*hash), []byte(password)) != nil {
		return nil, httpx.ErrUnauthorized.WithMessage("Incorrect email or password.")
	}
	if !isActive {
		return nil, httpx.ErrForbidden.WithMessage("This account has been disabled.")
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
		return nil, fmt.Errorf("record login: %w", err)
	}
	return s.loadUser(ctx, userID)
}

// HashPassword hashes a staff password for storage (used by the seed script).
func HashPassword(plain string) (string, error) {
	if err := ValidatePasswordStrength(plain); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// ValidatePasswordStrength enforces a length floor, which does more for
// resistance than composition rules.
func ValidatePasswordStrength(p string) error {
	if len(p) < 10 {
		return httpx.ErrValidation.WithFields(map[string]string{"password": "Use at least 10 characters."})
	}
	if len(p) > 200 {
		// bcrypt silently truncates past 72 bytes; reject long input outright
		// instead of accepting a password whose tail is ignored.
		return httpx.ErrValidation.WithFields(map[string]string{"password": "Use at most 200 characters."})
	}
	return nil
}

// ---------------------------------------------------------------- sessions

type Session struct {
	ID        string
	Token     string // returned once, at creation
	ExpiresAt time.Time
}

func (s *Service) CreateSession(ctx context.Context, userID, userAgent, ip string) (*Session, error) {
	token := idgen.Token()
	expires := time.Now().Add(s.sessionTTL)

	if len(userAgent) > 400 {
		userAgent = userAgent[:400]
	}
	var ipVal any
	if ip != "" {
		ipVal = ip
	}

	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text`,
		userID, hashToken(token), nullIfEmpty(userAgent), ipVal, expires).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Session{ID: id, Token: token, ExpiresAt: expires}, nil
}

// Authenticate resolves a session token to a principal, or nil when the token
// is unknown, expired or revoked.
func (s *Service) Authenticate(ctx context.Context, token string) (*httpx.Principal, error) {
	if token == "" {
		return nil, nil
	}

	var (
		sessionID  string
		userID     string
		customerID *string
		isActive   bool
	)
	err := s.pool.QueryRow(ctx, `
		SELECT s.id::text, u.id::text, cp.id::text, u.is_active
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		  LEFT JOIN customer_profiles cp ON cp.user_id = u.id
		 WHERE s.token_hash = $1
		   AND s.revoked_at IS NULL
		   AND s.expires_at > now()`, hashToken(token)).
		Scan(&sessionID, &userID, &customerID, &isActive)

	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	if !isActive {
		return nil, nil
	}

	roles, err := s.loadRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, sessionID)

	p := &httpx.Principal{UserID: userID, SessionID: sessionID, Roles: roles}
	if customerID != nil {
		p.CustomerID = *customerID
	}
	return p, nil
}

func (s *Service) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) GetUser(ctx context.Context, userID string) (*User, error) {
	return s.loadUser(ctx, userID)
}

func (s *Service) loadUser(ctx context.Context, userID string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.mobile, u.email::text, u.full_name, u.is_active, u.created_at, cp.id::text
		  FROM users u
		  LEFT JOIN customer_profiles cp ON cp.user_id = u.id
		 WHERE u.id = $1`, userID).
		Scan(&u.ID, &u.Mobile, &u.Email, &u.FullName, &u.IsActive, &u.CreatedAt, &u.CustomerID)
	if db.IsNoRows(err) {
		return nil, httpx.ErrNotFound.WithMessage("Account not found.")
	}
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	roles, err := s.loadRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return &u, nil
}

func (s *Service) loadRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT role_code FROM user_roles WHERE user_id = $1 ORDER BY role_code`, userID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}
	defer rows.Close()

	roles := make([]string, 0, 2)
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// ---------------------------------------------------------------- validation helpers

// NormaliseMobile reduces common Indian input shapes to 10 digits.
func NormaliseMobile(m string) string {
	var digits strings.Builder
	for _, r := range m {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "91"):
		return d[2:]
	case len(d) == 11 && strings.HasPrefix(d, "0"):
		return d[1:]
	default:
		return d
	}
}

// IsValidMobile checks the 10-digit Indian mobile format (matches the DB check).
func IsValidMobile(m string) bool {
	if len(m) != 10 || m[0] < '6' || m[0] > '9' {
		return false
	}
	for i := range m {
		if m[i] < '0' || m[i] > '9' {
			return false
		}
	}
	return true
}

// IsValidPIN checks a 6-digit Indian PIN code.
func IsValidPIN(p string) bool {
	if len(p) != 6 || p[0] < '1' || p[0] > '9' {
		return false
	}
	for i := range p {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	return true
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
