// Package audit records an append-only trail of sensitive operations.
//
// What belongs here: anything a regulator, an auditor or an incident
// investigation would need to reconstruct — who changed an order's status, who
// approved a prescription, who adjusted stock, who issued a refund.
//
// What must never be written here: prescription images or their contents,
// credentials, payment instrument details, or bulk personal data. The metadata
// map is for identifiers and before/after values of business fields only.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/platform/logger"
)

// Action constants keep the audit vocabulary closed, so reports can group by
// action without worrying about typos creating new categories.
const (
	ActionOrderCreated       = "order.created"
	ActionOrderStatusChanged = "order.status_changed"
	ActionOrderCancelled     = "order.cancelled"
	ActionOrderConfirmed     = "order.whatsapp_confirmed"
	ActionOrderNoteAdded     = "order.note_added"

	ActionPaymentCreated   = "payment.attempt_created"
	ActionPaymentSucceeded = "payment.succeeded"
	ActionPaymentFailed    = "payment.failed"
	ActionRefundInitiated  = "refund.initiated"
	ActionRefundCompleted  = "refund.completed"

	ActionPrescriptionUploaded = "prescription.uploaded"
	ActionPrescriptionViewed   = "prescription.viewed"
	ActionPrescriptionApproved = "prescription.approved"
	ActionPrescriptionRejected = "prescription.rejected"

	ActionProductCreated     = "product.created"
	ActionProductUpdated     = "product.updated"
	ActionProductDeactivated = "product.deactivated"

	ActionBatchReceived  = "inventory.batch_received"
	ActionBatchBlocked   = "inventory.batch_blocked"
	ActionStockAdjusted  = "inventory.stock_adjusted"
	ActionExpiryWriteoff = "inventory.expiry_writeoff"

	ActionLoginSucceeded = "auth.login_succeeded"
	ActionLoginFailed    = "auth.login_failed"
	ActionLogout         = "auth.logout"
	ActionRoleGranted    = "auth.role_granted"
	ActionRoleRevoked    = "auth.role_revoked"

	ActionCustomerDataViewed = "customer.data_viewed"
	ActionPolicyUpdated      = "policy.updated"
	ActionAISettingUpdated   = "ai.setting_updated"

	ActionAffiliateApplied        = "affiliate.applied"
	ActionAffiliateApproved       = "affiliate.approved"
	ActionAffiliateRejected       = "affiliate.rejected"
	ActionAffiliateSuspended      = "affiliate.suspended"
	ActionAffiliateCommissionPaid = "affiliate.commission_paid"
	ActionAffiliateSettingUpdated = "affiliate.setting_updated"
)

// Entry is one audit record.
type Entry struct {
	ActorID    *string
	ActorRole  string
	Action     string
	EntityType string
	EntityID   string
	Metadata   map[string]any
	IPAddress  string
	RequestID  string
}

// Recorder writes audit entries.
type Recorder struct{ pool *db.Pool }

// New returns a Recorder backed by pool.
func New(pool *db.Pool) *Recorder { return &Recorder{pool: pool} }

// Record appends an entry using the pool.
//
// An audit write failure must not fail the business operation that succeeded —
// refusing a completed refund because logging broke would be worse than the
// gap. The failure is logged at error level so it is visible in monitoring.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	if err := r.RecordTx(ctx, r.pool, e); err != nil {
		logger.FromContext(ctx).Error("audit write failed",
			"action", e.Action, "entity_type", e.EntityType, "entity_id", e.EntityID,
			"err", err.Error())
	}
}

// RecordTx appends an entry using an explicit querier, so the audit row can
// commit atomically with the change it describes. Prefer this inside a
// transaction: it guarantees there is no state change without its audit row.
func (r *Recorder) RecordTx(ctx context.Context, q db.Querier, e Entry) error {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		// Fall back to an empty object rather than losing the whole entry.
		raw = []byte(`{}`)
	}

	var ip any
	if e.IPAddress != "" {
		ip = e.IPAddress
	}

	_, err = q.Exec(ctx, `
		INSERT INTO audit_logs
			(actor_id, actor_role, action, entity_type, entity_id, metadata, ip_address, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ActorID, nullIfEmpty(e.ActorRole), e.Action, e.EntityType, e.EntityID,
		raw, ip, nullIfEmpty(e.RequestID))
	return err
}

// Record is the queryable view of an audit entry. Times marshal as RFC 3339
// UTC, which the frontend renders in the viewer's locale.
type Record struct {
	ID         int64          `json:"id"`
	ActorID    *string        `json:"actorId"`
	ActorRole  *string        `json:"actorRole"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	EntityID   string         `json:"entityId"`
	Metadata   map[string]any `json:"metadata"`
	RequestID  *string        `json:"requestId"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ListForEntity returns the trail for one entity, newest first.
func (r *Recorder) ListForEntity(ctx context.Context, entityType, entityID string, limit int) ([]Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, actor_id::text, actor_role, action, entity_type, entity_id,
		       metadata, request_id, created_at
		  FROM audit_logs
		 WHERE entity_type = $1 AND entity_id = $2
		 ORDER BY created_at DESC, id DESC
		 LIMIT $3`, entityType, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Record, 0, limit)
	for rows.Next() {
		var rec Record
		var raw []byte
		if err := rows.Scan(&rec.ID, &rec.ActorID, &rec.ActorRole, &rec.Action,
			&rec.EntityType, &rec.EntityID, &raw, &rec.RequestID, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.Metadata)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
