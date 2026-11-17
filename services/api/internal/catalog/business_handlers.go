package catalog

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// RegisterBusinessCatalogAdmin mounts the "pick what to sell" screen for a
// business admin — distinct from RegisterAdmin, which edits the global
// master catalogue and is platform-admin only.
func (h *Handlers) RegisterBusinessCatalogAdmin(r gin.IRouter) {
	r.GET("/catalog", h.listBusinessCatalog)
	r.PUT("/catalog/:productId", h.setBusinessListing)
}

func (h *Handlers) listBusinessCatalog(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	rows, err := h.repo.ListCatalogForBusiness(c.Request.Context(), businessID, c.Query("q"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, rows)
}

type setListingReq struct {
	IsListed    bool   `json:"isListed"`
	PricePaise  *int64 `json:"pricePaise"`
	IsVisible   bool   `json:"isVisible"`
	IsFeatured  bool   `json:"isFeatured"`
	MinOrderQty int    `json:"minOrderQty"`
}

func (h *Handlers) setBusinessListing(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req setListingReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.repo.SetListing(c.Request.Context(), businessID, c.Param("productId"), ListingInput{
		IsListed: req.IsListed, PricePaise: req.PricePaise, IsVisible: req.IsVisible,
		IsFeatured: req.IsFeatured, MinOrderQty: req.MinOrderQty,
	}); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"saved": true})
}
