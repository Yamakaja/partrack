// Package blob provides content-addressed binary storage. Bytes are addressed
// by the lowercase-hex SHA-256 of their content, so storing identical bytes
// twice is a no-op that yields the same reference (dedup), and a stored blob is
// immutable — an "edit" produces new bytes with a new address.
//
// Store is the backend seam. The filesystem implementation (FSStore) ships now;
// an object-store backend can be slotted in behind the same interface later, at
// which point URL returns a (presigned/CDN) URL the client fetches directly and
// callers stop using Open.
package blob

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by Open when no blob exists for the given hash.
var ErrNotFound = errors.New("blob: not found")

// Ref is a content-addressed reference to stored bytes.
type Ref struct {
	SHA256 string // lowercase hex SHA-256 of the content
	Size   int64  // byte length
}

// Store is a content-addressed blob store.
type Store interface {
	// Put streams content into the store and returns its content-addressed Ref.
	// Implementations commit atomically: a partially-written blob is never
	// visible at its final address. Storing content already present is a no-op
	// that returns the same Ref (dedup).
	Put(ctx context.Context, r io.Reader) (Ref, error)

	// Open returns a reader for the content at sha256 along with its size, or
	// ErrNotFound if absent. Used by the serve handler for backends that stream
	// bytes through the application (the filesystem backend).
	Open(ctx context.Context, sha256 string) (io.ReadCloser, int64, error)

	// URL returns a directly-fetchable URL for the content. The filesystem
	// backend returns the application's own serve path; an object-store backend
	// returns a presigned or CDN URL the client fetches directly. The caller
	// uses this when building a manifest of byte URLs for the frontend.
	URL(ctx context.Context, sha256 string) (string, error)
}

// ValidHash reports whether s is a syntactically valid blob address: exactly 64
// lowercase hex characters. The serve path takes the hash from the URL, so this
// guards against path traversal and malformed lookups.
func ValidHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
