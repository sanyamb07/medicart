package affiliate

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/audit"
	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct {
	svc          *Service
	rec          *audit.Recorder
	cookieDomain string
	cookieSecure bool
}

func NewHandlers(svc *Service, rec *audit.Recorder, cookieDomain string, cookieSecure bool) *Handlers {
	return &Handlers{svc: svc, rec: rec, cookieDomain: cookieDomain, cookieSecure: cookieSecure}
}

// Register mounts the credential-checking routes (application + login),
// which belong behind a per-IP rate limit like every other login path.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/affiliate/apply", h.apply)
	r.POST("/affiliate/login", h.login)
	r.POST("/affiliate/logout", h.logout)
}

// RegisterSessionReads mounts the cheap "am I signed in" check, unthrottled
// for the same reason auth.Handlers.RegisterSessionReads is — fired on every
// affiliate portal page load.
func (h *Handlers) RegisterSessionReads(r gin.IRouter) {
	r.GET("/affiliate/me", h.me)
}

// RegisterDashboard mounts the affiliate's own data views. Mount under a
// group that already applies SessionMiddleware(authSvc, AffiliateCookieName)
// and httpx.RequireRole(httpx.RoleAffiliate).
func (h *Handlers) RegisterDashboard(r gin.IRouter) {
	r.GET("/affiliate/summary", h.summary)
	r.GET("/affiliate/referrals", h.myReferrals)
}

// RegisterPlatformAdmin mounts the program's back-office routes. Mount under
// the platform-admin group (already role-gated to PLATFORM_SUPER_ADMIN).
func (h *Handlers) RegisterPlatformAdmin(r gin.IRouter) {
	r.GET("/affiliates", h.adminList)
	r.POST("/affiliates/:id/approve", h.adminApprove)
	r.POST("/affiliates/:id/reject", h.adminReject)
	r.POST("/affiliates/:id/suspend", h.adminSuspend)
	r.GET("/affiliates/referrals", h.adminListReferrals)
	r.POST("/affiliates/referrals/mark-paid", h.adminMarkPaid)
	r.GET("/affiliates/settings", h.adminGetSettings)
	r.PUT("/affiliates/settings", h.adminSetSettings)
}

// ---------------------------------------------------------------- public

type applyReq struct {
	Email         string `json:"email" binding:"required,email"`
	Password      string `json:"password" binding:"required"`
	FullName      string `json:"fullName" binding:"required,min=1,max=120"`
	Phone         string `json:"phone" binding:"max=15"`
	PayoutDetails string `json:"payoutDetails" binding:"max=500"`
}

func (h *Handlers) apply(c *gin.Context) {
	var req applyReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	a, err := h.svc.Apply(c.Request.Context(), req.Email, req.Password, req.FullName, req.Phone, req.PayoutDetails)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		Action: audit.ActionAffiliateApplied, EntityType: "affiliate", EntityID: a.ID,
		Metadata: map[string]any{"email": a.Email},
	})
	httpx.Created(c, a)
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handlers) login(c *gin.Context) {
	var req loginReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	sess, a, err := h.svc.Login(c.Request.Context(), req.Email, req.Password, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	auth.SetSessionCookie(c, auth.AffiliateCookieName, sess.Token, maxAge, h.cookieDomain, h.cookieSecure)
	httpx.OK(c, a)
}

func (h *Handlers) logout(c *gin.Context) {
	if token, err := c.Cookie(auth.AffiliateCookieName); err == nil && token != "" {
		if p, _ := h.svc.authSvc.Authenticate(c.Request.Context(), token); p != nil {
			_ = h.svc.authSvc.RevokeSession(c.Request.Context(), p.SessionID)
		}
	}
	auth.ClearSessionCookie(c, auth.AffiliateCookieName, h.cookieDomain, h.cookieSecure)
	httpx.NoContent(c)
}

func (h *Handlers) me(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	a, err := h.svc.GetByPrincipal(c.Request.Context(), p.UserID)
	if err != nil {
		if err == ErrAffiliateNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, a)
}

// ---------------------------------------------------------------- dashboard

func (h *Handlers) summary(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	a, err := h.svc.GetByPrincipal(c.Request.Context(), p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sum, err := h.svc.MySummary(c.Request.Context(), a.ID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"affiliate": a, "summary": sum})
}

func (h *Handlers) myReferrals(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	a, err := h.svc.GetByPrincipal(c.Request.Context(), p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page := httpx.QueryInt(c, "page", 1, 1, 10000)
	pageSize := httpx.QueryInt(c, "pageSize", 25, 1, 100)
	items, total, err := h.svc.MyReferrals(c.Request.Context(), a.ID, page, pageSize)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, items, httpx.NewMeta(page, pageSize, total))
}

// ---------------------------------------------------------------- platform admin

func (h *Handlers) adminList(c *gin.Context) {
	f := ListFilter{
		Status:   c.Query("status"),
		Page:     httpx.QueryInt(c, "page", 1, 1, 10000),
		PageSize: httpx.QueryInt(c, "pageSize", 25, 1, 100),
	}
	items, total, err := h.svc.ListAffiliates(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, items, httpx.NewMeta(f.Page, f.PageSize, total))
}

func (h *Handlers) adminApprove(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	a, err := h.svc.Approve(c.Request.Context(), c.Param("id"), p.UserID)
	if err != nil {
		if err == ErrAffiliateNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		ActorID: &p.UserID, Action: audit.ActionAffiliateApproved, EntityType: "affiliate", EntityID: a.ID,
	})
	httpx.OK(c, a)
}

