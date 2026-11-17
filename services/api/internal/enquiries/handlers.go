package enquiries

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ repo *Repo }

func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Register mounts the business admin's "Enquiries" (AI leads) screen — the
// caller must already require an ADMIN/ORDER_MANAGER/CUSTOMER_SUPPORT role
// at the resolved business.
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/enquiries", h.list)
	r.PUT("/enquiries/:id", h.update)
}

func (h *Handlers) list(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	rows, err := h.repo.List(c.Request.Context(), businessID, c.Query("status"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, rows)
}

type updateReq struct {
	Status     string  `json:"status" binding:"required"`
	Notes      *string `json:"notes"`
	FollowUpAt *string `json:"followUpAt"` // RFC3339, optional
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
	var followUp *time.Time
	if req.FollowUpAt != nil && *req.FollowUpAt != "" {
		t, err := time.Parse(time.RFC3339, *req.FollowUpAt)
		if err != nil {
			httpx.Fail(c, httpx.ErrValidation.WithFields(map[string]string{"followUpAt": "Use an RFC3339 timestamp."}))
			return
		}
		followUp = &t
	}
	row, err := h.repo.Update(c.Request.Context(), businessID, c.Param("id"), UpdateInput{
		Status: req.Status, Notes: req.Notes, FollowUpAt: followUp,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, row)
}
