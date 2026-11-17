// Command catalog-sync reconciles the catalogue with the supplier price list
// and with the enrichment team's verified findings. Every step is idempotent:
// run it any number of times and it converges on the same database state.
//
//	go run ./cmd/catalog-sync -import ../../data/source/medicine_list_2026A.json
//	go run ./cmd/catalog-sync -audit  ../../data/enrichment/audit/audit_rows.json
//	go run ./cmd/catalog-sync -images ../../data/enrichment/batch01/integrate.json
//
// Rules it keeps:
//   - An existing product's id, name, price, MRP and relationships are never
//     changed. Missing rows are inserted; existing rows only gain provenance.
//   - Nothing is marked VERIFIED here; only a pharmacist does that.
//   - Every write is in a transaction per product and is logged to stdout as
//     one JSON line, so the run log is the change list.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"math"
	"math/bits"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/config"
	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/enrichment"
	"github.com/sanyam/medicart/services/api/internal/storage"
)

const demoBusiness = "00000000-0000-0000-0000-000000000001"

type sourceRow struct {
	SR      int    `json:"sr"`
	Name    string `json:"name"`
	Generic string `json:"generic"`
	Price   string `json:"price"`
}

var logOut = json.NewEncoder(os.Stdout)

func logf(action, sku string, kv ...any) {
	m := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339), "action": action, "sku": sku}
	for i := 0; i+1 < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = kv[i+1]
	}
	_ = logOut.Encode(m)
}

func main() {
	importPath := flag.String("import", "", "supplier list JSON (from data/tools/parse_source.py)")
	auditPath := flag.String("audit", "", "auditor output audit_rows.json")
	imagesPath := flag.String("images", "", "QA-approved integration file for researched products")
	dry := flag.Bool("dry-run", false, "log what would change, write nothing")
	backfill := flag.String("backfill-images", "", "directory tree of original downloads: add dimensions, format, pHash and the preserved original to stored images")
	lifecycle := flag.String("lifecycle", "", "images_unresolved.tsv: set each unresolved product's image research status")
	flag.Parse()

	cfg, err := config.Load()
	must(err)
	ctx := context.Background()
	pool, err := db.Connect(ctx, db.Config{URL: cfg.DatabaseURL})
	must(err)
	defer pool.Close()

	if *importPath != "" {
		must(runImport(ctx, pool, *importPath, *dry))
	}
	if *auditPath != "" {
		must(runAudit(ctx, pool, *auditPath, *dry))
	}
	if *lifecycle != "" {
		must(runLifecycle(ctx, pool, *lifecycle, *dry))
	}
	if *imagesPath != "" || *backfill != "" {
		var store storage.Store
		if cfg.Storage.Driver == "s3" {
			store, err = storage.NewS3(cfg.Storage.S3Bucket, cfg.Storage.S3Region, cfg.Storage.S3Endpoint,
				cfg.Storage.S3AccessKeyID, cfg.Storage.S3SecretAccessKey, cfg.Storage.PublicBaseURL)
		} else {
			store, err = storage.NewLocal(cfg.Storage.LocalDir, cfg.Storage.PublicBaseURL, []byte(cfg.SessionSecret))
		}
		must(err)
		if *imagesPath != "" {
			must(runImages(ctx, pool, store, *imagesPath, *dry))
		}
		if *backfill != "" {
			must(runBackfill(ctx, pool, store, *backfill, *dry))
		}
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalog-sync:", err)
		os.Exit(1)
	}
}

func sku(sr int) string { return fmt.Sprintf("IMP-%04d", sr) }

// ---------------------------------------------------------------- classify

