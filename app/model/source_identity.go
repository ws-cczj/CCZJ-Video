package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
)

// NewSourceKey allocates a persisted identity independent of a source's address.
// Existing and explicitly imported keys remain unchanged.
func NewSourceKey() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("allocate source identity: %w", err)
	}
	return "src_" + hex.EncodeToString(id[:]), nil
}

// ValidateSourceURL checks the source entry point without making a network call.
func ValidateSourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("source address must be an absolute HTTP(S) URL")
	}
	return nil
}
