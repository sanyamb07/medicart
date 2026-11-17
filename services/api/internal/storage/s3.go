package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// S3 targets any S3-compatible object store: Cloudflare R2, Backblaze B2,
// AWS S3, MinIO.
//
// Signature Version 4 is implemented here directly rather than pulling in
// aws-sdk-go-v2. MediCart needs four operations (put, get, delete, presign
// get); the SDK would add ~40 modules and several MB to the build for those.
// SigV4 is a stable, fully-specified algorithm, and the implementation is
// covered by unit tests against the AWS specification test vectors.
type S3 struct {
	bucket    string
	region    string
	endpoint  string // e.g. https://<account>.r2.cloudflarestorage.com
	accessKey string
	secretKey string
	publicURL string
	client    *http.Client
}

// NewS3 builds the driver. endpoint must include the scheme.
func NewS3(bucket, region, endpoint, accessKey, secretKey, publicBaseURL string) (*S3, error) {
	if bucket == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("storage: s3 driver needs bucket, access key and secret key")
	}
	if endpoint == "" {
		// Default to AWS S3's regional endpoint when none is given.
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", region)
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	return &S3{
		bucket:    bucket,
		region:    region,
		endpoint:  strings.TrimRight(endpoint, "/"),
		accessKey: accessKey,
		secretKey: secretKey,
		publicURL: strings.TrimRight(publicBaseURL, "/"),
		// A bounded client: an object store that stops responding must not
		// hold request goroutines open indefinitely.
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (s *S3) Driver() string { return "s3" }

// objectURL builds the path-style URL for a key. Path style works on R2, B2,
// MinIO and S3 alike, whereas virtual-host style needs per-provider handling.
func (s *S3) objectURL(key string) string {
	return s.endpoint + "/" + s.bucket + "/" + encodeKeyPath(key)
}

func (s *S3) Put(ctx context.Context, in PutInput) (string, error) {
	if _, err := validateKey(in.Key); err != nil {
		return "", err
	}

	// SigV4 needs the payload hash up front, so the body is buffered. Uploads
	// here are capped at MAX_UPLOAD_BYTES (10 MB by default), which is safe to
	// hold in memory; streaming would require chunked signing for little gain.
	var buf strings.Builder
	sum, size, err := Checksum(in.Body, &buf)
	if err != nil {
		return "", fmt.Errorf("read upload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(in.Key),
		strings.NewReader(buf.String()))
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	if in.ContentType != "" {
		req.Header.Set("Content-Type", in.ContentType)
	}
	// Private is the default for every bucket MediCart uses; public product
	// images are served through a CDN/public bucket binding rather than an ACL,
	// because R2 does not implement per-object ACLs.
	req.Header.Set("X-Amz-Content-Sha256", sum)

	s.sign(req, sum)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("s3 put: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("s3 put: %s: %s", resp.Status, snippet(resp.Body))
	}
	return in.Key, nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	if _, err := validateKey(key); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, nil, err
	}
	s.sign(req, emptyPayloadHash)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("s3 get: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		drain(resp)
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer drain(resp)
		return nil, nil, fmt.Errorf("s3 get: %s: %s", resp.Status, snippet(resp.Body))
	}

	vis := Public
	if IsPrivateKey(key) {
		vis = Private
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	obj := &Object{
		Key:         key,
		ContentType: resp.Header.Get("Content-Type"),
		Size:        size,
		Visibility:  vis,
	}
	// The caller closes the body.
	return resp.Body, obj, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if _, err := validateKey(key); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	s.sign(req, emptyPayloadHash)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	defer drain(resp)
	// S3 returns 204 for a successful delete and also for a missing key.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("s3 delete: %s: %s", resp.Status, snippet(resp.Body))
	}
	return nil
}

func (s *S3) PublicURL(key string) (string, error) {
	if IsPrivateKey(key) {
		return "", fmt.Errorf("storage: refusing a public URL for a private object")
	}
	if s.publicURL == "" {
		return "", fmt.Errorf("storage: STORAGE_PUBLIC_BASE_URL is not configured")
	}
	return s.publicURL + "/" + key, nil
}

// SignedURL creates a presigned GET. This is the only way a prescription is
// ever readable, and the grant expires with ttl.
func (s *S3) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if _, err := validateKey(key); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	// SigV4 caps presigned expiry at 7 days.
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	scopeDate := now.Format("20060102")
	credScope := fmt.Sprintf("%s/%s/s3/aws4_request", scopeDate, s.region)

	u, err := url.Parse(s.objectURL(key))
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.accessKey+"/"+credScope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	u.RawQuery = q.Encode()

	canonicalReq := strings.Join([]string{
		http.MethodGet,
		u.EscapedPath(),
		u.RawQuery,
		"host:" + u.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, credScope, sha256Hex(canonicalReq),
	}, "\n")

	sig := hex.EncodeToString(hmacSHA256(s.signingKey(scopeDate), stringToSign))
	q.Set("X-Amz-Signature", sig)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ---- SigV4 ----

const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// sign adds the SigV4 Authorization header to req.
func (s *S3) sign(req *http.Request, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	scopeDate := now.Format("20060102")

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if req.Host == "" {
		req.Host = req.URL.Host
	}

	// Canonical headers must be sorted, lowercase, and whitespace-trimmed.
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		signed = append(signed, "content-type")
	}
	sort.Strings(signed)

	var canonHeaders strings.Builder
	for _, h := range signed {
		var v string
		switch h {
		case "host":
			v = req.URL.Host
		default:
			v = req.Header.Get(h)
		}
		canonHeaders.WriteString(h + ":" + strings.TrimSpace(v) + "\n")
	}
	signedHeaders := strings.Join(signed, ";")

	canonicalReq := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	credScope := fmt.Sprintf("%s/%s/s3/aws4_request", scopeDate, s.region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, credScope, sha256Hex(canonicalReq),
	}, "\n")

	sig := hex.EncodeToString(hmacSHA256(s.signingKey(scopeDate), stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, credScope, signedHeaders, sig))
}

// signingKey derives the date/region/service-scoped key.
func (s *S3) signingKey(scopeDate string) []byte {
	k := hmacSHA256([]byte("AWS4"+s.secretKey), scopeDate)
	k = hmacSHA256(k, s.region)
	k = hmacSHA256(k, "s3")
	return hmacSHA256(k, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// encodeKeyPath percent-encodes each path segment, leaving the separators.
// url.PathEscape would encode the slashes too.
func encodeKeyPath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func validateKey(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("storage: empty key")
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("storage: illegal key %q", key)
	}
	return key, nil
}

// drain consumes and closes a response body so the connection can be reused.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}

// snippet reads a short error body for diagnostics, bounded so a hostile or
// broken endpoint cannot flood the logs.
func snippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return strings.TrimSpace(string(b))
}
