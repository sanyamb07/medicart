package assistant

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ svc *Service }

func NewHandlers(svc *Service) *Handlers { return &Handlers{svc: svc} }

func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/assistant/messages", h.respond)
}

type messageReq struct {
	Message string `json:"message" binding:"required,max=500"`
}

func (h *Handlers) respond(c *gin.Context) {
	var req messageReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	p := httpx.CurrentPrincipal(c)
	customerID := ""
	if p != nil {
		customerID = p.CustomerID
	}
	reply, err := h.svc.Respond(c.Request.Context(), businessID, customerID, req.Message)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, reply)
}
