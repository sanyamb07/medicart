// Package affiliate implements a third-party referral program: an external
// promoter (the affiliate) signs up, gets a unique referral code/link, and
// earns a commission on orders placed by shoppers who arrived through it —
// the same mechanic a dropshipping storefront's affiliate program uses.
//
// Deliberately restricted to non-prescription (OTC) products only. MediCart
// sells medicines, some prescription-only; paying a third party to induce
// sales of a prescription drug is a different, and here unreviewed,
// regulatory question from ordinary e-commerce affiliate marketing (in
// India this brushes against Drugs and Cosmetics Act / pharmacy-practice
// restrictions on inducement-based drug promotion). Every referral this
// package records excludes Rx line items from the commissionable amount —
// see AttributeOrder — so the commission calculation itself enforces the
// restriction rather than relying on a policy nobody can see.
//
// Scope in this iteration: commission payouts are a ledger only (PENDING ->
// CONFIRMED -> PAID, or VOID on cancellation/refund). No payout API is
// wired up — an admin marks a batch PAID after sending money outside the
// system, the same mock-first approach already used for PAYMENT_DRIVER and
// AI_DRIVER elsewhere in this project. See docs/DEPLOYMENT.md.
package affiliate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
)

var (
	ErrAffiliateNotFound = errors.New("affiliate: not found")
	ErrCodeInUse         = errors.New("affiliate: code already registered")
)

// Affiliate is the profile returned to the affiliate themself and to
// platform admins.
type Affiliate struct {
	ID             string     `json:"id"`
	Code           string     `json:"code"`
	FullName       string     `json:"fullName"`
	Email          string     `json:"email"`
	Phone          *string    `json:"phone"`
	PayoutDetails  *string    `json:"payoutDetails,omitempty"`
	Status         string     `json:"status"`
	RejectedReason *string    `json:"rejectedReason,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ApprovedAt     *time.Time `json:"approvedAt"`
}

// Referral is one order's attribution + commission ledger row.
type Referral struct {
	ID                    string     `json:"id"`
	OrderID               string     `json:"orderId"`
	OrderReference        string     `json:"orderReference"`
	CodeUsed              string     `json:"codeUsed"`
	EligibleSubtotalPaise int64      `json:"eligibleSubtotalPaise"`
	CommissionRateBps     int        `json:"commissionRateBps"`
	CommissionPaise       int64      `json:"commissionPaise"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"createdAt"`
	ConfirmedAt           *time.Time `json:"confirmedAt"`
	PaidAt                *time.Time `json:"paidAt"`
}

// Summary is the affiliate dashboard's headline numbers.
type Summary struct {
	PendingPaise   int64 `json:"pendingPaise"`
	ConfirmedPaise int64 `json:"confirmedPaise"`
	PaidPaise      int64 `json:"paidPaise"`
	ReferralCount  int   `json:"referralCount"`
}

type Service struct {
	pool    *db.Pool
	authSvc *auth.Service
}

func NewService(pool *db.Pool, authSvc *auth.Service) *Service {
	return &Service{pool: pool, authSvc: authSvc}
}

// ---------------------------------------------------------------- signup / profile

// Apply registers a new affiliate application. The account starts PENDING
// and cannot log in until a platform admin approves it (RequireActive,
// called from Login, enforces this).
func (s *Service) Apply(ctx context.Context, email, password, fullName, phone, payoutDetails string) (*Affiliate, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	fullName = strings.TrimSpace(fullName)
	phone = auth.NormaliseMobile(phone)
	if fullName == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"fullName": "Enter your name."})
	}
	if email == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"email": "Enter a valid email address."})
	}
	if phone != "" && !auth.IsValidMobile(phone) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"phone": "Enter a valid 10-digit mobile number."})
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}

	var a Affiliate
	err = s.pool.InTx(ctx, func(q db.Querier) error {
		var userID string
		if err := q.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, is_active)
			VALUES ($1, $2, $3, TRUE)
			RETURNING id::text`, email, hash, fullName).Scan(&userID); err != nil {
			return fmt.Errorf("create affiliate user: %w", err)
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'AFFILIATE')`, userID); err != nil {
			return fmt.Errorf("grant affiliate role: %w", err)
		}

		code, err := uniqueCode(ctx, q)
		if err != nil {
			return err
		}

		return q.QueryRow(ctx, `
			INSERT INTO affiliates (user_id, code, full_name, email, phone, payout_details, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'PENDING')
			RETURNING id::text, code, full_name, email, phone, payout_details, status, created_at, approved_at`,
			userID, code, fullName, email, nullIfEmpty(phone), nullIfEmpty(payoutDetails)).
			Scan(&a.ID, &a.Code, &a.FullName, &a.Email, &a.Phone, &a.PayoutDetails, &a.Status, &a.CreatedAt, &a.ApprovedAt)
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, httpx.ErrConflict.WithMessage("An affiliate account with this email already exists.")
		}
		return nil, err
	}
	return &a, nil
}

// uniqueCode generates a referral code, retrying on the rare collision
// (8 characters from a 32-symbol alphabet is ~2^40 of space, so this almost
// always succeeds on the first try).
func uniqueCode(ctx context.Context, q db.Querier) (string, error) {
	for i := 0; i < 5; i++ {
		code := idgen.AffiliateCode()
		var exists bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM affiliates WHERE code = $1)`, code).Scan(&exists); err != nil {
			return "", fmt.Errorf("check code uniqueness: %w", err)
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("affiliate: could not generate a unique code")
}

