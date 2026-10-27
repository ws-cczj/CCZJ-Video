package db

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

// Preserve scheme, path case, trailing slashes and query values: these may
// identify different interfaces/accounts. Fragments are not sent in requests.
func videoEndpointIdentity(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// All interactive writes call this inside an immediate SQLite transaction.
// Backup restoration retains historical identities, including old duplicates.
func ensureUniqueVideoEndpoint(ctx context.Context, tx *sqlx.Tx, desired *model.Source, editing bool) error {
	if desired == nil {
		return fmt.Errorf("source is required")
	}
	kind := desired.GetKind()
	identity := videoEndpointIdentity(desired.ApiUrl)
	if editing {
		var current model.Source
		if err := tx.GetContext(ctx, &current, `SELECT * FROM sources WHERE source_key=?`, desired.SourceKey); err != nil {
			return err
		}
		kind = current.GetKind()
		// Renaming/disabling legacy duplicates must remain possible.
		if identity == videoEndpointIdentity(current.ApiUrl) {
			return nil
		}
	}
	if kind != model.SourceKindVideo || identity == "" {
		return nil
	}
	var urls []string
	if err := tx.SelectContext(ctx, &urls, `SELECT api_url FROM sources WHERE kind='video' AND source_key<>?`, desired.SourceKey); err != nil {
		return err
	}
	for _, raw := range urls {
		if identity == videoEndpointIdentity(raw) {
			return apperror.New(apperror.SourceURLDuplicate, "A video source with this API URL already exists")
		}
	}
	return nil
}
