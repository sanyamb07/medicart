package integration_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sanyam/medicart/services/api/internal/httpx"
	"github.com/sanyam/medicart/services/api/internal/prescriptions"
	"github.com/sanyam/medicart/services/api/internal/tenancy"
)

func TestPrescriptionUploadRejectsContentTypeMismatch(t *testing.T) {
	ctx := context.Background()
	customerID := insertCustomer(t, "9876500001")

	// Declares PDF, but the bytes are plain text — the sniff must catch this
	// regardless of what the client claims.
	_, err := prescriptionSvc.Upload(ctx, tenancy.DefaultBusinessID, customerID, "fake.pdf", "application/pdf", 20, bytes.NewReader([]byte("not actually a pdf")))
	if err == nil {
		t.Fatal("expected a validation error for a content-type mismatch, got none")
	}
}

func TestPrescriptionUploadAndPharmacistReview(t *testing.T) {
	ctx := context.Background()
	ownerID := insertCustomer(t, "9876500002")
	otherID := insertCustomer(t, "9876500003")

	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 64)...)
	rx, err := prescriptionSvc.Upload(ctx, tenancy.DefaultBusinessID, ownerID, "rx.jpg", "image/jpeg", int64(len(jpeg)), bytes.NewReader(jpeg))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if rx.Status != prescriptions.StatusPendingReview {
		t.Fatalf("status = %s, want PENDING_REVIEW", rx.Status)
	}

	// It must show up in the owner's own list and in the pharmacist queue.
	mine, err := prescriptionSvc.ListForCustomer(ctx, ownerID)
	if err != nil || len(mine) != 1 || mine[0].ID != rx.ID {
		t.Fatalf("ListForCustomer = %v, %v", mine, err)
	}
	pending, err := prescriptionSvc.ListPendingReview(ctx, tenancy.DefaultBusinessID)
	if err != nil {
		t.Fatalf("ListPendingReview: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.ID == rx.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("uploaded prescription did not appear in the pending-review queue")
	}

	// The owner can get a signed URL; an unrelated customer cannot.
	if _, err := prescriptionSvc.SignedURL(ctx, rx.ID, httpx.Principal{CustomerID: ownerID}, ""); err != nil {
		t.Errorf("owner SignedURL: %v", err)
	}
	if _, err := prescriptionSvc.SignedURL(ctx, rx.ID, httpx.Principal{CustomerID: otherID}, ""); !errors.Is(err, prescriptions.ErrForbidden) {
		t.Errorf("other customer SignedURL err = %v, want ErrForbidden", err)
	}
	// Staff (a pharmacist or admin) at the same business can always view it to
	// review it; the same staff member resolved to a different business
	// cannot (a cross-business staff session must never see it).
	if _, err := prescriptionSvc.SignedURL(ctx, rx.ID, httpx.Principal{UserID: staffUserID, Roles: []string{httpx.RolePharmacist}}, tenancy.DefaultBusinessID); err != nil {
		t.Errorf("pharmacist SignedURL: %v", err)
	}
	if _, err := prescriptionSvc.SignedURL(ctx, rx.ID, httpx.Principal{UserID: staffUserID, Roles: []string{httpx.RolePharmacist}}, "00000000-0000-0000-0000-000000000099"); !errors.Is(err, prescriptions.ErrForbidden) {
		t.Errorf("pharmacist at a different business SignedURL err = %v, want ErrForbidden", err)
	}

	// A pharmacist's approval moves it out of the pending queue.
	reviewed, err := prescriptionSvc.Review(ctx, tenancy.DefaultBusinessID, rx.ID, staffUserID, "APPROVED", nil)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if reviewed.Status != prescriptions.StatusApproved {
		t.Errorf("status after review = %s, want APPROVED", reviewed.Status)
	}
	pendingAfter, err := prescriptionSvc.ListPendingReview(ctx, tenancy.DefaultBusinessID)
	if err != nil {
		t.Fatalf("ListPendingReview after review: %v", err)
	}
	for _, p := range pendingAfter {
		if p.ID == rx.ID {
			t.Fatal("approved prescription is still in the pending-review queue")
		}
	}
}
