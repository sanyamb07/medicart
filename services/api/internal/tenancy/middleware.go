package tenancy

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// StorefrontBusinessHeader is how the frontend tells the API which business's
// storefront it is browsing. Production would resolve this from the Host
// header (business.medicart.com); locally there is one host, so the
// frontend's "current storefront" context sends this header instead on every
// storefront call — see docs/MULTI_TENANT_PLAN.md §1.
const StorefrontBusinessHeader = "X-Business-Slug"

// ResolveStorefrontBusiness resolves the tenant for a public storefront
// request from StorefrontBusinessHeader, falling back to DefaultBusinessID
// when the header is absent — so any caller that predates multi-tenancy
// (or a test that doesn't set it) keeps working against the original
// storefront. An inactive/unknown slug fails closed with 404 rather than
// silently falling back, so a suspended business's storefront actually stops
// serving.
func ResolveStorefrontBusiness(repo *Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.GetHeader(StorefrontBusinessHeader)
		if slug == "" {
			httpx.SetBusinessID(c, DefaultBusinessID)
			c.Next()
			return
		}
		b, err := repo.ResolveBySlug(c.Request.Context(), slug)
		if err != nil {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("Unknown or unavailable storefront."))
			return
		}
		httpx.SetBusinessID(c, b.ID)
		c.Next()
	}
}

// ResolveStaffBusiness resolves the tenant for an authenticated staff member
// from their business_staff membership, not from anything the client sends —
// a staff session always acts for exactly the business it belongs to.
func ResolveStaffBusiness(repo *Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		businessID, err := repo.StaffBusinessID(c.Request.Context(), p.UserID)
		if err != nil {
			httpx.Fail(c, httpx.ErrForbidden.WithMessage("This account is not staff of any business."))
			return
		}
		httpx.SetBusinessID(c, businessID)
		c.Next()
	}
}

// RequireBusinessRole rejects a staff caller who lacks every listed role at
// the business already resolved onto the request context (by
// ResolveStaffBusiness, mounted first). This re-queries business_staff fresh
// on every request rather than trusting a role baked into the session, so a
// role change or an employee's offboarding takes effect immediately.
func RequireBusinessRole(repo *Repo, roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		businessID, ok := httpx.MustBusinessID(c)
		if !ok {
			return
		}
		have, err := repo.StaffRoles(c.Request.Context(), businessID, p.UserID)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if !hasAny(have, roles) {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		c.Next()
	}
}

// RequirePlatformAdmin rejects any caller who is not a platform super admin.
// Deliberately independent of business resolution: a platform admin manages
// businesses, not any one business's data, so no CtxBusinessID is set here.
func RequirePlatformAdmin(repo *Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		isAdmin, err := repo.IsPlatformAdmin(c.Request.Context(), p.UserID)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if !isAdmin {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		c.Next()
	}
}

func hasAny(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}
