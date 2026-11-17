package enrichment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
)

type Handlers struct{ svc *Service }

func NewHandlers(svc *Service) *Handlers { return &Handlers{svc: svc} }

// Register mounts the public, read-only medicine information endpoint.
func (h *Handlers) Register(r gin.IRouter) {
	r.GET("/products/:slug/info", h.publicInfo)
}

// RegisterPlatformAdmin mounts the catalogue-wide management routes; mount
// under the platform-admin group.
func (h *Handlers) RegisterPlatformAdmin(r gin.IRouter) {
	r.GET("/enrichment/summary", h.summary)
	r.GET("/enrichment/products", h.list)
	r.PATCH("/enrichment/products/:id", h.update)
	r.GET("/enrichment/products/:id/images", h.images)
	r.POST("/enrichment/images/bulk-approve", h.bulkApprove)
	r.POST("/enrichment/run", h.run)
}

// Info is the enriched information for one product. Every text field is
// nullable: null means "not available from a verified source".
type Info struct {
	ProductID           string  `json:"productId"`
	Name                string  `json:"name"`
	GenericName         *string `json:"genericName"`
	Status              string  `json:"status"`
	TherapeuticCategory *string `json:"therapeuticCategory"`
	ShortDescription    *string `json:"shortDescription"`
	Description         *string `json:"description"`
	Uses                *string `json:"uses"`
	MechanismOfAction   *string `json:"mechanismOfAction"`
	Precautions         *string `json:"precautions"`
	SideEffects         *string `json:"sideEffects"`
	Warnings            *string `json:"warnings"`
	Contraindications   *string `json:"contraindications"`
	StorageInfo         *string `json:"storageInfo"`
	Manufacturer        *string `json:"manufacturer"`
	SeoTitle            *string `json:"seoTitle"`
	MetaDescription     *string `json:"metaDescription"`
	InfoSource          *string `json:"infoSource"`
	InfoSourceURL       *string `json:"infoSourceUrl"`
	ImageURL            *string `json:"imageUrl"`
	ImageStatus         string  `json:"imageStatus"`
	ImageSource         *string `json:"imageSource"`
	// Provenance of the image shown above (admin view: the newest candidate;
	// public view: only a VERIFIED photo or the placeholder).
	ImageVerification  *string  `json:"imageVerification"`
	ImageSourceURL     *string  `json:"imageSourceUrl"`
	ImageSourcePageURL *string  `json:"imageSourcePageUrl"`
	ImagePackText      *string  `json:"imagePackText,omitempty"`
	VerificationStatus string   `json:"verificationStatus"`
	ReviewReasons      []string `json:"reviewReasons"`
	RegulatoryGroup    *string  `json:"regulatoryGroup,omitempty"`
	ManufacturerSource *string  `json:"manufacturerSourceUrl"`
	OfficialProductURL *string  `json:"officialProductUrl"`
	Strength           *string  `json:"strength"`
	DosageForm         *string  `json:"dosageForm"`
	// Marketer = company whose own catalogue lists the brand; Manufacturer
	// name = the maker, only when the pack or an official document names it.
	Marketer             *string    `json:"marketer"`
	ManufacturerName     *string    `json:"manufacturerName"`
	ManufacturerEvidence *string    `json:"manufacturerEvidence,omitempty"`
	ImageResearchStatus  string     `json:"imageResearchStatus"`
	ImageResearchNote    *string    `json:"imageResearchNote,omitempty"`
	CatalogueCleanup     *string    `json:"catalogueCleanup,omitempty"`
	ImageID              *string    `json:"imageId,omitempty"`
	ImageWidth           *int       `json:"imageWidth,omitempty"`
	ImageHeight          *int       `json:"imageHeight,omitempty"`
	ImageFormat          *string    `json:"imageFormat,omitempty"`
	ImageQAStatus        *string    `json:"imageQaStatus,omitempty"`
	ImageQANotes         *string    `json:"imageQaNotes,omitempty"`
	ImageSourceType      *string    `json:"imageSourceType,omitempty"`
	ImageOriginalURL     *string    `json:"imageOriginalUrl,omitempty"`
	ImageReviewAction    *string    `json:"imageReviewAction,omitempty"`
	MissingFields        []string   `json:"missingFields"`
	Unmatched            []string   `json:"unmatchedIngredients"`
	LastVerifiedAt       *time.Time `json:"lastVerifiedAt"`
	LastError            *string    `json:"lastError,omitempty"`
}

