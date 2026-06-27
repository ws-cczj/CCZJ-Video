package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/applog"
)

// Version 当前应用版本号
const Version = "1.1.0"

// semverRegex 用于从 tag 名称中提取语义化版本号（如 release-v1.0.0 → 1.0.0）
var semverRegex = regexp.MustCompile(`(\d+\.\d+\.\d+(?:-[\w.]+)?)`)

// extractSemver 从任意字符串中提取语义化版本号
func extractSemver(s string) string {
	m := semverRegex.FindString(s)
	return m
}

// GitHub 仓库信息
const (
	repoOwner = "ws-cczj"
	repoName  = "CCZJ-Video"
	// DownloadTimeout 下载超时时间（参考 lx-music-desktop 的 60 分钟）
	DownloadTimeout = 60 * time.Minute
)

// GitHub 代理列表（按优先级排序）
var githubProxies = []string{
	"https://gh-proxy.org/",
	"https://v4.gh-proxy.org/",
	"https://v6.gh-proxy.org/",
}

// maxRetryPerSource 每个源的最大重试次数（参考 lx-music-desktop 的 3 次重试）
const maxRetryPerSource = 3

// versionInfoCache 版本信息缓存（参考 lx-music-desktop 的 versionInfo.resolved）
// 避免短时间内重复请求，reCheck 为 true 时强制刷新
var (
	versionInfoCache     *VersionInfo
	versionInfoCacheTime time.Time
)

// versionInfoCacheTTL 缓存有效期
const versionInfoCacheTTL = 5 * time.Minute

// versionInfoSources 多渠道版本信息获取源（参考 lx-music-desktop 设计）
// 按优先级排列，每个源失败后自动尝试下一个
type versionInfoSource struct {
	url    string
	source string // "github_api" | "github_raw" | "jsdelivr" | "gitee"
}

