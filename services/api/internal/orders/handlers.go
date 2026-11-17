package orders

import (
	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/affiliate"
	"github.com/sanyam/medicart/services/api/internal/audit"
	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/cart"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/whatsapp"
)

type Handlers struct {
	svc            *Service
	cartSvc        *cart.Service
	rec            *audit.Recorder
	whatsappNumber string
	// affiliateSvc may be nil (tests that don't exercise the affiliate
	// program): every call site below checks for nil first.
	affiliateSvc *affiliate.Service
}

func NewHandlers(svc *Service, cartSvc *cart.Service, rec *audit.Recorder, whatsappNumber string, affiliateSvc *affiliate.Service) *Handlers {
	return &Handlers{svc: svc, cartSvc: cartSvc, rec: rec, whatsappNumber: whatsappNumber, affiliateSvc: affiliateSvc}
}

// Register mounts the public (customer-facing) order routes.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/checkout", h.checkout)
	r.POST("/orders/:reference/track", h.track)
	r.POST("/orders/:reference/whatsapp-handoff", h.whatsappHandoff)
	r.POST("/orders/:reference/cancel", h.customerCancel)

	// Signed-in customer's own order history — an alternative to
	// reference+mobile tracking, gated by session rather than a shared secret.
	r.GET("/me/orders", httpx.RequireAuth(), h.myOrders)
	r.GET("/me/orders/:id", httpx.RequireAuth(), h.myOrderDetail)
	r.GET("/me/buy-again", httpx.RequireAuth(), h.buyAgain)
}

// RegisterAdmin mounts the staff-only order routes under an already
// role-guarded group.
func (h *Handlers) RegisterAdmin(r gin.IRouter) {
	r.GET("/orders", h.adminList)
	r.GET("/orders/:id", h.adminGet)
	r.POST("/orders/:id/confirm-whatsapp", h.adminConfirmWhatsApp)
	r.POST("/orders/:id/status", h.adminUpdateStatus)
	r.POST("/orders/:id/cancel", h.adminCancel)
	r.POST("/orders/:id/notes", h.adminAddNote)
}

type checkoutReq struct {
	RecipientName string `json:"recipientName" binding:"required,min=1,max=120"`
	ContactMobile string `json:"contactMobile" binding:"required"`
	ContactEmail  string `json:"contactEmail" binding:"omitempty,email"`
	AddressLine1  string `json:"addressLine1" binding:"required,min=1,max=200"`
	AddressLine2  string `json:"addressLine2" binding:"max=200"`
	Landmark      string `json:"landmark" binding:"max=120"`
	City          string `json:"city" binding:"required,min=1,max=100"`
	State         string `json:"state" binding:"required,min=1,max=100"`
	PINCode       string `json:"pinCode" binding:"required"`
	DeliveryNotes string `json:"deliveryNotes" binding:"max=500"`
	// AffiliateCode is optional marketing metadata (a ?ref=CODE link the
	// shopper arrived through, stored client-side). An invalid, unknown or
	// inactive code is silently ignored — it must never block checkout.
	AffiliateCode string `json:"affiliateCode" binding:"max=12"`
}

func (h *Handlers) checkout(c *gin.Context) {
	var req checkoutReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	cartToken, _ := c.Cookie(cart.CookieName)
	if cartToken == "" {
		httpx.Fail(c, httpx.ErrCartEmpty)
		return
	}
	cartID, _, err := h.cartSvc.GetOrCreateGuestCart(c.Request.Context(), businessID, cartToken)
	if err != nil {
		httpx.Fail(c, err)
		return
	}

	in := CreateInput{
		CartID: cartID, BusinessID: businessID, RecipientName: req.RecipientName, ContactMobile: req.ContactMobile,
		ContactEmail: req.ContactEmail, AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		Landmark: req.Landmark, City: req.City, State: req.State, PINCode: req.PINCode,
		DeliveryNotes: req.DeliveryNotes,
	}
	// A signed-in customer's order ties to their existing profile rather than
	// creating a second guest identity for the same person.
	if p := httpx.CurrentPrincipal(c); p != nil && p.CustomerID != "" {
		in.AuthenticatedCustomerID = p.CustomerID
	}

	order, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}

	h.rec.Record(c.Request.Context(), audit.Entry{
		Action: audit.ActionOrderCreated, EntityType: "order", EntityID: order.ID,
		Metadata: map[string]any{"reference": order.Reference, "total_paise": order.TotalPaise},
	})

	affiliateApplied := false
	if h.affiliateSvc != nil && req.AffiliateCode != "" {
		// Its own transaction, deliberately after the order already
		// committed — a bad or stale referral code must never fail checkout.
		affiliateApplied, _ = h.affiliateSvc.AttributeOrder(c.Request.Context(), order.ID, businessID, req.AffiliateCode)
	}

	link := whatsapp.BuildHandoffLink(h.whatsappNumber, toWhatsAppSummary(order))
	httpx.Created(c, gin.H{"order": order, "whatsappLink": link, "affiliateApplied": affiliateApplied})
}

func toWhatsAppSummary(o *Order) whatsapp.OrderSummary {
	lines := make([]whatsapp.LineSummary, 0, len(o.Items))
	for _, it := range o.Items {
		pack := ""
		if it.PackSize != nil {
			pack = *it.PackSize
		}
		lines = append(lines, whatsapp.LineSummary{Name: it.ProductName, PackSize: pack, Qty: it.Qty})
	}
	return whatsapp.OrderSummary{Reference: o.Reference, PINCode: o.PINCode, Items: lines, TotalPaise: o.TotalPaise}
}