// Login authenticates an affiliate's email+password and starts a session on
// the affiliate cookie namespace. Reuses the staff login check (it is
// generic: email + bcrypt password, not staff-specific) but additionally
// requires an ACTIVE affiliate row, so a pending or rejected applicant
// cannot sign in to the dashboard.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (*auth.Session, *Affiliate, error) {
	user, err := s.authSvc.LoginStaff(ctx, email, password)
	if err != nil {
		return nil, nil, err
	}
	if !hasRole(user.Roles, httpx.RoleAffiliate) {
		return nil, nil, httpx.ErrUnauthorized.WithMessage("Incorrect email or password.")
	}
	a, err := s.getByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	switch a.Status {
	case "PENDING":
		return nil, nil, httpx.ErrForbidden.WithMessage("Your affiliate application is still awaiting approval.")
	case "REJECTED":
		return nil, nil, httpx.ErrForbidden.WithMessage("This affiliate application was not approved.")
	case "SUSPENDED":
		return nil, nil, httpx.ErrForbidden.WithMessage("This affiliate account has been suspended.")
	}
	sess, err := s.authSvc.CreateSession(ctx, user.ID, userAgent, ip)
	if err != nil {
		return nil, nil, err
	}
	return sess, a, nil
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// GetByPrincipal returns the affiliate profile for the signed-in user.
func (s *Service) GetByPrincipal(ctx context.Context, userID string) (*Affiliate, error) {
	return s.getByUserID(ctx, userID)
}

func (s *Service) getByUserID(ctx context.Context, userID string) (*Affiliate, error) {
	var a Affiliate
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, code, full_name, email, phone, payout_details, status, rejected_reason, created_at, approved_at
		  FROM affiliates WHERE user_id = $1`, userID).
		Scan(&a.ID, &a.Code, &a.FullName, &a.Email, &a.Phone, &a.PayoutDetails, &a.Status, &a.RejectedReason, &a.CreatedAt, &a.ApprovedAt)
	if db.IsNoRows(err) {
		return nil, ErrAffiliateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load affiliate: %w", err)
	}
	return &a, nil
}

// MySummary returns the dashboard headline numbers for one affiliate.
func (s *Service) MySummary(ctx context.Context, affiliateID string) (*Summary, error) {
	var sum Summary
	err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(commission_paise) FILTER (WHERE status = 'PENDING'), 0),
			COALESCE(SUM(commission_paise) FILTER (WHERE status = 'CONFIRMED'), 0),
			COALESCE(SUM(commission_paise) FILTER (WHERE status = 'PAID'), 0),
			COUNT(*) FILTER (WHERE status <> 'VOID')
		  FROM affiliate_referrals WHERE affiliate_id = $1`, affiliateID).
		Scan(&sum.PendingPaise, &sum.ConfirmedPaise, &sum.PaidPaise, &sum.ReferralCount)
	if err != nil {
		return nil, fmt.Errorf("load affiliate summary: %w", err)
	}
	return &sum, nil
}

