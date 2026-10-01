package updater

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/netstats"
)

// getAppDir 返回当前应用可执行文件所在目录（解析符号链接）
func getAppDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return filepath.Dir(exe)
	}
	return filepath.Dir(exe)
}

// getAppExeName 返回当前应用可执行文件名（不含路径）
func getAppExeName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Base(exe)
}

// uniqueSavePath 已随暂存命名一起移除：产物名现在由 updateStagingName 决定。

// scanAlreadyDownloaded 扫描应用目录，查找已下载的更新包
// 返回 (文件路径, 版本号估计) 或 ("", "") 如果不存在
func scanAlreadyDownloaded() (string, string) {
	appDir := getAppDir()
	exeName := getAppExeName()
	ext := filepath.Ext(exeName)
	baseName := strings.TrimSuffix(exeName, ext)

	// 新命名 CCZJ-Video-Update* 在前，历史命名 {exeBase}_update* 兜底；
	// 两组都限定在应用目录内，与 isUpdateArtifact 的判定保持一致。
	patterns := []string{
		filepath.Join(appDir, stagingPrefix+".*"),
		filepath.Join(appDir, baseName+"_update*"+ext),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() || info.Size() < 1024 {
				continue // 跳过无效/太小的文件
			}
			applog.Info("[Updater] 发现已下载的更新包: %s (%d bytes)", m, info.Size())
			return m, "" // 版本号未知，由前端根据 pendingInfo 判断
		}
	}
	return "", ""
}

// DownloadUpdate 下载更新包到应用同目录，通过回调函数报告进度
// 先并发测速所有下载源（直连+代理），选最快的下载；失败自动回退下一个源
// 返回下载文件的本地路径
func DownloadUpdate(downloadURL string, progress DownloadProgress) (string, error) {
	applog.Info("[Updater] 开始下载: %s", downloadURL)

	// 确定保存路径：应用同目录
	appDir := getAppDir()
	applog.Info("[Updater] 应用目录: %s", appDir)

	// 下载前先要到可信摘要：拿不到就不下载，而不是下完再警告一句。
	expectedSHA, err := requireArtifact(downloadURL)
	if err != nil {
		return "", err
	}

	// 暂存名固定但保留资源的真实扩展名（见 updateStagingName）。
	savePath := filepath.Join(appDir, updateStagingName(downloadURL))
	applog.Info("[Updater] 保存路径: %s", savePath)

	// 如果文件已存在，删除后重新下载
	_ = os.Remove(savePath)

	// ====== 并发测速所有下载源 ======
	applog.Info("[Updater] 开始并发测速所有下载源...")
	if progress != nil {
		progress(0, 0, 0) // 通知前端：开始连接
	}
	candidates := speedTestSources(downloadURL)
	if len(candidates) == 0 {
		return "", apperror.New(apperror.Unavailable, "所有下载源均不可用")
	}
	applog.Info("[Updater] HEAD 测速完成，进行吞吐量测试...")

	// 对前 3 个候选源进行真实吞吐量测试（Range 下载 512KB）
	sources := rankSourcesByThroughput(candidates, 3)
	if len(sources) > 0 {
		applog.Info("[Updater] 吞吐量测试完成，最快源: %s", sources[0].source)
	}

	// ====== 按速度顺序尝试下载 ======
	client := &http.Client{
		Timeout: DownloadTimeout,
		Transport: netstats.WrapTransport(netstats.CategoryUpdate, &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		}),
	}

	for i, src := range sources {
		applog.Info("[Updater] 使用下载源 [%d/%d] %s", i+1, len(sources), src.source)

		req, err := http.NewRequest("GET", src.url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")

		resp, err := client.Do(req)
		if err != nil {
			applog.Warn("[Updater] 源 %s 下载失败: %v", src.source, err)
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
		defer resp.Body.Close()
		total := resp.ContentLength
		if total > 0 {
			applog.Info("[Updater] 文件大小: %d 字节", total)
		}

		out, err := os.Create(savePath)
		if err != nil {
			return "", apperror.Wrap(apperror.Storage, err, "创建文件失败")
		}
		defer out.Close()

		buf := make([]byte, 128*1024)
		var downloaded int64
		lastBytes := int64(0)
		lastTime := time.Now()
		startTime := lastTime

		for {
			if time.Since(startTime) > DownloadTimeout {
				os.Remove(savePath)
				return "", apperror.Newf(apperror.Timeout, "下载超时（超过 %v）", DownloadTimeout)
			}

			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					return "", apperror.Wrap(apperror.Storage, werr, "写入文件失败")
				}
				downloaded += int64(n)

				if time.Since(lastTime) >= time.Second {
					elapsed := time.Since(lastTime).Seconds()
					speed := float64(downloaded-lastBytes) / elapsed
					lastBytes = downloaded
					lastTime = time.Now()
					if progress != nil {
						progress(downloaded, total, speed)
					}
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				os.Remove(savePath)
				return "", apperror.Wrap(apperror.Corrupt, readErr, "读取失败")
			}
		}

		// 先落盘再校验：out 上挂着 defer Close，显式 Close 保证数据已刷出去。
		if cerr := out.Close(); cerr != nil {
			os.Remove(savePath)
			return "", apperror.Wrap(apperror.Storage, cerr, "关闭文件失败")
		}

		// 最终进度回调
		if progress != nil {
			progress(downloaded, total, 0)
		}

		// Content-Length 对不上说明是被截断的响应；原先 EOF 就算成功，
		// 于是半截 exe 会一路走到 InstallUpdate 覆盖掉应用本体。
		if total > 0 && downloaded != total {
			applog.Warn("[Updater] 源 %s 下载不完整: %d/%d", src.source, downloaded, total)
			os.Remove(savePath)
			continue
		}

		if err := verifyArtifact(savePath, expectedSHA); err != nil {
			applog.Error("[Updater] 校验失败，删除产物: %v", err)
			os.Remove(savePath)
			return "", apperror.Wrap(apperror.Corrupt, err, "更新包校验失败")
		}

		applog.Info("[Updater] 下载完成并校验通过: %s (%d 字节) 源: %s", savePath, downloaded, src.source)
		return savePath, nil
	}

	return "", apperror.New(apperror.Unavailable, "所有下载源均失败")
}