const infoColumns = `
	SELECT p.id::text, p.name, p.generic_name, e.status, e.therapeutic_category, e.short_description, p.description,
	       p.key_uses, e.mechanism_of_action, e.precautions, e.side_effects, e.warnings, e.contraindications,
	       p.storage_info, p.manufacturer_info, e.seo_title, e.meta_description, e.info_source, e.info_source_url,
	       img.storage_key, e.image_status, img.source_name, e.missing_fields, e.unmatched_ingredients,
	       e.last_verified_at, e.last_error,
	       img.verification_status, img.source_url, img.source_page_url, img.visible_pack_text,
	       e.verification_status, e.review_reasons, e.regulatory_group, e.manufacturer_source_url, e.official_product_url,
	       p.strength, p.dosage_form, e.marketer_name, e.manufacturer_name, e.manufacturer_evidence,
	       e.image_research_status, e.image_research_note, e.catalogue_cleanup_reason,
	       img.id::text, img.width, img.height, img.format, img.qa_status, img.qa_notes, img.source_type,
	       img.original_storage_key, img.review_action
	  FROM products p
	  JOIN product_enrichment e ON e.product_id = p.id`

// infoSelect is the admin view: it shows the image under review (a real
// candidate ahead of the placeholder; rejected images are never picked).
const infoSelect = infoColumns + `
	  LEFT JOIN LATERAL (SELECT id, storage_key, source_name, verification_status, source_url, source_page_url, visible_pack_text,
	                            width, height, format, qa_status, qa_notes, source_type, original_storage_key, review_action
	                       FROM product_images pi
	                      WHERE pi.product_id = p.id AND pi.verification_status <> 'REJECTED'
	                      ORDER BY pi.is_placeholder, pi.created_at DESC, pi.sort_order LIMIT 1) img ON TRUE`

// publicInfoSelect only ever sees images a customer may see.
const publicInfoSelect = infoColumns + `
	  LEFT JOIN LATERAL (SELECT pi.id, pi.storage_key, pi.source_name, pi.verification_status, pi.source_url, pi.source_page_url,
	                            NULL::text AS visible_pack_text, pi.width, pi.height, pi.format, NULL::text AS qa_status,
	                            NULL::text AS qa_notes, pi.source_type, NULL::text AS original_storage_key, NULL::text AS review_action
	                       FROM product_images pi JOIN customer_product_images c ON c.id = pi.id
	                      WHERE pi.product_id = p.id ORDER BY c.display_order LIMIT 1) img ON TRUE`

type scanner interface{ Scan(dest ...any) error }

func scanInfo(s scanner) (*Info, error) {
	var i Info
	err := s.Scan(&i.ProductID, &i.Name, &i.GenericName, &i.Status, &i.TherapeuticCategory, &i.ShortDescription,
		&i.Description, &i.Uses, &i.MechanismOfAction, &i.Precautions, &i.SideEffects, &i.Warnings, &i.Contraindications,
		&i.StorageInfo, &i.Manufacturer, &i.SeoTitle, &i.MetaDescription, &i.InfoSource, &i.InfoSourceURL,
		&i.ImageURL, &i.ImageStatus, &i.ImageSource, &i.MissingFields, &i.Unmatched, &i.LastVerifiedAt, &i.LastError,
		&i.ImageVerification, &i.ImageSourceURL, &i.ImageSourcePageURL, &i.ImagePackText,
		&i.VerificationStatus, &i.ReviewReasons, &i.RegulatoryGroup, &i.ManufacturerSource, &i.OfficialProductURL,
		&i.Strength, &i.DosageForm, &i.Marketer, &i.ManufacturerName, &i.ManufacturerEvidence,
		&i.ImageResearchStatus, &i.ImageResearchNote, &i.CatalogueCleanup,
		&i.ImageID, &i.ImageWidth, &i.ImageHeight, &i.ImageFormat, &i.ImageQAStatus, &i.ImageQANotes, &i.ImageSourceType,
		&i.ImageOriginalURL, &i.ImageReviewAction)
	return &i, err
}

