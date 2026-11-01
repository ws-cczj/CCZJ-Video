package provider

import (
	"cczjVideo/app/apperror"
	"fmt"
)

type EndpointMissingError struct{ Status int }

func (e *EndpointMissingError) Error() string {
	return fmt.Sprintf("源站地址已失效（HTTP %d）", e.Status)
}
func (e *EndpointMissingError) Unwrap() error { return apperror.New(apperror.NotFound, e.Error()) }
