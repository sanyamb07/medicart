// Command seed loads clearly-labelled demonstration data: categories,
// brands, products with batches and stock, serviceable PIN codes, shipping
// policies, and the bootstrap admin account.
//
// Safe to re-run: every insert is idempotent (ON CONFLICT DO NOTHING/UPDATE),
// so `make seed` after `make seed` does not create duplicates.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/config"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

func main() {
	adminOnly := flag.Bool("admin-only", false, "only create the bootstrap admin user")
	flag.Parse()

	if err := run(*adminOnly); err != nil {
		fmt.Fprintln(os.Stderr, "seed: "+err.Error())
		os.Exit(1)
	}
}

func run(adminOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, db.Config{URL: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := seedAdmin(ctx, pool, cfg); err != nil {
		return err
	}
	if adminOnly {
		fmt.Println("admin user seeded")
		return nil
	}

	if err := seedCatalog(ctx, pool); err != nil {
		return fmt.Errorf("seed catalog: %w", err)
	}
	if err := seedServiceability(ctx, pool); err != nil {
		return fmt.Errorf("seed serviceability: %w", err)
	}
	if err := seedPolicies(ctx, pool); err != nil {
		return fmt.Errorf("seed policies: %w", err)
	}
	if err := seedMultiTenant(ctx, pool, cfg); err != nil {
		return fmt.Errorf("seed multi-tenant demo data: %w", err)
	}

	fmt.Println("seed complete")
	return nil
}

// seedAdmin creates the bootstrap back-office account for the pre-existing
// storefront, now named and slugged as the "medicart-demo" business
// (tenancy.DefaultBusinessID). Staff access is granted via business_staff,
// not the global user_roles table — see docs/MULTI_TENANT_PLAN.md.
func seedAdmin(ctx context.Context, pool *db.Pool, cfg *config.Config) error {
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPassword == "" {
		fmt.Println("BOOTSTRAP_ADMIN_EMAIL/PASSWORD not set — skipping admin creation")
		return nil
	}
	userID, created, err := upsertStaffUser(ctx, pool, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword, "MediCart Admin")
	if err != nil {
		return err
	}
	if !created {
		fmt.Println("admin already exists:", cfg.BootstrapAdminEmail)
	} else {
		fmt.Println("created admin:", cfg.BootstrapAdminEmail)
	}
	for _, role := range []string{"ADMIN", "ORDER_MANAGER", "INVENTORY_MANAGER", "PHARMACIST", "CUSTOMER_SUPPORT"} {
		if err := grantBusinessStaff(ctx, pool, tenancy.DefaultBusinessID, userID, role); err != nil {
			return fmt.Errorf("grant %s at default business: %w", role, err)
		}
	}
	return nil
}

// upsertStaffUser finds-or-creates a staff login by email, returning whether
// it was newly created. Idempotent, so re-running seed is always safe.
func upsertStaffUser(ctx context.Context, pool *db.Pool, email, password, fullName string) (userID string, created bool, err error) {
	if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email = $1`, email).Scan(&userID); err == nil {
		return userID, false, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", false, err
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, is_active)
		VALUES ($1, $2, $3, TRUE) RETURNING id::text`,
		email, hash, fullName).Scan(&userID); err != nil {
		return "", false, fmt.Errorf("insert staff user: %w", err)
	}
	return userID, true, nil
}

