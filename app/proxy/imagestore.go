package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
)

const (
	// imageCacheTTL 是「一张海报在磁盘上还直接端得出去」的年龄上限，按最后被看到的时刻算：
	// 天天出现在首页的海报不会过期，再也没人看的才让位。7 天跟热榜快照的 chartStaleCeiling
	// 同一档——海报 URL 由源站给出，换封面就换 URL，所以过期的代价只是重抓一次。
	imageCacheTTL = 7 * 24 * time.Hour
	// imageCacheMaxBytes 是整目录的上限。海报一张约 20KB，80MB 装得下几千张，
	// 而数据目录在系统盘上，上限的意义是别把它堆成第二个视频缓存。
	imageCacheMaxBytes = 80 << 20
	// imageCachePruneGap 限制扫目录的频率：淘汰只在两次写入之间隔够时间后才跑一次。
	imageCachePruneGap = 10 * time.Minute
	// imageFailureTTL 是抓失败的冷却。源站 CDN 挂了或防盗链收紧时，一张取不到的图会被
	// 每个渲染它的 <img> 反复要一遍，没有冷却就是一次开屏一轮无用请求。
	imageFailureTTL = 5 * time.Minute
)

// imageStore 是海报的磁盘副本，键是前端给的那个 URL 的 sha256。
//
// 为什么这一层要在 Go 而不是前端：前端那层缓存装在 localStorage 里，塞的是 data URL，
// 20KB 的图编码成 base64 就是 25K 个字符，80 条已经吃掉浏览器配额的两成，所以容量
// 不敢再加（frontend/src/utils/index.ts 的 IMAGE_PROXY_MAX）。而首页轮播 + 继续观看 +
// 相关推荐一屏就超过 80 张，结果是最先缓存的那批被 LRU 挤掉，下次进同一页重新出网——
// 30 天的 TTL 根本轮不到生效。磁盘没有这个天花板，也就不用再拿容量换寿命。
//
// ttl 与 maxBytes 是字段而不是常量，为的是让淘汰分支能在单测里跑到：把这两个值调小，
// 才能在不写 80MB、不等 7 天的前提下证明「该删的删了、不该删的还在」。
type imageStore struct {
	dirFn func() string

	ttl      time.Duration
	maxBytes int64

	mu        sync.Mutex
	lastPrune time.Time
	failures  map[string]time.Time
}

func newImageStore(dirFn func() string) *imageStore {
	return &imageStore{dirFn: dirFn, ttl: imageCacheTTL, maxBytes: imageCacheMaxBytes, failures: map[string]time.Time{}}
}

func (s *imageStore) enabled() bool { return s != nil && s.dirFn != nil }

func (s *imageStore) pathFor(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	hexKey := hex.EncodeToString(sum[:])
	return filepath.Join(s.dirFn(), "imagecache", hexKey[:2], hexKey)
}

// read 取回一份还能用的海报。命中会把 mtime 推到当下：淘汰判定读的就是这个时间，
// 于是「最近 7 天被看到过」等价于「不会被删」，天天看的图不必反复重抓。
func (s *imageStore) read(rawURL string) ([]byte, string, bool) {
	if !s.enabled() {
		return nil, "", false
	}
	path := s.pathFor(rawURL)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > s.ttl {
		return nil, "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	contentType, data, ok := splitImageBlob(raw)
	if !ok {
		// 写坏或被截断的一份：丢掉它，让调用方去抓一次，而不是把坏图一直端下去。
		_ = os.Remove(path)
		return nil, "", false
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return data, contentType, true
}

func (s *imageStore) write(rawURL string, data []byte, contentType string) {
	if !s.enabled() {
		return
	}
	path := s.pathFor(rawURL)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		applog.Warn("[ImageCache] 建目录失败: %v", err)
		return
	}
	// 先写临时文件再改名：同一个 URL 会被多个请求同时要（首页和详情页可能撞在一起），
	// 直接覆写会让后到的读者看见半张图，而一张截断的 JPEG 在界面上就是永久坏图。
	tmp, err := os.CreateTemp(filepath.Dir(path), "part-")
	if err != nil {
		applog.Warn("[ImageCache] 建临时文件失败: %v", err)
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(append([]byte(contentType+"\n"), data...)); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(name, path); err != nil {
		applog.Warn("[ImageCache] 落盘失败: %v", err)
		return
	}
	s.pruneIfNeeded()
}

// noteFailure / allow 实现抓失败的冷却，与前端 imageProxyFallbackCache 同一套判定，
// 放在 Go 侧是因为现在出网与否由这里决定，冷却只有在这一层才拦得住请求。
func (s *imageStore) allow(rawURL string) bool {
	if !s.enabled() {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	failedAt, ok := s.failures[rawURL]
	if !ok {
		return true
	}
	if time.Since(failedAt) > imageFailureTTL {
		delete(s.failures, rawURL)
		return true
	}
	return false
}

func (s *imageStore) noteFailure(rawURL string) {
	if !s.enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 512 {
		s.failures = map[string]time.Time{}
	}
	s.failures[rawURL] = time.Now()
}

// pruneIfNeeded 删掉「超过 7 天没被看到」和超出容量上限的部分。
// 上限按最久未看先删：这条序和 read 推 mtime 的语义是同一套，所以留下的一定还是会被看的。
func (s *imageStore) pruneIfNeeded() {
	if !s.enabled() {
		return
	}
	s.mu.Lock()
	if !s.lastPrune.IsZero() && time.Since(s.lastPrune) < imageCachePruneGap {
		s.mu.Unlock()
		return
	}
	s.lastPrune = time.Now()
	s.mu.Unlock()

	root := filepath.Join(s.dirFn(), "imagecache")
	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var entries []entry
	var total int64
	cutoff := time.Now().Add(-s.ttl)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
			return nil
		}
		entries = append(entries, entry{path: path, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
		return nil
	})
	if total <= s.maxBytes {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
	for _, e := range entries {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(e.path); err == nil {
			total -= e.size
		}
	}
	applog.Debug("[ImageCache] 淘汰后占用 %d 张 / %.1f MB", len(entries), float64(total)/(1<<20))
}

// splitImageBlob 拆出首行的 Content-Type 和其余的图片字节。
//
// 类型单独存一行，是为了不必从字节猜（http.DetectContentType 认不出 SVG，
// 猜错就是一份永远显示不出来的图），也不必给每张图多配一个 meta 文件。
func splitImageBlob(raw []byte) (string, []byte, bool) {
	i := bytes.IndexByte(raw, '\n')
	if i <= 0 || i == len(raw)-1 {
		return "", nil, false
	}
	contentType := strings.TrimSpace(string(raw[:i]))
	if !strings.HasPrefix(contentType, "image/") {
		return "", nil, false
	}
	return contentType, raw[i+1:], true
}
