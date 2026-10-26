package collect

import (
	"context"
	"errors"

	"cczjVideo/app/apperror"
)

// FetchFailureKind separates transport, upstream refusal and adapter failures.
// It consumes error types and stages, never the source's display message.
func FetchFailureKind(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || apperror.Is(err, apperror.Cancelled) {
		return "cancelled"
	}
	var status *HTTPError
	if errors.As(err, &status) {
		return "http"
	}
	var api *APIError
	if errors.As(err, &api) || errors.Is(err, errSearchUnsupported) {
		return "source"
	}
	var response *ResponseError
	if errors.As(err, &response) && (response.Stage == "decode" || response.Stage == "decompress") {
		return "decode"
	}
	var path *PathError
	if errors.Is(err, ErrPathSyntax) {
		return "configuration"
	}
	if errors.As(err, &path) || apperror.Is(err, apperror.Corrupt) {
		return "decode"
	}
	if apperror.Is(err, apperror.Validation) {
		return "configuration"
	}
	return "request"
}