func grantBusinessStaff(ctx context.Context, pool *db.Pool, businessID, userID, role string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO business_staff (business_id, user_id, role_code)
		VALUES ($1, $2, $3)
		ON CONFLICT (business_id, user_id, role_code) DO UPDATE SET is_active = TRUE`,
		businessID, userID, role)
	return err
}

type productSeed struct {
	sku, slug, name, brand, generic, strength, dosageForm, packSize string
	packUnits                                                       int
	mrpPaise, pricePaise                                            int64
	requiresRx                                                      bool
	description, keyUses, storage                                   string
	category                                                        string
	featured                                                        bool
	ingredients                                                     []string
	stockQty                                                        int
	expiryDays                                                      int
}

func seedCatalog(ctx context.Context, pool *db.Pool) error {
	brands := map[string]string{} // name -> id
	for _, name := range []string{"MediCart Essentials", "WellCure", "VitaPlus", "CuraLab", "PureHeal"} {
		id, err := upsertBrand(ctx, pool, name)
		if err != nil {
			return err
		}
		brands[name] = id
	}

	categories := map[string]string{} // slug -> id
	for _, c := range []struct{ name, slug, icon string }{
		{"Pain Relief", "pain-relief", "/img/categories/pain-relief.svg"},
		{"Vitamins & Supplements", "vitamins-supplements", "/img/categories/vitamins.svg"},
		{"Cold & Immunity", "cold-immunity", "/img/categories/cold-immunity.svg"},
		{"Digestive Care", "digestive-care", "/img/categories/digestive-care.svg"},
		{"Skin Care", "skin-care", "/img/categories/skin-care.svg"},
		{"Diabetes Care", "diabetes-care", "/img/categories/diabetes-care.svg"},
		{"Antibiotics", "antibiotics", "/img/categories/antibiotics.svg"},
	} {
		id, err := upsertCategory(ctx, pool, c.name, c.slug, c.icon)
		if err != nil {
			return err
		}
		categories[c.slug] = id
	}

	products := []productSeed{
		{
			sku: "MC-PARA-500-15", slug: "paracetamol-500mg-strip-of-15", name: "Paracetamol 500mg Tablets",
			brand: "MediCart Essentials", generic: "Paracetamol", strength: "500 mg", dosageForm: "TABLET",
			packSize: "Strip of 15 tablets", packUnits: 15, mrpPaise: 3500, pricePaise: 2800,
			description: "Paracetamol is used for the relief of mild to moderate pain and fever.",
			keyUses:     "Fever, headache, body ache", storage: "Store below 30°C in a dry place, away from direct sunlight.",
			category: "pain-relief", featured: true, ingredients: []string{"Paracetamol 500 mg"},
			stockQty: 500, expiryDays: 540,
		},
		{
			sku: "MC-IBU-400-10", slug: "ibuprofen-400mg-strip-of-10", name: "Ibuprofen 400mg Tablets",
			brand: "WellCure", generic: "Ibuprofen", strength: "400 mg", dosageForm: "TABLET",
			packSize: "Strip of 10 tablets", packUnits: 10, mrpPaise: 4200, pricePaise: 3570,
			description: "Ibuprofen is a non-steroidal anti-inflammatory drug (NSAID) used for pain and inflammation relief.",
			keyUses:     "Pain, inflammation, fever", storage: "Store below 25°C, protect from moisture.",
			category: "pain-relief", featured: true, ingredients: []string{"Ibuprofen 400 mg"},
			stockQty: 320, expiryDays: 480,
		},
		{
			sku: "MC-VITC-500-30", slug: "vitamin-c-500mg-30-tablets", name: "Vitamin C 500mg Tablets",
			brand: "VitaPlus", generic: "Ascorbic Acid", strength: "500 mg", dosageForm: "TABLET",
			packSize: "Bottle of 30 tablets", packUnits: 30, mrpPaise: 25000, pricePaise: 18900,
			description: "A daily supplement to support immune health.",
			keyUses:     "Immune support, antioxidant", storage: "Store in a cool, dry place.",
			category: "vitamins-supplements", featured: true, ingredients: []string{"Ascorbic Acid 500 mg"},
			stockQty: 200, expiryDays: 720,
		},
		{
			sku: "MC-MULTI-60", slug: "multivitamin-daily-60-capsules", name: "Daily Multivitamin Capsules",
			brand: "VitaPlus", generic: "Multivitamin", dosageForm: "CAPSULE",
			packSize: "Bottle of 60 capsules", packUnits: 60, mrpPaise: 45000, pricePaise: 35900,
			description: "A complete multivitamin and multimineral formula for everyday nutritional support.",
			keyUses:     "General wellness, nutritional support", storage: "Store below 25°C. Keep bottle tightly closed.",
			category: "vitamins-supplements", ingredients: []string{"Vitamin A", "Vitamin C", "Vitamin D3", "Zinc"},
			stockQty: 150, expiryDays: 600,
		},
		{
			sku: "MC-CETZ-10-10", slug: "cetirizine-10mg-strip-of-10", name: "Cetirizine 10mg Tablets",
			brand: "CuraLab", generic: "Cetirizine Hydrochloride", strength: "10 mg", dosageForm: "TABLET",
			packSize: "Strip of 10 tablets", packUnits: 10, mrpPaise: 2800, pricePaise: 2100,
			description: "An antihistamine used to relieve allergy symptoms.",
			keyUses:     "Sneezing, runny nose, itchy eyes", storage: "Store below 30°C.",
			category: "cold-immunity", ingredients: []string{"Cetirizine Hydrochloride 10 mg"},
			stockQty: 400, expiryDays: 500,
		},
		{
			sku: "MC-ORS-21G", slug: "oral-rehydration-salts-21g", name: "Oral Rehydration Salts Sachets",
			brand: "PureHeal", generic: "ORS", dosageForm: "SACHET",
			packSize: "Box of 10 sachets (21g each)", packUnits: 10, mrpPaise: 4500, pricePaise: 3900,
			description: "WHO-formula oral rehydration salts to help prevent dehydration.",
			keyUses:     "Dehydration from diarrhoea, vomiting, or heat", storage: "Store in a cool, dry place. Use reconstituted solution within 24 hours.",
			category: "digestive-care", ingredients: []string{"Sodium chloride", "Potassium chloride", "Glucose"},
			stockQty: 250, expiryDays: 450,
		},
		{
			sku: "MC-ANTAC-650-10", slug: "antacid-tablets-10-strip", name: "Antacid Chewable Tablets",
			brand: "PureHeal", generic: "Calcium Carbonate", dosageForm: "TABLET",
			packSize: "Strip of 10 tablets", packUnits: 10, mrpPaise: 3000, pricePaise: 2400,
			description: "Fast-acting relief from acidity, heartburn and indigestion.",
			keyUses:     "Acidity, heartburn, indigestion", storage: "Store below 30°C.",
			category: "digestive-care", ingredients: []string{"Calcium Carbonate 500 mg"},
			stockQty: 300, expiryDays: 420,
		},
		{
			sku: "MC-MOIST-100ML", slug: "moisturising-lotion-100ml", name: "Gentle Moisturising Lotion",
			brand: "PureHeal", packSize: "100 ml bottle", packUnits: 1, mrpPaise: 32500, pricePaise: 27500,
			dosageForm:  "LOTION",
			description: "A fragrance-free moisturising lotion suitable for sensitive and dry skin.",
			keyUses:     "Dry skin, daily moisturising", storage: "Store below 30°C. For external use only.",
			category: "skin-care", stockQty: 180, expiryDays: 700,
		},
		{
			sku: "MC-GLUM-500-10", slug: "glucometer-test-strips-50", name: "Blood Glucose Test Strips (Pack of 50)",
			brand: "CuraLab", packSize: "Pack of 50 strips", packUnits: 50, mrpPaise: 85000, pricePaise: 69900,
			dosageForm:  "DEVICE",
			description: "Compatible test strips for home blood glucose monitoring.",
			keyUses:     "Blood glucose self-monitoring", storage: "Store in the original container below 30°C. Avoid humidity.",
			category: "diabetes-care", stockQty: 90, expiryDays: 365,
		},
		{
			// A deliberately prescription-only demo product, used to verify
			// the rx-blocking rule; not featured, so the storefront E2E flow
			// never needs to touch it.
			sku: "MC-AMOX-500-10", slug: "amoxicillin-500mg-strip-of-10", name: "Amoxicillin 500mg Capsules",
			brand: "CuraLab", generic: "Amoxicillin", strength: "500 mg", dosageForm: "CAPSULE",
			packSize: "Strip of 10 capsules", packUnits: 10, mrpPaise: 9000, pricePaise: 7500,
			requiresRx:  true,
			description: "An antibiotic used to treat a number of bacterial infections. Available only against a valid prescription.",
			keyUses:     "Bacterial infections (as prescribed)", storage: "Store below 25°C.",
			category: "antibiotics", ingredients: []string{"Amoxicillin 500 mg"},
			stockQty: 60, expiryDays: 300,
		},
	}

	for _, p := range products {
		if err := upsertProduct(ctx, pool, p, brands[p.brand], categories[p.category]); err != nil {
			return fmt.Errorf("seed product %s: %w", p.sku, err)
		}
	}
	return nil
}

func upsertBrand(ctx context.Context, pool *db.Pool, name string) (string, error) {
	slug := slugify(name)
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO brands (name, slug) VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, name, slug).Scan(&id)
	return id, err
}

func upsertCategory(ctx context.Context, pool *db.Pool, name, slug, icon string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO categories (name, slug, icon) VALUES ($1, $2, $3)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, icon = EXCLUDED.icon
		RETURNING id::text`, name, slug, icon).Scan(&id)
	return id, err
}