var versionInfoSources = []versionInfoSource{
	{fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName), "github_api"},
	{fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/version.json", repoOwner, repoName), "github_raw"},
	{fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s/%s@main/version.json", repoOwner, repoName), "jsdelivr"},
	{fmt.Sprintf("https://fastly.jsdelivr.net/gh/%s/%s@main/version.json", repoOwner, repoName), "jsdelivr"},
	{fmt.Sprintf("https://gcore.jsdelivr.net/gh/%s/%s@main/version.json", repoOwner, repoName), "jsdelivr"},
	{fmt.Sprintf("https://gitee.com/%s/%s/raw/main/version.json", repoOwner, repoName), "gitee"},
}

// VersionInfo 表示 version.json 的结构（多渠道回退用）
type VersionInfo struct {
	Version string        `json:"version"`
	Desc    string        `json:"desc"`
	History []VersionItem `json:"history,omitempty"`
}

// VersionItem 历史版本项
type VersionItem struct {
	Version string `json:"version"`
	Desc    string `json:"desc"`
}

// GitHubRelease 表示 GitHub Release API 返回的 JSON 结构
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset 表示 Release 中的附件
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// UpdateInfo 更新信息（返回给前端）
type UpdateInfo struct {
	HasUpdate     bool          `json:"has_update"`
	CurrentVer    string        `json:"current_version"`
	LatestVer     string        `json:"latest_version"`
	ReleaseName   string        `json:"release_name"`
	ReleaseNotes  string        `json:"release_notes"`
	DownloadURL   string        `json:"download_url"`
	AssetName     string        `json:"asset_name"`
	AssetSize     int64         `json:"asset_size"`
	PublishedAt   string        `json:"published_at"`
	History       []VersionItem `json:"history,omitempty"` // 从当前版本到最新版本之间的所有历史版本
}

// CheckUpdate 检查 GitHub 是否有新版本
// 多渠道回退策略：GitHub API → 代理 → 多源 version.json（参考 lx-music-desktop）
// reCheck 为 true 时强制刷新缓存
func CheckUpdate(reCheck bool) (*UpdateInfo, error) {
	// Windows ARM 架构：如果没有提供 ARM 包，跳过更新检查（参考 lx-music-desktop）
	if runtime.GOOS == "windows" && strings.Contains(runtime.GOARCH, "arm") {
		applog.Info("[Updater] Windows ARM 架构，跳过更新检查（暂无 ARM 安装包）")
		return &UpdateInfo{
			HasUpdate:  false,
			CurrentVer: Version,
			LatestVer:  Version,
		}, nil
	}

	// 策略一：优先使用 GitHub Release API（可直接获取下载资源，不缓存）
	release, err := fetchReleaseFromSources()
	if err == nil && release != nil {
		return buildUpdateInfoFromRelease(release), nil
	}

	// 策略二：通过多渠道 version.json 获取版本信息（支持缓存）
	applog.Info("[Updater] GitHub API 获取失败，尝试多渠道 version.json")

	// 如果有缓存且未过期且不需要强制刷新，直接使用缓存
	if !reCheck && versionInfoCache != nil && time.Since(versionInfoCacheTime) < versionInfoCacheTTL {
		applog.Info("[Updater] 使用缓存的版本信息（缓存时间: %v）", time.Since(versionInfoCacheTime))
		return buildUpdateInfoFromVersionInfo(versionInfoCache), nil
	}

	verInfo, verErr := fetchVersionInfoFromSources()
	if verErr != nil {
		// 如果获取失败但有缓存，使用过期缓存作为兜底
		if versionInfoCache != nil {
			applog.Warn("[Updater] 所有源均失败，使用过期缓存作为兜底")
			return buildUpdateInfoFromVersionInfo(versionInfoCache), nil
		}
		return nil, fmt.Errorf("所有更新源均失败: %w", verErr)
	}

	// 更新缓存
	versionInfoCache = verInfo
	versionInfoCacheTime = time.Now()

	return buildUpdateInfoFromVersionInfo(verInfo), nil
}

// buildUpdateInfoFromVersionInfo 从 VersionInfo（version.json）构建 UpdateInfo
func buildUpdateInfoFromVersionInfo(verInfo *VersionInfo) *UpdateInfo {
	latestVer := extractSemver(verInfo.Version)
	if latestVer == "" {
		latestVer = strings.TrimPrefix(verInfo.Version, "v")
	}
	hasUpdate := compareVersions(latestVer, Version) > 0

	// 筛选历史版本（仅保留大于当前版本的）
	var history []VersionItem
	for _, item := range verInfo.History {
		ver := extractSemver(item.Version)
		if ver == "" {
			ver = strings.TrimPrefix(item.Version, "v")
		}
		if compareVersions(ver, Version) > 0 {
			history = append(history, item)
		}
	}

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   Version,
		LatestVer:    latestVer,
		ReleaseName:  "v" + latestVer,
		ReleaseNotes: verInfo.Desc,
		History:      history,
	}

	if hasUpdate {
		applog.Info("[Updater] 发现新版本: %s -> %s (通过 version.json)", Version, latestVer)
	} else {
		applog.Info("[Updater] 当前已是最新版本: %s (通过 version.json)", Version)
	}

	return info
}

// fetchReleaseFromSources 尝试从多个源获取 GitHub Release（含代理）
// 每个源最多重试 maxRetryPerSource 次
func fetchReleaseFromSources() (*GitHubRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	// 先尝试直连
	for retry := 0; retry < maxRetryPerSource; retry++ {
		release, err := fetchRelease(apiURL)
		if err == nil {
			return release, nil
		}
		applog.Warn("[Updater] GitHub API 直连第%d次失败: %v", retry+1, err)
	}

	// 尝试代理
	for _, proxy := range githubProxies {
		proxyURL := proxy + apiURL
		for retry := 0; retry < maxRetryPerSource; retry++ {
			applog.Info("[Updater] 尝试代理: %s [第%d次]", proxy, retry+1)
			release, err := fetchRelease(proxyURL)
			if err == nil {
				applog.Info("[Updater] 代理 %s 成功", proxy)
				return release, nil
			}
			applog.Warn("[Updater] 代理 %s 第%d次失败: %v", proxy, retry+1, err)
		}
		applog.Warn("[Updater] 代理 %s 已重试%d次均失败，切换下一个代理", proxy, maxRetryPerSource)
	}

	return nil, fmt.Errorf("所有 GitHub Release 源均失败")
}

