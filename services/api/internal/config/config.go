// Package config loads all runtime configuration from environment variables.
//
// Nothing is read from a file at runtime and no secret has a usable default:
// Validate() refuses to start a production process that still carries a
// development placeholder.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// devSessionSecret is the placeholder shipped in .env.example. Production
// startup fails if it is still in use.
const devSessionSecret = "dev-only-insecure-secret-change-me-0000000000000000"

type Config struct {
	AppEnv             string
	Port               int
	LogLevel           string
	CORSAllowedOrigins []string

	DatabaseURL string
	DBMaxConns  int32
	DBMinConns  int32

	SessionSecret string
	SessionTTL    time.Duration
	CookieDomain  string
	CookieSecure  bool

	BootstrapAdminEmail    string
	BootstrapAdminPassword string

	Storage      StorageConfig
	WhatsApp     WhatsAppConfig
	Payments     PaymentsConfig
	AI           AIConfig
	Commerce     CommerceConfig
	RxSales      bool
	DemoDataFlag bool
}

type StorageConfig struct {
	Driver             string // local | s3
	LocalDir           string
	PublicBaseURL      string
	S3Bucket           string
	S3Region           string
	S3Endpoint         string
	S3AccessKeyID      string
	S3SecretAccessKey  string
	PrescriptionURLTTL time.Duration
	MaxUploadBytes     int64
}

type WhatsAppConfig struct {
	Driver             string // link | cloud
	BusinessNumber     string
	PhoneNumberID      string
	AccessToken        string
	WebhookVerifyToken string
	AppSecret          string
}

type PaymentsConfig struct {
	Driver            string // mock | razorpay
	Currency          string
	RazorpayKeyID     string
	RazorpayKeySecret string
	WebhookSecret     string
	CallbackBaseURL   string
}

type AIConfig struct {
	Driver            string // mock | anthropic
	Model             string
	AnthropicAPIKey   string
	MaxTokens         int
	Timeout           time.Duration
	RateLimitPerHour  int
	MaxToolIterations int
}

type CommerceConfig struct {
	DeliveryFeePaise           int64
	FreeDeliveryThresholdPaise int64
	MaxQtyPerItem              int
	ReservationTTL             time.Duration
}

