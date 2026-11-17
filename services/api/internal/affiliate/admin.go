package affiliate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// ListFilter narrows the platform admin's affiliate applications list.
type ListFilter struct {
	Status   string // "" = all
	Page     int
	PageSize int
}

func (s *Service) ListAffiliates(ctx context.Context, f ListFilter) ([]*Affiliate, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 25
	}
	where := "TRUE"
	args := []any{}
	n := 0
	if f.Status != "" {
		n++
		where += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, f.Status)
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM affiliates WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count affiliates: %w", err)
	}

	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, code, full_name, email, phone, payout_details, status, rejected_reason, created_at, approved_at
		  FROM affiliates WHERE %s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`, where, n+1, n+2), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list affiliates: %w", err)
	}
	defer rows.Close()

	out := make([]*Affiliate, 0, f.PageSize)
	for rows.Next() {
		var a Affiliate
		if err := rows.Scan(&a.ID, &a.Code, &a.FullName, &a.Email, &a.Phone, &a.PayoutDetails,
			&a.Status, &a.RejectedReason, &a.CreatedAt, &a.ApprovedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &a)
	}
	return out, total, rows.Err()
}

func (s *Service) Approve(ctx context.Context, affiliateID, adminUserID string) (*Affiliate, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE affiliates SET status = 'ACTIVE', approved_at = now(), approved_by = $2, rejected_reason = NULL
		 WHERE id = $1 AND status IN ('PENDING','SUSPENDED')`, affiliateID, adminUserID)
	if err != nil {
		return nil, fmt.Errorf("approve affiliate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAffiliateNotFound
	}
	return s.getByID(ctx, affiliateID)
}

func (s *Service) Reject(ctx context.Context, affiliateID, reason, adminUserID string) (*Affiliate, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"reason": "A reason is required."})
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE affiliates SET status = 'REJECTED', rejected_reason = $2, approved_by = $3
		 WHERE id = $1 AND status = 'PENDING'`, affiliateID, reason, adminUserID)
	if err != nil {
		return nil, fmt.Errorf("reject affiliate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAffiliateNotFound
	}
	return s.getByID(ctx, affiliateID)
}

func (s *Service) Suspend(ctx context.Context, affiliateID, adminUserID string) (*Affiliate, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE affiliates SET status = 'SUSPENDED', approved_by = $2
		 WHERE id = $1 AND status = 'ACTIVE'`, affiliateID, adminUserID)
	if err != nil {
		return nil, fmt.Errorf("suspend affiliate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAffiliateNotFound
	}
	return s.getByID(ctx, affiliateID)
}

func (s *Service) getByID(ctx context.Context, affiliateID string) (*Affiliate, error) {
	var a Affiliate
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, code, full_name, email, phone, payout_details, status, rejected_reason, created_at, approved_at
		  FROM affiliates WHERE id = $1`, affiliateID).
		Scan(&a.ID, &a.Code, &a.FullName, &a.Email, &a.Phone, &a.PayoutDetails, &a.Status, &a.RejectedReason, &a.CreatedAt, &a.ApprovedAt)
	if db.IsNoRows(err) {
		return nil, ErrAffiliateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load affiliate: %w", err)
	}
	return &a, nil
}

// ReferralFilter narrows the platform admin's commission ledger.
type ReferralFilter struct {
	Status   string // "" = all
	Page     int
	PageSize int
}

// AdminReferral is a ledger row joined with the affiliate it belongs to, for
// the platform-wide commission ledger view.
type AdminReferral struct {
	Referral
	AffiliateID   string `json:"affiliateId"`
	AffiliateCode string `json:"affiliateCode"`
	AffiliateName string `json:"affiliateName"`
}

func (s *Service) ListReferrals(ctx context.Context, f ReferralFilter) ([]*AdminReferral, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 25
	}
	where := "TRUE"
	args := []any{}
	n := 0
	if f.Status != "" {
		n++
		where += fmt.Sprintf(" AND r.status = $%d", n)
		args = append(args, f.Status)
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM affiliate_referrals r WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count referrals: %w", err)
	}

	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT r.id::text, r.order_id::text, o.reference, r.code_used, r.eligible_subtotal_paise,
		       r.commission_rate_bps, r.commission_paise, r.status, r.created_at, r.confirmed_at, r.paid_at,
		       a.id::text, a.code, a.full_name
		  FROM affiliate_referrals r
		  JOIN orders o ON o.id = r.order_id
		  JOIN affiliates a ON a.id = r.affiliate_id
		 WHERE %s
		 ORDER BY r.created_at DESC
		 LIMIT $%d OFFSET $%d`, where, n+1, n+2), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list referrals: %w", err)
	}
	defer rows.Close()

	out := make([]*AdminReferral, 0, f.PageSize)
	for rows.Next() {
		var r AdminReferral
		if err := rows.Scan(&r.ID, &r.OrderID, &r.OrderReference, &r.CodeUsed, &r.EligibleSubtotalPaise,
			&r.CommissionRateBps, &r.CommissionPaise, &r.Status, &r.CreatedAt, &r.ConfirmedAt, &r.PaidAt,
			&r.AffiliateID, &r.AffiliateCode, &r.AffiliateName); err != nil {
			return nil, 0, err
		}
		out = append(out, &r)
	}
	return out, total, rows.Err()
}

// MarkPaid marks a batch of CONFIRMED referrals PAID in one call — the admin
// has sent the money outside the system (UPI/bank transfer using the
// affiliate's payout_details) and is recording that here. Referrals not
// currently CONFIRMED are silently skipped rather than erroring the whole
// batch, so a stale selection in the admin UI never blocks the rest.
func (s *Service) MarkPaid(ctx context.Context, referralIDs []string, adminUserID string) (int, error) {
	if len(referralIDs) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE affiliate_referrals SET status = 'PAID', paid_at = now(), paid_by = $2
		 WHERE id = ANY($1) AND status = 'CONFIRMED'`, referralIDs, adminUserID)
	if err != nil {
		return 0, fmt.Errorf("mark referrals paid: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ---------------------------------------------------------------- settings

type Settings struct {
	CommissionRateBps int       `json:"commissionRateBps"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (s *Service) GetSettings(ctx context.Context) (*Settings, error) {
	var st Settings
	if err := s.pool.QueryRow(ctx,
		`SELECT commission_rate_bps, updated_at FROM affiliate_settings WHERE id = 1`).
		Scan(&st.CommissionRateBps, &st.UpdatedAt); err != nil {
		return nil, fmt.Errorf("load affiliate settings: %w", err)
	}
	return &st, nil
}

// SetCommissionRateBps updates the platform-wide flat rate applied to every
// referral created from now on (existing referrals keep the rate frozen at
// the time they were recorded — see AttributeOrder).
func (s *Service) SetCommissionRateBps(ctx context.Context, bps int, adminUserID string) (*Settings, error) {
	if bps < 0 || bps > 5000 {
		return nil, httpx.ErrValidation.WithFields(map[string]string{
			"commissionRateBps": "Enter a rate between 0 and 5000 basis points (0-50%).",
		})
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE affiliate_settings SET commission_rate_bps = $1, updated_at = now(), updated_by = $2 WHERE id = 1`,
		bps, adminUserID); err != nil {
		return nil, fmt.Errorf("update affiliate settings: %w", err)
	}
	return s.GetSettings(ctx)
}
