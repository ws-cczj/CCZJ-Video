package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type PlaylistQuality struct {
	OK        bool  `json:"ok"`
	Width     int   `json:"width"`
	Height    int   `json:"height"`
	Bandwidth int64 `json:"bandwidth"`
}

var manifestResolution = regexp.MustCompile(`(?:^|,)RESOLUTION=(\d+)[xX](\d+)(?:,|$)`)
var manifestBandwidth = regexp.MustCompile(`(?:^|,)(?:AVERAGE-)?BANDWIDTH=(\d+)(?:,|$)`)

// ProbePlaylistQuality reads only playlist metadata. A media playlist without
// RESOLUTION is valid but unknown; neither a URL nor a bitrate proves its size.
func ProbePlaylistQuality(ctx context.Context, raw string) (PlaylistQuality, error) {
	ctx, cancel := context.WithTimeout(ctx, lineProbeTimeout)
	defer cancel()
	target, err := parsePublicHTTPURL(raw)
	if err != nil {
		return PlaylistQuality{}, err
	}
	referer := RefererOrigin(raw)
	for hop := 0; hop <= lineMaxRedirects; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return PlaylistQuality{}, err
		}
		req.Header.Set("User-Agent", lineProbeUserAgent)
		req.Header.Set("Accept", "application/vnd.apple.mpegurl,application/x-mpegURL,*/*")
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		resp, err := lineProbeClient.Do(req)
		if err != nil {
			return PlaylistQuality{}, err
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			next, locErr := resp.Location()
			resp.Body.Close()
			if locErr != nil {
				return PlaylistQuality{}, locErr
			}
			referer = RefererOrigin(target.String())
			target, err = parsePublicHTTPURL(next.String())
			if err != nil {
				return PlaylistQuality{}, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return PlaylistQuality{}, fmt.Errorf("playlist HTTP %d", resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		prefix, prefixErr := reader.Peek(512)
		if prefixErr != nil && !errors.Is(prefixErr, io.EOF) {
			resp.Body.Close()
			return PlaylistQuality{}, prefixErr
		}
		if !bytes.HasPrefix(bytes.TrimSpace(bytes.TrimPrefix(prefix, []byte{0xef, 0xbb, 0xbf})), []byte("#EXTM3U")) {
			resp.Body.Close()
			return PlaylistQuality{OK: true}, nil
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, (512<<10)+1))
		resp.Body.Close()
		if readErr != nil {
			return PlaylistQuality{}, readErr
		}
		if len(body) > 512<<10 {
			return PlaylistQuality{}, fmt.Errorf("playlist metadata exceeds 512 KiB")
		}
		result := PlaylistQuality{OK: true}
		for _, line := range strings.Split(string(body), "\n") {
			attributes, ok := strings.CutPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF:")
			if !ok {
				continue
			}
			size := manifestResolution.FindStringSubmatch(attributes)
			if len(size) != 3 {
				continue
			}
			width, _ := strconv.Atoi(size[1])
			height, _ := strconv.Atoi(size[2])
			if width <= 0 || height <= 0 || width > 32768 || height > 32768 {
				continue
			}
			var bandwidth int64
			if match := manifestBandwidth.FindStringSubmatch(attributes); len(match) == 2 {
				if value, parseErr := strconv.ParseInt(match[1], 10, 64); parseErr == nil && value <= 1<<53-1 {
					bandwidth = value
				}
			}
			if height > result.Height || (height == result.Height && width > result.Width) {
				result.Width = width
				result.Height = height
				result.Bandwidth = bandwidth
			}
		}
		return result, nil
	}
	return PlaylistQuality{}, fmt.Errorf("too many playlist redirects")
}
