package updater

import (
	"cczjVideo/app/parallel"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/applog"
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
func speedTestSources(ctx context.Context, client *http.Client, originalURL string) []speedTestResult {
	// 构建所有候选源
	candidates := []struct{ url, source string }{
		{originalURL, "直连GitHub"},
	}
	for _, proxy := range githubProxies {
		candidates = append(candidates, struct{ url, source string }{
			proxy + originalURL, "代理:" + strings.TrimSuffix(strings.TrimPrefix(proxy, "https://"), "/"),
		})
	}

	measured := make([]speedTestResult, len(candidates))
	if err := parallel.Each(ctx, len(candidates), 5, func(ctx context.Context, index int) error {
		candidate := candidates[index]
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		start := time.Now()
		req, err := http.NewRequestWithContext(call, http.MethodHead, candidate.url, nil)
		if err == nil {
			req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")
			var response *http.Response
			response, err = client.Do(req)
			if err == nil {
				response.Body.Close()
				if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
					err = fmt.Errorf("HTTP %d", response.StatusCode)
				}
			}
		}
		measured[index] = speedTestResult{candidate.url, candidate.source, time.Since(start), err}
		return ctx.Err()
	}); err != nil {
		return nil
	}
	results := make([]speedTestResult, 0, len(measured))
	for _, result := range measured {
		if result.err != nil {
			applog.Info("[Updater] 测速 %s 失败: %v", result.source, result.err)
			continue
		}
		applog.Info("[Updater] 测速 %s: %dms", result.source, result.latency.Milliseconds())
		results = append(results, result)
	}

	// 按延迟从快到慢排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})
	return results
}

// downloadSpeedTest 通过 Range 下载一小段数据来测量实际吞吐量
// 返回测量的字节/秒速度，失败返回 0
func downloadSpeedTest(request context.Context, client *http.Client, url string) float64 {
	const testSize = 512 * 1024 // 512KB

	ctx, cancel := context.WithTimeout(request, 15*time.Second)
	defer cancel()

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
func rankSourcesByThroughput(ctx context.Context, client *http.Client, candidates []speedTestResult, maxTest int) []speedTestResult {
	if len(candidates) <= maxTest {
		maxTest = len(candidates)
	}

	bpsMap := make([]float64, maxTest)
	if err := parallel.Each(ctx, maxTest, 3, func(ctx context.Context, index int) error {
		bpsMap[index] = downloadSpeedTest(ctx, client, candidates[index].url)
		return ctx.Err()
	}); err != nil {
		return nil
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
