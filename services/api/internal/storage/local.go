package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Local stores objects on the local filesystem. It is the development default
// and requires no cloud account.
//
// Signed URLs are real HMAC grants, not a stub: the same code path and the
// same expiry semantics as production, so the authorisation behaviour being
// tested locally is the behaviour that ships. The API serves them from
// /files/private/... after verifying the signature.
type Local struct {
	root      string
	publicURL string
	// secret signs private-object grants. Derived from SESSION_SECRET so there
	// is no extra secret to manage.
	secret []byte
}

// NewLocal creates the driver, ensuring the root directory exists.
func NewLocal(root, publicBaseURL string, secret []byte) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage dir: %w", err)
	}
	// 0o700: prescriptions live under here, so the directory is owner-only.
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &Local{
		root:      abs,
		publicURL: strings.TrimRight(publicBaseURL, "/"),
		secret:    secret,
	}, nil
}

func (l *Local) Driver() string { return "local" }

// resolve maps a storage key to an absolute path, refusing anything that would
// escape the root. This is the defence against path traversal in a key.
func (l *Local) resolve(key string) (string, error) {
	if key == "" {
		return "", errors.New("storage: empty key")
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("storage: illegal key %q", key)
	}
	full := filepath.Join(l.root, filepath.FromSlash(key))
	// Re-check after joining: Join cleans the path, so this catches any case
	// the string check above missed.
	if !strings.HasPrefix(full, l.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: key escapes root: %q", key)
	}
	return full, nil
}

func (l *Local) Put(ctx context.Context, in PutInput) (string, error) {
	full, err := l.resolve(in.Key)
	if err != nil {
		return "", err
	}
	perm := os.FileMode(0o600)
	dirPerm := os.FileMode(0o700)
	if in.Visibility == Public {
		perm, dirPerm = 0o644, 0o755
	}
	if err := os.MkdirAll(filepath.Dir(full), dirPerm); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	// Write to a temporary file and rename, so a crash mid-upload cannot leave
	// a truncated object behind under the real key.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once the rename has succeeded
	}()

	if _, err := io.Copy(tmp, in.Body); err != nil {
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("sync object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close object: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return "", fmt.Errorf("set permissions: %w", err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return "", fmt.Errorf("commit object: %w", err)
	}

	// Record the content type alongside the object: the filesystem does not
	// store it, and Get must report it accurately.
	if in.ContentType != "" {
		_ = os.WriteFile(full+".type", []byte(in.ContentType), perm)
	}
	return in.Key, nil
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	full, err := l.resolve(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("open object: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("stat object: %w", err)
	}

	ct := "application/octet-stream"
	if b, err := os.ReadFile(full + ".type"); err == nil && len(b) > 0 {
		ct = strings.TrimSpace(string(b))
	}
	vis := Public
	if IsPrivateKey(key) {
		vis = Private
	}
	return f, &Object{Key: key, ContentType: ct, Size: st.Size(), Visibility: vis}, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete object: %w", err)
	}
	_ = os.Remove(full + ".type")
	return nil
}

func (l *Local) PublicURL(key string) (string, error) {
	if IsPrivateKey(key) {
		// Refusing here is the point: a caller that reaches for a public URL
		// on a prescription has a bug, and returning one would be a leak.
		return "", errors.New("storage: refusing a public URL for a private object")
	}
	return l.publicURL + "/" + key, nil
}

func (l *Local) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if _, err := l.resolve(key); err != nil {
		return "", err
	}
	exp := time.Now().Add(ttl).Unix()
	sig := signKey(l.secret, key, exp)
	q := url.Values{}
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("signature", sig)
	return fmt.Sprintf("%s/%s?%s", l.publicURL, key, q.Encode()), nil
}

// VerifySignature checks a grant minted by SignedURL. Returns nil when the
// signature matches and the deadline has not passed.
func (l *Local) VerifySignature(key, expires, signature string) error {
	exp, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return errors.New("storage: malformed expiry")
	}
	if time.Now().Unix() > exp {
		return errors.New("storage: link has expired")
	}
	want := signKey(l.secret, key, exp)
	// Constant-time compare: a timing side channel here would let an attacker
	// forge a grant byte by byte.
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return errors.New("storage: invalid signature")
	}
	return nil
}

// signKey computes the grant HMAC over the key and expiry together, so neither
// can be swapped independently.
func signKey(secret []byte, key string, exp int64) string {
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "%s\n%d", key, exp)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Checksum returns the SHA-256 of r while copying it into w, so an upload is
// hashed in the same pass it is stored in.
func Checksum(r io.Reader, w io.Writer) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), r)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