type rejectReq struct {
	Reason string `json:"reason" binding:"required,min=1,max=300"`
}

func (h *Handlers) adminReject(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	var req rejectReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	a, err := h.svc.Reject(c.Request.Context(), c.Param("id"), req.Reason, p.UserID)
	if err != nil {
		if err == ErrAffiliateNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		ActorID: &p.UserID, Action: audit.ActionAffiliateRejected, EntityType: "affiliate", EntityID: a.ID,
		Metadata: map[string]any{"reason": req.Reason},
	})
	httpx.OK(c, a)
}

func (h *Handlers) adminSuspend(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	a, err := h.svc.Suspend(c.Request.Context(), c.Param("id"), p.UserID)
	if err != nil {
		if err == ErrAffiliateNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		ActorID: &p.UserID, Action: audit.ActionAffiliateSuspended, EntityType: "affiliate", EntityID: a.ID,
	})
	httpx.OK(c, a)
}

func (h *Handlers) adminListReferrals(c *gin.Context) {
	f := ReferralFilter{
		Status:   c.Query("status"),
		Page:     httpx.QueryInt(c, "page", 1, 1, 10000),
		PageSize: httpx.QueryInt(c, "pageSize", 25, 1, 100),
	}
	items, total, err := h.svc.ListReferrals(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, items, httpx.NewMeta(f.Page, f.PageSize, total))
}

type markPaidReq struct {
	ReferralIDs []string `json:"referralIds" binding:"required,min=1"`
}

func (h *Handlers) adminMarkPaid(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	var req markPaidReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	n, err := h.svc.MarkPaid(c.Request.Context(), req.ReferralIDs, p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		ActorID: &p.UserID, Action: audit.ActionAffiliateCommissionPaid, EntityType: "affiliate_referral",
		EntityID: "batch", Metadata: map[string]any{"count": n, "referral_ids": req.ReferralIDs},
	})
	httpx.OK(c, gin.H{"markedPaid": n})
}

func (h *Handlers) adminGetSettings(c *gin.Context) {
	st, err := h.svc.GetSettings(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, st)
}

type settingsReq struct {
	// min=0 rather than "required": a 0% rate is a legitimate value, and gin's
	// "required" treats the int zero value as absent.
	CommissionRateBps int `json:"commissionRateBps" binding:"min=0,max=5000"`
}

func (h *Handlers) adminSetSettings(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	var req settingsReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	st, err := h.svc.SetCommissionRateBps(c.Request.Context(), req.CommissionRateBps, p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.rec.Record(c.Request.Context(), audit.Entry{
		ActorID: &p.UserID, Action: audit.ActionAffiliateSettingUpdated, EntityType: "affiliate_settings", EntityID: "1",
		Metadata: map[string]any{"commission_rate_bps": req.CommissionRateBps},
	})
	httpx.OK(c, st)
}
