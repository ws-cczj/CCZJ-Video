package updater

import (
	"cczjVideo/app/netbudget"
	"cczjVideo/app/netpolicy"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/netstats"
)

var mobileUpdateDirectory atomic.Value

// Downloader owns the HTTP transport shared by checksum, probes and package I/O.
// Its budget is supplied by application assembly, including the response bodies.
type Downloader struct{ budget *netbudget.Budget }

func NewDownloader(budget *netbudget.Budget) *Downloader { return &Downloader{budget: budget} }

func (d *Downloader) client() *http.Client {
	transport := netpolicy.WrapTransport("update", netstats.WrapTransport(netstats.CategoryUpdate, &http.Transport{MaxIdleConns: 10, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 15 * time.Second}))
	if d.budget != nil {
		transport = d.budget.Wrap("update", transport)
	}
	return &http.Client{Timeout: DownloadTimeout, Transport: transport}
}

// SetMobileUpdateDirectory is supplied by application assembly before requests.
func SetMobileUpdateDirectory(directory func() string) { mobileUpdateDirectory.Store(directory) }

// getAppDir resolves the legacy executable-adjacent desktop location.
func getAppDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return filepath.Dir(exe)
	}
	return filepath.Dir(resolved)
}

func legacyUpdateDirectory() string {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		return ""
	}
	return getAppDir()
}

// getAppExeName 返回当前应用可执行文件名（不含路径）
func getAppExeName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Base(exe)
}

// scanAlreadyDownloaded 扫描应用目录，查找已下载的更新包
// 只恢复指定版本的已校验产物，不把旧包或当前程序本身当成新版。
func scanAlreadyDownloaded(version string) (string, string) {
	if dir, err := updateDirectory(); err == nil {
		if path, ver := scanDownloadedArtifacts(dir, getAppExeName(), version); path != "" {
			return path, ver
		}
	}
	if legacy := legacyUpdateDirectory(); legacy != "" {
		return scanDownloadedArtifacts(legacy, getAppExeName(), version)
	}
	return "", ""
}

func scanDownloadedArtifacts(appDir, exeName, version string) (string, string) {
	if version == "" {
		return "", ""
	}
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return "", ""
	}
	prefix := stagingPrefix + "." + version + "."
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, prefix) || normalizeCase(name) == normalizeCase(exeName) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == digestSuffix || ext == ".part" {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() < 1024 {
			continue
		}
		path := filepath.Join(appDir, name)
		// The downloader can be between rename and receipt creation.
		if !hasDigestRecord(path) {
			continue
		}
		applog.Info("[Updater] 发现已下载的更新包: %s (%d bytes)", path, info.Size())
		return path, version
	}
	return "", ""
}

// DownloadUpdate 下载到平台的更新目录，通过回调函数报告进度
// 先并发测速所有下载源（直连+代理），选最快的下载；失败自动回退下一个源
// 返回下载文件的本地路径
func DownloadUpdate(downloadURL string, progress DownloadProgress) (string, error) {
	return NewDownloader(nil).DownloadContext(context.Background(), downloadURL, progress)
}

// DownloadContext keeps every phase under the caller's lifetime and waits for probes.
func (d *Downloader) DownloadContext(ctx context.Context, downloadURL string, progress DownloadProgress) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	client := d.client()
	defer client.CloseIdleConnections()
	applog.Info("[Updater] 开始下载: %s", downloadURL)

	appDir, err := updateDirectory()
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "无法定位更新下载目录")
	}
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "无法创建更新下载目录")
	}
	applog.Info("[Updater] 应用目录: %s", appDir)

	// 下载前先要到可信摘要：拿不到就不下载，而不是下完再警告一句。
	expectedSHA, err := requireArtifact(ctx, client, downloadURL)
	if err != nil {
		return "", err
	}

	// ====== 并发测速所有下载源 ======
	applog.Info("[Updater] 开始并发测速所有下载源...")
	if progress != nil {
		progress(0, 0, 0) // 通知前端：开始连接
	}
	candidates := speedTestSources(ctx, client, downloadURL)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", apperror.New(apperror.Unavailable, "所有下载源均不可用")
	}
	applog.Info("[Updater] HEAD 测速完成，进行吞吐量测试...")

	// 对前 3 个候选源进行真实吞吐量测试（Range 下载 512KB）
	sources := rankSourcesByThroughput(ctx, client, candidates, 3)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(sources) > 0 {
		applog.Info("[Updater] 吞吐量测试完成，最快源: %s", sources[0].source)
	}

	// ====== 按速度顺序尝试下载 ======
	return downloadFromSources(ctx, appDir, downloadURL, expectedSHA, sources, client, progress)
}

