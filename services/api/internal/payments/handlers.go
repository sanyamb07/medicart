package payments

import (
	"io"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/orders"
)

type Handlers struct {
	svc      *Service
	orderSvc *orders.Service
}

func NewHandlers(svc *Service, orderSvc *orders.Service) *Handlers {
	return &Handlers{svc: svc, orderSvc: orderSvc}
}

// Register mounts the public payment routes: starting a sandbox payment and
// receiving the (sandbox) webhook. Both require knowing the order reference,
// which is unguessable, and CreateAttempt additionally re-validates the
// order's own state machine — so this is not an open "pay any order" endpoint.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/orders/:reference/pay", h.createAttempt)
	r.POST("/payments/webhook/mock", h.webhook)
	r.GET("/payments/mock/:providerOrderId", h.mockCheckoutSummary)
	r.POST("/payments/mock/:providerOrderId/complete", h.mockCheckoutComplete)
}

// RegisterAdmin mounts the staff-only simulate action.
func (h *Handlers) RegisterAdmin(r gin.IRouter) {
	r.POST("/payments/:providerOrderId/simulate", h.adminSimulate)
}

type payReq struct {
	ContactMobile string `json:"contactMobile" binding:"required"`
}

func (h *Handlers) createAttempt(c *gin.Context) {
	var req payReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.orderSvc.GetByReferenceForCustomer(c.Request.Context(), c.Param("reference"), auth.NormaliseMobile(req.ContactMobile))
	if err != nil {
		if err == orders.ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("We could not find an order matching those details."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	att, err := h.svc.CreateAttempt(c.Request.Context(), order.ID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, att)
}

// webhook receives the sandbox gateway's signed event. The signature is
// verified over the exact raw bytes received — not a re-serialised struct —
// because a provider signs the bytes it sent, and re-encoding JSON can
// reorder keys or change whitespace and invalidate a correct signature.
func (h *Handlers) webhook(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<16))
	if err != nil {
		httpx.Fail(c, httpx.ErrBadRequest.WithInternal(err))
		return
	}
	signature := c.GetHeader("X-MediCart-Signature")

	alreadyProcessed, err := h.svc.HandleWebhook(c.Request.Context(), raw, signature)
	if err != nil {
		if err == ErrInvalidSignature {
			httpx.Fail(c, httpx.ErrUnauthorized.WithMessage("Invalid webhook signature."))
			return
		}
		if err == ErrAttemptNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("Unknown payment attempt."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"alreadyProcessed": alreadyProcessed})
}

// mockCheckoutSummaryResp is what the customer-facing mock checkout page
// renders: enough to show "Pay ₹133.00 for order MC-..." without exposing the
// customer's address or anything else from the order.
type mockCheckoutSummaryResp struct {
	OrderReference string `json:"orderReference"`
	AmountPaise    int64  `json:"amountPaise"`
	Status         string `json:"status"`
	IsTestMode     bool   `json:"isTestMode"`
}

func (h *Handlers) mockCheckoutSummary(c *gin.Context) {
	att, reference, err := h.svc.GetByProviderOrderID(c.Request.Context(), c.Param("providerOrderId"))
	if err != nil {
		if err == ErrAttemptNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("This payment link is invalid or has expired."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, mockCheckoutSummaryResp{
		OrderReference: reference, AmountPaise: att.AmountPaise, Status: att.Status, IsTestMode: att.IsTestMode,
	})
}

type mockCompleteReq struct {
	// Method is the payment method the customer picked on the mock checkout
	// page: "upi" | "card" | "netbanking". It is a label only — no payment
	// instrument details (card number, UPI PIN, etc.) are ever collected or
	// transmitted; this mirrors a real gateway, where MediCart never sees
	// those either.
	Method        string `json:"method" binding:"required,oneof=upi card netbanking"`
	Succeed       bool   `json:"succeed"`
	FailureReason string `json:"failureReason"`
}

// mockCheckoutComplete is what the customer's "Pay" click calls. It is the
// sandbox stand-in for the customer completing payment on a real gateway's
// own hosted page and that gateway then notifying us by webhook — so this
// handler builds and signs that same webhook event and runs it through
// HandleWebhook, the exact function a live Razorpay webhook would hit.
func (h *Handlers) mockCheckoutComplete(c *gin.Context) {
	var req mockCompleteReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.CompleteMock(c.Request.Context(), c.Param("providerOrderId"), req.Method, req.Succeed, req.FailureReason); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"completed": true})
}

type simulateReq struct {
	Succeed       bool   `json:"succeed"`
	FailureReason string `json:"failureReason"`
}

// adminSimulate lets an authenticated staff member trigger a sandbox payment
// outcome. It builds and signs the same event shape HandleWebhook expects and
// runs it through that exact function — there is no separate "pretend it
// succeeded" shortcut that bypasses signature/idempotency logic.
func (h *Handlers) adminSimulate(c *gin.Context) {
	var req simulateReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.SimulateFor(c.Request.Context(), c.Param("providerOrderId"), req.Succeed, req.FailureReason); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"simulated": true})
}
