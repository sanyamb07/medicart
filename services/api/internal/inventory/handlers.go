package inventory

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// AdminHandlers wires batch management to HTTP. Unlike most other packages,
// inventory has no repo type of its own — its functions take *db.Pool
// directly (see admin.go) — so this wraps the pool rather than a repo.
type AdminHandlers struct{ pool *db.Pool }

func NewAdminHandlers(pool *db.Pool) *AdminHandlers { return &AdminHandlers{pool: pool} }

// Register mounts the batch-management routes. The caller must already
// require the ADMIN or INVENTORY_MANAGER role.
func (h *AdminHandlers) Register(r gin.IRouter) {
	r.GET("/products/:id/batches", h.listBatches)
	r.POST("/products/:id/batches", h.receiveBatch)
	r.POST("/batches/:batchId/block", h.blockBatch)
	r.POST("/batches/:batchId/unblock", h.unblockBatch)
	r.GET("/inventory/expiring", h.expiringSoon)
}

func (h *AdminHandlers) listBatches(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	batches, err := ListBatchesForProduct(c.Request.Context(), h.pool, businessID, c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, batches)
}

type receiveReq struct {
	BatchNumber string  `json:"batchNumber" binding:"required"`
	ExpiryDate  string  `json:"expiryDate" binding:"required"` // "2027-03-31"
	Qty         int     `json:"qty" binding:"required"`
	CostPaise   *int64  `json:"costPaise"`
	Supplier    *string `json:"supplier"`
	InvoiceRef  *string `json:"invoiceRef"`
}

func (h *AdminHandlers) receiveBatch(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req receiveReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	expiry, err := time.Parse("2006-01-02", req.ExpiryDate)
	if err != nil {
		httpx.Fail(c, httpx.ErrValidation.WithFields(map[string]string{"expiryDate": "Use YYYY-MM-DD."}))
		return
	}
	batch, err := Receive(c.Request.Context(), h.pool, ReceiveInput{
		BusinessID: businessID, ProductID: c.Param("id"), BatchNumber: req.BatchNumber, ExpiryDate: expiry, Qty: req.Qty,
		CostPaise: req.CostPaise, Supplier: req.Supplier, InvoiceRef: req.InvoiceRef,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, batch)
}

type blockReq struct {
	Reason *string `json:"reason"`
}

func (h *AdminHandlers) blockBatch(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req blockReq
	_ = httpx.Bind(c, &req) // reason is optional; an empty body is fine
	if err := SetBlocked(c.Request.Context(), h.pool, businessID, c.Param("batchId"), true, req.Reason); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"isBlocked": true})
}

func (h *AdminHandlers) unblockBatch(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	if err := SetBlocked(c.Request.Context(), h.pool, businessID, c.Param("batchId"), false, nil); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"isBlocked": false})
}

func (h *AdminHandlers) expiringSoon(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	days := httpx.QueryInt(c, "days", 60, 1, 365)
	batches, err := ListExpiringSoon(c.Request.Context(), h.pool, businessID, days)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, batches)
}
