package prescriptions

import (
	"errors"
	"io"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct {
	svc *Service
}

func NewHandlers(svc *Service) *Handlers { return &Handlers{svc: svc} }

// Register mounts the customer-facing routes: upload, list own, and a
// signed view URL. The caller must already require a customer session and
// have resolved the storefront business (ResolveStorefrontBusiness).
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/me/prescriptions", httpx.RequireAuth(), h.upload)
	r.GET("/me/prescriptions", httpx.RequireAuth(), h.listMine)
	r.GET("/me/prescriptions/:id/url", httpx.RequireAuth(), h.signedURLMine)
}

// RegisterAdmin mounts the pharmacist/admin review routes. The caller must
// already require the PHARMACIST or ADMIN role at the resolved business
// (tenancy.RequireBusinessRole).
func (h *Handlers) RegisterAdmin(r gin.IRouter) {
	r.GET("/prescriptions/pending", h.listPending)
	r.GET("/prescriptions/:id/url", h.signedURLAdmin)
	r.POST("/prescriptions/:id/review", h.review)
}

const maxMultipartMemory = 2 << 20 // 2 MiB held in memory; the rest spills to a temp file Gin cleans up

func (h *Handlers) upload(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil || p.CustomerID == "" {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}

	if err := c.Request.ParseMultipartForm(maxMultipartMemory); err != nil {
		httpx.Fail(c, httpx.ErrValidation.WithMessage("Send the file as multipart/form-data, field \"file\"."))
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		httpx.Fail(c, httpx.ErrValidation.WithMessage("No file was attached."))
		return
	}
	defer file.Close()

	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	contentType := header.Header.Get("Content-Type")
	rec, err := h.svc.Upload(c.Request.Context(), businessID, p.CustomerID, header.Filename, contentType, header.Size, io.Reader(file))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, rec)
}

func (h *Handlers) listMine(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil || p.CustomerID == "" {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}
	list, err := h.svc.ListForCustomer(c.Request.Context(), p.CustomerID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handlers) listPending(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	list, err := h.svc.ListPendingReview(c.Request.Context(), businessID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, list)
}

// signedURLMine is the customer-facing path: ownership is the only check, so
// requesterBusinessID is always empty here regardless of what storefront the
// customer happens to be browsing.
func (h *Handlers) signedURLMine(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}
	url, err := h.svc.SignedURL(c.Request.Context(), c.Param("id"), *p, "")
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"url": url})
}

// signedURLAdmin is the staff-facing path: the caller's own resolved
// business is the one that may match the prescription's business.
func (h *Handlers) signedURLAdmin(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	url, err := h.svc.SignedURL(c.Request.Context(), c.Param("id"), *p, businessID)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"url": url})
}

type reviewReq struct {
	Decision string  `json:"decision" binding:"required"`
	Notes    *string `json:"notes"`
}

func (h *Handlers) review(c *gin.Context) {
	p := httpx.CurrentPrincipal(c)
	if p == nil {
		httpx.Fail(c, httpx.ErrUnauthorized)
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req reviewReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	rec, err := h.svc.Review(c.Request.Context(), businessID, c.Param("id"), p.UserID, req.Decision, req.Notes)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, rec)
}