type trackReq struct {
	ContactMobile string `json:"contactMobile" binding:"required"`
}

// track looks an order up by reference + mobile. POST (not GET) so the
// mobile number travels in the body, not a query string that could end up in
// logs or browser history.
func (h *Handlers) track(c *gin.Context) {
	var req trackReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.svc.GetByReferenceForCustomer(c.Request.Context(), c.Param("reference"), auth.NormaliseMobile(req.ContactMobile))
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("We could not find an order matching those details."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, order)
}

func (h *Handlers) whatsappHandoff(c *gin.Context) {
	var req trackReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.svc.GetByReferenceForCustomer(c.Request.Context(), c.Param("reference"), auth.NormaliseMobile(req.ContactMobile))
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("We could not find an order matching those details."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.MarkWhatsAppHandoff(c.Request.Context(), order.ID); err != nil {
		httpx.Fail(c, err)
		return
	}
	link := whatsapp.BuildHandoffLink(h.whatsappNumber, toWhatsAppSummary(order))
	httpx.OK(c, gin.H{"whatsappLink": link})
}

type cancelReq struct {
	ContactMobile string `json:"contactMobile" binding:"required"`
	Reason        string `json:"reason" binding:"required,min=1,max=300"`
}

func (h *Handlers) customerCancel(c *gin.Context) {
	var req cancelReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.svc.GetByReferenceForCustomer(c.Request.Context(), c.Param("reference"), auth.NormaliseMobile(req.ContactMobile))
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound.WithMessage("We could not find an order matching those details."))
			return
		}
		httpx.Fail(c, err)
		return
	}
	updated, err := h.svc.Cancel(c.Request.Context(), h.rec, "", order.ID, req.Reason, "customer", nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if h.affiliateSvc != nil {
		_ = h.affiliateSvc.VoidForOrder(c.Request.Context(), order.ID)
	}
	httpx.OK(c, updated)
}

func (h *Handlers) myOrders(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if p.CustomerID == "" {
		httpx.Fail(c, httpx.ErrForbidden.WithMessage("This account has no customer order history."))
		return
	}
	page := httpx.QueryInt(c, "page", 1, 1, 10000)
	pageSize := httpx.QueryInt(c, "pageSize", 20, 1, 50)
	items, total, err := h.svc.ListForCustomer(c.Request.Context(), p.CustomerID, page, pageSize)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, items, httpx.NewMeta(page, pageSize, total))
}

func (h *Handlers) myOrderDetail(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if p.CustomerID == "" {
		httpx.Fail(c, httpx.ErrForbidden)
		return
	}
	order, err := h.svc.GetByIDForCustomer(c.Request.Context(), c.Param("id"), p.CustomerID)
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, order)
}

// ---------------------------------------------------------------- admin

func (h *Handlers) adminList(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	f := ListFilter{
		BusinessID: businessID,
		Status:     c.Query("status"),
		Search:     c.Query("q"),
		Page:       httpx.QueryInt(c, "page", 1, 1, 10000),
		PageSize:   httpx.QueryInt(c, "pageSize", 25, 1, 100),
	}
	items, total, err := h.svc.List(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.List(c, items, httpx.NewMeta(f.Page, f.PageSize, total))
}

func (h *Handlers) adminGet(c *gin.Context) {
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	order, err := h.svc.GetByID(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, order)
}

func (h *Handlers) adminConfirmWhatsApp(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	order, err := h.svc.ConfirmWhatsApp(c.Request.Context(), h.rec, businessID, c.Param("id"), p.UserID)
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, order)
}

type statusReq struct {
	Status string `json:"status" binding:"required"`
	Reason string `json:"reason" binding:"max=300"`
}

func (h *Handlers) adminUpdateStatus(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req statusReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.svc.UpdateStatus(c.Request.Context(), h.rec, businessID, c.Param("id"), req.Status, req.Reason, p.UserID)
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	if h.affiliateSvc != nil && req.Status == StatusRefunded {
		_ = h.affiliateSvc.VoidForOrder(c.Request.Context(), order.ID)
	}
	httpx.OK(c, order)
}

type adminCancelReq struct {
	Reason string `json:"reason" binding:"required,min=1,max=300"`
}

func (h *Handlers) adminCancel(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req adminCancelReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	order, err := h.svc.Cancel(c.Request.Context(), h.rec, businessID, c.Param("id"), req.Reason, "staff", &p.UserID)
	if err != nil {
		if err == ErrOrderNotFound {
			httpx.Fail(c, httpx.ErrNotFound)
			return
		}
		httpx.Fail(c, err)
		return
	}
	if h.affiliateSvc != nil {
		_ = h.affiliateSvc.VoidForOrder(c.Request.Context(), order.ID)
	}
	httpx.OK(c, order)
}

type noteReq struct {
	Note string `json:"note" binding:"required,min=1,max=2000"`
}

func (h *Handlers) adminAddNote(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	businessID, ok := httpx.MustBusinessID(c)
	if !ok {
		return
	}
	var req noteReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.AddInternalNote(c.Request.Context(), h.rec, businessID, c.Param("id"), req.Note, p.UserID); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.NoContent(c)
}

// buyAgain lists the products this customer has ordered before, most
// recently ordered first, so a returning shopper can reorder from the home
// page. Only slugs are returned: the storefront loads each product through
// the normal catalogue endpoint, so current price, stock and listing rules
// for this storefront still apply.
func (h *Handlers) buyAgain(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if p.CustomerID == "" {
		httpx.OK(c, []string{})
		return
	}
	slugs, err := h.svc.BuyAgainSlugs(c.Request.Context(), p.CustomerID, 20)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, slugs)
}
