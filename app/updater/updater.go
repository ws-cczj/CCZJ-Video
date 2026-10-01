package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/netstats"
)

// Version 当前应用版本号（通过构建时 -ldflags -X 注入，勿手动修改）
// 构建命令示例: go build -ldflags="-X cczjVideo/app/updater.Version=1.1.1"
var Version = "dev"

// installedVersionFile 是安装版本标记文件名（存放在应用目录）
const installedVersionFile = ".installed_version"

// EffectiveVersion 返回当前有效版本号
// 取编译版本与已安装版本标记中的较高者，防止二进制替换失败时重复提示更新
func EffectiveVersion() string {
	installed := getInstalledVersion()
	if installed != "" && compareVersions(installed, Version) > 0 {
		return installed
	}
	return Version
}

// RecordInstalledVersion 记录已安装的版本号（安装前调用）
func RecordInstalledVersion(version string) {
	appDir := getAppDir()
	fpath := filepath.Join(appDir, installedVersionFile)
	if err := os.WriteFile(fpath, []byte(version), 0644); err != nil {
		applog.Warn("[Updater] 记录安装版本失败: %v", err)
	} else {
		applog.Info("[Updater] 已记录安装版本: %s", version)
	}
}

// ClearInstalledVersion 清除安装版本标记（启动时版本已匹配则无需保留）
func ClearInstalledVersion() {
	appDir := getAppDir()
	fpath := filepath.Join(appDir, installedVersionFile)
	os.Remove(fpath)
}

// getInstalledVersion 读取已安装的版本号
func getInstalledVersion() string {
	appDir := getAppDir()
	fpath := filepath.Join(appDir, installedVersionFile)
	data, err := os.ReadFile(fpath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

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

// versionInfoSources 多渠道版本信息获取源（按优先级排列）
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
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
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
	HasUpdate             bool          `json:"has_update"`
	CurrentVer            string        `json:"current_version"`
	LatestVer             string        `json:"latest_version"`
	ReleaseName           string        `json:"release_name"`
	ReleaseNotes          string        `json:"release_notes"`
	DownloadURL           string        `json:"download_url"`
	AssetName             string        `json:"asset_name"`
	AssetSize             int64         `json:"asset_size"`
	PublishedAt           string        `json:"published_at"`
	History               []VersionItem `json:"history,omitempty"`
	AlreadyDownloadedPath string        `json:"already_downloaded_path,omitempty"`    // 已下载的更新包路径（Go 端扫描）
	AlreadyDownloadedVer  string        `json:"already_downloaded_version,omitempty"` // 已下载的更新包版本号
}

// CheckUpdate 检查 GitHub 是否有新版本
// 多渠道回退策略：GitHub API → 代理 → 多源 version.json（无缓存）
func CheckUpdate() (*UpdateInfo, error) {
	// Windows ARM 架构：如果没有提供 ARM 包，跳过更新检查
	if runtime.GOOS == "windows" && strings.Contains(runtime.GOARCH, "arm") {
		applog.Info("[Updater] Windows ARM 架构，跳过更新检查（暂无 ARM 安装包）")
		return &UpdateInfo{
			HasUpdate:  false,
			CurrentVer: EffectiveVersion(),
			LatestVer:  EffectiveVersion(),
		}, nil
	}

	// 策略一：优先使用 GitHub Release API
	release, err := fetchReleaseFromSources()
	if err == nil && release != nil {
		info := buildUpdateInfoFromRelease(release)
		info.AlreadyDownloadedPath, info.AlreadyDownloadedVer = scanAlreadyDownloaded()
		return info, nil
	}

	// 策略二：通过多渠道 version.json 获取版本信息
	applog.Info("[Updater] GitHub API 获取失败，尝试多渠道 version.json")

	verInfo, verErr := fetchVersionInfoFromSources()
	if verErr != nil {
		return nil, apperror.Wrap(apperror.Unavailable, verErr, "所有更新源均失败")
	}

	info := buildUpdateInfoFromVersionInfo(verInfo)
	info.AlreadyDownloadedPath, info.AlreadyDownloadedVer = scanAlreadyDownloaded()
	return info, nil
}

// buildUpdateInfoFromVersionInfo 从 VersionInfo（version.json）构建 UpdateInfo
func buildUpdateInfoFromVersionInfo(verInfo *VersionInfo) *UpdateInfo {
	latestVer := extractSemver(verInfo.Version)
	if latestVer == "" {
		latestVer = strings.TrimPrefix(verInfo.Version, "v")
	}
	hasUpdate := compareVersions(latestVer, EffectiveVersion()) > 0

	// 筛选历史版本（仅保留大于当前版本的）
	var history []VersionItem
	for _, item := range verInfo.History {
		ver := extractSemver(item.Version)
		if ver == "" {
			ver = strings.TrimPrefix(item.Version, "v")
		}
		if compareVersions(ver, EffectiveVersion()) > 0 {
			history = append(history, item)
		}
	}

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   EffectiveVersion(),
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
	client := &http.Client{Timeout: 15 * time.Second, Transport: netstats.WrapTransport(netstats.CategoryUpdate, nil)}
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
	hasUpdate := compareVersions(latestVer, EffectiveVersion()) > 0

	assetName, downloadURL, assetSize := findBestAsset(release.Assets)

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   EffectiveVersion(),
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
	client := &http.Client{Timeout: 15 * time.Second, Transport: netstats.WrapTransport(netstats.CategoryUpdate, nil)}
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
