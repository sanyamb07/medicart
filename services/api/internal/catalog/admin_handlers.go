package catalog

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// RegisterAdmin mounts the product-management routes. The caller must
// already require the ADMIN or INVENTORY_MANAGER role.
func (h *Handlers) RegisterAdmin(r gin.IRouter) {
	r.GET("/products", h.adminListProducts)
	r.GET("/products/:id", h.adminGetProduct)
	r.POST("/products", h.adminCreateProduct)
	r.PATCH("/products/:id", h.adminUpdateProduct)
	r.POST("/products/:id/activate", h.adminSetActive(true))
	r.POST("/products/:id/deactivate", h.adminSetActive(false))
}

func (h *Handlers) adminListProducts(c *gin.Context) {
	f := ListFilter{
		Query:           c.Query("q"),
		Sort:            "newest",
		Page:            httpx.QueryInt(c, "page", 1, 1, 10000),
		PageSize:        httpx.QueryInt(c, "pageSize", 25, 1, 100),
		IncludeInactive: true,
	}
	products, total, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, products, httpx.NewMeta(f.Page, f.PageSize, total))
}

func (h *Handlers) adminGetProduct(c *gin.Context) {
	p, err := h.repo.GetByIDAdmin(c.Request.Context(), c.Param("id"))
	if err != nil {
		if db.IsNoRows(err) {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, p)
}

type productReq struct {
	SKU                  string  `json:"sku" binding:"required"`
	Slug                 string  `json:"slug" binding:"required"`
	Name                 string  `json:"name" binding:"required"`
	BrandID              *string `json:"brandId"`
	GenericName          *string `json:"genericName"`
	Strength             *string `json:"strength"`
	DosageForm           *string `json:"dosageForm"`
	PackSize             *string `json:"packSize"`
	Description          *string `json:"description"`
	MRPPaise             int64   `json:"mrpPaise"`
	PricePaise           int64   `json:"pricePaise"`
	RequiresPrescription bool    `json:"requiresPrescription"`
	MaxQtyPerOrder       int     `json:"maxQtyPerOrder"`
	IsActive             bool    `json:"isActive"`
	IsFeatured           bool    `json:"isFeatured"`
}

func (req productReq) toInput() AdminProductInput {
	return AdminProductInput{
		SKU: req.SKU, Slug: req.Slug, Name: req.Name, BrandID: req.BrandID,
		GenericName: req.GenericName, Strength: req.Strength, DosageForm: req.DosageForm, PackSize: req.PackSize,
		Description: req.Description, MRPPaise: req.MRPPaise, PricePaise: req.PricePaise,
		RequiresPrescription: req.RequiresPrescription, MaxQtyPerOrder: req.MaxQtyPerOrder,
		IsActive: req.IsActive, IsFeatured: req.IsFeatured,
	}
}

func (h *Handlers) adminCreateProduct(c *gin.Context) {
	var req productReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, err := h.repo.AdminCreate(c.Request.Context(), req.toInput())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.Created(c, p)
}

func (h *Handlers) adminUpdateProduct(c *gin.Context) {
	var req productReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, err := h.repo.AdminUpdate(c.Request.Context(), c.Param("id"), req.toInput())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, p)
}

func (h *Handlers) adminSetActive(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h.repo.AdminSetActive(c.Request.Context(), c.Param("id"), active); err != nil {
			httpx.Fail(c, err)
			return
		}
		httpx.OK(c, gin.H{"isActive": active})
	}
}