// MyReferrals returns a page of one affiliate's referral ledger, newest first.
func (s *Service) MyReferrals(ctx context.Context, affiliateID string, page, pageSize int) ([]*Referral, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}
	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM affiliate_referrals WHERE affiliate_id = $1`, affiliateID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count referrals: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id::text, r.order_id::text, o.reference, r.code_used, r.eligible_subtotal_paise,
		       r.commission_rate_bps, r.commission_paise, r.status, r.created_at, r.confirmed_at, r.paid_at
		  FROM affiliate_referrals r
		  JOIN orders o ON o.id = r.order_id
		 WHERE r.affiliate_id = $1
		 ORDER BY r.created_at DESC
		 LIMIT $2 OFFSET $3`, affiliateID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list referrals: %w", err)
	}
	defer rows.Close()

	out := make([]*Referral, 0, pageSize)
	for rows.Next() {
		var r Referral
		if err := rows.Scan(&r.ID, &r.OrderID, &r.OrderReference, &r.CodeUsed, &r.EligibleSubtotalPaise,
			&r.CommissionRateBps, &r.CommissionPaise, &r.Status, &r.CreatedAt, &r.ConfirmedAt, &r.PaidAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &r)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- attribution

// AttributeOrder records a referral for a just-placed order, called by the
// checkout handler right after order creation succeeds — deliberately its
// own small transaction rather than part of order creation's, so an invalid
// or stale referral code can never fail a checkout; a shopper's purchase
// always succeeds regardless of whether the marketing metadata attached to
// it is valid. codeRaw is whatever the frontend sent (may be empty, may be
// garbage); applied reports whether a referral was actually recorded.
func (s *Service) AttributeOrder(ctx context.Context, orderID, businessID, codeRaw string) (applied bool, err error) {
	code := strings.ToUpper(strings.TrimSpace(codeRaw))
	if code == "" {
		return false, nil
	}

	err = s.pool.InTx(ctx, func(q db.Querier) error {
		var affiliateID string
		if err := q.QueryRow(ctx,
			`SELECT id::text FROM affiliates WHERE code = $1 AND status = 'ACTIVE'`, code).Scan(&affiliateID); err != nil {
			if db.IsNoRows(err) {
				return nil // unknown or inactive code: silently not applied
			}
			return fmt.Errorf("look up affiliate by code: %w", err)
		}

		var eligiblePaise int64
		if err := q.QueryRow(ctx, `
			SELECT COALESCE(SUM(line_total_paise), 0)
			  FROM order_items WHERE order_id = $1 AND requires_prescription = FALSE`, orderID).
			Scan(&eligiblePaise); err != nil {
			return fmt.Errorf("sum eligible order items: %w", err)
		}
		if eligiblePaise <= 0 {
			return nil // every line was prescription-only: nothing commissionable
		}

		var rateBps int
		if err := q.QueryRow(ctx, `SELECT commission_rate_bps FROM affiliate_settings WHERE id = 1`).Scan(&rateBps); err != nil {
			return fmt.Errorf("load commission rate: %w", err)
		}
		commissionPaise := (eligiblePaise*int64(rateBps) + 5000) / 10000 // round half-up

		tag, err := q.Exec(ctx, `
			INSERT INTO affiliate_referrals
				(affiliate_id, order_id, business_id, code_used, eligible_subtotal_paise, commission_rate_bps, commission_paise)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (order_id) DO NOTHING`,
			affiliateID, orderID, businessID, code, eligiblePaise, rateBps, commissionPaise)
		if err != nil {
			return fmt.Errorf("insert referral: %w", err)
		}
		applied = tag.RowsAffected() > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

// ConfirmForOrder moves a referral from PENDING to CONFIRMED once the order's
// payment has actually settled — called from the payments package after a
// verified payment.captured webhook event. A no-op when no referral exists
// for this order (the common case: most orders have no affiliate code) or
// it is already past PENDING.
func (s *Service) ConfirmForOrder(ctx context.Context, orderID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE affiliate_referrals SET status = 'CONFIRMED', confirmed_at = now()
		 WHERE order_id = $1 AND status = 'PENDING'`, orderID)
	if err != nil {
		return fmt.Errorf("confirm referral: %w", err)
	}
	return nil
}

// VoidForOrder cancels a referral's commission when its order is cancelled
// or refunded — called from the orders package's cancel/status-update
// handlers. A referral already marked PAID is left alone: reclaiming a
// commission already paid out is a manual, outside-the-system matter (the
// same reasoning as refunds generally in this mock-payments iteration).
func (s *Service) VoidForOrder(ctx context.Context, orderID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE affiliate_referrals SET status = 'VOID', voided_at = now()
		 WHERE order_id = $1 AND status IN ('PENDING','CONFIRMED')`, orderID)
	if err != nil {
		return fmt.Errorf("void referral: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
