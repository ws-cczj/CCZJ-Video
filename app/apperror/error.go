// Package apperror defines stable application error codes for Wails callers.
package apperror

import (
	"errors"
	"fmt"
)

// Code identifies an error category independently of its human message.
type Code string

const (
	Validation        Code = "VALIDATION"
	Conflict          Code = "CONFLICT"
	NotFound          Code = "NOT_FOUND"
	Cancelled         Code = "CANCELLED"
	DownloadDuplicate Code = "DOWNLOAD_DUPLICATE"
)

// Error is a transport-friendly coded error that still supports errors.Is and
// errors.As through Unwrap.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New creates a coded application error.
func New(code Code, message string) error { return &Error{Code: code, Message: message} }

// CodeOf returns a stable code for coded errors, or an empty string otherwise.
func CodeOf(err error) Code {
	var appError *Error
	if errors.As(err, &appError) {
		return appError.Code
	}
	return ""
}
