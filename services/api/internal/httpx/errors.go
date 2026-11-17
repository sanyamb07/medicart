// Package httpx holds HTTP concerns only: error shapes, response helpers,
// middleware and the router. Business rules live in the domain packages; this
// package translates between them and the wire.
package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is the single error shape the API returns. A stable machine-readable
// `code` lets the frontend react (e.g. show the prescription modal) without
// string-matching human messages.
type APIError struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	// Internal is logged but never serialised to the client — it can contain
	// details that would help an attacker.
	Internal error `json:"-"`
}

func (e *APIError) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Internal)
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.Internal }

// WithInternal attaches a cause for the logs.
func (e *APIError) WithInternal(err error) *APIError {
	c := *e
	c.Internal = err
	return &c
}

// WithFields attaches per-field validation messages.
func (e *APIError) WithFields(f map[string]string) *APIError {
	c := *e
	c.Fields = f
	return &c
}

// Message overrides the human-readable text, keeping the code.
func (e *APIError) WithMessage(m string) *APIError {
	c := *e
	c.Message = m
	return &c
}

func newErr(status int, code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

// The error catalogue. Codes are stable API surface — the web app switches on
// them — so change the message freely but never the code.
var (
	ErrBadRequest   = newErr(http.StatusBadRequest, "bad_request", "The request could not be understood.")
	ErrValidation   = newErr(http.StatusUnprocessableEntity, "validation_failed", "Some details need correcting.")
	ErrUnauthorized = newErr(http.StatusUnauthorized, "unauthorized", "Please sign in to continue.")
	ErrForbidden    = newErr(http.StatusForbidden, "forbidden", "You do not have access to this resource.")
	ErrNotFound     = newErr(http.StatusNotFound, "not_found", "We could not find what you asked for.")
	ErrConflict     = newErr(http.StatusConflict, "conflict", "That action conflicts with the current state.")
	ErrRateLimited  = newErr(http.StatusTooManyRequests, "rate_limited", "Too many requests. Please wait a moment and try again.")
	ErrPayloadLarge = newErr(http.StatusRequestEntityTooLarge, "payload_too_large", "That file is larger than the allowed limit.")
	ErrInternal     = newErr(http.StatusInternalServerError, "internal_error", "Something went wrong on our side. Please try again.")
	ErrUnavailable  = newErr(http.StatusServiceUnavailable, "service_unavailable", "This service is temporarily unavailable. Please try again shortly.")

	// Domain-specific codes the UI handles explicitly.
	ErrOutOfStock = newErr(http.StatusConflict, "out_of_stock",
		"One or more items are no longer available in the quantity requested.")
	ErrPrescriptionRequired = newErr(http.StatusUnprocessableEntity, "prescription_required",
		"This order contains prescription-only medicines and needs a valid prescription.")
	ErrRxSalesDisabled = newErr(http.StatusForbidden, "rx_sales_disabled",
		"Prescription-only medicines cannot be ordered at the moment.")
	ErrNotServiceable = newErr(http.StatusUnprocessableEntity, "not_serviceable",
		"We do not deliver to that PIN code yet.")
	ErrInvalidTransition = newErr(http.StatusConflict, "invalid_state_transition",
		"That change is not allowed from the order's current status.")
	ErrCartEmpty = newErr(http.StatusUnprocessableEntity, "cart_empty",
		"Your cart is empty.")
	ErrPaymentNotVerified = newErr(http.StatusBadRequest, "payment_not_verified",
		"The payment could not be verified.")
)

// AsAPIError extracts an *APIError from an error chain, mapping anything
// unrecognised to a 500 so handlers can return domain errors directly.
func AsAPIError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	return ErrInternal.WithInternal(err)
}
