// Package tenancy implements the multi-tenant foundation: businesses (the
// tenant), platform-level administrators, and business staff membership.
//
// Master catalogue, customer identity and the master product data stay
// global (see internal/catalog, internal/auth) — this package is only about
// *which business* a request belongs to and *who may act for it*. See
// docs/MULTI_TENANT_PLAN.md for the full reasoning.
package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// DefaultBusinessID is the tenant every pre-existing row backfilled to in
// migration 000008. New code should resolve a business from context rather
// than hardcode this, but seed/test fixtures may reference it directly.
const DefaultBusinessID = "00000000-0000-0000-0000-000000000001"

var (
	ErrNotFound         = errors.New("tenancy: business not found")
	ErrSlugTaken        = errors.New("tenancy: slug already in use")
	ErrNotStaffAnywhere = errors.New("tenancy: user is not staff of any business")
)

type Business struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	LogoURL         *string   `json:"logoUrl"`
	Tagline         *string   `json:"tagline"`
	SupportWhatsApp *string   `json:"supportWhatsapp"`
	LicenseStatus   string    `json:"licenseStatus"`
	CreatedAt       time.Time `json:"createdAt"`
}

const (
	StatusPending   = "PENDING"
	StatusActive    = "ACTIVE"
	StatusSuspended = "SUSPENDED"
	StatusRejected  = "REJECTED"
)

type Repo struct{ pool *db.Pool }

func NewRepo(pool *db.Pool) *Repo { return &Repo{pool: pool} }

var bizColumns = `id::text, slug, name, status, logo_url, tagline, support_whatsapp, license_status, created_at`

func scanBusiness(row interface{ Scan(...any) error }) (*Business, error) {
	var b Business
	if err := row.Scan(&b.ID, &b.Slug, &b.Name, &b.Status, &b.LogoURL, &b.Tagline, &b.SupportWhatsApp, &b.LicenseStatus, &b.CreatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// ResolveBySlug loads an ACTIVE business by slug — used by storefront tenant
// resolution, which must never resolve a pending/suspended business (nothing
// should be orderable there).
func (r *Repo) ResolveBySlug(ctx context.Context, slug string) (*Business, error) {
	b, err := scanBusiness(r.pool.QueryRow(ctx, `SELECT `+bizColumns+` FROM businesses WHERE slug = $1 AND status = 'ACTIVE'`, slug))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve business by slug: %w", err)
	}
	return b, nil
}

// GetByID loads a business regardless of status — for platform-admin screens
// and internal lookups that must still see a suspended business.
func (r *Repo) GetByID(ctx context.Context, id string) (*Business, error) {
	b, err := scanBusiness(r.pool.QueryRow(ctx, `SELECT `+bizColumns+` FROM businesses WHERE id = $1`, id))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get business: %w", err)
	}
	return b, nil
}

// ListActive returns every active business — the public storefront directory.
func (r *Repo) ListActive(ctx context.Context) ([]*Business, error) {
	return r.list(ctx, `WHERE status = 'ACTIVE' ORDER BY name`)
}

// ListAll returns every business regardless of status, for the platform admin.
func (r *Repo) ListAll(ctx context.Context) ([]*Business, error) {
	return r.list(ctx, `ORDER BY created_at DESC`)
}

func (r *Repo) list(ctx context.Context, whereOrderBy string) ([]*Business, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+bizColumns+` FROM businesses `+whereOrderBy)
	if err != nil {
		return nil, fmt.Errorf("list businesses: %w", err)
	}
	defer rows.Close()
	out := []*Business{}
	for rows.Next() {
		b, err := scanBusiness(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateInput is a new business registration, raised by anyone (a prospective
// pharmacy owner) and left PENDING until a platform admin approves it.
type CreateInput struct {
	Slug            string
	Name            string
	Tagline         string
	SupportWhatsApp string
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Create registers a new business as PENDING — a platform admin must
// Approve it before it can serve a storefront.
func (r *Repo) Create(ctx context.Context, in CreateInput) (*Business, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"name": "Business name is required."})
	}
	slug := slugify(in.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"slug": "Could not derive a URL slug from that name."})
	}

	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO businesses (slug, name, tagline, support_whatsapp, status)
		VALUES ($1, $2, $3, $4, 'PENDING')
		RETURNING id::text`,
		slug, name, nullIfEmpty(in.Tagline), nullIfEmpty(in.SupportWhatsApp)).Scan(&id)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, httpx.ErrValidation.WithFields(map[string]string{"slug": "That storefront URL is already taken."})
		}
		return nil, fmt.Errorf("create business: %w", err)
	}
	return r.GetByID(ctx, id)
}

// SetStatus moves a business between PENDING/ACTIVE/SUSPENDED/REJECTED. Any
// transition is allowed (a platform admin can reinstate a suspended
// business) — this is an administrative action, not a customer-facing state
// machine with legal transitions to protect.
func (r *Repo) SetStatus(ctx context.Context, businessID, status string) (*Business, error) {
	switch status {
	case StatusPending, StatusActive, StatusSuspended, StatusRejected:
	default:
		return nil, httpx.ErrValidation.WithMessage("Invalid business status.")
	}
	tag, err := r.pool.Exec(ctx, `UPDATE businesses SET status = $2 WHERE id = $1`, businessID, status)
	if err != nil {
		return nil, fmt.Errorf("set business status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, businessID)
}

// ---------------------------------------------------------------- platform admins

// IsPlatformAdmin reports whether userID holds platform-wide super-admin
// access (manages businesses, not any one business's data).
func (r *Repo) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM platform_admins WHERE user_id = $1)`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check platform admin: %w", err)
	}
	return exists, nil
}

// ---------------------------------------------------------------- business staff

// StaffBusinessID returns the one business userID is active staff of. A
// user may in principle hold rows at more than one business, in which case
// the most-recently-granted membership is used — one business in scope per
// request keeps every admin route's tenant resolution unambiguous.
func (r *Repo) StaffBusinessID(ctx context.Context, userID string) (string, error) {
	var businessID string
	err := r.pool.QueryRow(ctx, `
		SELECT business_id::text FROM business_staff
		 WHERE user_id = $1 AND is_active = TRUE
		 ORDER BY granted_at DESC LIMIT 1`, userID).Scan(&businessID)
	if db.IsNoRows(err) {
		return "", ErrNotStaffAnywhere
	}
	if err != nil {
		return "", fmt.Errorf("resolve staff business: %w", err)
	}
	return businessID, nil
}

// StaffRoles returns userID's active role codes at businessID specifically
// (never roles they hold at a different business) — the authoritative check
// behind RequireBusinessRole.
func (r *Repo) StaffRoles(ctx context.Context, businessID, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT role_code FROM business_staff
		 WHERE business_id = $1 AND user_id = $2 AND is_active = TRUE`, businessID, userID)
	if err != nil {
		return nil, fmt.Errorf("load business staff roles: %w", err)
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GrantStaff adds (or reactivates) a role for a user at a business. Used by
// the seed script and, in future, a business admin's "invite employee" flow.
func (r *Repo) GrantStaff(ctx context.Context, businessID, userID, roleCode string, grantedBy *string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO business_staff (business_id, user_id, role_code, granted_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, user_id, role_code) DO UPDATE SET is_active = TRUE`,
		businessID, userID, roleCode, grantedBy)
	if err != nil {
		return fmt.Errorf("grant staff role: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
