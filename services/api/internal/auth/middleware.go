package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// Two separate cookies, two separate namespaces: a customer and a staff
// member can be signed in on the same browser at once (e.g. while testing)
// without one session overwriting the other.
const (
	CookieName          = "mc_session"           // customer
	StaffCookieName     = "mc_staff_session"     // staff / admin
	AffiliateCookieName = "mc_affiliate_session" // affiliate portal
)

// SessionMiddleware reads the named session cookie (when present) and
// attaches the resolved principal to the request context. It never rejects
// an anonymous request itself — RequireAuth/RequireRole do that — so public
// endpoints can still render differently for a signed-in caller when useful.
// Mount it once globally with CookieName for customer context, and again on
// just the /admin group with StaffCookieName — the second overrides the
// first for that subtree.
func SessionMiddleware(svc *Service, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(cookieName)
		if err == nil && token != "" {
			p, aerr := svc.Authenticate(c.Request.Context(), token)
			if aerr == nil && p != nil {
				httpx.SetPrincipal(c, p)
			}
		}
		c.Next()
	}
}

// SetSessionCookie writes the named session token cookie with security flags
// appropriate to the environment.
func SetSessionCookie(c *gin.Context, cookieName, token string, maxAgeSeconds int, domain string, secure bool) {
	c.SetSameSite(sameSiteFor(secure))
	c.SetCookie(cookieName, token, maxAgeSeconds, "/", domain, secure, true)
}

// ClearSessionCookie removes the named session cookie (logout).
func ClearSessionCookie(c *gin.Context, cookieName, domain string, secure bool) {
	c.SetSameSite(sameSiteFor(secure))
	c.SetCookie(cookieName, "", -1, "/", domain, secure, true)
}

func sameSiteFor(secure bool) http.SameSite {
	if secure {
		// Cross-site capable deployments (separate frontend/API domains) need
		// SameSite=None, which browsers require to be paired with Secure.
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}
