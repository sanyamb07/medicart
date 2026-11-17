package httpx

import "github.com/gin-gonic/gin"

// Gin context keys. Declared here, next to the middleware that reads them, so
// there is one list rather than string literals scattered across handlers.
const (
	CtxUserID     = "auth.user_id"
	CtxUserRoles  = "auth.roles"
	CtxCustomerID = "auth.customer_id"
	CtxSessionID  = "auth.session_id"
	CtxCartID     = "cart.id"
	// CtxBusinessID is the resolved tenant for this request: the storefront
	// business a shopper is browsing, or the business a staff member works
	// for. Set by tenancy.ResolveStorefrontBusiness / ResolveStaffBusiness.
	CtxBusinessID = "tenant.business_id"
)

// Roles recognised by the platform. A user may hold several. Staff roles
// (everything except CUSTOMER and PLATFORM_SUPER_ADMIN) are granted per
// business via business_staff, not globally — see internal/tenancy.
const (
	RoleCustomer           = "CUSTOMER"
	RoleAdmin              = "ADMIN"
	RoleInventoryManager   = "INVENTORY_MANAGER"
	RoleOrderManager       = "ORDER_MANAGER"
	RolePharmacist         = "PHARMACIST"
	RoleCustomerSupport    = "CUSTOMER_SUPPORT"
	RolePlatformSuperAdmin = "PLATFORM_SUPER_ADMIN"
	// RoleAffiliate, like CUSTOMER and PLATFORM_SUPER_ADMIN, is granted
	// globally in user_roles rather than per-business in business_staff — an
	// affiliate promotes the platform as a whole, not one pharmacy on it.
	RoleAffiliate = "AFFILIATE"
)

// StaffRoles are the roles that may reach /api/v1/admin at all.
var StaffRoles = []string{
	RoleAdmin, RoleInventoryManager, RoleOrderManager, RolePharmacist, RoleCustomerSupport,
}

// Principal is the authenticated caller, as far as HTTP cares.
type Principal struct {
	UserID     string
	CustomerID string // empty for staff-only accounts
	SessionID  string
	Roles      []string
}

// HasRole reports whether the principal holds role. ADMIN is deliberately NOT
// treated as implying every other role: permissions are checked explicitly so
// that reading the route tells you exactly who can call it. ADMIN is granted
// the other roles at the data level instead (see the role seed).
func (p *Principal) HasRole(role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole reports whether the principal holds at least one of roles.
func (p *Principal) HasAnyRole(roles ...string) bool {
	for _, r := range roles {
		if p.HasRole(r) {
			return true
		}
	}
	return false
}

// IsStaff reports whether the principal holds any back-office role.
func (p *Principal) IsStaff() bool { return p.HasAnyRole(StaffRoles...) }

// SetPrincipal records the authenticated caller on the request context.
func SetPrincipal(c *gin.Context, p *Principal) {
	c.Set(CtxUserID, p.UserID)
	c.Set(CtxUserRoles, p.Roles)
	c.Set(CtxCustomerID, p.CustomerID)
	c.Set(CtxSessionID, p.SessionID)
}

// CurrentPrincipal returns the authenticated caller, or nil when anonymous.
func CurrentPrincipal(c *gin.Context) *Principal {
	uid, ok := c.Get(CtxUserID)
	if !ok {
		return nil
	}
	s, _ := uid.(string)
	if s == "" {
		return nil
	}
	roles, _ := c.Get(CtxUserRoles)
	rs, _ := roles.([]string)
	cid, _ := c.Get(CtxCustomerID)
	cs, _ := cid.(string)
	sid, _ := c.Get(CtxSessionID)
	ss, _ := sid.(string)
	return &Principal{UserID: s, CustomerID: cs, SessionID: ss, Roles: rs}
}

// MustPrincipal returns the caller or writes 401. The boolean reports whether
// the handler should continue.
func MustPrincipal(c *gin.Context) (*Principal, bool) {
	p := CurrentPrincipal(c)
	if p == nil {
		Fail(c, ErrUnauthorized)
		return nil, false
	}
	return p, true
}

// SetBusinessID records the resolved tenant for this request.
func SetBusinessID(c *gin.Context, businessID string) { c.Set(CtxBusinessID, businessID) }

// CurrentBusinessID returns the tenant resolved for this request, or ""
// when none was resolved (e.g. a platform-admin route, which has no single
// business in scope).
func CurrentBusinessID(c *gin.Context) string {
	v, ok := c.Get(CtxBusinessID)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// MustBusinessID returns the resolved tenant or writes 500 — a programming
// error if hit, since every route that needs one mounts a resolver middleware
// first. Handlers call this rather than re-deriving the business id so a
// missing resolver fails loudly instead of silently querying unscoped.
func MustBusinessID(c *gin.Context) (string, bool) {
	id := CurrentBusinessID(c)
	if id == "" {
		Fail(c, ErrInternal.WithMessage("No business resolved for this request."))
		return "", false
	}
	return id, true
}

// RequireAuth rejects anonymous callers.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CurrentPrincipal(c) == nil {
			Fail(c, ErrUnauthorized)
			return
		}
		c.Next()
	}
}

// RequireRole rejects callers lacking every listed role. Authorisation always
// happens here on the server; the frontend's menu visibility is cosmetic.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := CurrentPrincipal(c)
		if p == nil {
			Fail(c, ErrUnauthorized)
			return
		}
		if !p.HasAnyRole(roles...) {
			Fail(c, ErrForbidden)
			return
		}
		c.Next()
	}
}
