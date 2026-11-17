package markets

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ repo *Repo }

func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Register mounts the public market list (country selector data).
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/markets", h.listLive)
}

// RegisterPlatformAdmin mounts market and eligibility management; the group
// must already require a platform admin.
func (h *Handlers) RegisterPlatformAdmin(r gin.IRouter) {
	r.GET("/markets", h.listAll)
	r.GET("/products/:id/markets", h.productListings)
	r.PUT("/products/:id/markets/:code", h.setListing)
}

func (h *Handlers) listLive(c *gin.Context) {
	ms, err := h.repo.ListLive(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, ms)
}

func (h *Handlers) listAll(c *gin.Context) {
	ms, err := h.repo.ListAll(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, ms)
}

func (h *Handlers) productListings(c *gin.Context) {
	ls, err := h.repo.ProductListings(c.Request.Context(), c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, ls)
}

func (h *Handlers) setListing(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}
	var in ListingInput
	if err := httpx.Bind(c, &in); err != nil {
		httpx.Fail(c, err)
		return
	}
	l, err := h.repo.SetListing(c.Request.Context(), p.UserID, c.Param("id"), c.Param("code"), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, l)
}
