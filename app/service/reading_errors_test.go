package service

import (
	"context"
	"errors"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/readingstate"
)

func TestReadingPublicErrorsPreserveFailureSemantics(t *testing.T) {
	for _, test := range []struct {
		cause error
		code  apperror.Code
	}{
		{readingstate.ErrInvalidRecord, apperror.Validation}, {db.ErrCorruptReadingState, apperror.Corrupt},
		{db.ErrReadingNotHydrated, apperror.Conflict}, {context.Canceled, apperror.Cancelled},
		{context.DeadlineExceeded, apperror.Timeout}, {errors.New("SQL database is closed"), apperror.Storage},
	} {
		mapped := readingStateError(test.cause, "阅读状态不可用")
		if apperror.CodeOf(mapped) != test.code || !errors.Is(mapped, test.cause) {
			t.Errorf("%v mapped to %v, want %s", test.cause, mapped, test.code)
		}
		if public := apperror.Public(mapped); public.Message == test.cause.Error() {
			t.Error("diagnostic cause leaked as public message")
		}
	}
}
