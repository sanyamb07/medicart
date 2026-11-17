// Package prescriptions implements upload and pharmacist review of
// prescription images/PDFs. This is deliberately independent of order
// checkout: FEATURE_RX_SALES_ENABLED gates whether a prescription-required
// item can be bought at all, and that gate is untouched by this package. A
// customer can upload a prescription for pharmacist review regardless, but
// nothing here unblocks a sale.
package prescriptions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sanyam/medicart/services/api/internal/db"
	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/platform/idgen"
	"github.com/sanyam/medicart/services/api/internal/storage"
)

type Status string

const (
	StatusPendingReview Status = "PENDING_REVIEW"
	StatusApproved      Status = "APPROVED"
	StatusRejected      Status = "REJECTED"
	StatusExpired       Status = "EXPIRED"
)

type Prescription struct {
	ID           string     `json:"id"`
	CustomerID   string     `json:"customerId"`
	BusinessID   string     `json:"businessId"`
	OriginalName *string    `json:"originalName"`
	ContentType  string     `json:"contentType"`
	SizeBytes    int64      `json:"sizeBytes"`
	Status       Status     `json:"status"`
	DoctorName   *string    `json:"doctorName"`
	DoctorRegNo  *string    `json:"doctorRegNo"`
	IssuedOn     *time.Time `json:"issuedOn"`
	ValidUntil   *time.Time `json:"validUntil"`
	UploadedAt   time.Time  `json:"uploadedAt"`

	storageKey string
}

type Review struct {
	ID             string    `json:"id"`
	PrescriptionID string    `json:"prescriptionId"`
	ReviewerID     string    `json:"reviewerId"`
	Decision       string    `json:"decision"`
	Notes          *string   `json:"notes"`
	ReviewedAt     time.Time `json:"reviewedAt"`
}

type Service struct {
	pool           *db.Pool
	store          storage.Store
	maxUploadBytes int64
	urlTTL         time.Duration
}

func NewService(pool *db.Pool, store storage.Store, maxUploadBytes int64, urlTTL time.Duration) *Service {
	return &Service{pool: pool, store: store, maxUploadBytes: maxUploadBytes, urlTTL: urlTTL}
}

// Upload validates, stores, and records a prescription for later pharmacist
// review. The file is buffered in memory (bounded by maxUploadBytes, a few MB
// at most) so its real bytes can be sniffed and hashed before anything is
// written to storage — a client can claim any Content-Type, and the sniff
// catches a mismatch.
func (s *Service) Upload(ctx context.Context, businessID, customerID, originalName, declaredContentType string, size int64, body io.Reader) (*Prescription, error) {
	ext, err := storage.ValidatePrescriptionUpload(declaredContentType, size, s.maxUploadBytes)
	if err != nil {
		return nil, httpx.ErrValidation.WithMessage(err.Error())
	}

	data, err := io.ReadAll(io.LimitReader(body, s.maxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > s.maxUploadBytes {
		return nil, httpx.ErrValidation.WithMessage("File exceeds the maximum upload size.")
	}
	if int64(len(data)) != size {
		// The declared Content-Length and the body disagree; trust the body.
		size = int64(len(data))
	}

	sniffWindow := data
	if len(sniffWindow) > 32 {
		sniffWindow = sniffWindow[:32]
	}
	sniffed := storage.SniffContentType(sniffWindow)
	normalisedDeclared := declaredContentType
	if sniffed == "" || !contentTypeFamily(sniffed, declaredContentType) {
		return nil, httpx.ErrValidation.WithMessage(
			"The file's actual format doesn't match its declared type. Upload a JPG, PNG, HEIC, WEBP, or PDF.")
	}

	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])

	key := storage.BuildKey(storage.PrefixPrescriptions, customerID, idgen.Token(), ext)
	if _, err := s.store.Put(ctx, storage.PutInput{
		Key: key, ContentType: normalisedDeclared, Visibility: storage.Private,
		Body: bytes.NewReader(data), Size: size,
	}); err != nil {
		return nil, fmt.Errorf("store prescription: %w", err)
	}

	var p Prescription
	var name *string
	if originalName != "" {
		name = &originalName
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO prescriptions (customer_id, business_id, storage_key, original_name, content_type, size_bytes, sha256, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'PENDING_REVIEW')
		RETURNING id, customer_id, business_id, original_name, content_type, size_bytes, status, doctor_name, doctor_reg_no, issued_on, valid_until, uploaded_at
	`, customerID, businessID, key, name, normalisedDeclared, size, checksum).Scan(
		&p.ID, &p.CustomerID, &p.BusinessID, &p.OriginalName, &p.ContentType, &p.SizeBytes, &p.Status,
		&p.DoctorName, &p.DoctorRegNo, &p.IssuedOn, &p.ValidUntil, &p.UploadedAt,
	)
	if err != nil {
		_ = s.store.Delete(ctx, key)
		return nil, fmt.Errorf("record prescription: %w", err)
	}
	p.storageKey = key
	return &p, nil
}

// ListForCustomer returns a customer's own prescriptions, newest first,
// across every business they've uploaded one to.
func (s *Service) ListForCustomer(ctx context.Context, customerID string) ([]Prescription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, customer_id, business_id, original_name, content_type, size_bytes, status, doctor_name, doctor_reg_no, issued_on, valid_until, uploaded_at
		FROM prescriptions WHERE customer_id = $1 ORDER BY uploaded_at DESC
	`, customerID)
	if err != nil {
		return nil, fmt.Errorf("list prescriptions: %w", err)
	}
	defer rows.Close()
	return scanList(rows)
}