func (h *Handlers) publicInfo(c *gin.Context) {
	i, err := scanInfo(h.svc.pool.QueryRow(c.Request.Context(),
		publicInfoSelect+` WHERE p.slug = $1 AND p.is_active`, c.Param("slug")))
	if db.IsNoRows(err) {
		httpx.Fail(c, httpx.ErrNotFound)
		return
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	// Review internals are for staff only.
	i.LastError, i.RegulatoryGroup, i.ManufacturerEvidence, i.ImageResearchNote, i.CatalogueCleanup = nil, nil, nil, nil, nil
	i.ImageID, i.ImageQAStatus, i.ImageQANotes, i.ImageOriginalURL, i.ImageReviewAction = nil, nil, nil, nil, nil
	i.ReviewReasons, i.MissingFields, i.Unmatched = []string{}, []string{}, []string{}
	if i.ImageVerification == nil || *i.ImageVerification != "VERIFIED" {
		// Only the placeholder can reach here; say so plainly, and do not
		// expose review details of a photo customers are not shown.
		i.ImageStatus = "placeholder"
		i.ImageVerification, i.ImageSourceURL, i.ImageSourcePageURL = nil, nil, nil
	}
	httpx.OK(c, i)
}

func (h *Handlers) summary(c *gin.Context) {
	ctx := c.Request.Context()
	out := gin.H{}
	byStatus := map[string]int{}
	rows, err := h.svc.pool.Query(ctx, `SELECT status, count(*) FROM product_enrichment GROUP BY 1`)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	for rows.Next() {
		var s string
		var n int
		_ = rows.Scan(&s, &n)
		byStatus[s] = n
	}
	rows.Close()
	var total, withDesc, verifiedImg, placeholderImg, noImg, humanVerified int
	if err := h.svc.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE p.description IS NOT NULL),
		       count(*) FILTER (WHERE e.image_status = 'verified'),
		       count(*) FILTER (WHERE e.image_status = 'placeholder' AND EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = p.id)),
		       count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = p.id)),
		       count(*) FILTER (WHERE e.last_verified_at IS NOT NULL)
		  FROM products p JOIN product_enrichment e ON e.product_id = p.id`).
		Scan(&total, &withDesc, &verifiedImg, &placeholderImg, &noImg, &humanVerified); err != nil {
		httpx.Fail(c, err)
		return
	}
	out["total"], out["byStatus"], out["withDescription"] = total, byStatus, withDesc
	out["verifiedImages"], out["placeholderImages"], out["noImage"], out["humanVerified"] = verifiedImg, placeholderImg, noImg, humanVerified
	byVerification := map[string]int{}
	vrows, err := h.svc.pool.Query(ctx, `SELECT verification_status, count(*) FROM product_enrichment GROUP BY 1`)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	for vrows.Next() {
		var s string
		var n int
		_ = vrows.Scan(&s, &n)
		byVerification[s] = n
	}
	vrows.Close()
	var pendingImg, rejectedImg int
	if err := h.svc.pool.QueryRow(ctx, `
		SELECT count(DISTINCT product_id) FILTER (WHERE verification_status = 'PENDING' AND NOT is_placeholder),
		       count(*) FILTER (WHERE verification_status = 'REJECTED')
		  FROM product_images`).Scan(&pendingImg, &rejectedImg); err != nil {
		httpx.Fail(c, err)
		return
	}
	out["byVerification"], out["pendingImages"], out["rejectedImages"] = byVerification, pendingImg, rejectedImg
	httpx.OK(c, out)
}

func (h *Handlers) list(c *gin.Context) {
	page := httpx.QueryInt(c, "page", 1, 1, 100000)
	size := httpx.QueryInt(c, "pageSize", 25, 1, 100)
	where, args := "TRUE", []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if v := c.Query("status"); v != "" {
		add("e.status = $%d", v)
	}
	if v := c.Query("image"); v != "" {
		add("e.image_status = $%d", v)
	}
	if v := c.Query("verification"); v != "" {
		add("e.verification_status = $%d", v)
	}
	if v := c.Query("research"); v != "" {
		add("e.image_research_status = $%d", v)
	}
	if v := c.Query("qa"); v != "" {
		add("EXISTS (SELECT 1 FROM product_images q WHERE q.product_id = p.id AND NOT q.is_placeholder AND q.verification_status = 'PENDING' AND q.qa_status = $%d)", v)
	}
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		add("(p.name ILIKE $%[1]d OR p.generic_name ILIKE $%[1]d)", "%"+v+"%")
	}
	if c.Query("unverified") == "1" {
		where += " AND e.last_verified_at IS NULL"
	}
	ctx := c.Request.Context()
	var total int
	if err := h.svc.pool.QueryRow(ctx, `SELECT count(*) FROM products p JOIN product_enrichment e ON e.product_id = p.id WHERE `+where, args...).Scan(&total); err != nil {
		httpx.Fail(c, err)
		return
	}
	args = append(args, size, (page-1)*size)
	rows, err := h.svc.pool.Query(ctx, infoSelect+` WHERE `+where+
		fmt.Sprintf(` ORDER BY p.name LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer rows.Close()
	items := []*Info{}
	for rows.Next() {
		i, err := scanInfo(rows)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		items = append(items, i)
	}
	httpx.List(c, items, httpx.NewMeta(page, size, total))
}