func upsertProduct(ctx context.Context, pool *db.Pool, p productSeed, brandID, categoryID string) error {
	var productID string
	err := pool.QueryRow(ctx, `
		INSERT INTO products (
			sku, slug, name, brand_id, generic_name, strength, dosage_form, pack_size, pack_units,
			description, key_uses, storage_info, mrp_paise, price_paise,
			requires_prescription, is_demo_data, is_featured
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,TRUE,$16)
		ON CONFLICT (sku) DO UPDATE SET
			name = EXCLUDED.name, mrp_paise = EXCLUDED.mrp_paise, price_paise = EXCLUDED.price_paise,
			description = EXCLUDED.description, is_featured = EXCLUDED.is_featured
		RETURNING id::text`,
		p.sku, p.slug, p.name, brandID, nullIfEmpty(p.generic), nullIfEmpty(p.strength),
		nullIfEmpty(p.dosageForm), nullIfEmpty(p.packSize), nullIfZero(p.packUnits),
		nullIfEmpty(p.description), nullIfEmpty(p.keyUses), nullIfEmpty(p.storage),
		p.mrpPaise, p.pricePaise, p.requiresRx, p.featured).Scan(&productID)
	if err != nil {
		return fmt.Errorf("upsert product row: %w", err)
	}

	if categoryID != "" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO product_categories (product_id, category_id, is_primary)
			VALUES ($1, $2, TRUE) ON CONFLICT DO NOTHING`, productID, categoryID); err != nil {
			return fmt.Errorf("link category: %w", err)
		}
	}

	for i, ing := range p.ingredients {
		if _, err := pool.Exec(ctx, `
			INSERT INTO product_ingredients (product_id, ingredient, sort_order, verified_at)
			VALUES ($1, $2, $3, now()) ON CONFLICT (product_id, ingredient) DO NOTHING`,
			productID, ing, i); err != nil {
			return fmt.Errorf("link ingredient: %w", err)
		}
	}

	// Local placeholder image served directly by the frontend's static
	// assets (no backend storage dependency for seed images).
	if _, err := pool.Exec(ctx, `
		INSERT INTO product_images (product_id, storage_key, alt_text, sort_order)
		VALUES ($1, $2, $3, 0) ON CONFLICT DO NOTHING`,
		productID, fmt.Sprintf("/img/products/%s.svg", p.slug), p.name); err != nil {
		return fmt.Errorf("link image: %w", err)
	}

	// One fresh batch per product, unless one already exists for this batch
	// number (idempotent re-seed).
	batchNumber := "SEED-" + p.sku
	var batchExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM inventory_batches WHERE product_id = $1 AND batch_number = $2)`,
		productID, batchNumber).Scan(&batchExists); err != nil {
		return fmt.Errorf("check batch: %w", err)
	}
	if !batchExists {
		expiry := time.Now().AddDate(0, 0, p.expiryDays)
		if _, err := pool.Exec(ctx, `
			INSERT INTO inventory_batches (product_id, batch_number, expiry_date, qty_received, qty_on_hand, supplier_name)
			VALUES ($1, $2, $3, $4, $4, 'Demo Supplier Pvt Ltd')`,
			productID, batchNumber, expiry, p.stockQty); err != nil {
			return fmt.Errorf("insert batch: %w", err)
		}
	}
	return nil
}