// fetchVersionInfoFromSources 多渠道获取 version.json（参考 lx-music-desktop）
// 每个源最多重试 maxRetryPerSource 次，失败后自动切换到下一个源
func fetchVersionInfoFromSources() (*VersionInfo, error) {
	var lastErr error
	for _, source := range versionInfoSources {
		for retry := 0; retry < maxRetryPerSource; retry++ {
			applog.Info("[Updater] 尝试获取版本信息: %s (%s) [第%d次]",
				source.url, source.source, retry+1)
			info, err := fetchVersionInfo(source.url)
			if err == nil {
				applog.Info("[Updater] 版本信息获取成功: %s", source.source)
				return info, nil
			}
			lastErr = err
			applog.Warn("[Updater] 源 %s 第%d次失败: %v", source.source, retry+1, err)
		}
		applog.Warn("[Updater] 源 %s 已重试%d次均失败，切换下一个源", source.source, maxRetryPerSource)
	}
	return nil, fmt.Errorf("所有版本信息源均失败: %w", lastErr)
}

// fetchVersionInfo 从指定 URL 获取 version.json
func fetchVersionInfo(url string) (*VersionInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	if info.Version == "" {
		return nil, fmt.Errorf("version.json 中缺少 version 字段")
	}
	return &info, nil
}

// buildUpdateInfoFromRelease 从 GitHub Release 构建 UpdateInfo
func buildUpdateInfoFromRelease(release *GitHubRelease) *UpdateInfo {
	latestVer := extractSemver(release.TagName)
	if latestVer == "" {
		applog.Warn("[Updater] 无法从 tag 提取版本号: %s", release.TagName)
		latestVer = strings.TrimPrefix(release.TagName, "v")
	}
	hasUpdate := compareVersions(latestVer, Version) > 0

	assetName, downloadURL, assetSize := findBestAsset(release.Assets)

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   Version,
		LatestVer:    latestVer,
		ReleaseName:  release.Name,
		ReleaseNotes: release.Body,
		DownloadURL:  downloadURL,
		AssetName:    assetName,
		AssetSize:    assetSize,
		PublishedAt:  release.PublishedAt,
	}

	if hasUpdate {
		applog.Info("[Updater] 发现新版本: %s -> %s", Version, latestVer)
	} else {
		applog.Info("[Updater] 当前已是最新版本: %s", Version)
	}

	return info
}

// fetchRelease 从 URL 获取 Release 信息
func fetchRelease(url string) (*GitHubRelease, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// findBestAsset 在 Release 资源中找到最适合当前平台的安装包
func findBestAsset(assets []GitHubAsset) (name, url string, size int64) {
	// 根据当前平台确定优先匹配的后缀
	var preferredExts []string
	switch runtime.GOOS {
	case "windows":
		preferredExts = []string{".exe", ".msi", ".zip"}
	case "darwin":
		preferredExts = []string{".dmg", ".pkg", ".zip"}
	default:
		preferredExts = []string{".AppImage", ".deb", ".rpm", ".tar.gz"}
	}

	// 平台关键词
	platformKeys := []string{runtime.GOOS, runtime.GOARCH}

	// 第一轮：精确匹配平台 + 架构
	for _, ext := range preferredExts {
		for _, asset := range assets {
			lower := strings.ToLower(asset.Name)
			if !strings.HasSuffix(lower, ext) {
				continue
			}
			// 检查是否包含当前平台关键词
			matchesPlatform := false
			for _, key := range platformKeys {
				if strings.Contains(lower, key) {
					matchesPlatform = true
					break
				}
			}
			if matchesPlatform {
				return asset.Name, asset.BrowserDownloadURL, asset.Size
			}
		}
	}

	// 第二轮：只匹配扩展名
	for _, ext := range preferredExts {
		for _, asset := range assets {
			lower := strings.ToLower(asset.Name)
			if strings.HasSuffix(lower, ext) {
				return asset.Name, asset.BrowserDownloadURL, asset.Size
			}
		}
	}

	// 第三轮：返回第一个资源
	if len(assets) > 0 {
		return assets[0].Name, assets[0].BrowserDownloadURL, assets[0].Size
	}

	return "", "", 0
}

// compareVersions 比较两个语义化版本号
// 返回 -1: v1 < v2, 0: v1 == v2, 1: v1 > v2
func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			n1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(parts2[i])
		}
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}
	return 0
}