// updateReq: a nil field is left unchanged; an empty string clears it.
type updateReq struct {
	GenericName       *string `json:"genericName"`
	Description       *string `json:"description"`
	Uses              *string `json:"uses"`
	MechanismOfAction *string `json:"mechanismOfAction"`
	Precautions       *string `json:"precautions"`
	SideEffects       *string `json:"sideEffects"`
	Warnings          *string `json:"warnings"`
	Contraindications *string `json:"contraindications"`
	StorageInfo       *string `json:"storageInfo"`
	Manufacturer      *string `json:"manufacturer"`
	ImageURL          *string `json:"imageUrl"`
	ImageSource       *string `json:"imageSource"`
	ImageSourceURL    *string `json:"imageSourceUrl"`
	ImageVerified     *bool   `json:"imageVerified"`
	// ImageRejected marks the image under review as wrong (wrong brand,
	// strength, form or pack); the product falls back to the placeholder.
	ImageRejected *bool   `json:"imageRejected"`
	ImageNote     *string `json:"imageNote"`
	// ImageDecision is the pharmacist's decision on one candidate (ImageID,
	// or the newest pending one): APPROVE, REJECTED, REQUEST_BETTER_IMAGE,
	// WRONG_MANUFACTURER, WRONG_MEDICINE or MANUAL_VERIFICATION.
	ImageDecision *string `json:"imageDecision"`
	ImageID       *string `json:"imageId"`
	Verified      *bool   `json:"verified"`
	Status        *string `json:"status"`
}

