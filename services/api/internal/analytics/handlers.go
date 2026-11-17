package analytics

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ svc *Service }

func NewHandlers(svc *Service) *Handlers { return &Handlers{svc: svc} }

func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/analytics/events", h.record)
}

type recordReq struct {
	AnonID string  `json:"anonId" binding:"required"`
	Events []Event `json:"events" binding:"required,min=1,max=20,dive"`
}

func (h *Handlers) record(c *gin.Context) {
	var req recordReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	accepted, err := h.svc.Record(c.Request.Context(), req.AnonID, req.Events)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"accepted": accepted})
}
