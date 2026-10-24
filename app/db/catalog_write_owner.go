package db

import (
	"context"
	"sync/atomic"
)

// Catalog pages reuse an in-memory identity index. Manual merges can delete
// IDs from that index, so those two write paths share admission until commit.
// SQLite still owns persistence locking; this gate owns index coherence only.
var catalogIdentityWrites = make(chan struct{}, 1)
var catalogIdentityRevision atomic.Uint64

func ownCatalogIdentityWrite(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case catalogIdentityWrites <- struct{}{}:
		return func() { <-catalogIdentityWrites }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