var (
	spaceRe = regexp.MustCompile(`\s+`)
	slugRe  = regexp.MustCompile(`[^a-z0-9]+`)
	strRe   = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?(?:\s?/\s?\d+(?:\.\d+)?)?\s?(?:MG|MCG|IU|GM|G|ML|MD)|\d+(?:\.\d+)?\s?%)`)
	massRe  = regexp.MustCompile(`(?i)^\d+(?:\.\d+)?(?:/\d+(?:\.\d+)?)?\s?(?:MG|MCG|IU)$|%$`)
	packRe  = regexp.MustCompile(`(?i)^\d+(?:\.\d+)?\s?(?:GM|G|ML|MD)$`)
)

// formRules map explicit form words in a product name to a dosage form,
// most specific first. A name with no form word is not classified here.
var formRules = []struct {
	re   *regexp.Regexp
	form string
}{
	{regexp.MustCompile(`(?i)\b(SYRINGE|SYRING|NEEDLE|MONITOR|OXYMETER|OXIMETER|PULS OXY|B\.P|MASK|I V SET|PEN ERGO|BAND GARD|CREPE|NEBULIZER KIT|MONITOR KIT|TEST KIT)\b`), "DEVICE"},
	{regexp.MustCompile(`(?i)\bORAL JELLY\b`), "OTHER"},
	{regexp.MustCompile(`(?i)\bNS \d+\s?ML\b`), "SOLUTION"},
	{regexp.MustCompile(`(?i)\b(INHALER|INHR|ROTACAPS|TURBUHALER|ACCUHALER|CIPHALER|ELLIPTA)\b`), "INHALER"},
	{regexp.MustCompile(`(?i)\b(RES|RESP|RESPULES|RESPIRATOR)\b`), "SOLUTION"},
	{regexp.MustCompile(`(?i)\b(INJ|INJECTION|AMP|AMPS|VIAL|DEPOT|PFS|KWIK PEN|FLEXPEN|SOLOSTAR|CARTRIDGE|NORDIFLEX)\b`), "INJECTION"},
	{regexp.MustCompile(`(?i)\b(OINT|OINTMENT|ONT|OIT)\b`), "OINTMENT"},
	{regexp.MustCompile(`(?i)\bDROP SPRAY\b`), "SPRAY"},
	{regexp.MustCompile(`(?i)\b(EYE|EAR|DROP|DROPS)\b`), "DROPS"},
	{regexp.MustCompile(`(?i)\b(SPRAY|NS)\b`), "SPRAY"},
	{regexp.MustCompile(`(?i)\bCREAM\b`), "CREAM"},
	{regexp.MustCompile(`(?i)\b(LOTION)\b`), "LOTION"},
	{regexp.MustCompile(`(?i)\b(SHAMPOO|SOL|SOLUTION|MOUTHWASH|LIQ)\b`), "SOLUTION"},
	{regexp.MustCompile(`(?i)\b(GEL|JELLY)\b`), "GEL"},
	{regexp.MustCompile(`(?i)\b(SUSP|SUSPENSION|SUS)\b`), "SUSPENSION"},
	{regexp.MustCompile(`(?i)\b(SYP|SYRP|SYRUP|SYR)\b`), "SYRUP"},
	{regexp.MustCompile(`(?i)\b(SACHET)\b`), "SACHET"},
	{regexp.MustCompile(`(?i)\b(POWDER|GRANULES)\b`), "POWDER"},
	{regexp.MustCompile(`(?i)\b(CAP|CAPS|CAPSULE|SOFTGEL)\b`), "CAPSULE"},
	{regexp.MustCompile(`(?i)\b(TAB|TABS|TABLET|TABLETS|DT|MD|ODS|SR|XL|ER|CR|XR)\b`), "TABLET"},
}

// correctableForms are the forms whose name words are unambiguous enough to
// correct a previously derived dosage form on an existing product.
var correctableForms = map[string]bool{"INHALER": true, "INJECTION": true, "DROPS": true, "CREAM": true,
	"OINTMENT": true, "CAPSULE": true, "SYRUP": true, "SUSPENSION": true, "LOTION": true}

var pde5Re = regexp.MustCompile(`(?i)SILDENAFIL|TADALAFIL|VARDENAFIL|AVANAFIL|UDENAFIL`)

// formFor refines the name-based form with the composition for new rows:
// abortion "kits" are packs of tablets, and a PDE5 "jelly" is an oral jelly.
func formFor(name, generic string) string {
	f := explicitForm(name)
	switch {
	case strings.Contains(strings.ToUpper(name), "KIT") && regexp.MustCompile(`(?i)MIFEPRIST|MISOPROST`).MatchString(generic):
		return "TABLET"
	case f == "GEL" && pde5Re.MatchString(generic) && strings.Contains(strings.ToUpper(name), "JELLY"):
		return "OTHER"
	}
	return f
}

// explicitForm returns the dosage form only when the name states it.
func explicitForm(name string) string {
	n := strings.ReplaceAll(name, ".", " ")
	for _, r := range formRules {
		if r.re.MatchString(n) {
			return r.form
		}
	}
	return ""
}

// Regulatory groups. These decide visibility, not legality: anything held
// is in the database, inactive, and waiting for a person.
var (
	notHumanRe = regexp.MustCompile(`(?i)TRENBOL|BOLDENON|DROSTANOL|METHENOLON|STANOZOL|OXANDROL|OXYMETH|METHANDIEN|METANDIEN|` +
		`CHLORODEHYDRO|CLENBUTER|MK-677|FLUOXYMESTER|CUT MIX`)
	controlledRe = regexp.MustCompile(`(?i)TAPENTADOL|\bMODAFINIL|ARMODAFINIL|ZOPICLONE|ZALEPLON|CARISOPRODOL|TIANEPTINE`)
	vetRe        = regexp.MustCompile(`(?i)\bVET\b|FENBENDAZ|FEBENDAZ|OCLACITINIB|FIROCOXIB|SELAMECTIN|PIMOBENDAN|KOMEC|PETMECTIN|HITEK|PANACUR|EAZYPET|RADICATE|KETOCHLOR|APPOQUIL|OCLAPET|FIROTAIL|DICHRYSTICIN`)
	otcRe        = regexp.MustCompile(`(?i)SYRINGE|SYRING|NEEDLE|FACE MASK|MONITOR|OXYMETER|PULSE|B\.P|GLUCOSE MONITOR|PREGNANCY TEST|ALCHOL PREP|` +
		`HERBAL|HIMALAYA|SHILAJIT|ASHWAGANDHA|CASTOR OIL|CETAPHIL|PHYSIOGEL|LUBRICATING|FIRST AID|NEBULIZER KIT|HICK|I\.V\. SET|MERCUROCHROME|` +
		`INTALON|ORAL REHYDRATION|DIETARY SUPPLEMENT|STERILE WATER|\bNS 250ML`)
)

func regulatoryGroup(name, generic string) string {
	t := name + " " + generic
	switch {
	case notHumanRe.MatchString(t):
		return "not_approved_for_human_use"
	case vetRe.MatchString(t):
		return "veterinary"
	case controlledRe.MatchString(t):
		return "controlled_abuse_prone"
	case otcRe.MatchString(t):
		return "otc_or_device"
	}
	return "approved_rx"
}

func title(s string) string {
	w := strings.Fields(strings.ToLower(s))
	for i := range w {
		w[i] = strings.ToUpper(w[i][:1]) + w[i][1:]
	}
	return strings.Join(w, " ")
}

// ---------------------------------------------------------------- import

func runImport(ctx context.Context, pool *db.Pool, path string, dry bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var rows []sourceRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return err
	}
	if len(rows) != 3300 {
		return fmt.Errorf("expected 3300 source rows, got %d — refusing to run on a partial list", len(rows))
	}
	expiry := time.Now().AddDate(1, 6, 0).Format("2006-01-02")
	var inserted, provenance, unchanged int
	for _, r := range rows {
		s := sku(r.SR)
		price, err := strconv.ParseFloat(r.Price, 64)
		if err != nil || price <= 0 {
			logf("skip_bad_price", s, "price", r.Price)
			continue
		}
		pricePaise := int64(math.Round(price * 100))
		group := regulatoryGroup(r.Name, r.Generic)
		srcJSON, _ := json.Marshal(r)

		var reasons []string
		active := true
		switch group {
		case "not_approved_for_human_use":
			active = false
			reasons = append(reasons, "REGULATORY: not approved for human use in India; inactive until a pharmacist reviews it")
		case "veterinary":
			reasons = append(reasons, "Veterinary product: confirm it may be sold by this pharmacy")
		}
		if strings.TrimSpace(r.Generic) == "" {
			reasons = append(reasons, "Supplier list gives no composition")
		}
		if r.SR == 1151 {
			// GENTALAB 2 ML at ₹46,935 next to GENTALAB 30ML at ₹35: almost
			// certainly a typo. The price is kept as supplied, not sold.
			active = false
			reasons = append(reasons, "Price ₹"+r.Price+" looks like a typo (GENTALAB INJ 30ML is ₹35); inactive until the price is confirmed")
		}
		if len(strings.TrimSpace(r.Name)) <= 2 {
			active = false
			reasons = append(reasons, "Product name in supplier list is only '"+strings.TrimSpace(r.Name)+"'; real name needed")
		}

		var id string
		err = pool.QueryRow(ctx, `SELECT id::text FROM products WHERE sku = $1`, s).Scan(&id)
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if err == nil {
			// Existing product: provenance only. Name, price and flags stay.
			if dry {
				unchanged++
				continue
			}
			tag, err := pool.Exec(ctx, `
				UPDATE product_enrichment SET source_row = $2::jsonb, regulatory_group = COALESCE(regulatory_group, $3)
				 WHERE product_id = $1 AND (source_row IS DISTINCT FROM $2::jsonb OR regulatory_group IS NULL)`, id, string(srcJSON), group)
			if err != nil {
				return fmt.Errorf("%s provenance: %w", s, err)
			}
			if tag.RowsAffected() > 0 {
				provenance++
				logf("provenance", s)
			} else {
				unchanged++
			}
			continue
		}

		name := strings.TrimSpace(spaceRe.ReplaceAllString(r.Name, " "))
		form := formFor(name, r.Generic)
		var strength, pack string
		for _, m := range strRe.FindAllString(name, -1) {
			m = strings.ToUpper(strings.ReplaceAll(m, " ", ""))
			switch {
			case massRe.MatchString(m) && strength == "":
				strength = m
			case packRe.MatchString(m) && pack == "":
				pack = m
			}
		}
		if form == "" && strength != "" {
			form = "TABLET"
			reasons = append(reasons, "Dosage form not stated in name; assumed tablet")
		}
		if form == "" {
			form = "OTHER"
			reasons = append(reasons, "Dosage form not stated in name")
		}
		rx := group != "otc_or_device"
		controlled := group == "controlled_abuse_prone"
		slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
		if slug == "" {
			slug = "product"
		}
		slug = fmt.Sprintf("%s-%d", slug, r.SR)
		generic := title(strings.TrimSpace(r.Generic))

		logf("insert", s, "name", name, "price_paise", pricePaise, "form", form, "group", group, "active", active)
		inserted++
		if dry {
			continue
		}
		err = pool.InTx(ctx, func(q db.Querier) error {
			var pid string
			if err := q.QueryRow(ctx, `
				INSERT INTO products (sku, slug, name, generic_name, strength, dosage_form, pack_size, mrp_paise, price_paise,
				                      requires_prescription, is_controlled_substance, is_demo_data, is_active)
				VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8,$9,$10,$11,TRUE,$12)
				RETURNING id::text`,
				s, slug, name, generic, strength, form, pack, int64(math.Round(price*125)), pricePaise, rx, controlled, active).Scan(&pid); err != nil {
				return fmt.Errorf("insert product: %w", err)
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO product_images (product_id, storage_key, alt_text, sort_order, source_name, is_placeholder)
				VALUES ($1, $2, $3, 0, 'MediCart illustration', TRUE)`, pid, "/img/forms/"+strings.ToLower(form)+".svg", name); err != nil {
				return fmt.Errorf("placeholder image: %w", err)
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO business_products (business_id, product_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, demoBusiness, pid); err != nil {
				return fmt.Errorf("list in demo store: %w", err)
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO inventory_batches (business_id, product_id, batch_number, expiry_date, qty_received, qty_on_hand, supplier_name)
				VALUES ($1, $2, $3, $4, 200, 200, 'Demo Supplier Pvt Ltd') ON CONFLICT DO NOTHING`, demoBusiness, pid, "IMPORT-"+s, expiry); err != nil {
				return fmt.Errorf("demo stock: %w", err)
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO product_enrichment (product_id, source_row, regulatory_group, review_reasons)
				VALUES ($1, $2::jsonb, $3, $4)`, pid, string(srcJSON), group, nonNil(reasons)); err != nil {
				return fmt.Errorf("enrichment row: %w", err)
			}
			_, err := q.Exec(ctx, enrichment.RecomputeVerificationSQL, pid)
			return err
		})
		if err != nil {
			logf("insert_failed", s, "error", err.Error())
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	logf("import_done", "", "inserted", inserted, "provenance_added", provenance, "unchanged", unchanged)
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---------------------------------------------------------------- audit

type auditRow struct {
	SR     int    `json:"sr"`
	Name   string `json:"name"`
	Parsed struct {
		DosageForm *string `json:"dosage_form"`
	} `json:"parsed"`
	Flags []struct {
		Code     string  `json:"code"`
		Severity string  `json:"severity"`
		Detail   string  `json:"detail"`
		Evidence *string `json:"evidence_url"`
	} `json:"flags"`
}

// Flags that send a product to a person. Informational flags (combination
// product, misspelt generic with an obvious INN) are recorded but do not.
// A listed flag sends the product to a person unless the auditor rated it
// low; any high-severity flag always does.
var reviewFlags = map[string]bool{
	"suspected_wrong_generic": true, "exact_duplicate": true, "same_name_different_price": true,
	"same_name_different_generic": true, "missing_generic": true, "placeholder_name": true,
	"price_outlier": true, "db_mismatch": true, "incomplete_generic": true, "strength_unit_suspect": true,
	"strength_mismatch": true, "form_generic_conflict": true,
}

func runAudit(ctx context.Context, pool *db.Pool, path string, dry bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var rows []auditRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return err
	}
	var flagged, held, formFixed int
	for _, r := range rows {
		s := sku(r.SR)
		var reasons []string
		hold := false
		for _, f := range r.Flags {
			if f.Severity != "high" && !(reviewFlags[f.Code] && f.Severity != "low") {
				continue
			}
			msg := "AUDIT " + f.Code + ": " + f.Detail
			if f.Evidence != nil && *f.Evidence != "" {
				msg += " (" + *f.Evidence + ")"
			}
			reasons = append(reasons, msg)
			if f.Code == "suspected_wrong_generic" || f.Code == "form_generic_conflict" {
				// The listed composition would describe a different medicine.
				hold = true
			}
		}
		// Dosage form: correct it only where the NAME states the form and the
		// stored value was derived (not edited by a person).
		explicit := explicitForm(r.Name)
		if !correctableForms[explicit] {
			explicit = ""
		}
		if dry {
			if len(reasons) > 0 {
				logf("would_flag", s, "reasons", reasons, "hold", hold)
			}
			continue
		}
		err := pool.InTx(ctx, func(q db.Querier) error {
			var pid, curForm string
			var edited bool
			if err := q.QueryRow(ctx, `
				SELECT p.id::text, COALESCE(p.dosage_form,''), e.edited_at IS NOT NULL
				  FROM products p JOIN product_enrichment e ON e.product_id = p.id WHERE p.sku = $1`, s).Scan(&pid, &curForm, &edited); err != nil {
				if db.IsNoRows(err) {
					return nil
				}
				return err
			}
			if explicit != "" && explicit != curForm && !edited {
				if _, err := q.Exec(ctx, `UPDATE products SET dosage_form = $2 WHERE id = $1`, pid, explicit); err != nil {
					return err
				}
				// The placeholder illustration follows the form.
				if _, err := q.Exec(ctx, `UPDATE product_images SET storage_key = $2 WHERE product_id = $1 AND is_placeholder`,
					pid, "/img/forms/"+strings.ToLower(explicit)+".svg"); err != nil {
					return err
				}
				// The FDA lookup depends on the route, so the text is rebuilt.
				if _, err := q.Exec(ctx, `UPDATE product_enrichment SET status = 'pending', attempts = 0 WHERE product_id = $1 AND last_verified_at IS NULL AND edited_at IS NULL`, pid); err != nil {
					return err
				}
				logf("dosage_form_corrected", s, "from", curForm, "to", explicit)
				formFixed++
			}
			// Audit reasons replace earlier audit reasons; other reasons stay.
			if _, err := q.Exec(ctx, `
				UPDATE product_enrichment SET
					review_reasons = ARRAY(SELECT x FROM unnest(review_reasons) x WHERE x NOT LIKE 'AUDIT %') || $2::text[],
					hold_generated_text = $3
				 WHERE product_id = $1`, pid, nonNil(reasons), hold); err != nil {
				return err
			}
			if hold {
				// Re-run through the pipeline, which withdraws generated text.
				if _, err := q.Exec(ctx, `UPDATE product_enrichment SET status = 'pending', attempts = 0 WHERE product_id = $1 AND last_verified_at IS NULL`, pid); err != nil {
					return err
				}
			}
			_, err := q.Exec(ctx, enrichment.RecomputeVerificationSQL, pid)
			return err
		})
		if err != nil {
			logf("audit_failed", s, "error", err.Error())
			return fmt.Errorf("%s: %w", s, err)
		}
		if len(reasons) > 0 {
			flagged++
			logf("flagged", s, "reasons", len(reasons), "hold", hold)
		}
		if hold {
			held++
		}
	}
	logf("audit_done", "", "flagged", flagged, "text_held", held, "dosage_form_corrected", formFixed)
	return nil
}

