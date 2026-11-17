package httpx

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/sanyam/medicart/services/api/internal/platform/logger"
)

const RequestIDHeader = "X-Request-Id"

// RequestID assigns every request a correlation id and echoes it back, so a
// customer-reported error can be traced to one log line.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		// Never trust a client-supplied id for anything but correlation, and
		// cap its length so it cannot be used to bloat logs.
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// Logging records one structured line per request. Query strings are omitted:
// they can carry search terms and PIN codes we have no reason to retain.
func Logging(root *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		reqID, _ := c.Get("request_id")

		lg := root.With("request_id", reqID, "method", c.Request.Method, "path", c.FullPath())
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), lg))

		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", c.Writer.Size(),
		}
		switch {
		case status >= 500:
			lg.Error("request", attrs...)
		case status >= 400:
			lg.Warn("request", attrs...)
		default:
			lg.Info("request", attrs...)
		}
	}
}

// Recovery converts a panic into a 500 without leaking the stack to the client.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.FromContext(c.Request.Context()).Error("panic recovered",
					"panic", r, "path", c.FullPath())
				if !c.Writer.Written() {
					Fail(c, ErrInternal)
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

// CORS allows exactly the configured origins. A wildcard is never emitted
// because the API authenticates with cookies, and `*` is incompatible with
// credentialed requests anyway.
func CORS(allowed []string) gin.HandlerFunc {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[strings.TrimRight(strings.TrimSpace(o), "/")] = true
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" && set[origin] {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			h.Set("Access-Control-Allow-Headers",
				"Content-Type,Authorization,"+RequestIDHeader+",X-CSRF-Token,Idempotency-Key,X-Business-Slug")
			h.Set("Access-Control-Expose-Headers", RequestIDHeader)
			h.Set("Access-Control-Max-Age", "600")
			h.Add("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			// Answer the preflight regardless; a disallowed origin simply gets
			// no CORS headers and the browser blocks it.
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// SecurityHeaders sets defensive response headers. The API serves JSON and
// uploaded files, so the CSP is deliberately restrictive — it stops an
// uploaded SVG or HTML file from executing in the API's origin.
func SecurityHeaders(production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'none'; frame-ancestors 'none'; base-uri 'none'; sandbox")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if production {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// BodyLimit rejects oversized request bodies before they are read into memory.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			Fail(c, ErrPayloadLarge)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// ---- rate limiting ----

// RateLimiter is a fixed-capacity token bucket per key, held in process memory.
//
// Deliberate scope: this is correct for the single-instance deployment the
// platform targets. Running two or more API instances would give each its own
// counters, multiplying the effective limit — at that point move the counters
// to Postgres or a shared cache. Documented in docs/SECURITY.md.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity int
	window   time.Duration
	// lastSweep bounds memory: stale buckets are dropped periodically rather
	// than with a background goroutine we would also have to shut down.
	lastSweep time.Time
}

type bucket struct {
	tokens   int
	resetsAt time.Time
}

// NewRateLimiter allows `capacity` requests per `window` per key.
func NewRateLimiter(capacity int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		capacity:  capacity,
		window:    window,
		lastSweep: time.Now(),
	}
}

// Allow consumes a token for key, reporting whether the request may proceed
// and how long to wait if not.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastSweep) > rl.window {
		for k, b := range rl.buckets {
			if now.After(b.resetsAt) {
				delete(rl.buckets, k)
			}
		}
		rl.lastSweep = now
	}

	b, ok := rl.buckets[key]
	if !ok || now.After(b.resetsAt) {
		rl.buckets[key] = &bucket{tokens: rl.capacity - 1, resetsAt: now.Add(rl.window)}
		return true, 0
	}
	if b.tokens <= 0 {
		return false, time.Until(b.resetsAt)
	}
	b.tokens--
	return true, 0
}

// Reset clears all counters. Used by tests.
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.buckets = make(map[string]*bucket)
}

// KeyFunc derives the bucket key for a request.
type KeyFunc func(*gin.Context) string

// ByIP buckets by client IP. Gin's ClientIP honours proxy headers only when
// trusted proxies are configured, which the router does explicitly.
func ByIP(c *gin.Context) string { return "ip:" + c.ClientIP() }

// ByUserOrIP prefers the authenticated user so one customer behind a shared
// NAT cannot exhaust everyone else's quota.
func ByUserOrIP(c *gin.Context) string {
	if uid, ok := c.Get(CtxUserID); ok {
		if s, ok := uid.(string); ok && s != "" {
			return "user:" + s
		}
	}
	return ByIP(c)
}

// RateLimit applies a limiter, returning 429 with Retry-After when exhausted.
func RateLimit(rl *RateLimiter, key KeyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, retry := rl.Allow(key(c))
		if !ok {
			secs := int(retry.Seconds()) + 1
			c.Header("Retry-After", strings.TrimSpace(itoa(secs)))
			Fail(c, ErrRateLimited)
			return
		}
		c.Next()
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "1"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
