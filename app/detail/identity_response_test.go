package detail

import (
	"context"
	"errors"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestDetailRejectsAnotherVideoInsteadOfPublishingIt(t *testing.T) {
	s := New(time.Minute)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	s.requestFn = func(*db.CatalogItem) (*model.Video, error) {
		return &model.Video{VodId: "other", VodName: "双面游戏"}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "source", SourceVodID: "requested", GlobalID: 9, VodName: "凡人修仙传"}
	result, err := s.getCatalogContext(context.Background(), catalog)
	var failure *Error
	if !errors.As(err, &failure) || failure.Fallback.VodName != catalog.VodName || failure.Retryable || result == nil || result.Video.VodId.String() != catalog.SourceVodID || result.Video.VodName != catalog.VodName || len(result.Lines) != 0 {
		t.Fatalf("mismatched detail was published or retried: result=%+v err=%+v", result, err)
	}
}
