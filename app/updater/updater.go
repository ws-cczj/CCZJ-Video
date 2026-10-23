package updater

import (
	"cczjVideo/app/netpolicy"
	"context"
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

// getInstalledVersion 读取已安装的版本号。读到的值留在 lastInstalledMarker 里：
// 标记紧接着就会被启动流程清掉，而诊断台要拿它回答"上一次安装声称换成了哪个版本"
// —— 那是判断"装上了没有"的唯一现场证据。
var lastInstalledMarker string

func getInstalledVersion() string {
	appDir := getAppDir()
	fpath := filepath.Join(appDir, installedVersionFile)
	data, err := os.ReadFile(fpath)
	if err != nil {
		return ""
	}
	marker := strings.TrimSpace(string(data))
	if marker != "" {
		lastInstalledMarker = marker
	}
	return marker
}

// InstalledMarkerSeenThisRun 返回本进程读到过的最后一次安装标记（清空之前的值）。
// 标记文件在应用目录、不在数据目录，路径由本包独占，别让调用方猜。
func InstalledMarkerSeenThisRun() string { return lastInstalledMarker }

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
	Assets  []GitHubAsset `json:"assets,omitempty"`
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
	return CheckUpdateContext(context.Background())
}

func CheckUpdateContext(ctx context.Context) (*UpdateInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	release, err := fetchReleaseFromSourcesContext(ctx)
	if err == nil && release != nil {
		info := buildUpdateInfoFromRelease(release)
		info.AlreadyDownloadedPath, info.AlreadyDownloadedVer = scanAlreadyDownloaded(info.LatestVer)
		return info, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 策略二：通过多渠道 version.json 获取版本信息
	applog.Info("[Updater] GitHub API 获取失败，尝试多渠道 version.json")

	verInfo, verErr := fetchVersionInfoFromSourcesContext(ctx)
	if verErr != nil {
		return nil, apperror.Wrap(apperror.Unavailable, verErr, "所有更新源均失败")
	}

	info := buildUpdateInfoFromVersionInfo(verInfo)
	info.AlreadyDownloadedPath, info.AlreadyDownloadedVer = scanAlreadyDownloaded(info.LatestVer)
	return info, nil
}

// buildUpdateInfoFromVersionInfo 从 VersionInfo（version.json）构建 UpdateInfo
func buildUpdateInfoFromVersionInfo(verInfo *VersionInfo) *UpdateInfo {
	latestVer := extractSemver(verInfo.Version)
	if latestVer == "" {
		latestVer = strings.TrimPrefix(verInfo.Version, "v")
	}
	current := EffectiveVersion()
	hasUpdate := compareVersions(latestVer, current) > 0

	// 筛选历史版本（仅保留大于当前版本的）
	var history []VersionItem
	for _, item := range verInfo.History {
		ver := extractSemver(item.Version)
		if ver == "" {
			ver = strings.TrimPrefix(item.Version, "v")
		}
		if compareVersions(ver, current) > 0 {
			history = append(history, item)
		}
	}

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   current,
		LatestVer:    latestVer,
		ReleaseName:  "v" + latestVer,
		ReleaseNotes: verInfo.Desc,
		History:      history,
	}
	name, assetURL, assetSize := findBestAsset(verInfo.Assets)
	owner, repo, tag, file, valid := parseReleaseAsset(assetURL)
	if valid && owner == repoOwner && repo == repoName && extractSemver(tag) == latestVer && file == name {
		info.AssetName, info.DownloadURL, info.AssetSize = name, assetURL, assetSize
	}

	if hasUpdate {
		applog.Info("[Updater] 发现新版本: %s -> %s (通过 version.json)", current, latestVer)
	} else {
		applog.Info("[Updater] 当前已是最新版本: %s (通过 version.json)", current)
	}

	return info
}

// fetchReleaseFromSources 尝试从多个源获取 GitHub Release（含代理）
// 每个源最多重试 maxRetryPerSource 次
func fetchReleaseFromSources() (*GitHubRelease, error) {
	return fetchReleaseFromSourcesContext(context.Background())
}