func seedServiceability(ctx context.Context, pool *db.Pool) error {
	areas := []struct {
		pin, city, state string
		etaMin, etaMax   int
		cod              bool
	}{
		{"400001", "Mumbai", "Maharashtra", 1, 2, true},
		{"110001", "New Delhi", "Delhi", 1, 2, true},
		{"560001", "Bengaluru", "Karnataka", 1, 3, true},
		{"600001", "Chennai", "Tamil Nadu", 2, 3, true},
		{"700001", "Kolkata", "West Bengal", 2, 4, false},
		{"411001", "Pune", "Maharashtra", 1, 2, true},
		{"500001", "Hyderabad", "Telangana", 1, 3, true},
		{"380001", "Ahmedabad", "Gujarat", 2, 3, false},
	}
	for _, a := range areas {
		if _, err := pool.Exec(ctx, `
			INSERT INTO serviceable_pin_codes (pin_code, city, state, is_serviceable, eta_min_days, eta_max_days, cod_available)
			VALUES ($1, $2, $3, TRUE, $4, $5, $6)
			ON CONFLICT (pin_code) DO UPDATE SET city = EXCLUDED.city, state = EXCLUDED.state`,
			a.pin, a.city, a.state, a.etaMin, a.etaMax, a.cod); err != nil {
			return fmt.Errorf("seed pin %s: %w", a.pin, err)
		}
	}
	return nil
}

