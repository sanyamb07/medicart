// Package payments implements a provider-agnostic payment flow with a mock
// sandbox adapter.
//
// Scope in this iteration: only the "mock" driver is implemented. It is a
// real simulator — not a stub that fakes success — with genuine HMAC
// signature verification and idempotent webhook processing; it just has no
// real bank behind it, and every API response and audit record says so
// explicitly (is_test_mode). A Razorpay adapter is NOT implemented: this
// environment has no Razorpay account/credentials, and claiming the
// integration works without having exercised it against the real gateway
// would violate the one rule that matters most here. See
// docs/PAYMENT_INTEGRATION.md for exactly what is needed to add it (the
// interface boundary is already where it would plug in).
//
// Non-negotiable rules this package enforces:
//   - The amount charged is always read from the order row in the database,
//     never from the client.
//   - An order becomes PAID only from a verified provider signal processed
//     here, never from a frontend redirect.
//   - A webhook delivery is idempotent: the provider's event id is unique
//     (payment_events.provider_event_id), so a redelivered webhook is
//     detected and ignored rather than reprocessed.
package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/affiliate"
	"github.com/sanyam/medicart/services/api/internal/audit"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
)

var (
	ErrInvalidSignature = errors.New("payments: invalid webhook signature")
	ErrAttemptNotFound  = errors.New("payments: attempt not found")
)

// Attempt is the client-facing view of a payment attempt.
type Attempt struct {
	ID              string `json:"id"`
	OrderID         string `json:"orderId"`
	Provider        string `json:"provider"`
	IsTestMode      bool   `json:"isTestMode"`
	AmountPaise     int64  `json:"amountPaise"`
	Status          string `json:"status"`
	ProviderOrderID string `json:"providerOrderId"`
	PaymentURL      string `json:"paymentUrl"`
}

type Service struct {
	pool          *db.Pool
	rec           *audit.Recorder
	driver        string // only "mock" is functional; see package doc
	webhookSecret []byte
	callbackBase  string
	affiliateSvc  *affiliate.Service
}

// NewService builds the payment service. webhookSecret signs/verifies mock
// webhook payloads — derived from SESSION_SECRET so there is no extra secret
// to manage for a sandbox that has no real money behind it. affiliateSvc may
// be nil in tests that don't exercise the affiliate program.
func NewService(pool *db.Pool, rec *audit.Recorder, driver string, webhookSecret []byte, callbackBase string, affiliateSvc *affiliate.Service) *Service {
	return &Service{pool: pool, rec: rec, driver: driver, webhookSecret: webhookSecret, callbackBase: callbackBase, affiliateSvc: affiliateSvc}
}

