package storage

import (
	"strings"
	"testing"
)

func TestValidatePrescriptionUpload(t *testing.T) {
	const maxBytes = 10 << 20

	cases := []struct {
		contentType string
		size        int64
		wantErr     bool
	}{
		{"image/jpeg", 1024, false},
		{"image/jpeg; charset=binary", 1024, false}, // parameters must be stripped
		{"application/pdf", 1024, false},
		{"image/svg+xml", 1024, true},      // SVG can carry script — must be rejected
		{"text/html", 1024, true},          // active content — must be rejected
		{"image/jpeg", 0, true},            // empty file
		{"image/jpeg", maxBytes + 1, true}, // over the limit
	}
	for _, c := range cases {
		_, err := ValidatePrescriptionUpload(c.contentType, c.size, maxBytes)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidatePrescriptionUpload(%q, %d) error = %v, wantErr %v", c.contentType, c.size, err, c.wantErr)
		}
	}
}

func TestSniffContentTypeDetectsRealFormat(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00"), "image/png"},
		{"pdf", []byte("%PDF-1.4"), "application/pdf"},
		{"unknown", []byte("not a real file"), ""},
	}
	for _, c := range cases {
		if got := SniffContentType(c.head); got != c.want {
			t.Errorf("%s: SniffContentType() = %q, want %q", c.name, got, c.want)
		}
	}
}

// This is the actual security property BuildKey exists for: no attacker-
// controlled input (a filename) reaches the storage key, so there is nothing
// in it to traverse or inject.
func TestBuildKeyNeverEmbedsCallerInput(t *testing.T) {
	key := BuildKey(PrefixPrescriptions, "owner-123", "abc123", ".jpg")
	if strings.Contains(key, "..") {
		t.Errorf("key must never contain '..': %s", key)
	}
	if !strings.HasPrefix(key, PrefixPrescriptions+"/") {
		t.Errorf("key must start with its prefix: %s", key)
	}
	if !IsPrivateKey(key) {
		t.Errorf("a prescriptions-prefixed key must be classified private: %s", key)
	}
}

func TestIsPrivateKey(t *testing.T) {
	if IsPrivateKey("public/products/x.jpg") {
		t.Error("a public/ key must not be classified private")
	}
	if !IsPrivateKey("private/prescriptions/x.jpg") {
		t.Error("a private/ key must be classified private")
	}
}
