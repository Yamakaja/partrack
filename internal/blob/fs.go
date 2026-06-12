package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FSStore is a content-addressed Store backed by the local filesystem.
//
// Layout: <root>/blobs/<first-2-hex>/<full-sha256>. The two-character fan-out
// keeps any single directory from growing without bound. Writes land in
// <root>/tmp first and are atomically renamed into place, so a crash can never
// leave a partially-written blob visible at its final (content) address —
// "write-before-commit". Identical content already present is left untouched.
type FSStore struct {
	root string
}

// NewFSStore prepares root for use, creating the blobs/ and tmp/ subdirectories
// and probing writability so a misconfigured data directory fails at startup
// rather than on the first upload.
func NewFSStore(root string) (*FSStore, error) {
	s := &FSStore{root: root}
	for _, d := range []string{s.blobsDir(), s.tmpDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("blob: create %s: %w", d, err)
		}
	}
	probe := filepath.Join(s.tmpDir(), ".probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return nil, fmt.Errorf("blob: data dir %s not writable: %w", root, err)
	}
	_ = os.Remove(probe)
	return s, nil
}

func (s *FSStore) blobsDir() string { return filepath.Join(s.root, "blobs") }
func (s *FSStore) tmpDir() string   { return filepath.Join(s.root, "tmp") }

func (s *FSStore) path(hash string) string {
	return filepath.Join(s.blobsDir(), hash[:2], hash)
}

// Put streams r to a temp file while hashing, then atomically renames it to its
// content address. If the address already exists, the temp file is discarded
// (dedup).
func (s *FSStore) Put(ctx context.Context, r io.Reader) (Ref, error) {
	tmp, err := os.CreateTemp(s.tmpDir(), "put-*")
	if err != nil {
		return Ref{}, err
	}
	tmpName := tmp.Name()
	// Remove the temp file unless it was renamed into place. Safe to call after a
	// successful rename (it simply finds nothing).
	defer os.Remove(tmpName)

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		tmp.Close()
		return Ref{}, err
	}
	if err := tmp.Close(); err != nil {
		return Ref{}, err
	}

	hash := hex.EncodeToString(h.Sum(nil))
	dst := s.path(hash)
	if _, err := os.Stat(dst); err == nil {
		return Ref{SHA256: hash, Size: size}, nil // already stored
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Ref{}, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return Ref{}, err
	}
	return Ref{SHA256: hash, Size: size}, nil
}

// Open returns the content at hash. It returns ErrNotFound for a malformed hash
// or a missing blob.
func (s *FSStore) Open(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	if !ValidHash(hash) {
		return nil, 0, ErrNotFound
	}
	f, err := os.Open(s.path(hash))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// URL returns the application's own serve path; the FS backend streams bytes
// through ServeBlob rather than handing the client a direct URL.
func (s *FSStore) URL(ctx context.Context, hash string) (string, error) {
	return "/api/v1/blobs/" + hash, nil
}