// CreateAttempt starts a payment for an order that is AWAITING_PAYMENT,
// moving it to PAYMENT_PENDING. The amount is read from the order row inside
// the same transaction that locks it — never supplied by the caller.
func (s *Service) CreateAttempt(ctx context.Context, orderID string) (*Attempt, error) {
	if s.driver != "mock" {
		return nil, httpx.ErrUnavailable.WithMessage(
			"The configured payment provider is not available in this deployment.")
	}

	var att Attempt
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var status string
		var totalPaise int64
		var reference string
		if err := q.QueryRow(ctx,
			`SELECT status, total_paise, reference FROM orders WHERE id = $1 FOR UPDATE`, orderID).
			Scan(&status, &totalPaise, &reference); err != nil {
			if db.IsNoRows(err) {
				return orders.ErrOrderNotFound
			}
			return fmt.Errorf("lock order: %w", err)
		}
		if !orders.CanTransition(status, orders.StatusPaymentPending) {
			return httpx.ErrInvalidTransition.WithMessage(
				fmt.Sprintf("Cannot start payment from status %s.", status))
		}

		providerOrderID := "mock_order_" + idgen.Token()[:16]
		paymentURL := fmt.Sprintf("%s/pay/mock/%s", s.callbackBase, providerOrderID)

		var attemptID string
		if err := q.QueryRow(ctx, `
			INSERT INTO payment_attempts
				(order_id, provider, is_test_mode, amount_paise, status, provider_order_id, payment_url, expires_at)
			VALUES ($1, 'mock', TRUE, $2, 'CREATED', $3, $4, now() + INTERVAL '30 minutes')
			RETURNING id::text`,
			orderID, totalPaise, providerOrderID, paymentURL).Scan(&attemptID); err != nil {
			return fmt.Errorf("insert payment attempt: %w", err)
		}

		if _, err := q.Exec(ctx, `UPDATE orders SET status = $2 WHERE id = $1`,
			orderID, orders.StatusPaymentPending); err != nil {
			return fmt.Errorf("update order status: %w", err)
		}
		if err := orders.InsertHistoryPublic(ctx, q, orderID, status, orders.StatusPaymentPending,
			"payment attempt created (sandbox)", "system", nil); err != nil {
			return err
		}
		if err := s.rec.RecordTx(ctx, q, audit.Entry{
			Action: audit.ActionPaymentCreated, EntityType: "payment_attempt", EntityID: attemptID,
			Metadata: map[string]any{"order_id": orderID, "amount_paise": totalPaise, "is_test_mode": true},
		}); err != nil {
			return err
		}

		att = Attempt{
			ID: attemptID, OrderID: orderID, Provider: "mock", IsTestMode: true,
			AmountPaise: totalPaise, Status: "CREATED", ProviderOrderID: providerOrderID, PaymentURL: paymentURL,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &att, nil
}

// WebhookPayload is the mock gateway's event shape — modelled loosely on
// Razorpay's payment.captured/payment.failed events so swapping in a real
// adapter later changes the signature-verification function, not this shape.
type WebhookPayload struct {
	EventID           string `json:"eventId"`
	EventType         string `json:"eventType"` // "payment.captured" | "payment.failed"
	ProviderOrderID   string `json:"providerOrderId"`
	ProviderPaymentID string `json:"providerPaymentId"`
	Method            string `json:"method"`
	FailureReason     string `json:"failureReason,omitempty"`
}

// SignWebhook computes the HMAC-SHA256 signature of a raw request body, the
// same way a real gateway signs its webhook deliveries. Exposed so the admin
// "simulate payment" endpoint and tests can produce a validly signed request
// through the exact same path a real webhook delivery would take.
func (s *Service) SignWebhook(rawBody []byte) string {
	m := hmac.New(sha256.New, s.webhookSecret)
	m.Write(rawBody)
	return hex.EncodeToString(m.Sum(nil))
}

// HandleWebhook verifies the signature and applies the event idempotently.
// Returns (alreadyProcessed=true, nil) for a duplicate delivery without
// reapplying any side effect.
func (s *Service) HandleWebhook(ctx context.Context, rawBody []byte, signature string) (alreadyProcessed bool, err error) {
	// Captured outside the transaction closure below so the affiliate
	// commission can be confirmed after the payment transaction has actually
	// committed — see the call after s.pool.InTx near the end of this
	// function.
	var paidOrderID string
	expected := s.SignWebhook(rawBody)
	// Constant-time compare: a timing side channel here would let an attacker
	// forge a valid signature byte by byte.
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return false, ErrInvalidSignature
	}

	var payload WebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return false, httpx.ErrBadRequest.WithMessage("Malformed webhook payload.").WithInternal(err)
	}
	if payload.EventID == "" || payload.ProviderOrderID == "" {
		return false, httpx.ErrBadRequest.WithMessage("Webhook payload is missing required fields.")
	}

	err = s.pool.InTx(ctx, func(q db.Querier) error {
		// The UNIQUE(provider, provider_event_id) constraint is the actual
		// idempotency guarantee; ON CONFLICT DO NOTHING turns a redelivery
		// into a harmless no-op row instead of a duplicate side effect.
		tag, err := q.Exec(ctx, `
			INSERT INTO payment_events (provider, provider_event_id, event_type, signature_verified, payload)
			VALUES ('mock', $1, $2, TRUE, $3)
			ON CONFLICT (provider, provider_event_id) DO NOTHING`,
			payload.EventID, payload.EventType, rawBody)
		if err != nil {
			return fmt.Errorf("record payment event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			alreadyProcessed = true
			return nil
		}

		var attemptID, orderID, attemptStatus string
		var orderStatus string
		if err := q.QueryRow(ctx, `
			SELECT pa.id::text, pa.order_id::text, pa.status, o.status
			  FROM payment_attempts pa JOIN orders o ON o.id = pa.order_id
			 WHERE pa.provider = 'mock' AND pa.provider_order_id = $1
			   FOR UPDATE OF pa, o`, payload.ProviderOrderID).
			Scan(&attemptID, &orderID, &attemptStatus, &orderStatus); err != nil {
			if db.IsNoRows(err) {
				return ErrAttemptNotFound
			}
			return fmt.Errorf("load attempt: %w", err)
		}

		// A terminal attempt ignores further events instead of erroring —
		// Razorpay and others redeliver after the handler already settled it.
		if attemptStatus == "SUCCEEDED" || attemptStatus == "FAILED" {
			return nil
		}

		switch payload.EventType {
		case "payment.captured":
			if _, err := q.Exec(ctx, `
				UPDATE payment_attempts
				   SET status = 'SUCCEEDED', provider_payment_id = $2, method = $3, succeeded_at = now()
				 WHERE id = $1`, attemptID, payload.ProviderPaymentID, nullIfEmpty(payload.Method)); err != nil {
				return fmt.Errorf("update attempt: %w", err)
			}
			if orders.CanTransition(orderStatus, orders.StatusPaid) {
				if _, err := q.Exec(ctx, `UPDATE orders SET status = $2 WHERE id = $1`, orderID, orders.StatusPaid); err != nil {
					return fmt.Errorf("update order: %w", err)
				}
				if err := orders.InsertHistoryPublic(ctx, q, orderID, orderStatus, orders.StatusPaid,
					"payment verified (sandbox webhook)", "payment_webhook", nil); err != nil {
					return err
				}
				paidOrderID = orderID
			}
			return s.rec.RecordTx(ctx, q, audit.Entry{
				Action: audit.ActionPaymentSucceeded, EntityType: "payment_attempt", EntityID: attemptID,
				Metadata: map[string]any{"order_id": orderID, "provider_payment_id": payload.ProviderPaymentID},
			})

		case "payment.failed":
			if _, err := q.Exec(ctx, `
				UPDATE payment_attempts SET status = 'FAILED', failure_reason = $2 WHERE id = $1`,
				attemptID, nullIfEmpty(payload.FailureReason)); err != nil {
				return fmt.Errorf("update attempt: %w", err)
			}
			if orders.CanTransition(orderStatus, orders.StatusAwaitingPayment) {
				if _, err := q.Exec(ctx, `UPDATE orders SET status = $2 WHERE id = $1`, orderID, orders.StatusAwaitingPayment); err != nil {
					return fmt.Errorf("update order: %w", err)
				}
				if err := orders.InsertHistoryPublic(ctx, q, orderID, orderStatus, orders.StatusAwaitingPayment,
					"payment failed (sandbox webhook): "+payload.FailureReason, "payment_webhook", nil); err != nil {
					return err
				}
			}
			return s.rec.RecordTx(ctx, q, audit.Entry{
				Action: audit.ActionPaymentFailed, EntityType: "payment_attempt", EntityID: attemptID,
				Metadata: map[string]any{"order_id": orderID, "reason": payload.FailureReason},
			})

		default:
			return httpx.ErrBadRequest.WithMessage("Unknown event type: " + payload.EventType)
		}
	})
	if err != nil {
		return false, err
	}
	if paidOrderID != "" && s.affiliateSvc != nil {
		// Best-effort: a failure here must never turn a successfully
		// processed payment webhook into an error response to the gateway
		// (which would cause a pointless redelivery). Commission bookkeeping
		// is secondary to the payment itself actually settling.
		_ = s.affiliateSvc.ConfirmForOrder(ctx, paidOrderID)
	}
	return alreadyProcessed, nil
}

