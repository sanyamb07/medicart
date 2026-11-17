package orders

import "testing"

func TestLegalTransitions(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{StatusCreated, StatusAwaitingCustomerConfirmation, true},
		{StatusAwaitingCustomerConfirmation, StatusAwaitingPayment, true},
		{StatusAwaitingPayment, StatusPaymentPending, true},
		{StatusPaymentPending, StatusPaid, true},
		{StatusPaymentPending, StatusAwaitingPayment, true}, // failed payment, retry
		{StatusPaid, StatusProcessing, true},
		{StatusProcessing, StatusReadyForDispatch, true},
		{StatusReadyForDispatch, StatusShipped, true},
		{StatusShipped, StatusDelivered, true},
		{StatusDelivered, StatusRefundPending, true},
		{StatusRefundPending, StatusRefunded, true},

		// Illegal: skipping states, going backwards, or acting on a terminal status.
		{StatusCreated, StatusPaid, false},
		{StatusAwaitingCustomerConfirmation, StatusPaid, false},
		{StatusPaid, StatusAwaitingPayment, false},
		{StatusShipped, StatusProcessing, false},
		{StatusDelivered, StatusShipped, false},
		{StatusCancelled, StatusAwaitingPayment, false},
		{StatusRefunded, StatusPaid, false},
		{StatusPaymentPending, StatusDelivered, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{StatusCancelled, StatusRefunded} {
		if !IsTerminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range []string{StatusCreated, StatusPaid, StatusShipped} {
		if IsTerminal(s) {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestCanCustomerCancel(t *testing.T) {
	allowed := []string{StatusCreated, StatusAwaitingCustomerConfirmation, StatusAwaitingPayment, StatusPaymentPending}
	for _, s := range allowed {
		if !CanCustomerCancel(s) {
			t.Errorf("customer should be able to cancel from %s", s)
		}
	}
	// Once money has moved, cancellation becomes a staff/refund decision.
	disallowed := []string{StatusPaid, StatusProcessing, StatusShipped, StatusDelivered}
	for _, s := range disallowed {
		if CanCustomerCancel(s) {
			t.Errorf("customer should NOT be able to cancel from %s", s)
		}
	}
}

// Every status referenced by the transition table must also be a key in it
// (even if its value is an empty slice) — otherwise a typo'd status string
// would silently behave as terminal instead of failing loudly.
func TestTransitionTableCoversAllStatuses(t *testing.T) {
	all := []string{
		StatusCreated, StatusAwaitingCustomerConfirmation, StatusAwaitingPrescriptionReview,
		StatusAwaitingPayment, StatusPaymentPending, StatusPaid, StatusProcessing,
		StatusReadyForDispatch, StatusShipped, StatusDelivered, StatusCancelled,
		StatusRefundPending, StatusRefunded,
	}
	for _, s := range all {
		if _, ok := legalTransitions[s]; !ok {
			t.Errorf("status %s has no entry in legalTransitions", s)
		}
	}
}
