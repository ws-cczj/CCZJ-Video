package updater

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/netstats"
)

// DownloadProgress 下载进度回调
type DownloadProgress func(downloaded, total int64, speedBps float64)

// speedTestResult 速度测试结果
type speedTestResult struct {
	url     string
	source  string
	latency time.Duration
	err     error
}

// speedTestSources 并发测试所有下载源的连接延迟（HEAD 请求）
// 返回按延迟从快到慢排序的候选列表，跳过测试失败的源
func speedTestSources(originalURL string) []speedTestResult {
	// 构建所有候选源
	candidates := []struct{ url, source string }{
		{originalURL, "直连GitHub"},
	}
	for _, proxy := range githubProxies {
		candidates = append(candidates, struct{ url, source string }{
			proxy + originalURL, "代理:" + strings.TrimSuffix(strings.TrimPrefix(proxy, "https://"), "/"),
		})
	}

	type result struct {
		url, source string
		latency     time.Duration
		err         error
	}
	resultsCh := make(chan result, len(candidates))

	for _, c := range candidates {
		go func(url, source string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client := &http.Client{
				Timeout: 12 * time.Second,
				Transport: netstats.WrapTransport(netstats.CategoryUpdate, &http.Transport{
					TLSHandshakeTimeout:   8 * time.Second,
					ResponseHeaderTimeout: 10 * time.Second,
				}),
			}

			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
			if err != nil {
				resultsCh <- result{url, source, 0, err}
				return
			}
			req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")

			resp, err := client.Do(req)
			latency := time.Since(start)
			if err != nil {
				resultsCh <- result{url, source, latency, err}
				return
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
				resultsCh <- result{url, source, latency, fmt.Errorf("HTTP %d", resp.StatusCode)}
				return
			}
			resultsCh <- result{url, source, latency, nil}
		}(c.url, c.source)
	}

	// 收集所有结果
	var results []speedTestResult
	for i := 0; i < len(candidates); i++ {
		r := <-resultsCh
		if r.err != nil {
			applog.Info("[Updater] 测速 %s 失败: %v", r.source, r.err)
			continue
		}
		applog.Info("[Updater] 测速 %s: %dms", r.source, r.latency.Milliseconds())
		results = append(results, speedTestResult{r.url, r.source, r.latency, nil})
	}

	// 按延迟从快到慢排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})
	return results
}

// downloadSpeedTest 通过 Range 下载一小段数据来测量实际吞吐量
// 返回测量的字节/秒速度，失败返回 0
func downloadSpeedTest(url string) float64 {
	const testSize = 512 * 1024 // 512KB

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := &http.Client{
		Timeout: 18 * time.Second,
		// 512KB 的 Range 取样是本项目里最接近「带宽实测」的一次请求，
		// 它的字节数与耗时进诊断页的吞吐表，比任何估算都有说服力。
		Transport: netstats.WrapTransport(netstats.CategoryUpdate, &http.Transport{
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		}),
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", testSize-1))

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		// 服务器不支持 Range，无法测试吞吐量
		return 0
	}

	// 丢弃读取的数据（只测速率）
	var totalRead int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		totalRead += int64(n)
		if readErr != nil {
			break
		}
		if totalRead >= testSize {
			break
		}
	}

	elapsed := time.Since(start).Seconds()
	if elapsed < 0.01 {
		elapsed = 0.01
	}

	bps := float64(totalRead) / elapsed
	applog.Info("[Updater] 吞吐量测试: %.1f KB/s (%.1f KB / %.2fs)", bps/1024, float64(totalRead)/1024, elapsed)
	return bps
}

// rankSourcesByThroughput 对候选源进行真实吞吐量测试，按下载速度排序
// 仅测试前 maxTest 个候选源（HEAD 延迟排序后的前 N 个），避免并发抢占带宽
func rankSourcesByThroughput(candidates []speedTestResult, maxTest int) []speedTestResult {
	if len(candidates) <= maxTest {
		maxTest = len(candidates)
	}

	type throughputResult struct {
		idx int
		bps float64
	}
	resultsCh := make(chan throughputResult, maxTest)

	for i := 0; i < maxTest; i++ {
		go func(idx int, url string) {
			bps := downloadSpeedTest(url)
			resultsCh <- throughputResult{idx, bps}
		}(i, candidates[i].url)
	}

	// 收集吞吐量结果
	bpsMap := make(map[int]float64, maxTest)
	for i := 0; i < maxTest; i++ {
		r := <-resultsCh
		bpsMap[r.idx] = r.bps
	}

	// 构建带吞吐量值的结果列表
	type rankedSource struct {
		speedTestResult
		bps float64
	}
	var ranked []rankedSource
	for i := 0; i < maxTest; i++ {
		c := candidates[i]
		bps := bpsMap[i]
		ranked = append(ranked, rankedSource{c, bps})
		if bps <= 0 {
			applog.Info("[Updater] 吞吐量 %s: 失败", c.source)
		} else {
			applog.Info("[Updater] 吞吐量 %s: %.1f KB/s", c.source, bps/1024)
		}
	}

	// 未测试的候选源追加到末尾（bps=0，保持 HEAD 排序）
	for i := maxTest; i < len(candidates); i++ {
		ranked = append(ranked, rankedSource{candidates[i], 0})
	}

	// 按吞吐量降序排序（速度最快的排前面，未测试的保持 HEAD 排序）
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].bps > ranked[j].bps
	})

	// 提取 speedTestResult 切片
	result := make([]speedTestResult, len(ranked))
	for i, r := range ranked {
		result[i] = r.speedTestResult
	}
	return result
}
