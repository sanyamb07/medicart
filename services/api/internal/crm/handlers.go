package crm

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ repo *Repo }

func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Register mounts the business admin's "Customers" (CRM) screen — the
// caller must already require an ADMIN/ORDER_MANAGER/CUSTOMER_SUPPORT role
// at the resolved business.
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/customers", h.list)
	r.PUT("/customers/:customerId", h.update)
}

func (h *Handlers) list(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	rows, err := h.repo.List(c.Request.Context(), businessID, c.Query("leadStatus"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, rows)
}

type updateReq struct {
	LeadStatus string  `json:"leadStatus" binding:"required"`
	Notes      *string `json:"notes"`
}

func (h *Handlers) update(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req updateReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	row, err := h.repo.Update(c.Request.Context(), businessID, c.Param("customerId"), UpdateInput{
		LeadStatus: req.LeadStatus, Notes: req.Notes,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, row)
}