// DownloadProgress 下载进度回调
type DownloadProgress func(downloaded, total int64, speedBps float64)

// DownloadUpdate 下载更新包到临时目录，通过回调函数报告进度
// 返回下载文件的本地路径
func DownloadUpdate(downloadURL string, progress DownloadProgress) (string, error) {
	applog.Info("[Updater] 开始下载: %s", downloadURL)

	// 确定保存路径
	tmpDir := filepath.Join(os.TempDir(), "CCZJ-Video-Updates")
	os.MkdirAll(tmpDir, 0755)

	// 从 URL 提取文件名
	filename := filepath.Base(downloadURL)
	if idx := strings.Index(filename, "?"); idx >= 0 {
		filename = filename[:idx]
	}
	if filename == "" || filename == "." {
		filename = "CCZJ-Video-Update.exe"
	}
	savePath := filepath.Join(tmpDir, filename)

	// 如果文件已存在，删除后重新下载
	os.Remove(savePath)

	client := &http.Client{
		Timeout: DownloadTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")

	resp, err := client.Do(req)
	if err != nil {
		// 尝试代理下载
		for _, proxy := range githubProxies {
			proxyURL := proxy + downloadURL
			applog.Info("[Updater] 尝试代理下载: %s", proxy)
			req2, _ := http.NewRequest("GET", proxyURL, nil)
			req2.Header.Set("User-Agent", "CCZJ-Video-Updater/1.0")
			resp, err = client.Do(req2)
			if err == nil && resp.StatusCode == http.StatusOK {
				downloadURL = proxyURL
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			applog.Warn("[Updater] 代理下载 %s 失败: %v", proxy, err)
			resp = nil
		}
		if err != nil || resp == nil {
			return "", fmt.Errorf("下载失败: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength

	out, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	buf := make([]byte, 128*1024)
	var downloaded int64
	lastBytes := int64(0)
	lastTime := time.Now()
	startTime := lastTime

	for {
		// 检查下载超时（参考 lx-music-desktop）
		if time.Since(startTime) > DownloadTimeout {
			os.Remove(savePath)
			return "", fmt.Errorf("下载超时（超过 %v）", DownloadTimeout)
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("写入文件失败: %w", werr)
			}
			downloaded += int64(n)

			// 每秒报告一次进度
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
			return "", fmt.Errorf("读取失败: %w", readErr)
		}
	}

	// 最终进度回调
	if progress != nil {
		progress(downloaded, total, 0)
	}

	applog.Info("[Updater] 下载完成: %s (%d 字节)", savePath, downloaded)
	return savePath, nil
}

// InstallUpdate 执行安装：启动新版本安装包并退出当前程序
// 对于 Windows .exe 安装包，直接启动
// 对于 .zip 等压缩包，需要用户手动解压
func InstallUpdate(filePath string) error {
	applog.Info("[Updater] 准备安装更新: %s", filePath)

	ext := strings.ToLower(filepath.Ext(filePath))

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		switch ext {
		case ".exe", ".msi":
			cmd = exec.Command(filePath)
		case ".zip", ".7z", ".rar":
			// 打开所在目录，让用户手动处理
			dir := filepath.Dir(filePath)
			cmd = exec.Command("explorer", "/select,", filePath)
			_ = cmd.Start()
			cmd = exec.Command("explorer", dir)
		default:
			cmd = exec.Command("explorer", "/select,", filePath)
		}
	case "darwin":
		switch ext {
		case ".dmg":
			cmd = exec.Command("open", filePath)
		case ".pkg":
			cmd = exec.Command("open", filePath)
		default:
			cmd = exec.Command("open", filePath)
		}
	default:
		cmd = exec.Command("xdg-open", filePath)
	}

	if cmd != nil {
		_ = cmd.Start()
	}

	// 退出当前程序（给新安装包一点启动时间）
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	return nil
}