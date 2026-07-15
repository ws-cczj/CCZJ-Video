package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/applog"
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
		return nil, fmt.Errorf("所有更新源均失败: %w", verErr)
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
				Transport: &http.Transport{
					TLSHandshakeTimeout:   8 * time.Second,
					ResponseHeaderTimeout: 10 * time.Second,
				},
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
		Transport: &http.Transport{
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
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

// uniqueSavePath 在目标目录中生成不冲突的保存路径
// 如果文件名与当前可执行文件相同，添加 _update 后缀避免冲突
// 如果仍存在同名文件，追加数字后缀 _update(2), _update(3)...
func uniqueSavePath(dir, filename string) string {
	appExe := getAppExeName()
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	// 如果与当前可执行文件同名，修改名称
	if strings.EqualFold(filename, appExe) {
		base = strings.TrimSuffix(appExe, filepath.Ext(appExe)) + "_update"
		ext = filepath.Ext(appExe)
		filename = base + ext
	}

	candidate := filepath.Join(dir, filename)
	if !fileExists(candidate) {
		return candidate
	}

	// 追加数字后缀直到找到可用名称
	for i := 2; i <= 99; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if !fileExists(candidate) {
			return candidate
		}
	}
	// 极端情况：使用时间戳
	candidate = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext))
	return candidate
}

// scanAlreadyDownloaded 扫描应用目录，查找已下载的更新包（*_update.exe 或 *_update(N).exe）
// 返回 (文件路径, 版本号估计) 或 ("", "") 如果不存在
func scanAlreadyDownloaded() (string, string) {
	appDir := getAppDir()
	// The current updater always stages to this stable path.
	stablePath := filepath.Join(appDir, "CCZJ-Video-Update.exe")
	if info, err := os.Stat(stablePath); err == nil && !info.IsDir() && info.Size() >= 1024 {
		return stablePath, ""
	}
	exeName := getAppExeName()
	ext := filepath.Ext(exeName)
	baseName := strings.TrimSuffix(exeName, ext)

	// 查找模式：{exeBase}_update.exe, {exeBase}_update(2).exe 等
	pattern := filepath.Join(appDir, baseName+"_update*"+ext)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", ""
	}

	// 取第一个匹配的文件（优先用 _update.exe，不带数字后缀的）
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || info.IsDir() || info.Size() < 1024 {
			continue // 跳过无效/太小的文件
		}
		applog.Info("[Updater] 发现已下载的更新包: %s (%d bytes)", m, info.Size())
		return m, "" // 版本号未知，由前端根据 pendingInfo 判断
	}
	return "", ""
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DownloadUpdate 下载更新包到应用同目录，通过回调函数报告进度
// 先并发测速所有下载源（直连+代理），选最快的下载；失败自动回退下一个源
// 返回下载文件的本地路径
func DownloadUpdate(downloadURL string, progress DownloadProgress) (string, error) {
	applog.Info("[Updater] 开始下载: %s", downloadURL)

	// 确定保存路径：应用同目录
	appDir := getAppDir()
	applog.Info("[Updater] 应用目录: %s", appDir)

	// 从 URL 提取文件名
	// Always use one staging name. Release asset names are allowed to change
	// between versions; using the URL basename leaves stale update executables.
	savePath := filepath.Join(appDir, "CCZJ-Video-Update.exe")
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
		return "", fmt.Errorf("所有下载源均不可用")
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
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
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
			return "", fmt.Errorf("创建文件失败: %w", err)
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
				return "", fmt.Errorf("下载超时（超过 %v）", DownloadTimeout)
			}

			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					return "", fmt.Errorf("写入文件失败: %w", werr)
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
				return "", fmt.Errorf("读取失败: %w", readErr)
			}
		}

		// 最终进度回调
		if progress != nil {
			progress(downloaded, total, 0)
		}

		applog.Info("[Updater] 下载完成: %s (%d 字节) 源: %s", savePath, downloaded, src.source)
		return savePath, nil
	}

	return "", fmt.Errorf("所有下载源均失败")
}