func (h *Handlers) update(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	var req updateReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	id := c.Param("id")
	ctx := c.Request.Context()
	err := h.svc.pool.InTx(ctx, func(q db.Querier) error {
		set := func(table, col, key string, v *string) error {
			if v == nil {
				return nil
			}
			if _, err := q.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = NULLIF($2,'') WHERE %s = $1`, table, col, key), id, strings.TrimSpace(*v)); err != nil {
				return err
			}
			// A person's edit: the pipeline must never overwrite it on a re-run.
			_, err := q.Exec(ctx, `UPDATE product_enrichment SET edited_at = now() WHERE product_id = $1`, id)
			return err
		}
		for _, f := range []struct {
			t, c, k string
			v       *string
		}{
			{"products", "generic_name", "id", req.GenericName}, {"products", "description", "id", req.Description},
			{"products", "key_uses", "id", req.Uses}, {"products", "storage_info", "id", req.StorageInfo},
			{"products", "manufacturer_info", "id", req.Manufacturer},
			{"product_enrichment", "mechanism_of_action", "product_id", req.MechanismOfAction},
			{"product_enrichment", "precautions", "product_id", req.Precautions},
			{"product_enrichment", "side_effects", "product_id", req.SideEffects},
			{"product_enrichment", "warnings", "product_id", req.Warnings},
			{"product_enrichment", "contraindications", "product_id", req.Contraindications},
		} {
			if err := set(f.t, f.c, f.k, f.v); err != nil {
				return err
			}
		}
		if req.ImageURL != nil && strings.TrimSpace(*req.ImageURL) != "" {
			src := ""
			if req.ImageSource != nil {
				src = strings.TrimSpace(*req.ImageSource)
			}
			srcURL := ""
			if req.ImageSourceURL != nil {
				srcURL = strings.TrimSpace(*req.ImageSourceURL)
			}
			// The previous candidate is superseded, not deleted: its provenance
			// stays on record. The placeholder row is kept as the fallback.
			if _, err := q.Exec(ctx, `
				UPDATE product_images SET verification_status = 'REJECTED', verification_notes = 'Superseded by a newer image'
				 WHERE product_id = $1 AND NOT is_placeholder AND verification_status <> 'REJECTED'`, id); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO product_images (product_id, storage_key, alt_text, sort_order, source_name, source_url, is_placeholder, verification_status)
				SELECT id, $2, name, 0, NULLIF($3,''), NULLIF($4,''), FALSE, 'PENDING' FROM products WHERE id = $1`,
				id, strings.TrimSpace(*req.ImageURL), src, srcURL); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `UPDATE product_enrichment SET image_status = 'needs_review' WHERE product_id = $1`, id); err != nil {
				return err
			}
		}
		if req.ImageDecision != nil {
			if err := decideImage(ctx, q, id, p.UserID, *req.ImageDecision, req.ImageID, note(req.ImageNote)); err != nil {
				return err
			}
		}
		if req.ImageVerified != nil && *req.ImageVerified {
			tag, err := q.Exec(ctx, `
				UPDATE product_enrichment e SET image_status = 'verified'
				 WHERE product_id = $1 AND EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = e.product_id AND NOT i.is_placeholder)`, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.ErrValidation.WithMessage("A placeholder illustration cannot be marked as a verified product image. Add the real image first.")
			}
			if _, err := q.Exec(ctx, `
				UPDATE product_images SET verification_status = 'VERIFIED', verified_at = now(), verified_by = $2,
				       verification_notes = COALESCE(NULLIF($3,''), verification_notes)
				 WHERE product_id = $1 AND NOT is_placeholder AND verification_status = 'PENDING'`, id, p.UserID, note(req.ImageNote)); err != nil {
				return err
			}
		}
		if req.ImageRejected != nil && *req.ImageRejected {
			if _, err := q.Exec(ctx, `
				UPDATE product_images SET verification_status = 'REJECTED', verified_at = now(), verified_by = $2,
				       verification_notes = COALESCE(NULLIF($3,''), 'Rejected by reviewer')
				 WHERE product_id = $1 AND NOT is_placeholder AND verification_status <> 'REJECTED'`, id, p.UserID, note(req.ImageNote)); err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `UPDATE product_enrichment SET image_status = 'placeholder' WHERE product_id = $1`, id); err != nil {
				return err
			}
		}
		if req.Verified != nil {
			if *req.Verified {
				_, err := q.Exec(ctx, `UPDATE product_enrichment SET last_verified_at = now(), verified_by = $2, status = 'completed' WHERE product_id = $1`, id, p.UserID)
				if err != nil {
					return err
				}
			} else if _, err := q.Exec(ctx, `UPDATE product_enrichment SET last_verified_at = NULL, verified_by = NULL WHERE product_id = $1`, id); err != nil {
				return err
			}
		}
		if req.Status != nil {
			if _, err := q.Exec(ctx, `UPDATE product_enrichment SET status = $2 WHERE product_id = $1`, id, *req.Status); err != nil {
				return httpx.ErrValidation.WithMessage("Unknown status.")
			}
		}
		_, err := q.Exec(ctx, RecomputeVerificationSQL, id)
		return err
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	i, err := scanInfo(h.svc.pool.QueryRow(ctx, infoSelect+` WHERE p.id = $1`, id))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, i)
}

type runReq struct {
	Limit int `json:"limit"`
}

// run processes one small batch on request. The full catalogue is run from
// the command line (cmd/enrich); this is for topping up from the admin screen.
func (h *Handlers) run(c *gin.Context) {
	var req runReq
	_ = c.ShouldBindJSON(&req)
	if req.Limit < 1 || req.Limit > 40 {
		req.Limit = 20
	}
	res, err := h.svc.Run(c.Request.Context(), req.Limit, 10, nil)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, res)
}