func seedPolicies(ctx context.Context, pool *db.Pool) error {
	policies := []struct{ key, title, body string }{
		{"delivery", "Delivery", "Orders are delivered within the estimated window shown at checkout for your PIN code. Delivery times are estimates, not guarantees, and may vary due to courier availability or local conditions."},
		{"returns", "Returns & Cancellations", "You may cancel an order before it has been dispatched. Medicines cannot be returned once delivered, in line with standard pharmacy practice, except in case of a verified quality issue — contact support with your order reference."},
		{"refunds", "Refunds", "Approved refunds are issued to the original payment method. Processing times depend on your bank or payment provider and are not guaranteed by MediCart."},
		{"prescriptions", "Prescriptions", "Prescription-only medicines require a valid prescription reviewed by a pharmacist before dispatch. We do not dispense prescription medicines without this review."},
	}
	for _, p := range policies {
		if _, err := pool.Exec(ctx, `
			INSERT INTO shipping_policies (policy_key, title, body) VALUES ($1, $2, $3)
			ON CONFLICT (policy_key) DO UPDATE SET title = EXCLUDED.title, body = EXCLUDED.body`,
			p.key, p.title, p.body); err != nil {
			return fmt.Errorf("seed policy %s: %w", p.key, err)
		}
	}
	return nil
}

