package offsite

import (
	"context"
	"io"
	"time"
)

// Object is what a listing says about a stored file.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Store is the part of S3 this package uses. An interface so tests can fake the bucket
// without a network, and so a failed upload can be modelled the way S3 behaves: an object
// exists only once an upload has completed.
type Store interface {
	// Put stores r under key and returns the stored size. The length of r is unknown.
	// If reading r fails, or ctx ends, Put must fail and leave no object under key — and
	// no half-finished multipart upload that would keep costing storage unseen.
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	// Get streams an object and says how large it is.
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// List returns the objects whose key starts with prefix, without descending
	// into "folders" beyond it being a plain prefix match (callers filter further).
	List(ctx context.Context, prefix string) ([]Object, error)
	Remove(ctx context.Context, key string) error
}