// ---------------------------------------------------------------- images

// integrated is one QA-approved research result.
type integrated struct {
	SKU string `json:"sku"`
	// Manufacturer is the company whose own catalogue/site lists the brand,
	// i.e. the MARKETER (older files used this name). ActualManufacturer is
	// set only when the pack or an official document names the maker.
	Manufacturer          *string  `json:"manufacturer"`
	ActualManufacturer    *string  `json:"actual_manufacturer"`
	ManufacturerEvidence  *string  `json:"manufacturer_evidence"`
	SourceType            string   `json:"source_type"`
	ManufacturerSourceURL *string  `json:"manufacturer_source_url"`
	OfficialProductURL    *string  `json:"official_product_page_url"`
	PackSize              *string  `json:"pack_size"`
	ReviewReasons         []string `json:"review_reasons"`
	Image                 *struct {
		LocalFile       string `json:"local_file"`
		ImageURL        string `json:"image_url"`
		PageURL         string `json:"page_url"`
		SourceName      string `json:"source_name"`
		VisiblePackText string `json:"visible_pack_text"`
		QANote          string `json:"qa_note"`
	} `json:"image"`
}

var imageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

func runImages(ctx context.Context, pool *db.Pool, store storage.Store, path string, dry bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var items []integrated
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	base := filepath.Dir(filepath.Dir(filepath.Dir(path))) // .../data
	var withImage, withMfr int
	for _, it := range items {
		var pid string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM products WHERE sku = $1`, it.SKU).Scan(&pid); err != nil {
			logf("image_skip_unknown_sku", it.SKU, "error", err.Error())
			continue
		}
		var key, sum, imgFmt, imgPH, imgExt, origKey string
		var imgW, imgH int
		var layoutNote string
		var body []byte
		if it.Image != nil && it.Image.LocalFile != "" {
			body, err = os.ReadFile(filepath.Join(filepath.Dir(base), it.Image.LocalFile))
			if err != nil {
				return fmt.Errorf("%s: read image: %w", it.SKU, err)
			}
			ct := http.DetectContentType(body)
			ext, ok := imageTypes[ct]
			if !ok {
				logf("image_rejected_type", it.SKU, "content_type", ct)
				continue
			}
			_ = ext
			h := sha256.Sum256(body)
			sum = hex.EncodeToString(h[:])
			// The same file on two different products means one is wrong.
			var other string
			if err := pool.QueryRow(ctx, `
				SELECT p.sku FROM product_images i JOIN products p ON p.id = i.product_id
				 WHERE i.content_sha256 = $1 AND i.product_id <> $2 LIMIT 1`, sum, pid).Scan(&other); err == nil {
				logf("image_rejected_duplicate", it.SKU, "same_file_as", other)
				continue
			}
			var pname string
			if err := pool.QueryRow(ctx, `SELECT name FROM products WHERE id = $1`, pid).Scan(&pname); err != nil {
				return err
			}
			large, small, w, hgt, err := optimise(body)
			if err != nil {
				logf("image_rejected_unreadable", it.SKU, "error", err.Error())
				continue
			}
			if min(w, hgt) < 300 {
				logf("image_rejected_too_small", it.SKU, "size", fmt.Sprintf("%dx%d", w, hgt))
				continue
			}
			ph, err := dhash(body)
			if err != nil {
				logf("image_rejected_unreadable", it.SKU, "error", err.Error())
				continue
			}
			// A close pHash is a supporting signal only: brand families share one
			// carton template (e.g. 50 mg and 100 mg differ only in printed
			// text), so it flags the image for a strength check, never rejects.
			if other, dist, err := nearDuplicate(ctx, pool, ph, pid); err != nil {
				return err
			} else if other != "" {
				layoutNote = fmt.Sprintf("SIMILAR_LAYOUT: pHash within %d bits of %s's image; confirm the strength printed on this pack", dist, other)
				logf("image_similar_layout", it.SKU, "similar_to", other, "hamming", dist)
			}
			imgW, imgH, imgFmt, imgPH, imgExt = w, hgt, strings.TrimPrefix(ext, "."), ph, ext
			// Descriptive, stable name; the hash makes a changed image a new key.
			stem := fmt.Sprintf("products/%s/%s-%s", it.SKU, strings.Trim(slugRe.ReplaceAllString(strings.ToLower(pname), "-"), "-"), sum[:8])
			key = stem + ".webp"
			origKey = stem + "-original" + imgExt
			if !dry {
				for k, v := range map[string]struct {
					b  []byte
					ct string
				}{key: {large, "image/webp"}, stem + "-400.webp": {small, "image/webp"}, origKey: {body, ct}} {
					if _, err := store.Put(ctx, storage.PutInput{Key: k, ContentType: v.ct, Visibility: storage.Public,
						Body: bytes.NewReader(v.b), Size: int64(len(v.b))}); err != nil {
						return fmt.Errorf("%s: store image: %w", it.SKU, err)
					}
				}
			}
		}
		if dry {
			logf("would_integrate", it.SKU, "image", key != "", "manufacturer", it.Manufacturer != nil)
			continue
		}
		err := pool.InTx(ctx, func(q db.Querier) error {
			if it.Manufacturer != nil && *it.Manufacturer != "" {
				marketer := strings.TrimSpace(strings.TrimSuffix(*it.Manufacturer, "(marketer)"))
				maker := deref(it.ActualManufacturer)
				// Marketer and manufacturer are kept apart; neither overwrites
				// a value a person entered (edited_at) or an existing one.
				if _, err := q.Exec(ctx, `
					UPDATE product_enrichment SET marketer_name = COALESCE(marketer_name, $2),
					       marketer_source_url = COALESCE(marketer_source_url, NULLIF($3,'')),
					       manufacturer_name = COALESCE(manufacturer_name, NULLIF($4,'')),
					       manufacturer_evidence = COALESCE(manufacturer_evidence, NULLIF($5,''))
					 WHERE product_id = $1 AND edited_at IS NULL`, pid, marketer, deref(it.ManufacturerSourceURL), maker, deref(it.ManufacturerEvidence)); err != nil {
					return err
				}
				if _, err := q.Exec(ctx, DisplayManufacturerSQL, pid); err != nil {
					return err
				}
				withMfr++
			}
			if it.PackSize != nil && *it.PackSize != "" {
				if _, err := q.Exec(ctx, `UPDATE products SET pack_size = $2 WHERE id = $1 AND pack_size IS NULL`, pid, *it.PackSize); err != nil {
					return err
				}
			}
			if _, err := q.Exec(ctx, `
				UPDATE product_enrichment SET manufacturer_source_url = COALESCE(NULLIF($2,''), manufacturer_source_url),
				       official_product_url = COALESCE(NULLIF($3,''), official_product_url),
				       review_reasons = ARRAY(SELECT x FROM unnest(review_reasons) x WHERE x NOT LIKE 'RESEARCH %') || $4::text[]
				 WHERE product_id = $1`, pid, deref(it.ManufacturerSourceURL), deref(it.OfficialProductURL), prefixed("RESEARCH ", it.ReviewReasons)); err != nil {
				return err
			}
			if key != "" {
				url, err := store.PublicURL(key)
				if err != nil {
					return err
				}
				// Idempotent: the same file for the same product is one row.
				origURL, err := store.PublicURL(origKey)
				if err != nil {
					return err
				}
				srcType := it.SourceType
				if srcType == "" {
					srcType = "manufacturer_catalogue"
				}
				var qaNotes []string
				if it.Image.QANote != "" {
					qaNotes = append(qaNotes, it.Image.QANote)
				}
				if layoutNote != "" {
					qaNotes = append(qaNotes, layoutNote)
				}
				if min(imgW, imgH) < 1000 {
					qaNotes = append(qaNotes, fmt.Sprintf("LOW_RESOLUTION: best available source is %dx%d (not upscaled)", imgW, imgH))
				}
				if _, err := q.Exec(ctx, `
					INSERT INTO product_images (product_id, storage_key, alt_text, sort_order, source_name, source_url, source_page_url,
					                            visible_pack_text, content_sha256, verification_notes, is_placeholder, verification_status,
					                            width, height, format, source_type, qa_status, qa_notes, phash, original_storage_key)
					SELECT $1, $2, p.name, 0, $3, $4, $5, $6, $7, NULLIF($8,''), FALSE, 'PENDING',
					       $9, $10, $11, $12, 'QA_PASSED', NULLIF($13,''), $14, $15 FROM products p
					 WHERE p.id = $1 AND NOT EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = $1 AND i.content_sha256 = $7)`,
					pid, url, it.Image.SourceName, it.Image.ImageURL, it.Image.PageURL, it.Image.VisiblePackText, sum, it.Image.QANote,
					imgW, imgH, imgFmt, srcType, strings.Join(qaNotes, "; "), imgPH, origURL); err != nil {
					return err
				}
				if _, err := q.Exec(ctx, `UPDATE product_enrichment SET image_research_status = 'PHARMACIST_REVIEW_PENDING', image_research_note = NULL
					 WHERE product_id = $1 AND image_research_status NOT IN ('APPROVED')`, pid); err != nil {
					return err
				}
				if _, err := q.Exec(ctx, `UPDATE product_enrichment SET image_status = 'needs_review' WHERE product_id = $1 AND image_status = 'placeholder'`, pid); err != nil {
					return err
				}
				withImage++
			}
			_, err := q.Exec(ctx, enrichment.RecomputeVerificationSQL, pid)
			return err
		})
		if err != nil {
			logf("integrate_failed", it.SKU, "error", err.Error())
			return fmt.Errorf("%s: %w", it.SKU, err)
		}
		logf("integrated", it.SKU, "image_key", key, "manufacturer", deref(it.Manufacturer))
	}
	logf("images_done", "", "images_pending_review", withImage, "manufacturers", withMfr)
	return nil
}

// DisplayManufacturerSQL ($1 = product id) writes the customer-facing
// products.manufacturer_info from the separate marketer/manufacturer fields:
// "Marketed by X; manufactured by Y". A value a person edited is kept.
const DisplayManufacturerSQL = `
	UPDATE products p SET manufacturer_info =
	       CASE WHEN e.marketer_name IS NULL THEN p.manufacturer_info
	            WHEN e.manufacturer_name IS NULL OR e.manufacturer_name = e.marketer_name THEN 'Marketed by ' || e.marketer_name
	            ELSE 'Marketed by ' || e.marketer_name || '; manufactured by ' || e.manufacturer_name END
	  FROM product_enrichment e
	 WHERE p.id = $1 AND e.product_id = p.id AND e.edited_at IS NULL`

// dhash is a 64-bit difference hash: the image is reduced to 9x8 grey
// pixels and each bit records whether a pixel is brighter than its right
// neighbour. Re-encoded, resized or lightly recompressed copies of the same
// picture land within a few bits of each other.
func dhash(src []byte) (string, error) {
	dir, err := os.MkdirTemp("", "catalog-hash-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "h.png")
	if err := os.WriteFile(in, src, 0o600); err != nil {
		return "", err
	}
	if b, err := exec.Command("vips", "thumbnail", in, out, "9", "--height", "8", "--size", "force").CombinedOutput(); err != nil {
		return "", fmt.Errorf("vips: %v: %s", err, b)
	}
	f, err := os.Open(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", err
	}
	grey := func(x, y int) uint32 {
		r, g, b, _ := img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y).RGBA()
		return (299*r + 587*g + 114*b) / 1000
	}
	var h uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			h <<= 1
			if grey(x, y) > grey(x+1, y) {
				h |= 1
			}
		}
	}
	return fmt.Sprintf("%016x", h), nil
}

// nearDuplicate returns another product whose image hash is within 4 bits of
// ph. Two different products sharing one picture means one mapping is wrong.
func nearDuplicate(ctx context.Context, pool *db.Pool, ph, pid string) (string, int, error) {
	want, err := strconv.ParseUint(ph, 16, 64)
	if err != nil {
		return "", 0, err
	}
	rows, err := pool.Query(ctx, `
		SELECT p.sku, i.phash FROM product_images i JOIN products p ON p.id = i.product_id
		 WHERE i.phash IS NOT NULL AND i.product_id <> $1 AND i.verification_status <> 'REJECTED'`, pid)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var sku, h string
		if err := rows.Scan(&sku, &h); err != nil {
			return "", 0, err
		}
		if v, err := strconv.ParseUint(h, 16, 64); err == nil {
			if d := bits.OnesCount64(v ^ want); d <= 4 {
				return sku, d, nil
			}
		}
	}
	return "", 0, rows.Err()
}

// optimise produces the display image (WebP, at most 1600 px, never
// upscaled, metadata stripped) and a 400 px card-size variant, using libvips.
// It also returns the source's pixel size.
func optimise(src []byte) (large, small []byte, w, h int, err error) {
	dir, err := os.MkdirTemp("", "catalog-img-")
	if err != nil {
		return nil, nil, 0, 0, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, src, 0o600); err != nil {
		return nil, nil, 0, 0, err
	}
	out, err := exec.Command("vipsheader", "-f", "width", in).Output()
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("not a readable image: %w", err)
	}
	w, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	out, err = exec.Command("vipsheader", "-f", "height", in).Output()
	if err != nil {
		return nil, nil, 0, 0, err
	}
	h, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	for _, v := range []struct {
		size string
		dst  *[]byte
	}{{"1600", &large}, {"400", &small}} {
		o := filepath.Join(dir, v.size+".webp")
		if b, err := exec.Command("vips", "thumbnail", in, o+"[Q=85,strip]", v.size, "--size", "down").CombinedOutput(); err != nil {
			return nil, nil, 0, 0, fmt.Errorf("vips: %v: %s", err, b)
		}
		if *v.dst, err = os.ReadFile(o); err != nil {
			return nil, nil, 0, 0, err
		}
	}
	return large, small, w, h, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func prefixed(p string, in []string) []string {
	out := []string{}
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, p+s)
		}
	}
	return out
}

// runBackfill completes the evidence on images stored before migration 13:
// it finds each image's original download by its checksum, stores that
// original next to the WebP, and records size, format and pHash. It then
// reports any two products whose images are near-duplicates.
func runBackfill(ctx context.Context, pool *db.Pool, store storage.Store, dir string, dry bool) error {
	bySum := map[string]string{}
	if err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		bySum[hex.EncodeToString(h[:])] = p
		return nil
	}); err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `
		SELECT i.id::text, p.sku, i.content_sha256, i.storage_key FROM product_images i JOIN products p ON p.id = i.product_id
		 WHERE NOT i.is_placeholder AND i.content_sha256 IS NOT NULL AND i.phash IS NULL`)
	if err != nil {
		return err
	}
	type todo struct{ id, sku, sum, key string }
	var list []todo
	for rows.Next() {
		var t todo
		if err := rows.Scan(&t.id, &t.sku, &t.sum, &t.key); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	var done, missing int
	for _, t := range list {
		p, ok := bySum[t.sum]
		if !ok {
			logf("backfill_original_missing", t.sku)
			missing++
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		ct := http.DetectContentType(body)
		ext := imageTypes[ct]
		w, h, err := dims(body)
		if err != nil {
			return fmt.Errorf("%s: %w", t.sku, err)
		}
		ph, err := dhash(body)
		if err != nil {
			return fmt.Errorf("%s: %w", t.sku, err)
		}
		// The original sits beside the WebP: ".../<stem>.webp" -> "<stem>-original.<ext>".
		i := strings.Index(t.key, "/files/")
		if i < 0 || ext == "" {
			logf("backfill_unexpected_key", t.sku, "key", t.key)
			continue
		}
		origKey := strings.TrimSuffix(t.key[i+len("/files/"):], ".webp") + "-original" + ext
		var notes []string
		if min(w, h) < 1000 {
			notes = append(notes, fmt.Sprintf("LOW_RESOLUTION: best available source is %dx%d (not upscaled)", w, h))
		}
		if dry {
			logf("would_backfill", t.sku, "size", fmt.Sprintf("%dx%d", w, h), "phash", ph)
			continue
		}
		if _, err := store.Put(ctx, storage.PutInput{Key: origKey, ContentType: ct, Visibility: storage.Public,
			Body: bytes.NewReader(body), Size: int64(len(body))}); err != nil {
			return err
		}
		origURL, err := store.PublicURL(origKey)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `
			UPDATE product_images SET width = $2, height = $3, format = $4, phash = $5, original_storage_key = $6,
			       qa_notes = CASE WHEN $7 = '' THEN qa_notes ELSE concat_ws('; ', qa_notes, $7) END
			 WHERE id = $1`, t.id, w, h, strings.TrimPrefix(ext, "."), ph, origURL, strings.Join(notes, "; ")); err != nil {
			return err
		}
		done++
	}
	// Near-duplicate sweep across everything now hashed.
	all, err := pool.Query(ctx, `SELECT p.sku, i.phash FROM product_images i JOIN products p ON p.id = i.product_id
		WHERE i.phash IS NOT NULL AND i.verification_status <> 'REJECTED' ORDER BY p.sku`)
	if err != nil {
		return err
	}
	type hp struct {
		sku string
		v   uint64
	}
	var hs []hp
	for all.Next() {
		var sku, h string
		if err := all.Scan(&sku, &h); err != nil {
			all.Close()
			return err
		}
		v, _ := strconv.ParseUint(h, 16, 64)
		hs = append(hs, hp{sku, v})
	}
	all.Close()
	near := 0
	for a := 0; a < len(hs); a++ {
		for b := a + 1; b < len(hs); b++ {
			if hs[a].sku != hs[b].sku && bits.OnesCount64(hs[a].v^hs[b].v) <= 4 {
				near++
				logf("near_duplicate_pair", hs[a].sku, "other", hs[b].sku, "hamming", bits.OnesCount64(hs[a].v^hs[b].v))
			}
		}
	}
	logf("backfill_done", "", "updated", done, "original_missing", missing, "near_duplicate_pairs", near)
	return nil
}

func dims(src []byte) (int, int, error) {
	dir, err := os.MkdirTemp("", "catalog-dims-")
	if err != nil {
		return 0, 0, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, src, 0o600); err != nil {
		return 0, 0, err
	}
	var out [2]int
	for i, f := range []string{"width", "height"} {
		b, err := exec.Command("vipsheader", "-f", f, in).Output()
		if err != nil {
			return 0, 0, fmt.Errorf("not a readable image: %w", err)
		}
		out[i], _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	return out[0], out[1], nil
}

// runLifecycle sets image_research_status for products without a genuine
// image from the unresolved list's reason: products a person must decide on
// become MANUAL_VERIFICATION_REQUIRED, the rest NOT_FOUND; demo/test rows
// are also marked for catalogue cleanup. Products with an image are untouched.
func runLifecycle(ctx context.Context, pool *db.Pool, path string, dry bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if i == 0 || line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 {
			continue
		}
		sku, reason := f[0], f[4]
		status, cleanup := "NOT_FOUND", ""
		switch {
		case strings.HasPrefix(reason, "Brand sold by several"), strings.HasPrefix(reason, "Ambiguous"),
			strings.HasPrefix(reason, "Form conflict"), strings.HasPrefix(reason, "QA rejected"):
			status = "MANUAL_VERIFICATION_REQUIRED"
		case strings.HasPrefix(reason, "Demo/test"):
			cleanup = "Demo/test product with a fictional brand: remove or replace with a real product"
		case strings.HasPrefix(reason, "Inactive"):
			status = "MANUAL_VERIFICATION_REQUIRED"
		}
		counts[status]++
		if dry {
			continue
		}
		if _, err := pool.Exec(ctx, `
			UPDATE product_enrichment e SET image_research_status = $2, image_research_note = $3,
			       catalogue_cleanup_reason = COALESCE(NULLIF($4,''), catalogue_cleanup_reason)
			  FROM products p
			 WHERE p.id = e.product_id AND p.sku = $1
			   AND e.image_research_status NOT IN ('APPROVED','PHARMACIST_REVIEW_PENDING')`, sku, status, reason, cleanup); err != nil {
			return fmt.Errorf("%s: %w", sku, err)
		}
	}
	logf("lifecycle_done", "", "counts", counts)
	return nil
}
