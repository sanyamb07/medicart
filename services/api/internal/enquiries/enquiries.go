// Package enquiries records AI chat sessions and the leads they generate, so
// a business never silently loses a customer who showed interest in the chat
// widget but didn't place an order. It is deliberately independent of
// internal/assistant: the assistant decides *when* interest was shown, this
// package decides *how* that's stored and surfaced to a business.
package enquiries

import (
	"context"
	"fmt"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Row struct {
	ID                string     `json:"id"`
	CustomerID        *string    `json:"customerId"`
	Mobile            *string    `json:"mobile"`
	FullName          *string    `json:"fullName"`
	Channel           string     `json:"channel"`
	ProductsDiscussed []string   `json:"productsDiscussed"`
	SearchQueries     []string   `json:"searchQueries"`
	Intent            *string    `json:"intent"`
	Status            string     `json:"status"`
	ContactName       *string    `json:"contactName"`
	ContactMobile     *string    `json:"contactMobile"`
	ContactEmail      *string    `json:"contactEmail"`
	FollowUpAt        *time.Time `json:"followUpAt"`
	Notes             *string    `json:"notes"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Repo struct{ pool *db.Pool }

func NewRepo(pool *db.Pool) *Repo { return &Repo{pool: pool} }

// StartOrContinueSession returns an open chat_session for this customer at
// this business, reusing one created within the last 30 minutes (the same
// conversation) or starting a new one.
func (r *Repo) StartOrContinueSession(ctx context.Context, businessID, customerID string) (string, error) {
	var sessionID string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text FROM chat_sessions
		 WHERE business_id = $1 AND customer_id = $2 AND created_at > now() - INTERVAL '30 minutes'
		 ORDER BY created_at DESC LIMIT 1`, businessID, customerID).Scan(&sessionID)
	if err == nil {
		return sessionID, nil
	}
	if !db.IsNoRows(err) {
		return "", fmt.Errorf("find chat session: %w", err)
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO chat_sessions (business_id, customer_id, provider)
		VALUES ($1, $2, 'mock')
		RETURNING id::text`, businessID, customerID).Scan(&sessionID)
	if err != nil {
		return "", fmt.Errorf("create chat session: %w", err)
	}
	return sessionID, nil
}

// LogMessage appends one turn to a session's transcript and bumps its
// message count.
func (r *Repo) LogMessage(ctx context.Context, sessionID, role, content string) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO chat_messages (session_id, role, content) VALUES ($1, $2, $3)`,
		sessionID, role, content); err != nil {
		return fmt.Errorf("log chat message: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`UPDATE chat_sessions SET message_count = message_count + 1 WHERE id = $1`, sessionID); err != nil {
		return fmt.Errorf("bump chat session: %w", err)
	}
	return nil
}

// MarkEscalated records that a session was handed off (a clinical question,
// or anything the assistant refused to answer) — the business admin's
// enquiries screen can filter to these.
func (r *Repo) MarkEscalated(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE chat_sessions SET escalated_at = now() WHERE id = $1 AND escalated_at IS NULL`, sessionID)
	if err != nil {
		return fmt.Errorf("mark escalated: %w", err)
	}
	return nil
}

// RecordIntent upserts the open enquiry for a chat session — creating it the
// first time the session shows product/price/purchase intent, and appending
// to it on subsequent turns in the same session, so one conversation yields
// one lead rather than one per message.
func (r *Repo) RecordIntent(ctx context.Context, businessID, customerID, sessionID, intent string, products, queries []string) error {
	var existing string
	err := r.pool.QueryRow(ctx,
		`SELECT id::text FROM enquiries WHERE chat_session_id = $1`, sessionID).Scan(&existing)
	if err == nil {
		_, err := r.pool.Exec(ctx, `
			UPDATE enquiries SET
				products_discussed = (SELECT array_agg(DISTINCT v) FROM unnest(products_discussed || $2::text[]) AS v),
				search_queries = (SELECT array_agg(DISTINCT v) FROM unnest(search_queries || $3::text[]) AS v),
				intent = $4
			 WHERE id = $1`, existing, products, queries, intent)
		if err != nil {
			return fmt.Errorf("update enquiry: %w", err)
		}
		return nil
	}
	if !db.IsNoRows(err) {
		return fmt.Errorf("find enquiry: %w", err)
	}

	var contactName, contactMobile *string
	_ = r.pool.QueryRow(ctx, `
		SELECT u.full_name, u.mobile FROM customer_profiles cp JOIN users u ON u.id = cp.user_id
		 WHERE cp.id = $1`, customerID).Scan(&contactName, &contactMobile)

	_, err = r.pool.Exec(ctx, `
		INSERT INTO enquiries (business_id, customer_id, chat_session_id, channel, products_discussed, search_queries, intent, contact_name, contact_mobile)
		VALUES ($1, $2, $3, 'AI_CHAT', $4, $5, $6, $7, $8)`,
		businessID, customerID, sessionID, products, queries, intent, contactName, contactMobile)
	if err != nil {
		return fmt.Errorf("create enquiry: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- business admin

var rowColumns = `
	e.id::text, e.customer_id::text, u.mobile, u.full_name, e.channel, e.products_discussed, e.search_queries,
	e.intent, e.status, e.contact_name, e.contact_mobile, e.contact_email, e.follow_up_at, e.notes,
	e.created_at, e.updated_at`

func scanRow(row interface{ Scan(...any) error }) (*Row, error) {
	var r Row
	if err := row.Scan(&r.ID, &r.CustomerID, &r.Mobile, &r.FullName, &r.Channel, &r.ProductsDiscussed, &r.SearchQueries,
		&r.Intent, &r.Status, &r.ContactName, &r.ContactMobile, &r.ContactEmail, &r.FollowUpAt, &r.Notes,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// List returns a business's enquiries, most recently updated first,
// optionally filtered to one status.
func (r *Repo) List(ctx context.Context, businessID, status string) ([]*Row, error) {
	where := "e.business_id = $1"
	args := []any{businessID}
	if status != "" {
		where += " AND e.status = $2"
		args = append(args, status)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s
		  FROM enquiries e
		  LEFT JOIN customer_profiles cp ON cp.id = e.customer_id
		  LEFT JOIN users u ON u.id = cp.user_id
		 WHERE %s
		 ORDER BY e.updated_at DESC`, rowColumns, where), args...)
	if err != nil {
		return nil, fmt.Errorf("list enquiries: %w", err)
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

// UpdateInput is a business's edit to one enquiry.
type UpdateInput struct {
	Status     string
	Notes      *string
	FollowUpAt *time.Time
}

func validStatus(s string) bool {
	switch s {
	case "NEW", "CONTACTED", "INTERESTED", "AWAITING_RESPONSE", "CONVERTED", "LOST":
		return true
	default:
		return false
	}
}

// Update sets status/notes/follow-up for one of a business's own enquiries.
func (r *Repo) Update(ctx context.Context, businessID, enquiryID string, in UpdateInput) (*Row, error) {
	if !validStatus(in.Status) {
		return nil, httpx.ErrValidation.WithFields(map[string]string{"status": "Invalid enquiry status."})
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE enquiries SET status = $3, notes = $4, follow_up_at = $5
		 WHERE id = $1 AND business_id = $2`,
		enquiryID, businessID, in.Status, in.Notes, in.FollowUpAt)
	if err != nil {
		return nil, fmt.Errorf("update enquiry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM enquiries e
		  LEFT JOIN customer_profiles cp ON cp.id = e.customer_id
		  LEFT JOIN users u ON u.id = cp.user_id
		 WHERE e.id = $1`, rowColumns), enquiryID)
	if err != nil {
		return nil, fmt.Errorf("reload enquiry: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, httpx.ErrNotFound
	}
	return scanRow(rows)
}