func note(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

// imageResearchAfter maps a pharmacist decision to the product's image
// research status: a wrong or poor image sends the product back to research.
var imageResearchAfter = map[string]string{
	"APPROVE": "APPROVED", "REJECTED": "REJECTED", "REQUEST_BETTER_IMAGE": "RESEARCH_PENDING",
	"WRONG_MANUFACTURER": "MANUAL_VERIFICATION_REQUIRED", "WRONG_MEDICINE": "MANUAL_VERIFICATION_REQUIRED",
	"MANUAL_VERIFICATION": "MANUAL_VERIFICATION_REQUIRED",
}

// decideImage records one pharmacist decision on one pending candidate.
// Only APPROVE makes an image visible to customers; every other decision
// keeps it hidden and records why, so research can act on it.
func decideImage(ctx context.Context, q db.Querier, productID, userID, decision string, imageID *string, noteText string) error {
	research, ok := imageResearchAfter[decision]
	if !ok {
		return httpx.ErrValidation.WithMessage("Unknown image decision.")
	}
	target := `(SELECT id FROM product_images WHERE product_id = $1 AND NOT is_placeholder AND verification_status = 'PENDING'
	            ORDER BY created_at DESC LIMIT 1)`
	args := []any{productID, userID, noteText}
	if imageID != nil && *imageID != "" {
		target = `(SELECT id FROM product_images WHERE product_id = $1 AND id = $4::uuid AND NOT is_placeholder AND verification_status = 'PENDING')`
		args = append(args, *imageID)
	}
	var sql string
	switch decision {
	case "APPROVE":
		sql = `UPDATE product_images SET verification_status = 'VERIFIED', verified_at = now(), verified_by = $2, reviewed_at = now(),
		       review_action = NULL, verification_notes = COALESCE(NULLIF($3,''), verification_notes) WHERE id = ` + target
	case "MANUAL_VERIFICATION":
		sql = `UPDATE product_images SET qa_status = 'MANUAL_VERIFICATION_REQUIRED', review_action = 'MANUAL_VERIFICATION', reviewed_at = now(),
		       verification_notes = COALESCE(NULLIF($3,''), verification_notes), verified_by = $2 WHERE id = ` + target
	default:
		sql = `UPDATE product_images SET verification_status = 'REJECTED', review_action = '` + decision + `', reviewed_at = now(), verified_at = now(),
		       verified_by = $2, verification_notes = COALESCE(NULLIF($3,''), '` + decision + `') WHERE id = ` + target
	}
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrValidation.WithMessage("There is no pending image to decide on.")
	}
	imageStatus := "needs_review"
	switch decision {
	case "APPROVE":
		imageStatus = "verified"
	case "REJECTED", "REQUEST_BETTER_IMAGE", "WRONG_MANUFACTURER", "WRONG_MEDICINE":
		imageStatus = "placeholder"
	}
	_, err = q.Exec(ctx, `
		UPDATE product_enrichment SET image_research_status = $2, image_status = $3,
		       image_research_note = COALESCE(NULLIF($4,''), image_research_note)
		 WHERE product_id = $1`, productID, research, imageStatus, noteText)
	return err
}

// ImageCandidate is one stored image of a product, for side-by-side review.
type ImageCandidate struct {
	ID                 string     `json:"id"`
	URL                string     `json:"url"`
	OriginalURL        *string    `json:"originalUrl"`
	SourceName         *string    `json:"sourceName"`
	SourceURL          *string    `json:"sourceUrl"`
	SourcePageURL      *string    `json:"sourcePageUrl"`
	SourceType         *string    `json:"sourceType"`
	Width              *int       `json:"width"`
	Height             *int       `json:"height"`
	Format             *string    `json:"format"`
	QAStatus           *string    `json:"qaStatus"`
	QANotes            *string    `json:"qaNotes"`
	PackText           *string    `json:"packText"`
	VerificationStatus string     `json:"verificationStatus"`
	ReviewAction       *string    `json:"reviewAction"`
	Notes              *string    `json:"notes"`
	CreatedAt          time.Time  `json:"createdAt"`
	ReviewedAt         *time.Time `json:"reviewedAt"`
}

