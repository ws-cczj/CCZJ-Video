package proxy

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	posterCardWidth      = 480
	posterCardHeight     = 720
	posterCardDecodeArea = 8_000_000
)

// ServePosterCard serves the list-sized representation of a video poster.
func (s *Service) ServePosterCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rawURL := r.URL.Query().Get("u")
	if _, err := parsePublicHTTPURL(rawURL); len(rawURL) > 8192 || err != nil {
		http.Error(w, "invalid poster URL", http.StatusBadRequest)
		return
	}
	data, contentType, err := s.posterCardBytes(r.Context(), rawURL)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		http.Error(w, "poster unavailable", http.StatusBadGateway)
		return
	}
	if contentType == "" {
		http.Error(w, "poster format unavailable", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	serveCachedImage(w, r, data, contentType)
}

func (s *Service) posterCardBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	// The original and derivative share the disk budget while retaining their
	// own identities. A URL cannot contain the NUL used by the derivative key.
	key := "\x00poster-card-v1:" + rawURL
	if data, contentType, ok := s.images.read(key); ok {
		return data, contentType, nil
	}
	data, _, err := s.imageBytes(ctx, rawURL)
	if err != nil {
		return nil, "", err
	}
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case s.posterResize <- struct{}{}:
	}
	defer func() { <-s.posterResize }()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if cached, contentType, ok := s.images.read(key); ok {
		return cached, contentType, nil
	}
	resized, contentType := resizePosterCard(data)
	if resized == nil {
		// Only raster images may be served on the application origin. Other
		// formats retain the isolated data-URL path in the image component.
		switch originalType := http.DetectContentType(data); originalType {
		case "image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp", "image/x-icon":
			return data, originalType, nil
		default:
			return nil, "", nil
		}
	}
	if ctx.Err() == nil {
		s.images.write(key, resized, contentType)
	}
	return resized, contentType, nil
}

func resizePosterCard(data []byte) ([]byte, string) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > posterCardDecodeArea/config.Height {
		return nil, ""
	}
	switch format {
	case "jpeg":
		// The browser applies embedded orientation and color metadata to the original.
		if bytes.Contains(data, []byte("Exif\x00\x00")) || bytes.Contains(data, []byte("ICC_PROFILE\x00")) {
			return nil, ""
		}
	case "png":
		if bytes.Contains(data, []byte("acTL")) || bytes.Contains(data, []byte("eXIf")) || bytes.Contains(data, []byte("iCCP")) {
			return nil, ""
		}
	case "webp":
		const metadataOrAnimation = 0x20 | 0x08 | 0x02 // ICC, EXIF, animation
		if len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&metadataOrAnimation != 0 {
			return nil, ""
		}
	default:
		return nil, ""
	}
	scale := math.Min(float64(posterCardWidth)/float64(config.Width), float64(posterCardHeight)/float64(config.Height))
	if scale >= 1 {
		return nil, ""
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ""
	}
	width := max(1, int(math.Round(float64(config.Width)*scale)))
	height := max(1, int(math.Round(float64(config.Height)*scale)))
	thumb := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if thumb.Opaque() {
		if jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 88}) == nil {
			return out.Bytes(), "image/jpeg"
		}
	} else if png.Encode(&out, thumb) == nil {
		return out.Bytes(), "image/png"
	}
	return nil, ""
}
