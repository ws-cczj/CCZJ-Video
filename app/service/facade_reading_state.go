package service

import (
	"cczjVideo/app/db"
	"context"
	"encoding/json"
	"errors"

	"cczjVideo/app/apperror"
	"cczjVideo/app/readingstate"
)

func (a *App) ReadingStateLoad(ctx context.Context, kind, legacyJSON string) ([]readingstate.Record, error) {
	return runRequest(a, ctx, "reading.state_load", func(ctx context.Context) ([]readingstate.Record, error) {
		if a.readingState == nil {
			return nil, apperror.New(apperror.Unavailable, "阅读存储尚未就绪")
		}
		if len(legacyJSON) > 32<<20 {
			return nil, apperror.New(apperror.Validation, "旧阅读数据过大")
		}
		var legacy []readingstate.Record
		if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil {
			return nil, apperror.Wrap(apperror.Validation, err, "旧阅读数据格式无效")
		}
		records, err := a.readingState.Load(ctx, kind, legacy)
		if err != nil {
			return nil, readingStateError(err, "读取或导入阅读数据失败；原数据已保留")
		}
		return records, nil
	})
}

func (a *App) ReadingStateApply(ctx context.Context, kind, changesJSON string) error {
	_, err := runRequest(a, ctx, "reading.state_apply", func(ctx context.Context) (bool, error) {
		if a.readingState == nil {
			return false, apperror.New(apperror.Unavailable, "阅读存储尚未就绪")
		}
		if len(changesJSON) > 8<<20 {
			return false, apperror.New(apperror.Validation, "阅读写入批次过大")
		}
		var changes []readingstate.Change
		if err := json.Unmarshal([]byte(changesJSON), &changes); err != nil {
			return false, apperror.Wrap(apperror.Validation, err, "阅读数据格式无效")
		}
		if err := a.readingState.Apply(ctx, kind, changes); err != nil {
			return false, readingStateError(err, "保存阅读数据失败，请重试")
		}
		return true, nil
	})
	return err
}

func readingStateError(err error, message string) error {
	code := apperror.Storage
	switch {
	case errors.Is(err, db.ErrCorruptReadingState):
		code = apperror.Corrupt
	case errors.Is(err, readingstate.ErrInvalidRecord):
		code = apperror.Validation
	case errors.Is(err, db.ErrReadingNotHydrated):
		code = apperror.Conflict
	}
	return apperror.Wrap(code, err, message)
}
