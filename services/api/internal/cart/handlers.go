package cart

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// CookieName holds the guest cart token.
const CookieName = "mc_cart"

type Handlers struct {
	svc          *Service
	cookieDomain string
	cookieSecure bool
}

func NewHandlers(svc *Service, cookieDomain string, cookieSecure bool) *Handlers {
	return &Handlers{svc: svc, cookieDomain: cookieDomain, cookieSecure: cookieSecure}
}

func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/cart", h.get)
	r.POST("/cart/items", h.addItem)
	r.PATCH("/cart/items/:productId", h.setQty)
	r.DELETE("/cart/items/:productId", h.removeItem)
}

// cartID ensures a cart exists for this browser within the resolved
// business, setting the cookie when a new one is created, and returns its
// id. On failure it has already written the response — the caller should
// just return.
func (h *Handlers) cartID(c *gin.Context) (string, bool) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return "", false
	}
	token, _ := c.Cookie(CookieName)
	id, newToken, err := h.svc.GetOrCreateGuestCart(c.Request.Context(), businessID, token)
	if err != nil {
		httpx.Fail(c, err)
		return "", false
	}
	if newToken != token {
		// 180 days: a cart is a convenience, not a security boundary, so a
		// long-lived cookie is fine here (unlike the staff session cookie).
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(CookieName, newToken, 180*24*3600, "/", h.cookieDomain, h.cookieSecure, true)
	}
	return id, true
}

func (h *Handlers) get(c *gin.Context) {
	id, ok := h.cartID(c)
	if !ok {
		return
	}
	summary, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, summary)
}

type addItemReq struct {
	ProductID string `json:"productId" binding:"required,uuid"`
	Qty       int    `json:"qty" binding:"required,gte=1,lte=100"`
}

func (h *Handlers) addItem(c *gin.Context) {
	var req addItemReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	id, ok := h.cartID(c)
	if !ok {
		return
	}
	if err := h.svc.AddItem(c.Request.Context(), id, req.ProductID, req.Qty); err != nil {
		httpx.Fail(c, httpx.ErrBadRequest.WithInternal(err))
		return
	}
	summary, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, summary)
}

type setQtyReq struct {
	Qty int `json:"qty" binding:"gte=0,lte=100"`
}

func (h *Handlers) setQty(c *gin.Context) {
	var req setQtyReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	id, ok := h.cartID(c)
	if !ok {
		return
	}
	if err := h.svc.SetQty(c.Request.Context(), id, c.Param("productId"), req.Qty); err != nil {
		if err == ErrLineNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("That item is not in your cart."))
			return
		}
		httpx.Fail(c, httpx.ErrBadRequest.WithInternal(err))
		return
	}
	summary, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, summary)
}

func (h *Handlers) removeItem(c *gin.Context) {
	id, ok := h.cartID(c)
	if !ok {
		return
	}
	if err := h.svc.RemoveItem(c.Request.Context(), id, c.Param("productId")); err != nil {
		httpx.Fail(c, err)
		return
	}
	summary, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, summary)
}
