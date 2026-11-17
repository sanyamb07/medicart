package auth

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct {
	svc          *Service
	cookieDomain string
	cookieSecure bool
}

func NewHandlers(svc *Service, cookieDomain string, cookieSecure bool) *Handlers {
	return &Handlers{svc: svc, cookieDomain: cookieDomain, cookieSecure: cookieSecure}
}

// Register mounts the credential-checking routes — login and OTP issuance —
// which must sit behind a strict per-IP rate limit to resist brute force.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/staff/login", h.staffLogin)
	r.POST("/staff/logout", h.staffLogout)
	r.POST("/auth/otp/start", h.otpStart)
	r.POST("/auth/otp/verify", h.otpVerify)
	r.POST("/auth/logout", h.customerLogout)
}

// RegisterSessionReads mounts the "am I signed in" checks. These are cheap,
// idempotent reads fired on every page load (the header's useAuth hook calls
// /auth/me on every mount) — sharing a brute-force-grade rate limit with
// login would let ordinary browsing exhaust the budget before a real login
// attempt ever happens, so these are deliberately unthrottled here.
func (h *Handlers) RegisterSessionReads(r gin.IRouter) {
	r.GET("/staff/me", h.staffMe)
	r.GET("/auth/me", h.customerMe)
}

// ---------------------------------------------------------------- staff

type staffLoginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handlers) staffLogin(c *gin.Context) {
	var req staffLoginReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	user, err := h.svc.LoginStaff(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sess, err := h.svc.CreateSession(c.Request.Context(), user.ID, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	SetSessionCookie(c, StaffCookieName, sess.Token, maxAge, h.cookieDomain, h.cookieSecure)
	httpx.OK(c, user)
}

func (h *Handlers) staffLogout(c *gin.Context) {
	if token, err := c.Cookie(StaffCookieName); err == nil && token != "" {
		if p, _ := h.svc.Authenticate(c.Request.Context(), token); p != nil {
			_ = h.svc.RevokeSession(c.Request.Context(), p.SessionID)
		}
	}
	ClearSessionCookie(c, StaffCookieName, h.cookieDomain, h.cookieSecure)
	httpx.NoContent(c)
}

func (h *Handlers) staffMe(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	user, err := h.svc.GetUser(c.Request.Context(), p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}

// ---------------------------------------------------------------- customer

type otpStartReq struct {
	Mobile string `json:"mobile" binding:"required"`
}

func (h *Handlers) otpStart(c *gin.Context) {
	var req otpStartReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.svc.StartOTP(c.Request.Context(), req.Mobile)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, res)
}

type otpVerifyReq struct {
	Mobile string `json:"mobile" binding:"required"`
	Code   string `json:"code" binding:"required"`
}

func (h *Handlers) otpVerify(c *gin.Context) {
	var req otpVerifyReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	user, err := h.svc.VerifyOTP(c.Request.Context(), req.Mobile, req.Code)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sess, err := h.svc.CreateSession(c.Request.Context(), user.ID, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	SetSessionCookie(c, CookieName, sess.Token, maxAge, h.cookieDomain, h.cookieSecure)
	httpx.OK(c, user)
}

func (h *Handlers) customerLogout(c *gin.Context) {
	if token, err := c.Cookie(CookieName); err == nil && token != "" {
		if p, _ := h.svc.Authenticate(c.Request.Context(), token); p != nil {
			_ = h.svc.RevokeSession(c.Request.Context(), p.SessionID)
		}
	}
	ClearSessionCookie(c, CookieName, h.cookieDomain, h.cookieSecure)
	httpx.NoContent(c)
}

func (h *Handlers) customerMe(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	user, err := h.svc.GetUser(c.Request.Context(), p.UserID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, user)
}
