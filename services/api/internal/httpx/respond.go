package httpx

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/sanyam/medicart/services/api/internal/platform/logger"
)

// Envelope wraps every successful payload so clients have one shape to parse
// and so we can add top-level metadata (pagination) without breaking them.
type Envelope struct {
	Data any   `json:"data"`
	Meta *Meta `json:"meta,omitempty"`
}

// Meta carries pagination for list endpoints.
type Meta struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

// NewMeta computes page counts, guarding against a zero page size.
func NewMeta(page, pageSize, total int) *Meta {
	if pageSize < 1 {
		pageSize = 1
	}
	tp := (total + pageSize - 1) / pageSize
	return &Meta{Page: page, PageSize: pageSize, TotalItems: total, TotalPages: tp}
}

// OK writes 200 with the payload wrapped in an envelope.
func OK(c *gin.Context, data any) { c.JSON(http.StatusOK, Envelope{Data: data}) }

// Created writes 201.
func Created(c *gin.Context, data any) { c.JSON(http.StatusCreated, Envelope{Data: data}) }

// List writes 200 with pagination metadata.
func List(c *gin.Context, data any, m *Meta) {
	c.JSON(http.StatusOK, Envelope{Data: data, Meta: m})
}

// NoContent writes 204.
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// Fail writes an error response and records the cause for the logger.
// It aborts the handler chain so no later middleware writes a second body.
func Fail(c *gin.Context, err error) {
	ae := AsAPIError(err)

	if ae.Internal != nil {
		lg := logger.FromContext(c.Request.Context())
		// 5xx is our fault and deserves error level; 4xx is the client's and
		// would otherwise flood the logs with noise from ordinary validation.
		if ae.Status >= 500 {
			lg.Error("request failed", "code", ae.Code, "err", ae.Internal.Error())
		} else {
			lg.Debug("request rejected", "code", ae.Code, "err", ae.Internal.Error())
		}
	}
	c.AbortWithStatusJSON(ae.Status, gin.H{
		"code":    ae.Code,
		"message": ae.Message,
		"fields":  ae.Fields,
	})
}

// Bind decodes and validates a JSON body into dst, converting validator
// failures into per-field messages the frontend can place next to inputs.
func Bind(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return ErrValidation.WithFields(fieldMessages(ve))
		}
		return ErrBadRequest.WithMessage("The request body is not valid JSON or has the wrong types.").WithInternal(err)
	}
	return nil
}

// BindQuery does the same for query-string parameters.
func BindQuery(c *gin.Context, dst any) error {
	if err := c.ShouldBindQuery(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			return ErrValidation.WithFields(fieldMessages(ve))
		}
		return ErrBadRequest.WithMessage("Invalid query parameters.").WithInternal(err)
	}
	return nil
}

// fieldMessages renders validator tags as sentences a customer can act on.
func fieldMessages(ve validator.ValidationErrors) map[string]string {
	out := make(map[string]string, len(ve))
	for _, fe := range ve {
		name := jsonName(fe.Field())
		switch fe.Tag() {
		case "required":
			out[name] = "This field is required."
		case "email":
			out[name] = "Enter a valid email address."
		case "min":
			out[name] = "Must be at least " + fe.Param() + " characters."
		case "max":
			out[name] = "Must be at most " + fe.Param() + " characters."
		case "gte":
			out[name] = "Must be " + fe.Param() + " or more."
		case "lte":
			out[name] = "Must be " + fe.Param() + " or less."
		case "len":
			out[name] = "Must be exactly " + fe.Param() + " characters."
		case "oneof":
			out[name] = "Choose one of: " + fe.Param() + "."
		case "e164", "mobile_in":
			out[name] = "Enter a valid 10-digit Indian mobile number."
		case "pincode":
			out[name] = "Enter a valid 6-digit PIN code."
		case "uuid", "uuid4":
			out[name] = "Invalid identifier."
		default:
			out[name] = "This value is not valid."
		}
	}
	return out
}

// jsonName lowercases the leading character so error keys match the JSON field
// names the frontend uses (Gin reports the Go struct field name).
func jsonName(f string) string {
	if f == "" {
		return f
	}
	b := []byte(f)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// QueryInt reads an integer query parameter, clamped into [min,max].
func QueryInt(c *gin.Context, key string, def, min, max int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
