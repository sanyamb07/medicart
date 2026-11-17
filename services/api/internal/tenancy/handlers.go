package tenancy

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ repo *Repo }

func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Register mounts the public storefront directory — "choose your pharmacy".
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/businesses", h.listActive)
}

func (h *Handlers) listActive(c *gin.Context) {
	businesses, err := h.repo.ListActive(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, businesses)
}

// RegisterBusinessAdmin mounts the "which business am I managing" read for
// a signed-in staff member, under a group that already ran
// ResolveStaffBusiness.
func (h *Handlers) RegisterBusinessAdmin(r gin.IRouter) {
	r.GET("/business", h.getOwnBusiness)
}

func (h *Handlers) getOwnBusiness(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	b, err := h.repo.GetByID(c.Request.Context(), businessID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, b)
}

// RegisterPlatformAdmin mounts business management for the platform admin —
// the caller must already be behind RequirePlatformAdmin.
func (h *Handlers) RegisterPlatformAdmin(r gin.IRouter) {
	r.GET("/businesses", h.listAll)
	r.POST("/businesses", h.create)
	r.POST("/businesses/:id/status", h.setStatus)
}

func (h *Handlers) listAll(c *gin.Context) {
	businesses, err := h.repo.ListAll(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, businesses)
}

type createReq struct {
	Slug            string `json:"slug"`
	Name            string `json:"name" binding:"required"`
	Tagline         string `json:"tagline"`
	SupportWhatsApp string `json:"supportWhatsapp"`
}

func (h *Handlers) create(c *gin.Context) {
	var req createReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	b, err := h.repo.Create(c.Request.Context(), CreateInput{
		Slug: req.Slug, Name: req.Name, Tagline: req.Tagline, SupportWhatsApp: req.SupportWhatsApp,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, b)
}

type statusReq struct {
	Status string `json:"status" binding:"required"`
}

func (h *Handlers) setStatus(c *gin.Context) {
	var req statusReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	b, err := h.repo.SetStatus(c.Request.Context(), c.Param("id"), req.Status)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, b)
}
