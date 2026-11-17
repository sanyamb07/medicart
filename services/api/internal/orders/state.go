// Package orders implements order creation, the order status state machine,
// and customer/admin order access.
package orders

// Status values, matching the CHECK constraint on orders.status exactly —
// the two must never diverge, which is why this list is a literal copy of
// the migration's enum rather than something generated from it.
const (
	StatusCreated                      = "CREATED"
	StatusAwaitingCustomerConfirmation = "AWAITING_CUSTOMER_CONFIRMATION"
	StatusAwaitingPrescriptionReview   = "AWAITING_PRESCRIPTION_REVIEW"
	StatusAwaitingPayment              = "AWAITING_PAYMENT"
	StatusPaymentPending               = "PAYMENT_PENDING"
	StatusPaid                         = "PAID"
	StatusProcessing                   = "PROCESSING"
	StatusReadyForDispatch             = "READY_FOR_DISPATCH"
	StatusShipped                      = "SHIPPED"
	StatusDelivered                    = "DELIVERED"
	StatusCancelled                    = "CANCELLED"
	StatusRefundPending                = "REFUND_PENDING"
	StatusRefunded                     = "REFUNDED"
)

// legalTransitions is the single source of truth for which status changes are
// allowed. Every transition in the application goes through CanTransition —
// the frontend can never set a status directly, and staff/webhook code paths
// are checked against this table too, so an invalid jump is rejected
// regardless of which caller attempted it.
var legalTransitions = map[string][]string{
	StatusCreated:                      {StatusAwaitingCustomerConfirmation, StatusCancelled},
	StatusAwaitingCustomerConfirmation: {StatusAwaitingPrescriptionReview, StatusAwaitingPayment, StatusCancelled},
	StatusAwaitingPrescriptionReview:   {StatusAwaitingPayment, StatusCancelled},
	StatusAwaitingPayment:              {StatusPaymentPending, StatusCancelled},
	StatusPaymentPending:               {StatusPaid, StatusAwaitingPayment, StatusCancelled},
	StatusPaid:                         {StatusProcessing, StatusCancelled, StatusRefundPending},
	StatusProcessing:                   {StatusReadyForDispatch, StatusCancelled, StatusRefundPending},
	StatusReadyForDispatch:             {StatusShipped, StatusCancelled, StatusRefundPending},
	StatusShipped:                      {StatusDelivered, StatusRefundPending},
	StatusDelivered:                    {StatusRefundPending},
	StatusCancelled:                    {},
	StatusRefundPending:                {StatusRefunded},
	StatusRefunded:                     {},
}

// CanTransition reports whether moving from `from` to `to` is allowed.
func CanTransition(from, to string) bool {
	for _, allowed := range legalTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether a status has no further legal transitions.
func IsTerminal(status string) bool {
	return len(legalTransitions[status]) == 0
}

// CancellableStatuses are the statuses from which a customer-initiated
// cancellation is allowed. Once payment has succeeded, cancellation becomes a
// refund decision for staff instead.
var cancellableByCustomer = map[string]bool{
	StatusCreated:                      true,
	StatusAwaitingCustomerConfirmation: true,
	StatusAwaitingPrescriptionReview:   true,
	StatusAwaitingPayment:              true,
	StatusPaymentPending:               true,
}

// CanCustomerCancel reports whether the customer (as opposed to staff) may
// cancel from this status.
func CanCustomerCancel(status string) bool { return cancellableByCustomer[status] }
