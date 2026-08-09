// Package storagefs abstracts blob storage behind a small interface so the
// v1 local-filesystem implementation can be swapped for an S3-compatible one
// later (see plan Section 3.4 / Section 7) without touching callers.
package storagefs

import (
	"context"
	"io"
)

// Blob is the storage interface callers depend on. Keys are opaque,
// slash-separated strings (e.g. "{trip_id}/images/{media_asset_id}.jpg") —
// they work unchanged whether the backing store is a local directory or an
// S3-compatible bucket.
type Blob interface {
	Put(ctx context.Context, key string, r io.Reader) (size int64, err error)
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, key string) error
}
