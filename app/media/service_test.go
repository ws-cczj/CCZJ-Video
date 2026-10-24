package media

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestIdentityFailuresRemainDistinguishable(t *testing.T) {
	storageFailure := errors.New("database is locked")
	for _, tt := range []struct {
		name  string
		cause error
		code  apperror.Code
	}{
		{"missing", sql.ErrNoRows, apperror.NotFound},
		{"storage", storageFailure, apperror.Storage},
		{"invalid", apperror.New(apperror.Validation, "invalid key"), apperror.Validation},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(func(context.Context, string, string) (*model.Video, error) { return nil, tt.cause }, db.NewMediaStore(db.DB), nil)
			for _, err := range []error{s.AddFavorite(context.Background(), "source", "1"), s.SaveHistory(context.Background(), "source", "1", 1, 5)} {
				if apperror.CodeOf(err) != tt.code || !errors.Is(err, tt.cause) {
					t.Fatalf("classification or cause lost: %v", err)
				}
			}
			_, err := s.IsFavorite(context.Background(), "source", "1")
			if errors.Is(tt.cause, sql.ErrNoRows) {
				if err != nil {
					t.Fatalf("missing favorite should be false: %v", err)
				}
			} else if err == nil || !errors.Is(err, tt.cause) {
				t.Fatalf("lookup failure silently swallowed: %v", err)
			}
		})
	}
}
