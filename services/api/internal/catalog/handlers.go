package catalog

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// Handlers wires the catalogue repo to HTTP.
type Handlers struct{ repo *Repo }

func NewHandlers(repo *Repo) *Handlers { return &Handlers{repo: repo} }

// Register mounts the public catalogue routes.
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/products", h.listProducts)
	r.GET("/products/:slug", h.getProduct)
	r.GET("/categories", h.listCategories)
	r.GET("/brands", h.listBrands)
}

func (h *Handlers) listProducts(c *gin.Context) {
	f := ListFilter{
		Query:        c.Query("q"),
		CategorySlug: c.Query("category"),
		BrandSlug:    c.Query("brand"),
		WithPhoto:    c.Query("photo") == "1",
		Sort:         c.Query("sort"),
		Page:         httpx.QueryInt(c, "page", 1, 1, 10000),
		PageSize:     httpx.QueryInt(c, "pageSize", 20, 1, 100),
	}
	if v := c.Query("minPrice"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.MinPaise = n * 100
		}
	}
	if v := c.Query("maxPrice"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.MaxPaise = n * 100
		}
	}
	if v := c.Query("rxOnly"); v != "" {
		b := v == "true"
		f.RxOnly = &b
	}

	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	products, total, err := h.repo.ListForBusiness(c.Request.Context(), businessID, f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, products, httpx.NewMeta(f.Page, f.PageSize, total))
}

func (h *Handlers) getProduct(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	p, err := h.repo.GetBySlugForBusiness(c.Request.Context(), businessID, c.Param("slug"))
	if err != nil {
		if db.IsNoRows(err) {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("We could not find that product."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	related, err := h.repo.RelatedProducts(c.Request.Context(), p.ID, 6)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"product": p, "related": related})
}

func (h *Handlers) listCategories(c *gin.Context) {
	cats, err := h.repo.ListCategories(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, cats)
}

func (h *Handlers) listBrands(c *gin.Context) {
	brands, err := h.repo.ListBrands(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, brands)
}
