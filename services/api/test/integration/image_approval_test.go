package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/enrichment"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

// imageRouter mounts the real enrichment handlers with a signed-in
// pharmacist, as the platform-admin group does in production.
func imageRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (mobile, is_active) VALUES ($1, TRUE) RETURNING id::text`, "9"+itoa(int(randInt()))).Scan(&userID); err != nil {
		t.Fatalf("insert pharmacist: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := enrichment.NewHandlers(enrichment.NewService(pool, ""))
	h.Register(r.Group("/public"))
	admin := r.Group("/admin", func(c *gin.Context) {
		c.Set(httpx.CtxUserID, userID)
		c.Set(httpx.CtxUserRoles, []string{httpx.RolePlatformSuperAdmin})
	})
	h.RegisterPlatformAdmin(admin)
	return r, userID
}

var randSeq int64 = 100000000

func randInt() int64 { randSeq++; return randSeq }

// productWithCandidate creates an active product with the placeholder plus
// one QA-passed PENDING manufacturer image, as catalog-sync stores it.
func productWithCandidate(t *testing.T, qaNotes string) (id, slug string) {
	t.Helper()
	ctx := context.Background()
	slug = "img-flow-" + randomSuffix()
	id = insertProduct(t, "Image Flow 10mg", "T-IMG-"+randomSuffix(), slug, 1000, 1200, true)
	if _, err := pool.Exec(ctx, `INSERT INTO product_enrichment (product_id, image_research_status, status) VALUES ($1, 'PHARMACIST_REVIEW_PENDING', 'completed')`, id); err != nil {
		t.Fatalf("enrichment row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO product_images (product_id, storage_key, alt_text, is_placeholder, verification_status, source_type)
		VALUES ($1, '/img/forms/tablet.svg', 'x', TRUE, 'PENDING', 'illustration'),
		       ($1, 'http://cdn.test/products/x.webp', 'x', FALSE, 'PENDING', 'manufacturer_catalogue')`, id); err != nil {
		t.Fatalf("images: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE product_images SET qa_status = 'QA_PASSED', qa_notes = NULLIF($2,''), width = 1600, height = 1600
		WHERE product_id = $1 AND NOT is_placeholder`, id, qaNotes); err != nil {
		t.Fatalf("qa: %v", err)
	}
	return id, slug
}

func customerImage(t *testing.T, slug string) string {
	t.Helper()
	p, err := catalogRepo.GetBySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if p.ImageURL == nil {
		return ""
	}
	return *p.ImageURL
}

func call(t *testing.T, r *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPendingImageIsHiddenUntilPharmacistApproves(t *testing.T) {
	r, _ := imageRouter(t)
	id, slug := productWithCandidate(t, "")

	// Customer-facing catalogue and the public info endpoint: placeholder only.
	if got := customerImage(t, slug); got != "/img/forms/tablet.svg" {
		t.Fatalf("pending image leaked to customers: %q", got)
	}
	_, info := call(t, r, http.MethodGet, "/public/products/"+slug+"/info", nil)
	d := info["data"].(map[string]any)
	if d["imageUrl"] != "/img/forms/tablet.svg" || d["imageQaStatus"] != nil || d["imageVerification"] != nil {
		t.Fatalf("public info exposes a pending image or review data: %v", d)
	}

	// The admin view sees the candidate with its evidence.
	_, list := call(t, r, http.MethodGet, "/admin/enrichment/products?q=Image%20Flow&pageSize=100", nil)
	found := false
	for _, it := range list["data"].([]any) {
		m := it.(map[string]any)
		if m["productId"] == id {
			found = m["imageVerification"] == "PENDING" && m["imageQaStatus"] == "QA_PASSED" && m["imageWidth"] == float64(1600)
		}
	}
	if !found {
		t.Fatal("admin list does not show the pending candidate with its QA evidence")
	}

	// Approval is the only thing that makes it visible.
	if code, body := call(t, r, http.MethodPatch, "/admin/enrichment/products/"+id, map[string]any{"imageDecision": "APPROVE"}); code != 200 {
		t.Fatalf("approve: %d %v", code, body)
	}
	if got := customerImage(t, slug); got != "http://cdn.test/products/x.webp" {
		t.Fatalf("approved image not shown to customers: %q", got)
	}
	var research string
	_ = pool.QueryRow(context.Background(), `SELECT image_research_status FROM product_enrichment WHERE product_id = $1`, id).Scan(&research)
	if research != "APPROVED" {
		t.Fatalf("research status after approval = %s", research)
	}
}

func TestRejectionCategoriesKeepImageHidden(t *testing.T) {
	r, _ := imageRouter(t)
	for decision, wantResearch := range map[string]string{
		"REJECTED": "REJECTED", "REQUEST_BETTER_IMAGE": "RESEARCH_PENDING", "WRONG_MANUFACTURER": "MANUAL_VERIFICATION_REQUIRED",
		"WRONG_MEDICINE": "MANUAL_VERIFICATION_REQUIRED", "MANUAL_VERIFICATION": "MANUAL_VERIFICATION_REQUIRED",
	} {
		id, slug := productWithCandidate(t, "")
		if code, body := call(t, r, http.MethodPatch, "/admin/enrichment/products/"+id, map[string]any{"imageDecision": decision, "imageNote": "test"}); code != 200 {
			t.Fatalf("%s: %d %v", decision, code, body)
		}
		if got := customerImage(t, slug); got != "/img/forms/tablet.svg" {
			t.Errorf("%s: customer sees %q, want placeholder", decision, got)
		}
		var research string
		_ = pool.QueryRow(context.Background(), `SELECT image_research_status FROM product_enrichment WHERE product_id = $1`, id).Scan(&research)
		if research != wantResearch {
			t.Errorf("%s: research status %s, want %s", decision, research, wantResearch)
		}
	}
	if code, _ := call(t, r, http.MethodPatch, "/admin/enrichment/products/00000000-0000-0000-0000-000000000000",
		map[string]any{"imageDecision": "APPROVE"}); code == 200 {
		t.Error("approving a product with no pending image must fail")
	}
}

func TestBulkApproveOnlyApprovesIndividuallySafeImages(t *testing.T) {
	r, _ := imageRouter(t)
	ctx := context.Background()
	safe, safeSlug := productWithCandidate(t, "")
	similar, similarSlug := productWithCandidate(t, "SIMILAR_LAYOUT: same carton template as IMP-0001")
	manual, manualSlug := productWithCandidate(t, "")
	if _, err := pool.Exec(ctx, `UPDATE product_images SET qa_status = 'MANUAL_VERIFICATION_REQUIRED' WHERE product_id = $1 AND NOT is_placeholder`, manual); err != nil {
		t.Fatal(err)
	}
	disputed, disputedSlug := productWithCandidate(t, "")
	if _, err := pool.Exec(ctx, `UPDATE product_enrichment SET hold_generated_text = TRUE WHERE product_id = $1`, disputed); err != nil {
		t.Fatal(err)
	}
	two, twoSlug := productWithCandidate(t, "")
	if _, err := pool.Exec(ctx, `INSERT INTO product_images (product_id, storage_key, alt_text, is_placeholder, verification_status, qa_status)
		VALUES ($1, 'http://cdn.test/products/y.webp', 'y', FALSE, 'PENDING', 'QA_PASSED')`, two); err != nil {
		t.Fatal(err)
	}

	code, body := call(t, r, http.MethodPost, "/admin/enrichment/images/bulk-approve",
		map[string]any{"productIds": []string{safe, similar, manual, disputed, two}})
	if code != 200 {
		t.Fatalf("bulk: %d %v", code, body)
	}
	d := body["data"].(map[string]any)
	if a := d["approved"].([]any); len(a) != 1 || a[0] != safe {
		t.Fatalf("approved = %v, want only the single safe product", a)
	}
	if s := d["skipped"].(map[string]any); len(s) != 4 {
		t.Fatalf("skipped = %v, want 4", s)
	}
	if customerImage(t, safeSlug) != "http://cdn.test/products/x.webp" {
		t.Error("safe product not visible after bulk approval")
	}
	for _, s := range []string{similarSlug, manualSlug, disputedSlug, twoSlug} {
		if got := customerImage(t, s); got != "/img/forms/tablet.svg" {
			t.Errorf("unsafe product %s became visible via bulk approval: %q", s, got)
		}
	}
}
