package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
)

// These checks describe one sampled source path, never receiver playback or
// codec support. They share playback networking and end with the relay owner.
func (s *castStream) inspectHLS(raw, referer string) {
	owned, _ := operation.OwnedContext(s.ctx, s.ctx, "cast.source.check", selectedCastFields(s.ctx))
	ctx, cancel := context.WithTimeout(owned, 8*time.Second)
	defer cancel()
	ctx = operation.WithFields(ctx, applog.Fields{"observation_scope": "phone_sample"})
	requests := 0
	fetch := func(raw, kind string) ([]byte, *url.URL, bool) {
		requests++
		started := time.Now()
		operation.Stage(ctx, "cast.source.request", applog.Fields{"resource_kind": kind, "attempt": requests})
		target, err := parsePublicHTTPURL(raw)
		var response *http.Response
		var final *url.URL
		if err == nil {
			rangeHeader := "bytes=0-511"
			if kind == "playlist" {
				rangeHeader = ""
			}
			response, final, err = s.owner.hls.fetch(ctx, target, rangeHeader, referer)
		}
		status := 0
		contentType := ""
		var body []byte
		limit := int64(512)
		if kind == "playlist" {
			limit = 256 << 10
		}
		if response != nil {
			status = response.StatusCode
			contentType = response.Header.Get("Content-Type")
			if err == nil && (status == 200 || status == 206) {
				body, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
			}
			response.Body.Close()
		}
		code := castFetchCode(err, status)
		if code == "OK" && len(body) == 0 {
			code = "EMPTY_CONTENT"
		}
		if code == "OK" && kind == "playlist" && (len(body) > int(limit) || !bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U"))) {
			code = "INVALID_PLAYLIST"
		}
		if code == "OK" && kind != "playlist" && invalidCastContent(contentType, body) {
			code = "INVALID_CONTENT"
		}
		operation.Stage(ctx, "cast.source.response", applog.Fields{"resource_kind": kind, "attempt": requests, "code": code, "http_status": status, "bytes": len(body), "elapsed_ms": time.Since(started).Milliseconds(), "sampled": true})
		return body, final, code == "OK"
	}
	target := raw
	outcome := "depth_limit"
	for depth := 0; depth < 3; depth++ {
		body, base, ok := fetch(target, "playlist")
		if !ok {
			outcome = "source_failed"
			break
		}
		fields := castPlaylistFields(string(body))
		fields["attempt"] = depth + 1
		operation.Stage(ctx, "cast.source.playlist", fields)
		first := map[string]string{}
		rewriteTypedPlaylistURIs(string(body), base, func(uri, kind string) string {
			if first[kind] == "" {
				first[kind] = uri
			}
			return uri
		})
		if first["media"] == "" {
			if first["playlist"] != "" {
				target = first["playlist"]
				continue
			}
			outcome = "no_sampleable_media"
			break
		}
		outcome = "sample_accessible"
		for _, kind := range []string{"key", "init", "media"} {
			if first[kind] == "" {
				continue
			}
			if _, _, ok := fetch(first[kind], kind); !ok {
				outcome = "source_failed"
				break
			}
		}
		break
	}
	operation.Stage(ctx, "cast.source.summary", applog.Fields{"outcome": outcome, "requests": requests, "sampled": true})
}

func castPlaylistFields(playlist string) applog.Fields {
	fields := applog.Fields{"hls_master": false, "hls_encrypted": false, "hls_init_map": false, "hls_separate_audio": false, "hls_endlist": false, "hls_version": 0, "codec_avc": false, "codec_hevc": false, "codec_av1": false, "codec_aac": false, "codec_ac3": false}
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			if version, err := strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-VERSION:")); err == nil && version >= 0 && version <= 99 {
				fields["hls_version"] = version
			}
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"), strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:"):
			fields["hls_master"] = true
		case strings.HasPrefix(line, "#EXT-X-KEY:"), strings.HasPrefix(line, "#EXT-X-SESSION-KEY:"):
			if !strings.Contains(line, "METHOD=NONE") {
				fields["hls_encrypted"] = true
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			fields["hls_init_map"] = true
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			if strings.Contains(line, "TYPE=AUDIO") {
				fields["hls_separate_audio"] = true
			}
		case line == "#EXT-X-ENDLIST":
			fields["hls_endlist"] = true
		}
		if strings.HasPrefix(line, "#") && strings.Contains(line, "CODECS=") {
			lower := strings.ToLower(line)
			for key, markers := range map[string][]string{"codec_avc": {"avc1", "avc3"}, "codec_hevc": {"hvc1", "hev1"}, "codec_av1": {"av01"}, "codec_aac": {"mp4a"}, "codec_ac3": {"ac-3", "ec-3"}} {
				for _, marker := range markers {
					if strings.Contains(lower, marker) {
						fields[key] = true
					}
				}
			}
		}
	}
	return fields
}

func invalidCastContent(contentType string, body []byte) bool {
	contentType = strings.ToLower(contentType)
	prefix := bytes.ToLower(bytes.TrimSpace(body))
	return strings.Contains(contentType, "html") || strings.Contains(contentType, "json") || bytes.HasPrefix(prefix, []byte("<!doctype html")) || bytes.HasPrefix(prefix, []byte("<html"))
}

func castFetchCode(err error, status int) string {
	if err == nil {
		switch {
		case status == 401 || status == 403:
			return "UPSTREAM_AUTH"
		case status == 429:
			return "UPSTREAM_RATE_LIMIT"
		case status >= 400:
			return "UPSTREAM_HTTP"
		default:
			return "OK"
		}
	}
	var dns *net.DNSError
	var network net.Error
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case errors.Is(err, context.Canceled):
		return "CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT"
	case errors.As(err, &unknown), errors.As(err, &hostname), errors.As(err, &certificate):
		return "TLS_CERTIFICATE"
	case errors.As(err, &record):
		return "TLS_PROTOCOL"
	case errors.As(err, &dns):
		return "DNS_FAILED"
	case errors.As(err, &network) && network.Timeout():
		return "TIMEOUT"
	default:
		return "UPSTREAM_IO"
	}
}
