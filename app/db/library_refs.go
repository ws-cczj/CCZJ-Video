package db

import (
	"context"
	"fmt"
	"strings"

	"cczjVideo/app/model"
)

type LibraryRef struct {
	Kind      string `json:"kind"`
	SourceKey string `json:"sourceKey"`
	RemoteID  string `json:"remoteId"`
}

// GetLibraryItemsByRefs uses indexed natural identities in bounded SQL batches.
// It returns only item metadata, so revalidating a shelf never loads its chapters.
func GetLibraryItemsByRefs(ctx context.Context, refs []LibraryRef) ([]*model.LibraryItem, error) {
	if len(refs) > 256 {
		return nil, fmt.Errorf("too many library references")
	}
	rows := make([]*model.LibraryItem, 0, len(refs))
	for start := 0; start < len(refs); start += 64 {
		end := start + 64
		if end > len(refs) {
			end = len(refs)
		}
		predicates := make([]string, 0, end-start)
		args := make([]any, 0, 3*(end-start))
		for _, ref := range refs[start:end] {
			if ref.Kind != "novel" && ref.Kind != "comic" {
				return nil, fmt.Errorf("invalid library reference kind")
			}
			if ref.SourceKey == "" || ref.RemoteID == "" {
				return nil, fmt.Errorf("empty library reference")
			}
			predicates = append(predicates, "(kind=? AND source_key=? AND remote_id=?)")
			args = append(args, ref.Kind, ref.SourceKey, ref.RemoteID)
		}
		var batch []*model.LibraryItem
		if err := instance.SelectContext(ctx, &batch, `SELECT * FROM library_items WHERE `+strings.Join(predicates, " OR "), args...); err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}