// downloadFromSources writes and verifies a package after checksum and source selection.
func downloadFromSources(ctx context.Context, appDir, downloadURL, expectedSHA string, sources []speedTestResult, client *http.Client, progress DownloadProgress) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var lastErr error
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		applog.Info("[Updater] 使用下载源 [%d/%d] %s", i+1, len(sources), src.source)

		req, err := http.NewRequestWithContext(ctx, "GET", src.url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")

		resp, err := client.Do(req)
		if err != nil {
			applog.Warn("[Updater] 源 %s 下载失败: %v", src.source, err)
			lastErr = apperror.Wrap(apperror.Unavailable, err, "下载源连接失败")
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			applog.Warn("[Updater] 源 %s HTTP %d", src.source, resp.StatusCode)
			continue
		}
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			resp.Body.Close()
			applog.Warn("[Updater] source %s returned HTML instead of an update package", src.source)
			continue
		}

		// 成功连接，开始下载
		total := resp.ContentLength
		if total > 0 {
			applog.Info("[Updater] 文件大小: %d 字节", total)
		}

		// Never truncate a previous package: it may be running, scheduled for
		// installation, or held by OneDrive. Partial bytes are not discoverable.
		_, _, tag, _, _ := parseReleaseAsset(downloadURL)
		version := extractSemver(tag)
		out, err := os.CreateTemp(appDir, stagingPrefix+"."+version+".*.part")
		if err != nil {
			resp.Body.Close()
			return "", apperror.Wrap(apperror.Storage, err, "创建文件失败")
		}
		partialPath := out.Name()
		downloaded, copyErr := copyUpdateResponse(ctx, out, resp, progress)
		closeErr := out.Close()
		resp.Body.Close()
		if copyErr != nil {
			_ = os.Remove(partialPath)
			if err := ctx.Err(); err != nil {
				return "", err
			}
			applog.Warn("[Updater] 源 %s 下载失败: %v", src.source, copyErr)
			if apperror.CodeOf(copyErr) == apperror.Storage {
				return "", copyErr
			}
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			_ = os.Remove(partialPath)
			return "", apperror.Wrap(apperror.Storage, closeErr, "关闭文件失败")
		}

		// Content-Length 对不上说明是被截断的响应；原先 EOF 就算成功，
		// 于是半截 exe 会一路走到 InstallUpdate 覆盖掉应用本体。
		if total > 0 && downloaded != total {
			applog.Warn("[Updater] 源 %s 下载不完整: %d/%d", src.source, downloaded, total)
			_ = os.Remove(partialPath)
			lastErr = apperror.New(apperror.Corrupt, "更新包下载不完整")
			continue
		}

		if err := verifyArtifactContext(ctx, partialPath, expectedSHA); err != nil {
			applog.Error("[Updater] 校验失败，删除产物: %v", err)
			_ = os.Remove(partialPath)
			lastErr = apperror.Wrap(apperror.Corrupt, err, "更新包校验失败")
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = os.Remove(partialPath)
			return "", err
		}
		savePath := strings.TrimSuffix(partialPath, ".part") + filepath.Ext(updateStagingName(downloadURL))
		if err := os.Rename(partialPath, savePath); err != nil {
			_ = os.Remove(partialPath)
			return "", apperror.Wrap(apperror.Storage, err, "保存已校验更新包失败")
		}

		// 校验通过的这一刻才记下凭据：InstallUpdate 装之前会拿它再哈希一次，
		// "已下载"入口也只认带凭据的产物。
		if err := recordDigest(savePath, expectedSHA); err != nil {
			_ = os.Remove(savePath)
			clearDigestRecord(savePath)
			return "", apperror.Wrap(apperror.Storage, err, "记录更新包摘要失败")
		}

		applog.Info("[Updater] 下载完成并校验通过: %s (%d 字节) 源: %s", savePath, downloaded, src.source)
		return savePath, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", apperror.New(apperror.Unavailable, "所有下载源均失败")
}

func copyUpdateResponse(ctx context.Context, out io.Writer, resp *http.Response, progress DownloadProgress) (int64, error) {
	total := resp.ContentLength
	if progress != nil {
		progress(0, total, 0)
	}
	buf := make([]byte, 128*1024)
	var downloaded, lastBytes int64
	lastTime := time.Now()
	startTime := lastTime
	for {
		if err := ctx.Err(); err != nil {
			return downloaded, err
		}
		if time.Since(startTime) > DownloadTimeout {
			return downloaded, apperror.Newf(apperror.Timeout, "下载超时（超过 %v）", DownloadTimeout)
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, err := out.Write(buf[:n])
			if err != nil {
				return downloaded, apperror.Wrap(apperror.Storage, err, "写入文件失败")
			}
			if written != n {
				return downloaded, apperror.Wrap(apperror.Storage, io.ErrShortWrite, "写入文件失败")
			}
			downloaded += int64(n)
			if time.Since(lastTime) >= 250*time.Millisecond {
				speed := float64(downloaded-lastBytes) / time.Since(lastTime).Seconds()
				lastBytes, lastTime = downloaded, time.Now()
				if progress != nil {
					progress(downloaded, total, speed)
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return downloaded, apperror.Wrap(apperror.Corrupt, readErr, "读取失败")
			}
			if progress != nil {
				progress(downloaded, total, 0)
			}
			return downloaded, nil
		}
	}
}