// seedMultiTenant seeds the data that only makes sense once more than one
// business exists: a platform super admin (separate login from any
// business's own admin, per the platform/business separation requirement),
// and a second, independently-priced business so tenant isolation is
// something you can actually click through rather than just read about.
func seedMultiTenant(ctx context.Context, pool *db.Pool, cfg *config.Config) error {
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPassword == "" {
		return nil // no bootstrap credentials configured; nothing to derive demo logins from
	}

	platformEmail := "platform-" + cfg.BootstrapAdminEmail
	platformUserID, created, err := upsertStaffUser(ctx, pool, platformEmail, cfg.BootstrapAdminPassword, "MediCart Platform Owner")
	if err != nil {
		return fmt.Errorf("seed platform admin: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO platform_admins (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, platformUserID); err != nil {
		return fmt.Errorf("grant platform admin: %w", err)
	}
	if created {
		fmt.Println("created platform admin:", platformEmail)
	}

	const greenleafSlug = "greenleaf"
	var greenleafID string
	err = pool.QueryRow(ctx, `
		INSERT INTO businesses (slug, name, status, license_status, tagline, support_whatsapp)
		VALUES ($1, 'GreenLeaf Pharmacy', 'ACTIVE', 'VERIFIED',
		        'Neighbourhood pharmacy, now online', NULL)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, greenleafSlug).Scan(&greenleafID)
	if err != nil {
		return fmt.Errorf("seed greenleaf business: %w", err)
	}

	greenleafEmail := "greenleaf-" + cfg.BootstrapAdminEmail
	greenleafUserID, created, err := upsertStaffUser(ctx, pool, greenleafEmail, cfg.BootstrapAdminPassword, "GreenLeaf Admin")
	if err != nil {
		return fmt.Errorf("seed greenleaf admin: %w", err)
	}
	for _, role := range []string{"ADMIN", "ORDER_MANAGER", "INVENTORY_MANAGER", "PHARMACIST", "CUSTOMER_SUPPORT"} {
		if err := grantBusinessStaff(ctx, pool, greenleafID, greenleafUserID, role); err != nil {
			return fmt.Errorf("grant %s at greenleaf: %w", role, err)
		}
	}
	if created {
		fmt.Println("created greenleaf admin:", greenleafEmail)
	}

	// List a subset of the master catalogue at GreenLeaf's own prices —
	// deliberately different from the demo business's, so the two
	// storefronts visibly diverge rather than coincidentally matching.
	listings := []struct {
		sku        string
		pricePaise int64
		featured   bool
		stockQty   int
	}{
		{"MC-PARA-500-15", 2600, true, 300},
		{"MC-VITC-500-30", 19900, true, 150},
		{"MC-CETZ-10-10", 1900, false, 250},
		{"MC-ORS-21G", 3700, true, 180},
		{"MC-MOIST-100ML", 28900, false, 100},
	}
	for _, l := range listings {
		var productID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM products WHERE sku = $1`, l.sku).Scan(&productID); err != nil {
			return fmt.Errorf("find product %s for greenleaf: %w", l.sku, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO business_products (business_id, product_id, is_listed, price_paise, is_visible, is_featured)
			VALUES ($1, $2, TRUE, $3, TRUE, $4)
			ON CONFLICT (business_id, product_id) DO UPDATE SET
				price_paise = EXCLUDED.price_paise, is_featured = EXCLUDED.is_featured`,
			greenleafID, productID, l.pricePaise, l.featured); err != nil {
			return fmt.Errorf("list %s for greenleaf: %w", l.sku, err)
		}
		var batchExists bool
		batchNumber := "GREENLEAF-SEED-" + l.sku
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM inventory_batches WHERE business_id = $1 AND product_id = $2 AND batch_number = $3)`,
			greenleafID, productID, batchNumber).Scan(&batchExists); err != nil {
			return fmt.Errorf("check greenleaf batch %s: %w", l.sku, err)
		}
		if !batchExists {
			if _, err := pool.Exec(ctx, `
				INSERT INTO inventory_batches (business_id, product_id, batch_number, expiry_date, qty_received, qty_on_hand, supplier_name)
				VALUES ($1, $2, $3, $4, $5, $5, 'GreenLeaf Local Supplier')`,
				greenleafID, productID, batchNumber, time.Now().AddDate(1, 6, 0), l.stockQty); err != nil {
				return fmt.Errorf("stock greenleaf batch %s: %w", l.sku, err)
			}
		}
	}
	fmt.Println("seeded greenleaf catalog:", len(listings), "products")
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func slugify(s string) string {
	out := make([]rune, 0, len(s))
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			lastDash = false
		default:
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
