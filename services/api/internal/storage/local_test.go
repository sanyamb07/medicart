package storage

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestLocal(t *testing.T) *Local {
	t.Helper()
	l, err := NewLocal(t.TempDir(), "http://localhost:8080/files", []byte("test-signing-secret-0000000000000000"))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return l
}

func TestLocalPutGetRoundTrip(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()

	key, err := l.Put(ctx, PutInput{
		Key: "public/products/test.jpg", ContentType: "image/jpeg",
		Visibility: Public, Body: strings.NewReader("fake-jpeg-bytes"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	rc, obj, err := l.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "fake-jpeg-bytes" {
		t.Errorf("got body %q", body)
	}
	if obj.ContentType != "image/jpeg" {
		t.Errorf("content type = %q", obj.ContentType)
	}
}

func TestLocalPublicURLRefusesPrivateKey(t *testing.T) {
	l := newTestLocal(t)
	if _, err := l.PublicURL("private/prescriptions/x.jpg"); err == nil {
		t.Fatal("expected an error minting a public URL for a private key")
	}
}

// This is the actual authorisation mechanism for prescription access: a
// signed URL must verify correctly, must reject a tampered key or signature,
// and must reject an expired grant.
func TestSignedURLVerification(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()
	key := "private/prescriptions/2026/01/01/owner/abc.jpg"
	_, err := l.Put(ctx, PutInput{Key: key, ContentType: "image/jpeg", Visibility: Private, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	signed, err := l.SignedURL(ctx, key, 10*time.Minute)
	if err != nil {
		t.Fatalf("SignedURL: %v", err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed url: %v", err)
	}
	expires := u.Query().Get("expires")
	sig := u.Query().Get("signature")

	if err := l.VerifySignature(key, expires, sig); err != nil {
		t.Errorf("valid signature should verify, got: %v", err)
	}

	if err := l.VerifySignature(key, expires, sig+"tampered"); err == nil {
		t.Error("tampered signature should not verify")
	}
	if err := l.VerifySignature("private/prescriptions/other-key.jpg", expires, sig); err == nil {
		t.Error("signature for a different key should not verify")
	}

	expired, err := l.SignedURL(ctx, key, -1*time.Minute)
	if err != nil {
		t.Fatalf("SignedURL (expired): %v", err)
	}
	eu, _ := url.Parse(expired)
	if err := l.VerifySignature(key, eu.Query().Get("expires"), eu.Query().Get("signature")); err == nil {
		t.Error("an already-expired grant should not verify")
	}
}

func TestLocalResolveRejectsPathTraversal(t *testing.T) {
	l := newTestLocal(t)
	ctx := context.Background()
	if _, err := l.Put(ctx, PutInput{Key: "../../etc/passwd", Visibility: Private, Body: strings.NewReader("x")}); err == nil {
		t.Fatal("expected path traversal key to be rejected")
	}
	if _, _, err := l.Get(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("expected path traversal key to be rejected on Get")
	}
}