// ListPendingReview returns every prescription awaiting a pharmacist's
// decision at one business, oldest first (first in, first reviewed).
func (s *Service) ListPendingReview(ctx context.Context, businessID string) ([]Prescription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, customer_id, business_id, original_name, content_type, size_bytes, status, doctor_name, doctor_reg_no, issued_on, valid_until, uploaded_at
		FROM prescriptions WHERE business_id = $1 AND status = 'PENDING_REVIEW' ORDER BY uploaded_at ASC
	`, businessID)
	if err != nil {
		return nil, fmt.Errorf("list pending prescriptions: %w", err)
	}
	defer rows.Close()
	return scanList(rows)
}

func scanList(rows pgx.Rows) ([]Prescription, error) {
	out := []Prescription{}
	for rows.Next() {
		var p Prescription
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.BusinessID, &p.OriginalName, &p.ContentType, &p.SizeBytes, &p.Status,
			&p.DoctorName, &p.DoctorRegNo, &p.IssuedOn, &p.ValidUntil, &p.UploadedAt); err != nil {
			return nil, fmt.Errorf("scan prescription: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

var ErrForbidden = errors.New("prescriptions: not authorised to view this file")

// SignedURL authorises and mints a short-lived URL for one prescription. The
// requester must be the uploading customer, or a staff member (a pharmacist
// or admin) at the business it was uploaded to — never staff of a different
// business, even one holding the right role elsewhere.
func (s *Service) SignedURL(ctx context.Context, prescriptionID string, requester httpx.Principal, requesterBusinessID string) (string, error) {
	var key, ownerCustomerID, ownerBusinessID string
	err := s.pool.QueryRow(ctx, `SELECT storage_key, customer_id, business_id::text FROM prescriptions WHERE id = $1`, prescriptionID).
		Scan(&key, &ownerCustomerID, &ownerBusinessID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", httpx.ErrNotFound
		}
		return "", fmt.Errorf("look up prescription: %w", err)
	}

	isOwner := requester.CustomerID != "" && requester.CustomerID == ownerCustomerID
	isStaffHere := requesterBusinessID != "" && requesterBusinessID == ownerBusinessID &&
		(requester.HasRole(httpx.RolePharmacist) || requester.HasRole(httpx.RoleAdmin))
	if !isOwner && !isStaffHere {
		return "", ErrForbidden
	}

	return s.store.SignedURL(ctx, key, s.urlTTL)
}

// Review records a pharmacist's decision, scoped to businessID so a
// pharmacist can only review prescriptions uploaded to their own business.
// Only the decision + note are customer-visible; the full review history is
// kept for audit regardless.
func (s *Service) Review(ctx context.Context, businessID, prescriptionID, reviewerStaffID, decision string, notes *string) (*Prescription, error) {
	if decision != "APPROVED" && decision != "REJECTED" && decision != "MORE_INFO_NEEDED" {
		return nil, httpx.ErrValidation.WithMessage("decision must be APPROVED, REJECTED, or MORE_INFO_NEEDED")
	}

	var p Prescription
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		var owns bool
		if err := q.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM prescriptions WHERE id = $1 AND business_id = $2)`,
			prescriptionID, businessID).Scan(&owns); err != nil {
			return fmt.Errorf("check prescription ownership: %w", err)
		}
		if !owns {
			return httpx.ErrNotFound
		}

		if _, err := q.Exec(ctx, `
			INSERT INTO prescription_reviews (prescription_id, reviewer_id, decision, notes)
			VALUES ($1, $2, $3, $4)
		`, prescriptionID, reviewerStaffID, decision, notes); err != nil {
			return fmt.Errorf("record review: %w", err)
		}

		newStatus := "REJECTED"
		if decision == "APPROVED" {
			newStatus = "APPROVED"
		} else if decision == "MORE_INFO_NEEDED" {
			newStatus = "PENDING_REVIEW" // stays pending; the note explains what's missing
		}

		return q.QueryRow(ctx, `
			UPDATE prescriptions SET status = $2 WHERE id = $1
			RETURNING id, customer_id, business_id, original_name, content_type, size_bytes, status, doctor_name, doctor_reg_no, issued_on, valid_until, uploaded_at
		`, prescriptionID, newStatus).Scan(
			&p.ID, &p.CustomerID, &p.BusinessID, &p.OriginalName, &p.ContentType, &p.SizeBytes, &p.Status,
			&p.DoctorName, &p.DoctorRegNo, &p.IssuedOn, &p.ValidUntil, &p.UploadedAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// contentTypeFamily lets a declared type with parameters/synonyms still match
// a sniffed base type (both are already normalised by the caller chain for
// the common cases); kept simple and exact otherwise.
func contentTypeFamily(sniffed, declared string) bool {
	return sniffed == normaliseLoose(declared)
}

func normaliseLoose(ct string) string {
	for i, c := range ct {
		if c == ';' {
			return ct[:i]
		}
	}
	return ct
}