// InstallUpdate 执行安装：替换当前可执行文件为新版本并重启
// Windows: 创建批处理脚本，等待旧进程退出后替换 exe 并重启
// 其他平台: 直接启动新版本（或让用户手动处理）
func InstallUpdate(filePath string) error {
	applog.Info("[Updater] 准备安装更新: %s", filePath)

	ext := strings.ToLower(filepath.Ext(filePath))

	switch runtime.GOOS {
	case "windows":
		switch ext {
		case ".exe":
			return installBySwapWindows(filePath)
		case ".msi":
			// MSI 安装包有自己的安装逻辑，直接启动
			return launchAndExit(filePath)
		default:
			// zip/7z 等：打开所在目录让用户手动处理
			dir := filepath.Dir(filePath)
			_ = exec.Command("explorer", "/select,", filePath).Start()
			_ = exec.Command("explorer", dir).Start()
			go func() {
				time.Sleep(500 * time.Millisecond)
				os.Exit(0)
			}()
			return nil
		}
	case "darwin":
		return launchAndExit(filePath)
	default:
		return launchAndExit(filePath)
	}
}

// launchAndExit 启动文件并退出当前程序
func launchAndExit(filePath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command(filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	default:
		cmd = exec.Command("xdg-open", filePath)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// installBySwapWindows 通过批处理脚本实现 exe 热替换
// 流程：当前进程退出 → 脚本等待退出 → 替换 exe → 启动新版本 → 清理脚本
func installBySwapWindows(newExePath string) error {
	oldExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	oldExe, err = filepath.EvalSymlinks(oldExe)
	if err != nil {
		return fmt.Errorf("解析可执行文件路径失败: %w", err)
	}

	appDir := filepath.Dir(oldExe)
	oldExeFull := oldExe
	newExeFull, _ := filepath.Abs(newExePath)
	batPath := filepath.Join(appDir, "_update_swap.bat")
	oldExeName := filepath.Base(oldExe)

	// 使用纯 ASCII + CRLF 行尾，避免 cmd.exe 默认代码页编码问题
	// 使用绝对路径，避免 %~dp0 在含空格路径下的解析问题
	CRLF := "\r\n"
	lines := []string{
		"@echo off",
		fmt.Sprintf("echo Waiting for %s to exit...", oldExeName),
		":WAIT_LOOP",
		fmt.Sprintf(`tasklist /FI "IMAGENAME eq %s" 2>NUL | find /I "%s">NUL`, oldExeName, oldExeName),
		"if %ERRORLEVEL% EQU 0 (",
		"    timeout /t 1 /nobreak >NUL",
		"    goto WAIT_LOOP",
		")",
		"",
		"echo Process exited, replacing executable...",
		"timeout /t 1 /nobreak >NUL",
		"",
		// 带重试的替换循环（Windows 进程退出后文件可能仍被锁定）
		"set RETRY=0",
		":RETRY_REPLACE",
		fmt.Sprintf(`move /y "%s" "%s" >NUL 2>&1`, newExeFull, oldExeFull),
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		"",
		"set /a RETRY+=1",
		"if %RETRY% GEQ 15 goto :FORCE_REPLACE",
		"echo Move failed (retry %RETRY%), waiting...",
		"timeout /t 2 /nobreak >NUL",
		"goto RETRY_REPLACE",
		"",
		// 强制替换：先删除旧文件再移动
		":FORCE_REPLACE",
		"echo Force replacing...",
		fmt.Sprintf(`del /f /q "%s" >NUL 2>&1`, oldExeFull),
		"timeout /t 1 /nobreak >NUL",
		fmt.Sprintf(`move /y "%s" "%s" >NUL 2>&1`, newExeFull, oldExeFull),
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		"",
		"echo Replace failed after 15 retries!",
		`del /f /q "%~f0"`,
		"exit /b 1",
		"",
		":REPLACE_OK",
		// 验证替换成功（检查新文件是否存在于目标位置）
		fmt.Sprintf(`if not exist "%s" (`, oldExeFull),
		"echo Verification failed: target not found!",
		`del /f /q "%~f0"`,
		"exit /b 1",
		")",
		"echo Replace OK, starting new version...",
		fmt.Sprintf(`start "" "%s"`, oldExeFull),
		`del /f /q "%~f0"`,
	}
	batContent := strings.Join(lines, CRLF) + CRLF

	if err := os.WriteFile(batPath, []byte(batContent), 0755); err != nil {
		return fmt.Errorf("创建更新脚本失败: %w", err)
	}
	applog.Info("[Updater] 创建替换脚本: %s", batPath)
	applog.Info("[Updater] 旧 exe: %s", oldExeFull)
	applog.Info("[Updater] 新 exe: %s", newExeFull)

	// 启动批处理脚本（独立进程）
	// 不使用 start 命令包裹（嵌套引号在含空格路径下解析不可靠）
	cmd := exec.Command("cmd", "/c", batPath)
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		os.Remove(batPath)
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	// 退出当前程序，让脚本接管
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}
