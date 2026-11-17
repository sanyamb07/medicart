// Package storage abstracts object storage for uploaded files.
//
// Two visibility classes, and the distinction is enforced by the interface
// rather than by convention:
//
//	Public  — product images. Served directly; a stable URL is fine.
//	Private — prescriptions. No public URL exists at all. Access is only ever
//	          through SignedURL, which mints a short-lived, single-purpose
//	          grant for a caller the application has already authorised.
//
// Drivers: `local` writes to disk for development; `s3` targets any
// S3-compatible bucket (Cloudflare R2, Backblaze B2, AWS S3).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"
)

// Visibility of a stored object.
type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

var (
	ErrNotFound        = errors.New("storage: object not found")
	ErrTooLarge        = errors.New("storage: object exceeds the size limit")
	ErrUnsupportedType = errors.New("storage: content type not allowed")
)

// Object describes a stored file.
type Object struct {
	Key         string
	ContentType string
	Size        int64
	Visibility  Visibility
}

// PutInput is an upload request.
type PutInput struct {
	// Key is the storage path. Build it with BuildKey so prefixes stay
	// consistent and user input never reaches the filesystem path.
	Key         string
	ContentType string
	Visibility  Visibility
	Body        io.Reader
	Size        int64
}

// Store is the storage contract.
type Store interface {
	// Put stores an object, returning its canonical key.
	Put(ctx context.Context, in PutInput) (string, error)

	// Get opens an object for reading. The caller closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)

	// Delete removes an object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error

	// PublicURL returns a directly-fetchable URL for a public object.
	// It returns an error for a private key: a prescription must never
	// acquire a permanent URL, even by mistake.
	PublicURL(key string) (string, error)

	// SignedURL mints a time-limited URL for a private object. The grant is
	// what authorises the fetch, so ttl should be minutes, not hours.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)

	// Driver names the active implementation, for the health endpoint.
	Driver() string
}

// Key prefixes. Private prefixes are checked by IsPrivateKey, so a mis-built
// key cannot be served publicly.
const (
	PrefixProductImages = "public/products"
	PrefixPrescriptions = "private/prescriptions"
)

// IsPrivateKey reports whether a key belongs to the private namespace.
func IsPrivateKey(key string) bool { return strings.HasPrefix(key, "private/") }

// allowedPrescriptionTypes is deliberately narrow. Accepting SVG or HTML would
// let an uploader store an active document; accepting arbitrary types would let
// them store anything at all. A prescription is a photo or a PDF.
var allowedPrescriptionTypes = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/heic":      ".heic",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

// allowedImageTypes for admin-uploaded product images.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// ValidatePrescriptionUpload checks the declared type and size before any bytes
// are written. The caller must additionally verify the magic bytes — a client
// can claim any Content-Type — which Service.UploadPrescription does.
func ValidatePrescriptionUpload(contentType string, size, maxBytes int64) (ext string, err error) {
	ct := normaliseContentType(contentType)
	ext, ok := allowedPrescriptionTypes[ct]
	if !ok {
		return "", fmt.Errorf("%w: %s (accepted: JPG, PNG, HEIC, WEBP, PDF)", ErrUnsupportedType, ct)
	}
	if size <= 0 {
		return "", errors.New("storage: empty file")
	}
	if size > maxBytes {
		return "", ErrTooLarge
	}
	return ext, nil
}

// ValidateImageUpload checks an admin product-image upload.
func ValidateImageUpload(contentType string, size, maxBytes int64) (ext string, err error) {
	ct := normaliseContentType(contentType)
	ext, ok := allowedImageTypes[ct]
	if !ok {
		return "", fmt.Errorf("%w: %s (accepted: JPG, PNG, WEBP)", ErrUnsupportedType, ct)
	}
	if size <= 0 {
		return "", errors.New("storage: empty file")
	}
	if size > maxBytes {
		return "", ErrTooLarge
	}
	return ext, nil
}

// normaliseContentType strips parameters ("image/jpeg; charset=..." → "image/jpeg")
// and lowercases, so a well-formed but decorated header still matches.
func normaliseContentType(ct string) string {
	if parsed, _, err := mime.ParseMediaType(ct); err == nil {
		return strings.ToLower(parsed)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
}

// SniffContentType identifies a file from its leading bytes. Used to reject an
// upload whose real format contradicts its declared Content-Type.
func SniffContentType(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "image/jpeg"
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(head) >= 4 && string(head[:4]) == "%PDF":
		return "application/pdf"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp"
	case len(head) >= 12 && (string(head[4:12]) == "ftypheic" || string(head[4:12]) == "ftypheix" ||
		string(head[4:12]) == "ftypmif1" || string(head[4:12]) == "ftypmsf1"):
		return "image/heic"
	default:
		return ""
	}
}

// BuildKey composes a storage key from a trusted prefix, an owner id and a
// random component. The caller's filename never appears in the key: it could
// contain path traversal, control characters, or the patient's name.
func BuildKey(prefix, ownerID, random, ext string) string {
	day := time.Now().UTC().Format("2006/01/02")
	return path.Join(prefix, day, ownerID, random+ext)
}
