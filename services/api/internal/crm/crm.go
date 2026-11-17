// Package crm implements each business's customer relationship view: one row
// per customer who has interacted with that business (ordered, chatted, or
// both), with a lead status and notes a business can work from. It never
// duplicates the global customer identity (internal/auth) — business_customers
// is the business-scoped relationship, not a second copy of the person.
package crm

import (
	"context"
	"fmt"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Row struct {
	ID                 string     `json:"id"`
	CustomerID         string     `json:"customerId"`
	Mobile             *string    `json:"mobile"`
	FullName           *string    `json:"fullName"`
	LeadStatus         string     `json:"leadStatus"`
	Notes              *string    `json:"notes"`
	OrderCount         int        `json:"orderCount"`
	LifetimeValuePaise int64      `json:"lifetimeValuePaise"`
	FirstSeenAt        time.Time  `json:"firstSeenAt"`
	LastOrderAt        *time.Time `json:"lastOrderAt"`
}

const LeadStatuses = "NEW,CONTACTED,INTERESTED,AWAITING_RESPONSE,CONVERTED,LOST"

type Repo struct{ pool *db.Pool }

func NewRepo(pool *db.Pool) *Repo { return &Repo{pool: pool} }

var rowColumns = `
	bc.id::text, bc.customer_id::text, u.mobile, u.full_name, bc.lead_status, bc.notes,
	bc.order_count, bc.lifetime_value_paise, bc.first_seen_at, bc.last_order_at`

func scanRow(row interface{ Scan(...any) error }) (*Row, error) {
	var r Row
	if err := row.Scan(&r.ID, &r.CustomerID, &r.Mobile, &r.FullName, &r.LeadStatus, &r.Notes,
		&r.OrderCount, &r.LifetimeValuePaise, &r.FirstSeenAt, &r.LastOrderAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// List returns one business's CRM rows, newest activity first, optionally
// filtered to one lead status.
func (r *Repo) List(ctx context.Context, businessID, leadStatus string) ([]*Row, error) {
	where := "bc.business_id = $1"
	args := []any{businessID}
	if leadStatus != "" {
		where += " AND bc.lead_status = $2"
		args = append(args, leadStatus)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s
		  FROM business_customers bc
		  JOIN customer_profiles cp ON cp.id = bc.customer_id
		  JOIN users u ON u.id = cp.user_id
		 WHERE %s
		 ORDER BY COALESCE(bc.last_order_at, bc.first_seen_at) DESC`, rowColumns, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list business customers: %w", err)
	}
	defer rows.Close()
	out := []*Row{}
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// EnsureCustomer creates a NEW-status CRM row the first time a customer
// interacts with a business (e.g. an AI chat), without disturbing an
// existing row's lead status or notes.
func (r *Repo) EnsureCustomer(ctx context.Context, businessID, customerID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO business_customers (business_id, customer_id)
		VALUES ($1, $2)
		ON CONFLICT (business_id, customer_id) DO NOTHING`, businessID, customerID)
	if err != nil {
		return fmt.Errorf("ensure business customer: %w", err)
	}
	return nil
}

// UpdateInput is a business's edit to its own view of a customer.
type UpdateInput struct {
	LeadStatus string
	Notes      *string
}

func validLeadStatus(s string) bool {
	switch s {
	case "NEW", "CONTACTED", "INTERESTED", "AWAITING_RESPONSE", "CONVERTED", "LOST":
		return true
	default:
		return false
	}
}

// Update sets lead status/notes for businessID's view of one customer. The
// row must already exist (created by an order or EnsureCustomer) — this
// never creates a CRM relationship for a customer who has never interacted
// with the business.
func (r *Repo) Update(ctx context.Context, businessID, customerID string, in UpdateInput) (*Row, error) {
	if !validLeadStatus(in.LeadStatus) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"leadStatus": "Invalid lead status."})
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE business_customers SET lead_status = $3, notes = $4
		 WHERE business_id = $1 AND customer_id = $2`,
		businessID, customerID, in.LeadStatus, in.Notes)
	if err != nil {
		return nil, fmt.Errorf("update business customer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	row, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM business_customers bc
		  JOIN customer_profiles cp ON cp.id = bc.customer_id
		  JOIN users u ON u.id = cp.user_id
		 WHERE bc.business_id = $1 AND bc.customer_id = $2`, rowColumns), businessID, customerID)
	if err != nil {
		return nil, fmt.Errorf("reload business customer: %w", err)
	}
	defer row.Close()
	if !row.Next() {
		return nil, httpx.ErrNotFound
	}
	return scanRow(row)
}