func (h *Handlers) images(c *gin.Context) {
	rows, err := h.svc.pool.Query(c.Request.Context(), `
		SELECT id::text, storage_key, original_storage_key, source_name, source_url, source_page_url, source_type, width, height,
		       format, qa_status, qa_notes, visible_pack_text, verification_status, review_action, verification_notes, created_at, reviewed_at
		  FROM product_images WHERE product_id = $1 AND NOT is_placeholder ORDER BY created_at DESC`, c.Param("id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer rows.Close()
	out := []ImageCandidate{}
	for rows.Next() {
		var m ImageCandidate
		if err := rows.Scan(&m.ID, &m.URL, &m.OriginalURL, &m.SourceName, &m.SourceURL, &m.SourcePageURL, &m.SourceType, &m.Width,
			&m.Height, &m.Format, &m.QAStatus, &m.QANotes, &m.PackText, &m.VerificationStatus, &m.ReviewAction, &m.Notes,
			&m.CreatedAt, &m.ReviewedAt); err != nil {
			httpx.Fail(c, err)
			return
		}
		out = append(out, m)
	}
	httpx.OK(c, out)
}

type bulkReq struct {
	ProductIDs []string `json:"productIds"`
}

// bulkSafeSQL is the only definition of "safe to approve in bulk": exactly
// one pending candidate, which passed QA with no manual-verification flag,
// for an active product whose identity is not in question.
const bulkSafeSQL = `
	SELECT CASE
	  WHEN NOT p.is_active THEN 'product inactive'
	  WHEN e.regulatory_group = 'not_approved_for_human_use' THEN 'regulatory hold'
	  WHEN e.hold_generated_text THEN 'listed composition is disputed'
	  WHEN e.image_research_status <> 'PHARMACIST_REVIEW_PENDING' THEN 'research status is ' || e.image_research_status
	  WHEN (SELECT count(*) FROM product_images i WHERE i.product_id = p.id AND NOT i.is_placeholder AND i.verification_status = 'PENDING') <> 1
	       THEN 'not exactly one pending image: compare candidates individually'
	  WHEN NOT EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = p.id AND NOT i.is_placeholder
	                     AND i.verification_status = 'PENDING' AND i.qa_status = 'QA_PASSED') THEN 'image not QA-passed'
	  WHEN EXISTS (SELECT 1 FROM product_images i WHERE i.product_id = p.id AND NOT i.is_placeholder
	                 AND i.verification_status = 'PENDING' AND i.qa_notes LIKE '%SIMILAR_LAYOUT%') THEN 'similar layout to another product: check strength individually'
	  ELSE NULL END
	  FROM products p JOIN product_enrichment e ON e.product_id = p.id WHERE p.id = $1`

// bulkApprove approves only images that individually meet bulkSafeSQL;
// everything else is returned with the reason it was skipped.
func (h *Handlers) bulkApprove(c *gin.Context) {
	pr, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	var req bulkReq
	if err := httpx.Bind(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if len(req.ProductIDs) == 0 || len(req.ProductIDs) > 100 {
		httpx.Fail(c, httpx.ErrValidation.WithMessage("Select between 1 and 100 products."))
		return
	}
	ctx := c.Request.Context()
	approved, skipped := []string{}, map[string]string{}
	for _, id := range req.ProductIDs {
		err := h.svc.pool.InTx(ctx, func(q db.Querier) error {
			var reason *string
			if err := q.QueryRow(ctx, bulkSafeSQL+` FOR UPDATE OF e`, id).Scan(&reason); err != nil {
				if db.IsNoRows(err) {
					s := "unknown product"
					reason = &s
				} else {
					return err
				}
			}
			if reason != nil {
				skipped[id] = *reason
				return nil
			}
			if err := decideImage(ctx, q, id, pr.UserID, "APPROVE", nil, "Bulk-approved: single QA-passed candidate"); err != nil {
				return err
			}
			_, err := q.Exec(ctx, RecomputeVerificationSQL, id)
			approved = append(approved, id)
			return err
		})
		if err != nil {
			skipped[id] = err.Error()
		}
	}
	httpx.OK(c, gin.H{"approved": approved, "skipped": skipped})
}