// Load reads the environment. It does not touch the filesystem; `make dev`
// exports .env via the Makefile, and hosting providers inject real variables.
func Load() (*Config, error) {
	c := &Config{
		AppEnv:             str("APP_ENV", "development"),
		Port:               num("API_PORT", 8080),
		LogLevel:           str("LOG_LEVEL", "info"),
		CORSAllowedOrigins: list("CORS_ALLOWED_ORIGINS", "http://localhost:5173"),

		DatabaseURL: str("DATABASE_URL", ""),
		DBMaxConns:  int32(num("DB_MAX_CONNS", 10)),
		DBMinConns:  int32(num("DB_MIN_CONNS", 2)),

		SessionSecret: str("SESSION_SECRET", ""),
		SessionTTL:    time.Duration(num("SESSION_TTL_HOURS", 720)) * time.Hour,
		CookieDomain:  str("COOKIE_DOMAIN", ""),
		CookieSecure:  boolean("COOKIE_SECURE", false),

		BootstrapAdminEmail:    str("BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminPassword: str("BOOTSTRAP_ADMIN_PASSWORD", ""),

		Storage: StorageConfig{
			Driver:             str("STORAGE_DRIVER", "local"),
			LocalDir:           str("STORAGE_LOCAL_DIR", "./.localstorage"),
			PublicBaseURL:      str("STORAGE_PUBLIC_BASE_URL", "http://localhost:8080/files"),
			S3Bucket:           str("S3_BUCKET", ""),
			S3Region:           str("S3_REGION", "auto"),
			S3Endpoint:         str("S3_ENDPOINT", ""),
			S3AccessKeyID:      str("S3_ACCESS_KEY_ID", ""),
			S3SecretAccessKey:  str("S3_SECRET_ACCESS_KEY", ""),
			PrescriptionURLTTL: time.Duration(num("PRESCRIPTION_URL_TTL_MINUTES", 10)) * time.Minute,
			MaxUploadBytes:     int64(num("MAX_UPLOAD_BYTES", 10*1024*1024)),
		},
		WhatsApp: WhatsAppConfig{
			Driver:             str("WHATSAPP_DRIVER", "link"),
			BusinessNumber:     digitsOnly(str("WHATSAPP_BUSINESS_NUMBER", "")),
			PhoneNumberID:      str("WHATSAPP_PHONE_NUMBER_ID", ""),
			AccessToken:        str("WHATSAPP_ACCESS_TOKEN", ""),
			WebhookVerifyToken: str("WHATSAPP_WEBHOOK_VERIFY_TOKEN", ""),
			AppSecret:          str("WHATSAPP_APP_SECRET", ""),
		},
		Payments: PaymentsConfig{
			Driver:            str("PAYMENT_DRIVER", "mock"),
			Currency:          str("PAYMENT_CURRENCY", "INR"),
			RazorpayKeyID:     str("RAZORPAY_KEY_ID", ""),
			RazorpayKeySecret: str("RAZORPAY_KEY_SECRET", ""),
			WebhookSecret:     str("RAZORPAY_WEBHOOK_SECRET", ""),
			CallbackBaseURL:   str("PAYMENT_CALLBACK_BASE_URL", "http://localhost:5173"),
		},
		AI: AIConfig{
			Driver:            str("AI_DRIVER", "mock"),
			Model:             str("AI_MODEL", "claude-sonnet-5"),
			AnthropicAPIKey:   str("ANTHROPIC_API_KEY", ""),
			MaxTokens:         num("AI_MAX_TOKENS", 1024),
			Timeout:           time.Duration(num("AI_TIMEOUT_SECONDS", 30)) * time.Second,
			RateLimitPerHour:  num("AI_RATE_LIMIT_PER_HOUR", 40),
			MaxToolIterations: num("AI_MAX_TOOL_ITERATIONS", 5),
		},
		Commerce: CommerceConfig{
			DeliveryFeePaise:           int64(num("DELIVERY_FEE_PAISE", 4900)),
			FreeDeliveryThresholdPaise: int64(num("FREE_DELIVERY_THRESHOLD_PAISE", 49900)),
			MaxQtyPerItem:              num("MAX_QTY_PER_ITEM", 10),
			ReservationTTL:             time.Duration(num("CART_RESERVATION_MINUTES", 30)) * time.Minute,
		},
		RxSales:      boolean("FEATURE_RX_SALES_ENABLED", false),
		DemoDataFlag: boolean("FEATURE_DEMO_DATA_BANNER", true),
	}
	return c, c.Validate()
}

func (c *Config) IsProduction() bool { return strings.EqualFold(c.AppEnv, "production") }

// Validate rejects configurations that would be unsafe or simply broken.
// Credential-dependent integrations are *not* required: each falls back to a
// clearly-labelled local driver so the platform runs without paid accounts.
func (c *Config) Validate() error {
	var errs []string

	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, "API_PORT must be 1-65535")
	}
	if len(c.SessionSecret) < 32 {
		errs = append(errs, "SESSION_SECRET must be at least 32 characters")
	}
	if !oneOf(c.Storage.Driver, "local", "s3") {
		errs = append(errs, "STORAGE_DRIVER must be local or s3")
	}
	if !oneOf(c.WhatsApp.Driver, "link", "cloud") {
		errs = append(errs, "WHATSAPP_DRIVER must be link or cloud")
	}
	if !oneOf(c.Payments.Driver, "mock", "razorpay") {
		errs = append(errs, "PAYMENT_DRIVER must be mock or razorpay")
	}
	if !oneOf(c.AI.Driver, "mock", "anthropic") {
		errs = append(errs, "AI_DRIVER must be mock or anthropic")
	}
	if c.Commerce.MaxQtyPerItem < 1 {
		errs = append(errs, "MAX_QTY_PER_ITEM must be >= 1")
	}

	// Each non-default driver needs its credentials, whatever the environment —
	// a half-configured gateway fails at the worst possible moment otherwise.
	if c.Storage.Driver == "s3" {
		if c.Storage.S3Bucket == "" || c.Storage.S3AccessKeyID == "" || c.Storage.S3SecretAccessKey == "" {
			errs = append(errs, "STORAGE_DRIVER=s3 requires S3_BUCKET, S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY")
		}
	}
	if c.Payments.Driver == "razorpay" {
		if c.Payments.RazorpayKeyID == "" || c.Payments.RazorpayKeySecret == "" {
			errs = append(errs, "PAYMENT_DRIVER=razorpay requires RAZORPAY_KEY_ID and RAZORPAY_KEY_SECRET")
		}
		if c.Payments.WebhookSecret == "" {
			errs = append(errs, "PAYMENT_DRIVER=razorpay requires RAZORPAY_WEBHOOK_SECRET (webhooks are rejected without it)")
		}
	}
	if c.WhatsApp.Driver == "cloud" {
		if c.WhatsApp.PhoneNumberID == "" || c.WhatsApp.AccessToken == "" {
			errs = append(errs, "WHATSAPP_DRIVER=cloud requires WHATSAPP_PHONE_NUMBER_ID and WHATSAPP_ACCESS_TOKEN")
		}
	}
	if c.AI.Driver == "anthropic" && c.AI.AnthropicAPIKey == "" {
		errs = append(errs, "AI_DRIVER=anthropic requires ANTHROPIC_API_KEY")
	}
	if c.WhatsApp.BusinessNumber == "" {
		errs = append(errs, "WHATSAPP_BUSINESS_NUMBER is required to build the order handoff link")
	}

	// Production-only hardening.
	if c.IsProduction() {
		if c.SessionSecret == devSessionSecret {
			errs = append(errs, "SESSION_SECRET is still the development placeholder")
		}
		if !c.CookieSecure {
			errs = append(errs, "COOKIE_SECURE must be true in production")
		}
		for _, o := range c.CORSAllowedOrigins {
			if o == "*" {
				errs = append(errs, "CORS_ALLOWED_ORIGINS must not be '*' in production (credentials are sent)")
			}
			if strings.HasPrefix(o, "http://") && !strings.Contains(o, "localhost") {
				errs = append(errs, fmt.Sprintf("CORS origin %q must use https in production", o))
			}
		}
		if c.BootstrapAdminPassword != "" {
			errs = append(errs, "BOOTSTRAP_ADMIN_PASSWORD must be removed from the environment after the first admin is created")
		}
	}

	if len(errs) > 0 {
		return errors.New("invalid configuration:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

// Summary is a redacted, loggable view of the active configuration. It reports
// whether each secret is present, never its value.
func (c *Config) Summary() []any {
	return []any{
		"env", c.AppEnv,
		"port", c.Port,
		"storage_driver", c.Storage.Driver,
		"payment_driver", c.Payments.Driver,
		"whatsapp_driver", c.WhatsApp.Driver,
		"ai_driver", c.AI.Driver,
		"rx_sales_enabled", c.RxSales,
		"cors_origins", strings.Join(c.CORSAllowedOrigins, ","),
		"razorpay_configured", c.Payments.RazorpayKeyID != "",
		"anthropic_configured", c.AI.AnthropicAPIKey != "",
		"whatsapp_cloud_configured", c.WhatsApp.AccessToken != "",
	}
}

// ---- env helpers ----

func str(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func num(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func boolean(k string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func list(k, def string) []string {
	raw := str(k, def)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
