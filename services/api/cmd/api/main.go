// Command api runs the MediCart backend: one Go binary, serving the REST API
// used by the storefront and the admin dashboard.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/affiliate"
	"github.com/sanyam/medicart/services/api/internal/analytics"
	"github.com/sanyam/medicart/services/api/internal/assistant"
	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/cart"
	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/config"
	"github.com/sanyam/medicart/services/api/internal/crm"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/enquiries"
	"github.com/sanyam/medicart/services/api/internal/enrichment"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/inventory"
	"github.com/sanyam/medicart/services/api/internal/markets"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/payments"
	"github.com/sanyam/medicart/services/api/internal/platform/logger"
	"github.com/sanyam/medicart/services/api/internal/prescriptions"
	"github.com/sanyam/medicart/services/api/internal/shipping"
	"github.com/sanyam/medicart/services/api/internal/storage"
	"github.com/sanyam/medicart/services/api/internal/tenancy"

	"github.com/sanyam/medicart/services/api/internal/audit"
)

func main() {
	// `-healthcheck` is used by the Docker HEALTHCHECK instruction: a tiny
	// self-check that exits 0/1 without starting the server, since the
	// container has no curl/wget installed.
	for _, a := range os.Args[1:] {
		if a == "-healthcheck" {
			resp, err := http.Get("http://localhost:8080/healthz")
			if err != nil || resp.StatusCode != http.StatusOK {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.LogLevel, cfg.IsProduction())
	log.Info("starting MediCart API", cfg.Summary()...)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pool, err := db.Connect(ctx, db.Config{URL: cfg.DatabaseURL, MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	from, to, err := db.MigrateUp(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info("migrations applied", "from", from, "to", to)

	// ---------------------------------------------------------------- wiring

	rec := audit.New(pool)

	authSvc := auth.NewService(pool, cfg.SessionTTL, !cfg.IsProduction())
	authHandlers := auth.NewHandlers(authSvc, cfg.CookieDomain, cfg.CookieSecure)

	bizRepo := tenancy.NewRepo(pool)
	bizHandlers := tenancy.NewHandlers(bizRepo)

	catalogRepo := catalog.NewRepo(pool)
	catalogHandlers := catalog.NewHandlers(catalogRepo)

	crmRepo := crm.NewRepo(pool)
	crmHandlers := crm.NewHandlers(crmRepo)

	enquiryRepo := enquiries.NewRepo(pool)
	enquiryHandlers := enquiries.NewHandlers(enquiryRepo)

	cartSvc := cart.NewService(pool)
	cartHandlers := cart.NewHandlers(cartSvc, cfg.CookieDomain, cfg.CookieSecure)

	shipSvc := shipping.NewService(pool, cfg.Commerce.DeliveryFeePaise, cfg.Commerce.FreeDeliveryThresholdPaise)

	affiliateSvc := affiliate.NewService(pool, authSvc)
	affiliateHandlers := affiliate.NewHandlers(affiliateSvc, rec, cfg.CookieDomain, cfg.CookieSecure)

	orderSvc := orders.NewService(pool, cartSvc, shipSvc, cfg.RxSales)
	orderHandlers := orders.NewHandlers(orderSvc, cartSvc, rec, cfg.WhatsApp.BusinessNumber, affiliateSvc)

	paymentSvc := payments.NewService(pool, rec, cfg.Payments.Driver, []byte(cfg.SessionSecret), cfg.Payments.CallbackBaseURL, affiliateSvc)
	paymentHandlers := payments.NewHandlers(paymentSvc, orderSvc)

	analyticsSvc := analytics.NewService(pool)
	analyticsHandlers := analytics.NewHandlers(analyticsSvc)

	var store storage.Store
	if cfg.Storage.Driver == "s3" {
		store, err = storage.NewS3(cfg.Storage.S3Bucket, cfg.Storage.S3Region, cfg.Storage.S3Endpoint,
			cfg.Storage.S3AccessKeyID, cfg.Storage.S3SecretAccessKey, cfg.Storage.PublicBaseURL)
	} else {
		store, err = storage.NewLocal(cfg.Storage.LocalDir, cfg.Storage.PublicBaseURL, []byte(cfg.SessionSecret))
	}
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	prescriptionSvc := prescriptions.NewService(pool, store, cfg.Storage.MaxUploadBytes, cfg.Storage.PrescriptionURLTTL)
	prescriptionHandlers := prescriptions.NewHandlers(prescriptionSvc)

	assistantSvc := assistant.NewService(catalogRepo, shipSvc, orderSvc, crmRepo, enquiryRepo)
	assistantHandlers := assistant.NewHandlers(assistantSvc)

	inventoryAdminHandlers := inventory.NewAdminHandlers(pool)

	enrichmentHandlers := enrichment.NewHandlers(enrichment.NewService(pool, os.Getenv("OPENFDA_API_KEY")))
	marketHandlers := markets.NewHandlers(markets.NewRepo(pool, rec))

	// ---------------------------------------------------------------- router

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	// Production runs behind Cloudflare (Caddy on the origin just reverse-
	// proxies, it never strips or rewrites this header): CF-Connecting-IP is
	// set by Cloudflare's edge and cannot be forged by the original client,
	// unlike a bare X-Forwarded-For. ByIP rate limiting and audit log IPs are
	// meaningless without this — without it every request looks like it came
	// from Caddy's container IP. Harmless in local dev: the header is simply
	// absent there, so ClientIP() falls back to the direct connection as
	// before. The VPS firewall must still only accept 80/443 from
	// Cloudflare's published ranges (see docs/DEPLOYMENT.md) — otherwise
	// anyone connecting directly to the origin could forge this header too.
	router.TrustedPlatform = gin.PlatformCloudflare
	router.Use(httpx.RequestID(), httpx.Logging(log), httpx.Recovery(),
		httpx.SecurityHeaders(cfg.IsProduction()), httpx.CORS(cfg.CORSAllowedOrigins),
		httpx.BodyLimit(cfg.Storage.MaxUploadBytes), auth.SessionMiddleware(authSvc, auth.CookieName))

	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	registerFileServing(router, store)
	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	authLimiter := httpx.NewRateLimiter(10, time.Minute)
	writeLimiter := httpx.NewRateLimiter(30, time.Minute)
	aiLimiter := httpx.NewRateLimiter(cfg.AI.RateLimitPerHour, time.Hour)

	// Every storefront (public, customer-facing) request resolves a tenant
	// from the X-Business-Slug header, defaulting to the pre-multi-tenancy
	// storefront when absent — see tenancy.ResolveStorefrontBusiness.
	v1 := router.Group("/api/v1", tenancy.ResolveStorefrontBusiness(bizRepo))
	{
		authHandlers.Register(v1.Group("", httpx.RateLimit(authLimiter, httpx.ByIP)))
		authHandlers.RegisterSessionReads(v1)
		affiliateHandlers.Register(v1.Group("", httpx.RateLimit(authLimiter, httpx.ByIP)))
		affiliateHandlers.RegisterSessionReads(v1)
		bizHandlers.Register(v1)
		catalogHandlers.Register(v1)
		enrichmentHandlers.Register(v1)
		cartHandlers.Register(v1)
		orderHandlers.Register(v1.Group("", httpx.RateLimit(writeLimiter, httpx.ByUserOrIP)))
		paymentHandlers.Register(v1.Group("", httpx.RateLimit(writeLimiter, httpx.ByUserOrIP)))

		shippingHandlers(v1, shipSvc)
		analyticsHandlers.Register(v1.Group("", httpx.RateLimit(writeLimiter, httpx.ByIP)))
		prescriptionHandlers.Register(v1.Group("", httpx.RateLimit(writeLimiter, httpx.ByUserOrIP)))
		assistantHandlers.Register(v1.Group("", httpx.RateLimit(aiLimiter, httpx.ByUserOrIP)))
		marketHandlers.Register(v1)

		// A staff session always acts for exactly the business it belongs to
		// (business_staff), never a business the client names — see
		// tenancy.ResolveStaffBusiness.
		admin := v1.Group("/admin",
			auth.SessionMiddleware(authSvc, auth.StaffCookieName), httpx.RequireAuth(),
			tenancy.ResolveStaffBusiness(bizRepo))
		{
			bizHandlers.RegisterBusinessAdmin(admin)

			orderAdmin := admin.Group("", tenancy.RequireBusinessRole(bizRepo, httpx.RoleAdmin, httpx.RoleOrderManager))
			orderHandlers.RegisterAdmin(orderAdmin)

			paymentAdmin := admin.Group("", tenancy.RequireBusinessRole(bizRepo, httpx.RoleAdmin, httpx.RoleOrderManager))
			paymentHandlers.RegisterAdmin(paymentAdmin)

			rxAdmin := admin.Group("", tenancy.RequireBusinessRole(bizRepo, httpx.RoleAdmin, httpx.RolePharmacist))
			prescriptionHandlers.RegisterAdmin(rxAdmin)

			// A business's own catalog selection + pricing + inventory —
			// distinct from the platform's master-catalog editor below.
			catalogBizAdmin := admin.Group("", tenancy.RequireBusinessRole(bizRepo, httpx.RoleAdmin, httpx.RoleInventoryManager))
			catalogHandlers.RegisterBusinessCatalogAdmin(catalogBizAdmin)
			inventoryAdminHandlers.Register(catalogBizAdmin)

			crmAdmin := admin.Group("", tenancy.RequireBusinessRole(bizRepo,
				httpx.RoleAdmin, httpx.RoleOrderManager, httpx.RoleCustomerSupport))
			crmHandlers.Register(crmAdmin)
			enquiryHandlers.Register(crmAdmin)
		}

		// The affiliate's own portal: a signed-in affiliate sees only their
		// own referrals/earnings, never another affiliate's or a business's
		// data — a separate session namespace from both customer and staff.
		affiliatePortal := v1.Group("",
			auth.SessionMiddleware(authSvc, auth.AffiliateCookieName), httpx.RequireRole(httpx.RoleAffiliate))
		affiliateHandlers.RegisterDashboard(affiliatePortal)

		// The platform owner: manages businesses, not any one business's data.
		platform := v1.Group("/platform",
			auth.SessionMiddleware(authSvc, auth.StaffCookieName), httpx.RequireAuth(),
			tenancy.RequirePlatformAdmin(bizRepo))
		{
			bizHandlers.RegisterPlatformAdmin(platform)
			affiliateHandlers.RegisterPlatformAdmin(platform)
			enrichmentHandlers.RegisterPlatformAdmin(platform)
			marketHandlers.RegisterPlatformAdmin(platform)
			catalogHandlers.RegisterAdmin(platform) // master product catalog
		}
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-stop:
		log.Info("shutting down", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

// shippingHandlers mounts the one serviceability-check endpoint the
// storefront needs (PIN code entry on the homepage and checkout).
func shippingHandlers(r gin.IRouter, svc *shipping.Service) {
	r.GET("/serviceability/:pin", func(c *gin.Context) {
		area, err := svc.Check(c.Request.Context(), c.Param("pin"))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		httpx.OK(c, area)
	})
}

// registerFileServing mounts /files/* only for the local storage driver (an
// S3-compatible bucket serves its own objects directly from its own
// endpoint, never through this API). A public key is served unconditionally;
// a private key (a prescription) is served only once the HMAC grant in
// ?expires&signature — the same one SignedURL minted — checks out.
func registerFileServing(router *gin.Engine, store storage.Store) {
	local, ok := store.(*storage.Local)
	if !ok {
		return
	}
	router.GET("/files/*filepath", func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("filepath"), "/")
		if storage.IsPrivateKey(key) {
			if err := local.VerifySignature(key, c.Query("expires"), c.Query("signature")); err != nil {
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
				return
			}
		}
		rc, obj, err := local.Get(c.Request.Context(), key)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		defer rc.Close()
		c.Header("Content-Type", obj.ContentType)
		c.Header("Content-Length", strconv.FormatInt(obj.Size, 10))
		if obj.Visibility == storage.Private {
			c.Header("Cache-Control", "private, no-store")
		} else {
			c.Header("Cache-Control", "public, max-age=604800, immutable")
		}
		io.Copy(c.Writer, rc)
	})
}
