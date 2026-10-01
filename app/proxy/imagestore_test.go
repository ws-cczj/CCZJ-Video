package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// stubImages 让 Service 的出网路径返回固定图片，并数出真的发了几次请求。
// 计数就是这些测试的断言对象：磁盘缓存有没有用，只看「第二次还在不在出网」。
func stubImages(t *testing.T, service *Service, payload []byte, failures int) *int {
	t.Helper()
	requests := 0
	service.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests <= failures {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("boom")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	})
	return &requests
}

func TestImageContextFetchesOnceThenServesFromDisk(t *testing.T) {
	dir := t.TempDir()
	service := NewService(func() string { return dir })
	payload := []byte{0xff, 0xd8, 0xff, 0xe0, 'p', 'o', 's', 't', 'e', 'r'}
	requests := stubImages(t, service, payload, 0)

	const rawURL = "https://1.1.1.1/upload/poster.jpg"
	first, err := service.ImageContext(context.Background(), rawURL)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if *requests != 1 {
		t.Fatalf("requests = %d, want 1", *requests)
	}

	second, err := service.ImageContext(context.Background(), rawURL)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if *requests != 1 {
		t.Fatalf("命中磁盘缓存却还是出了网: requests=%d", *requests)
	}
	if first != second {
		t.Fatal("磁盘副本和首次抓到的内容不一致")
	}
	if !strings.HasPrefix(second, "data:image/jpeg;base64,") {
		t.Fatalf("返回的不是带类型的 data URL: %.40s", second)
	}
}

// TestImageContextKeepsPosterURLAsKey 盯豆瓣那条改写：请求发出去的是 l_ratio_poster，
// 但记账必须落在前端给的原 URL 上，否则同一张海报换个尺寸就是一份新缓存。
func TestImageContextKeepsPosterURLAsKey(t *testing.T) {
	dir := t.TempDir()
	service := NewService(func() string { return dir })
	requests := stubImages(t, service, []byte("jpeg-bytes"), 0)

	const rawURL = "https://1.1.1.1/s_ratio_poster/public/p123.jpg"
	if _, err := service.ImageContext(context.Background(), rawURL); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := service.ImageContext(context.Background(), rawURL); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if *requests != 1 {
		t.Fatalf("改写过的 URL 没落在同一个缓存键上: requests=%d", *requests)
	}
}

func TestImageContextCoolsDownAfterFailure(t *testing.T) {
	service := NewService(func() string { return t.TempDir() })
	requests := stubImages(t, service, []byte("jpeg-bytes"), 1)

	const rawURL = "https://1.1.1.1/broken.jpg"
	if _, err := service.ImageContext(context.Background(), rawURL); err == nil {
		t.Fatal("expected the failed fetch to surface")
	}
	if _, err := service.ImageContext(context.Background(), rawURL); err == nil {
		t.Fatal("expected the cooldown to reject the second attempt")
	}
	if *requests != 1 {
		t.Fatalf("取不到的图被反复重抓: requests=%d, want 1", *requests)
	}
}

func TestImageStoreDropsExpiredEntry(t *testing.T) {
	dir := t.TempDir()
	store := newImageStore(func() string { return dir })
	store.ttl = time.Hour
	store.write("https://1.1.1.1/old.jpg", []byte("bytes"), "image/jpeg")

	path := store.pathFor("https://1.1.1.1/old.jpg")
	ancient := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := store.read("https://1.1.1.1/old.jpg"); ok {
		t.Fatal("过了一份 TTL 的条目还能命中")
	}

	// 淘汰只在写入时顺带跑，所以先撤掉节流，模拟「下一次有图要落盘」。
	store.lastPrune = time.Time{}
	store.pruneIfNeeded()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("过期条目没有被删掉")
	}
}

// TestImageStoreReadKeepsEntryAlive 是「TTL 7 天」的语义本身：判定读的是最后被看到的
// 时刻，所以天天出现在首页的海报不会被删，只有真没人看的才让位。
func TestImageStoreReadKeepsEntryAlive(t *testing.T) {
	dir := t.TempDir()
	store := newImageStore(func() string { return dir })
	store.ttl = time.Hour
	store.write("https://1.1.1.1/seen.jpg", []byte("bytes"), "image/jpeg")

	path := store.pathFor("https://1.1.1.1/seen.jpg")
	stale := time.Now().Add(-59 * time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := store.read("https://1.1.1.1/seen.jpg"); !ok {
		t.Fatal("还没过期的条目读不到")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > time.Minute {
		t.Fatal("读命中没有把 mtime 推到当下，天天看的图会被当成没人看的删掉")
	}
}

func TestImageStorePrunesOldestFirstToCap(t *testing.T) {
	dir := t.TempDir()
	store := newImageStore(func() string { return dir })
	store.ttl = 24 * time.Hour
	urls := []string{"https://1.1.1.1/a.jpg", "https://1.1.1.1/b.jpg", "https://1.1.1.1/c.jpg"}
	for i, rawURL := range urls {
		store.write(rawURL, bytes.Repeat([]byte("x"), 40), "image/jpeg")
		// 三份内容一样长，只能靠 mtime 排出先后：a 最久没被看到。
		when := time.Now().Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(store.pathFor(rawURL), when, when); err != nil {
			t.Fatal(err)
		}
	}

	// 40 字节 + 一行类型 ≈ 52，上限压到 110 就只留得下最年轻的两份。
	store.maxBytes = 110
	store.lastPrune = time.Time{}
	store.pruneIfNeeded()

	if _, err := os.Stat(store.pathFor(urls[0])); !os.IsNotExist(err) {
		t.Fatal("最久未看的条目没有被淘汰")
	}
	for _, rawURL := range urls[1:] {
		if _, err := os.Stat(store.pathFor(rawURL)); err != nil {
			t.Fatalf("还该留下的条目被删了 %s: %v", rawURL, err)
		}
	}
}

func TestImageStoreTreatsCorruptBlobAsMiss(t *testing.T) {
	dir := t.TempDir()
	store := newImageStore(func() string { return dir })
	const rawURL = "https://1.1.1.1/truncated.jpg"
	store.write(rawURL, []byte("bytes"), "image/jpeg")

	// 模拟一次没写完的落盘：改名前进程被杀，留下一个只有类型行没有内容的文件。
	if err := os.WriteFile(store.pathFor(rawURL), []byte("image/jpeg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := store.read(rawURL); ok {
		t.Fatal("坏图还能命中")
	}
	if _, err := os.Stat(store.pathFor(rawURL)); !os.IsNotExist(err) {
		t.Fatal("坏图没有被清掉，会一直端给每一个 <img>")
	}
}

func TestImageStoreDisabledWithoutDirectory(t *testing.T) {
	service := NewService(nil)
	if _, _, ok := service.images.read("https://1.1.1.1/x.jpg"); ok {
		t.Fatal("没有目录时不该读到任何东西")
	}
	service.images.write("https://1.1.1.1/x.jpg", []byte("bytes"), "image/jpeg")
	if !service.images.allow("https://1.1.1.1/x.jpg") {
		t.Fatal("没有目录时冷却也不该拦路")
	}
}