func fetchReleaseFromSourcesContext(ctx context.Context) (*GitHubRelease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	// 先尝试直连
	for retry := 0; retry < maxRetryPerSource; retry++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, err := fetchReleaseContext(ctx, apiURL)
		if err == nil {
			return release, nil
		}
		applog.Warn("[Updater] GitHub API 直连第%d次失败: %v", retry+1, err)
	}

	// 尝试代理
	for _, proxy := range githubProxies {
		proxyURL := proxy + apiURL
		for retry := 0; retry < maxRetryPerSource; retry++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			applog.Info("[Updater] 尝试代理: %s [第%d次]", proxy, retry+1)
			release, err := fetchReleaseContext(ctx, proxyURL)
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
	return fetchVersionInfoFromSourcesContext(context.Background())
}

func fetchVersionInfoFromSourcesContext(ctx context.Context) (*VersionInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var lastErr error
	for _, source := range versionInfoSources {
		for retry := 0; retry < maxRetryPerSource; retry++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			applog.Info("[Updater] 尝试获取版本信息: %s (%s) [第%d次]",
				source.url, source.source, retry+1)
			info, err := fetchVersionInfoContext(ctx, source.url)
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
	return fetchVersionInfoContext(context.Background(), url)
}

func fetchVersionInfoContext(ctx context.Context, url string) (*VersionInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: netpolicy.WrapTransport(string(netstats.CategoryUpdate), netstats.WrapTransport(netstats.CategoryUpdate, nil))}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	current := EffectiveVersion()
	hasUpdate := compareVersions(latestVer, current) > 0

	assetName, downloadURL, assetSize := findBestAsset(release.Assets)

	info := &UpdateInfo{
		HasUpdate:    hasUpdate,
		CurrentVer:   current,
		LatestVer:    latestVer,
		ReleaseName:  release.Name,
		ReleaseNotes: release.Body,
		DownloadURL:  downloadURL,
		AssetName:    assetName,
		AssetSize:    assetSize,
		PublishedAt:  release.PublishedAt,
	}

	if hasUpdate {
		applog.Info("[Updater] 发现新版本: %s -> %s", current, latestVer)
	} else {
		applog.Info("[Updater] 当前已是最新版本: %s", current)
	}

	return info
}

// fetchRelease 从 URL 获取 Release 信息
func fetchRelease(url string) (*GitHubRelease, error) {
	return fetchReleaseContext(context.Background(), url)
}

func fetchReleaseContext(ctx context.Context, url string) (*GitHubRelease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: netpolicy.WrapTransport(string(netstats.CategoryUpdate), netstats.WrapTransport(netstats.CategoryUpdate, nil))}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	return findBestAssetForPlatform(assets, runtime.GOOS, runtime.GOARCH)
}

func findBestAssetForPlatform(assets []GitHubAsset, goos, goarch string) (name, url string, size int64) {
	extensions := map[string][]string{"windows": {".exe", ".msi", ".zip"}, "android": {".apk"}, "darwin": {".dmg", ".pkg", ".zip"}, "linux": {".appimage", ".deb", ".rpm", ".tar.gz"}}[goos]
	for _, ext := range extensions {
		best, rank := -1, 99
		for i, asset := range assets {
			lower := strings.ToLower(asset.Name)
			if !strings.HasSuffix(lower, ext) {
				continue
			}
			tokens := strings.FieldsFunc(strings.ReplaceAll(lower, "x86_64", "amd64"), func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })
			present := map[string]bool{}
			for _, token := range tokens {
				present[token] = true
			}
			if present["aarch64"] {
				present["arm64"] = true
			}
			if present["x64"] {
				present["amd64"] = true
			}
			if present["win"] || present["win64"] {
				present["windows"] = true
			}
			if present["macos"] || present["osx"] {
				present["darwin"] = true
			}
			wrong := false
			for _, platform := range []string{"windows", "android", "darwin", "linux"} {
				if present[platform] && platform != goos {
					wrong = true
				}
			}
			archSeen := false
			for _, arch := range []string{"amd64", "arm64", "386", "arm"} {
				if present[arch] {
					archSeen = true
					if arch != goarch {
						wrong = true
					}
				}
			}
			if wrong {
				continue
			}
			candidate := 2
			if archSeen {
				candidate = 0
			} else if goos == "android" && !present["universal"] {
				continue
			}
			if candidate < rank {
				best, rank = i, candidate
			}
		}
		if best >= 0 {
			asset := assets[best]
			return asset.Name, asset.BrowserDownloadURL, asset.Size
		}
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
