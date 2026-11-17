// Package logger provides structured logging with redaction of sensitive data.
//
// Built on log/slog from the standard library — no third-party logging
// dependency is warranted. Development uses a readable text handler; production
// emits JSON so hosted log viewers can index fields.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey struct{}

// redactedKeys never appear in logs with their value. Prescriptions, tokens
// and payment secrets must not land in a log aggregator; neither should
// personal data we have no operational reason to keep in logs.
var redactedKeys = map[string]bool{
	"password": true, "password_hash": true, "token": true, "session_token": true, "access_token": true, "api_key": true, "anthropic_api_key": true,
	"authorization": true, "cookie": true, "set-cookie": true,
	"secret": true, "key_secret": true, "webhook_secret": true, "signature": true,
	"otp": true, "otp_code": true, "card": true, "cvv": true, "upi_pin": true,
	"prescription_url": true, "prescription_contents": true, "file_bytes": true,
	"mobile": true, "email": true, "address_line1": true, "address_line2": true,
}

// New builds the root logger. level is one of debug|info|warn|error.
func New(level string, production bool) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lv, ReplaceAttr: redact}
	var h slog.Handler
	if production {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// redact replaces the values of sensitive keys. Applied by the handler itself
// so there is no way to bypass it by logging a struct field directly.
func redact(_ []string, a slog.Attr) slog.Attr {
	if redactedKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, "[redacted]")
	}
	return a
}

// WithContext stores lg on ctx so handlers deep in a call stack can log with
// the request's correlation fields already attached.
func WithContext(ctx context.Context, lg *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, lg)
}

// FromContext returns the request logger, or the default logger if none is set.
func FromContext(ctx context.Context) *slog.Logger {
	if lg, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && lg != nil {
		return lg
	}
	return slog.Default()
}

// MaskMobile renders a mobile number as "9xxxxx4321" for the rare log line
// where some identifying fragment genuinely aids support.
func MaskMobile(m string) string {
	if len(m) < 6 {
		return "[redacted]"
	}
	return m[:1] + strings.Repeat("x", len(m)-5) + m[len(m)-4:]
}
