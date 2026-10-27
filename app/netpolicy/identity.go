package netpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func identity(req *http.Request, category string) (string, string, error) {
	u := *req.URL
	q := u.Query()
	if u.Hostname() == "api.bilibili.com" && u.Path == "/x/web-interface/wbi/search/type" {
		q.Del("wts")
		q.Del("w_rid")
	}
	if u.Hostname() == "www.iqiyi.com" && u.Path == "/prelw/tvg/v2/lw/base_info" {
		q.Del("timestamp")
		q.Del("sign")
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\n%s\n%s\n%v\n", category, req.Method, u.String(), req.Context().Value(scopeKey{}))
	// All declared headers participate: source adapters may use custom auth or
	// representation headers. Only the hash is persisted, never header values.
	headers, _ := json.Marshal(req.Header)
	hash.Write(headers)
	hash.Write([]byte{'\n'})
	// Fingerprint only replayable bodies. Never consume the live request body.
	if req.Body != nil && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return "", "", err
		}
		_, err = io.Copy(hash, body)
		closeErr := body.Close()
		if err != nil {
			return "", "", err
		}
		if closeErr != nil {
			return "", "", closeErr
		}
	}
	host := sha256.Sum256([]byte(u.Hostname()))
	prefix := "request:"
	if req.Context().Value(probeCooldownKey{}) == true {
		prefix = "probe:"
	}
	return prefix + hex.EncodeToString(hash.Sum(nil)), "host:" + hex.EncodeToString(host[:]), nil
}
func queryRequest(category string, req *http.Request) bool {
	if req.Context().Value(probeCooldownKey{}) == true && req.Header.Get("Range") == "" {
		return req.Method == http.MethodGet || req.Method == http.MethodHead
	}
	switch category {
	case "playback", "download", "image", "reading-image", "lineprobe", "cast", "update-download":
		return false
	}
	if req.Header.Get("Range") != "" {
		return false
	}
	for _, suffix := range []string{".exe", ".apk", ".zip", ".tar.gz", ".msi"} {
		if strings.HasSuffix(strings.ToLower(req.URL.Path), suffix) {
			return false
		}
	}
	if req.Method == "GET" || req.Method == "HEAD" {
		return true
	}
	return req.Method == "POST" && (category == "collect" || category == "danmaku" || category == "reading" || category == "novel" || category == "comic") && req.GetBody != nil
}
