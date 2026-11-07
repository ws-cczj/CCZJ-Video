//go:build !android

package service

import "context"

func castRelayActive(context.Context, bool) {}