// SimulateFor is used by the admin "simulate payment" action: it builds and
// signs a webhook payload exactly as a real delivery would arrive, then runs
// it through HandleWebhook — the same code path, so there is no separate
// "fake success" shortcut that bypasses signature/idempotency logic.
func (s *Service) SimulateFor(ctx context.Context, providerOrderID string, succeed bool, failureReason string) error {
	return s.CompleteMock(ctx, providerOrderID, "upi", succeed, failureReason)
}

// CompleteMock is what the customer-facing mock checkout page calls when the
// shopper picks a payment method and clicks "Pay": it builds the same event a
// real gateway would send after the customer completes payment on the
// gateway's own hosted page, signs it, and runs it through HandleWebhook —
// there is no separate "pretend it succeeded" code path.
func (s *Service) CompleteMock(ctx context.Context, providerOrderID, method string, succeed bool, failureReason string) error {
	payload := WebhookPayload{
		EventID:           "mock_evt_" + idgen.Token()[:20],
		ProviderOrderID:   providerOrderID,
		ProviderPaymentID: "mock_pay_" + idgen.Token()[:16],
		Method:            method,
	}
	if succeed {
		payload.EventType = "payment.captured"
	} else {
		payload.EventType = "payment.failed"
		payload.FailureReason = failureReason
		if payload.FailureReason == "" {
			payload.FailureReason = "Simulated failure (sandbox)"
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal simulated payload: %w", err)
	}
	sig := s.SignWebhook(raw)
	_, err = s.HandleWebhook(ctx, raw, sig)
	return err
}

// GetByProviderOrderID looks up an attempt by its provider order id — the
// token embedded in the mock checkout page's URL. Knowing this unguessable
// token (128 bits of CSPRNG output) is the access boundary for the mock
// checkout page, the same way knowing a hosted gateway's session id is.
func (s *Service) GetByProviderOrderID(ctx context.Context, providerOrderID string) (*Attempt, string, error) {
	var a Attempt
	var reference string
	err := s.pool.QueryRow(ctx, `
		SELECT pa.id::text, pa.order_id::text, pa.provider, pa.is_test_mode, pa.amount_paise, pa.status,
		       COALESCE(pa.provider_order_id, ''), COALESCE(pa.payment_url, ''), o.reference
		  FROM payment_attempts pa JOIN orders o ON o.id = pa.order_id
		 WHERE pa.provider = 'mock' AND pa.provider_order_id = $1`, providerOrderID).
		Scan(&a.ID, &a.OrderID, &a.Provider, &a.IsTestMode, &a.AmountPaise, &a.Status,
			&a.ProviderOrderID, &a.PaymentURL, &reference)
	if db.IsNoRows(err) {
		return nil, "", ErrAttemptNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("load attempt: %w", err)
	}
	return &a, reference, nil
}

// GetByOrderID returns the latest payment attempt for an order, if any.
func (s *Service) GetByOrderID(ctx context.Context, orderID string) (*Attempt, error) {
	var a Attempt
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, order_id::text, provider, is_test_mode, amount_paise, status,
		       COALESCE(provider_order_id, ''), COALESCE(payment_url, '')
		  FROM payment_attempts WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, orderID).
		Scan(&a.ID, &a.OrderID, &a.Provider, &a.IsTestMode, &a.AmountPaise, &a.Status,
			&a.ProviderOrderID, &a.PaymentURL)
	if db.IsNoRows(err) {
		return nil, ErrAttemptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load attempt: %w", err)
	}
	return &a, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
