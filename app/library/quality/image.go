package quality

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	"cczjVideo/app/proxy"
	_ "golang.org/x/image/webp"
)

type ImageEvidence struct {
	Index       int      `json:"index"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	Format      string   `json:"format,omitempty"`
	Bytes       int      `json:"bytes,omitempty"`
	Host        string   `json:"host,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Failure     *Failure `json:"failure,omitempty"`
}
type ImageProbe func(context.Context, string, string) ImageEvidence

// NewImageProbe retains declared headers/Referer and uses the shared dialing
// policy. Bounds apply before pixel decoding; no remote image is saved to disk.
func NewImageProbe(headers map[string]string) (ImageProbe, func()) {
	return newImageProbe(headers, nil)
}

func NewImageProbeWithGate(headers map[string]string, gate *NetworkGate) (ImageProbe, func()) {
	return newImageProbe(headers, gate)
}
func newImageProbe(headers map[string]string, gate *NetworkGate) (ImageProbe, func()) {
	transport := &http.Transport{DialContext: proxy.SafeDialContext, TLSHandshakeTimeout: 10 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 2}
	wrapped := netpolicy.WrapTransport(string(netstats.CategoryReading), netstats.WrapTransport(netstats.CategoryReading, transport))
	if gate != nil {
		wrapped = gate.Wrap(wrapped)
	}
	client := &http.Client{Transport: wrapped, Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many image redirects")
		}
		return nil
	}}
	return func(ctx context.Context, address, referer string) (out ImageEvidence) {
		e := ImageEvidence{Host: imageHost(address)}
		fail := func(err error) ImageEvidence { f := failure("image", err); e.Failure = &f; return e }
		req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
		if err != nil {
			return fail(apperror.Wrap(apperror.Validation, err, "图片地址格式不合法"))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fail(apperror.Wrap(apperror.Unavailable, err, "图片网络请求失败"))
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil && out.Failure == nil {
				f := failure("image", apperror.Wrap(apperror.Unavailable, closeErr, "图片响应关闭失败"))
				out.Failure = &f
			}
		}()
		if resp.StatusCode != http.StatusOK {
			code := apperror.Unavailable
			switch resp.StatusCode {
			case 401:
				code = apperror.AuthenticationRequired
			case 403:
				code = apperror.PermissionDenied
			case 404:
				code = apperror.NotFound
			case 429:
				code = apperror.RateLimited
			}
			return fail(apperror.New(code, fmt.Sprintf("图片 HTTP %d", resp.StatusCode)))
		}
		const maxBytes = 8 << 20
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return fail(apperror.Wrap(apperror.Unavailable, err, "图片读取中断"))
		}
		if len(raw) > maxBytes {
			return fail(apperror.New(apperror.Unsupported, "图片超过 8 MiB 抽样上限，未验证"))
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return fail(apperror.Wrap(apperror.Corrupt, err, "响应不能解析为图片"))
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 8_000_000 {
			return fail(apperror.New(apperror.Unsupported, "图片超过 8 百万像素抽样上限，未验证"))
		}
		if _, _, err = image.Decode(bytes.NewReader(raw)); err != nil {
			return fail(apperror.Wrap(apperror.Corrupt, err, "图片数据不完整或解码失败"))
		}
		e.Width, e.Height, e.Format, e.Bytes = cfg.Width, cfg.Height, format, len(raw)
		hash := sha256.Sum256(raw)
		e.Fingerprint = hex.EncodeToString(hash[:])
		return e
	}, transport.CloseIdleConnections
}
