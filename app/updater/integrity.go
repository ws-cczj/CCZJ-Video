package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/netstats"
)

// checksumsAssetName 是发布产物里的校验清单，由 build/windows/package.ps1 生成。
const checksumsAssetName = "checksums.txt"

// stagingPrefix 是更新包的暂存文件名前缀。InstallUpdate 会直接执行落盘的文件，
// 所以"是不是本更新器产出的"这一条必须能用名字判定。
const stagingPrefix = "CCZJ-Video-Update"

// parseReleaseAsset 解析 GitHub Release 资源的下载地址：
//
//	https://github.com/{owner}/{repo}/releases/download/{tag}/{file}
//
// 输入可能带第三方代理前缀（https://gh-proxy.org/https://github.com/...），
// 所以从 "github.com/" 开始取，而不是整段匹配。
func parseReleaseAsset(rawURL string) (owner, repo, tag, file string, ok bool) {
	idx := strings.Index(strings.ToLower(rawURL), "github.com/")
	if idx < 0 {
		return "", "", "", "", false
	}
	parts := strings.Split(rawURL[idx+len("github.com/"):], "/")
	if len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", "", "", false
	}
	if parts[4] == "" || parts[5] == "" {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[4], strings.Join(parts[5:], "/"), true
}

// parseChecksums 解析 coreutils 风格的 "<sha256>  <文件名>" 清单。
// 只收 SHA-256；键统一小写，因为 Windows 文件系统不区分大小写，而清单里的名字来自
// 打包脚本，大小写不该影响能否匹配。
func parseChecksums(body []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields[0]) != sha256.Size*2 {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			continue
		}
		// 清单里可能写成 "./name" 或带目录；只认 basename，避免清单指到别处。
		name := strings.Join(fields[1:], " ")
		name = filepath.Base(filepath.ToSlash(name))
		// 存十六进制原文而不是 DecodeString 的结果：比对对象是 fileSHA256 的 hex 输出，
		// 解码后的 32 原始字节永远不相等。
		out[strings.ToLower(name)] = strings.ToLower(fields[0])
	}
	return out
}

// checksumChannel 是一个能取回 checksums.txt 的通道。
//
// party 是运营商标识：gh-proxy.org 与它的 v4/v6 子域是同一家，凑在一起只算一票——
// 「两处一致」若能让一家自问自答，就等于没有这道确认。
type checksumChannel struct {
	prefix string // 直连通道为空串
	party  string
}

// checksumChannels 按可信度排序：github.com 是唯一由我们自己发布内容的通道，
// 第三方加速站只在它取不回来时补位，而且必须互相印证。
//
// 清单不能只走直连：产物下载本身就是靠这些加速站兜住大陆网络的（speedTestSources），
// 让清单唯一可选的通道是这条最常见的路上最先断掉的，结果就是所有人都在第一步被挡住。
func checksumChannels() []checksumChannel {
	return []checksumChannel{
		{prefix: "", party: "github.com"},
		{prefix: "https://gh-proxy.org/", party: "gh-proxy.org"},
		{prefix: "https://v4.gh-proxy.org/", party: "gh-proxy.org"},
		{prefix: "https://v6.gh-proxy.org/", party: "gh-proxy.org"},
		{prefix: "https://gh-proxy.com/", party: "gh-proxy.com"},
		{prefix: "https://githubproxy.cc/", party: "githubproxy.cc"},
		{prefix: "https://ghproxy.net/", party: "ghproxy.net"},
	}
}

// requiredParties 是代理清单要被采信所需的独立运营商数量。
const requiredParties = 2

// fetchChecksums 从单个完整 URL 取回并解析清单。
func fetchChecksums(url string) (map[string]string, error) {
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: netstats.WrapTransport(netstats.CategoryUpdate, nil),
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	checksums := parseChecksums(body)
	if len(checksums) == 0 {
		return nil, fmt.Errorf("%s 内容为空或格式不认识", checksumsAssetName)
	}
	return checksums, nil
}

// checksumVote 是一家运营商对同一个清单的回答。
type checksumVote struct {
	party string
	sums  map[string]string
	err   error
}

// canonicalChecksums 把清单压成一个可比较的串：名字与摘要都已在 parseChecksums 里归一，
// 排序只是为了同一家不同子域、或不同家的写法差异不会误判成分歧。
func canonicalChecksums(sums map[string]string) string {
	keys := make([]string, 0, len(sums))
	for name := range sums {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, name := range keys {
		sb.WriteString(name)
		sb.WriteByte(' ')
		sb.WriteString(sums[name])
		sb.WriteByte('\n')
	}
	return sb.String()
}

// decideChecksums 是要紧的判定，与网络无关：
// 直连拿到就用直连；否则要求至少 requiredParties 家内部无分歧的运营商给出逐字一致的清单。
//
// 残余风险说清楚：两家勾结的加速站仍然能同时替换产物和清单。这一层防得住的是单点损坏
// 和单家投毒，防不住整条链路合谋——那需要代码签名，不是这里能解决的。
func decideChecksums(direct map[string]string, directErr error, votes []checksumVote) (map[string]string, string, error) {
	if directErr == nil {
		return direct, "github.com", nil
	}

	// party -> 该家给过的不同清单。同一家先后给出不一致的两份，这家整体作废。
	seen := make(map[string]map[string]bool)
	sumsByKey := make(map[string]map[string]string)
	for _, vote := range votes {
		if vote.err != nil || len(vote.sums) == 0 {
			continue
		}
		key := canonicalChecksums(vote.sums)
		if seen[vote.party] == nil {
			seen[vote.party] = make(map[string]bool)
		}
		seen[vote.party][key] = true
		sumsByKey[key] = vote.sums
	}

	voters := make(map[string][]string)
	for party, keys := range seen {
		if len(keys) != 1 {
			continue
		}
		for key := range keys {
			voters[key] = append(voters[key], party)
		}
	}
	if len(voters) == 0 {
		return nil, "", apperror.Wrap(apperror.Unavailable, directErr,
			"直连与加速站都没能取回校验清单，请到发布页手动下载")
	}

	// 票数多的先；同票按清单内容排序，保证同一网络环境下每次选的是同一份。
	bestKey, bestParties := "", []string(nil)
	for key, parties := range voters {
		sort.Strings(parties)
		if len(parties) > len(bestParties) || (len(parties) == len(bestParties) && key < bestKey) {
			bestKey, bestParties = key, parties
		}
	}
	if len(bestParties) < requiredParties {
		return nil, "", apperror.Newf(apperror.Unavailable,
			"只有 %d 处加速站给出校验清单，凑不出两处一致，为防产物被替换已停止下载（直连失败：%v）；可稍后重试或到发布页手动下载",
			len(bestParties), directErr)
	}
	return sumsByKey[bestKey], strings.Join(bestParties, "+"), nil
}

// fetchReleaseChecksums 读取某个 tag 下的校验清单：直连优先，取不回来时才由多家加速站互相印证。
func fetchReleaseChecksums(owner, repo, tag string) (map[string]string, error) {
	if owner != repoOwner || repo != repoName {
		return nil, apperror.Newf(apperror.Validation, "资源不属于本应用仓库 %s/%s", repoOwner, repoName)
	}
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, tag, checksumsAssetName)

	// 代理与直连同时发车：直连在这条路上是慢的那一个，串行试完再回退等于把
	// 「点下载」到「开始传字节」的时间全花在一次注定超时的等待上。
	channels := checksumChannels()
	var wg sync.WaitGroup
	votesCh := make(chan checksumVote, len(channels)-1)
	for _, ch := range channels[1:] {
		wg.Add(1)
		go func(ch checksumChannel) {
			defer wg.Done()
			sums, err := fetchChecksums(ch.prefix + base)
			// 通道容量够所有 goroutine 写完，没人读也不会把它们挂住。
			votesCh <- checksumVote{party: ch.party, sums: sums, err: err}
		}(ch)
	}

	direct, directErr := fetchChecksums(channels[0].prefix + base)
	if directErr == nil {
		wg.Wait()
		applog.Info("[Updater] 校验清单来源: %s", channels[0].party)
		return direct, nil
	}
	applog.Warn("[Updater] 校验清单直连失败: %v，改问加速站", directErr)

	wg.Wait()
	close(votesCh)
	var votes []checksumVote
	for vote := range votesCh {
		votes = append(votes, vote)
	}

	sums, party, err := decideChecksums(direct, directErr, votes)
	if err != nil {
		return nil, err
	}
	applog.Info("[Updater] 校验清单来源: %s", party)
	return sums, nil
}

// fileSHA256 计算文件摘要。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// requireArtifact 是下载前的闸门：拿不到可信摘要就不下载。
//
// 失败必须挡住而不是放行，否则这一层等于没有——第三方代理替换 31MB 的 exe 之后，
// InstallUpdate 会照着用户点下去的按钮把它换进应用目录。
func requireArtifact(rawURL string) (string, error) {
	owner, repo, tag, file, ok := parseReleaseAsset(rawURL)
	if !ok {
		return "", apperror.New(apperror.Validation, "下载地址不是 GitHub Release 资源，无法校验")
	}
	checksums, err := fetchReleaseChecksums(owner, repo, tag)
	if err != nil {
		// fetchReleaseChecksums 给的是带码错误，话里已经说清是直连没通还是各家对不上；
		// 再套一层只会把两句话拼成一句更长的。
		return "", err
	}
	digest, found := checksums[strings.ToLower(file)]
	if !found {
		return "", apperror.Newf(apperror.Corrupt, "%s 不在 %s 里，发布产物与清单不一致", file, checksumsAssetName)
	}
	applog.Info("[Updater] 校验摘要已获取: %s sha256=%s", file, digest[:12])
	return digest, nil
}

// verifyArtifact 比对下载结果。调用方拿到错误后必须删掉文件，
// 不能留在应用目录里等着被 InstallUpdate 执行。
func verifyArtifact(path, expected string) error {
	if expected == "" {
		return fmt.Errorf("缺少期望摘要")
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("校验和不匹配：期望 %.12s…，实际 %.12s…", expected, actual)
	}
	return nil
}

// digestSuffix 是"这个产物已经被校验过"的凭据文件后缀。
//
// 只有摘要在下载完成时算过一次、装的时候却不再核一遍，等于给"已下载"入口开了个
// 免检通道：更新包被截断、被别的程序改过、或者干脆是上一次安装到一半留下的残骸，
// 都能直接换掉应用本体。凭据写在产物旁边，随产物一起生灭。
const digestSuffix = ".sha256"

func digestRecordPath(path string) string { return path + digestSuffix }

// recordDigest 在 verifyArtifact 通过之后调用，把期望摘要落到凭据文件里。
func recordDigest(path, digest string) error {
	return os.WriteFile(digestRecordPath(path), []byte(digest+"\n"), 0644)
}

// clearDigestRecord 删凭据。凡是删产物的分支都要一起删，否则旧凭据会给一个
// 全新（还没校验过）的同名产物盖章。
func clearDigestRecord(path string) { _ = os.Remove(digestRecordPath(path)) }

func loadDigestRecord(path string) (string, error) {
	data, err := os.ReadFile(digestRecordPath(path))
	if err != nil {
		return "", err
	}
	digest := strings.ToLower(strings.TrimSpace(string(data)))
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("摘要记录长度不对：%d 而不是 %d", len(digest), sha256.Size*2)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("摘要记录不是十六进制")
	}
	return digest, nil
}

// hasDigestRecord 只做存在性与格式检查，不读产物内容：给扫描入口用，
// 免得为了判断"能不能装"在启动路径上哈希一个 31MB 的文件。
func hasDigestRecord(path string) bool {
	_, err := loadDigestRecord(path)
	return err == nil
}

// verifyRecordedArtifact 是安装前的最后一道：拿下载时记下的摘要重新哈希一遍。
// 没有凭据（旧版本下载的包、手工放进应用目录的文件）一律拒绝。
func verifyRecordedArtifact(path string) error {
	expected, err := loadDigestRecord(path)
	if err != nil {
		return apperror.Wrap(apperror.Corrupt, err, "更新包没有可信的摘要记录，请重新下载")
	}
	if err := verifyArtifact(path, expected); err != nil {
		return apperror.Wrap(apperror.Corrupt, err, "更新包与下载时记录的摘要不一致，请重新下载")
	}
	return nil
}

// updateStagingName 给下载产物定一个稳定的暂存名，但保留真实扩展名。
//
// 扩展名不能写死成 .exe：资源可能是 .zip/.msi，而 InstallUpdate 是按扩展名分派的。
// 把 zip 存成 .exe 会让热替换脚本拿 zip 字节覆盖掉应用本体，等于把安装目录弄坏。
func updateStagingName(rawURL string) string {
	_, _, _, file, ok := parseReleaseAsset(rawURL)
	ext := ""
	if ok {
		ext = strings.ToLower(filepath.Ext(file))
	}
	if !assetExtPattern.MatchString(ext) {
		// 认不出合法扩展名就统一成 .bin：InstallUpdate 只会走"打开目录"分支，不会执行它。
		ext = ".bin"
	}
	return stagingPrefix + ext
}

// assetExtPattern 只接受 1-5 位小写字母数字扩展名，避免把 URL 片段当文件名。
var assetExtPattern = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)

// normalizeCase 在 Windows 上按大小写不敏感比较路径：前端回传的路径大小写可能和
// os.Executable() 给出来的不一致，直接 filepath.Rel 会判定成"不在目录内"而装不上。
func normalizeCase(p string) string {
	if goruntime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// isUpdateArtifact 判定 filePath 是否是本更新器落在应用目录里的产物。
//
// InstallUpdate 最终会 exec.Command(filePath)，而这个方法是对前端（含扩展包）暴露的
// 绑定，不校验就等于给了一个"让应用启动任意可执行文件"的入口。
// 历史命名 {exeBase}_update*.ext 也要认，否则用户已经下载好的包会装不上。
func isUpdateArtifact(filePath string) bool {
	if strings.TrimSpace(filePath) == "" {
		return false
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	appDir, err := filepath.Abs(getAppDir())
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(normalizeCase(appDir), normalizeCase(abs))
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	// 只接受应用目录直属的文件，不接受子目录里的东西。
	if strings.ContainsRune(rel, rune(filepath.Separator)) {
		return false
	}
	base := strings.ToLower(filepath.Base(abs))
	if strings.HasPrefix(base, strings.ToLower(stagingPrefix)) {
		return true
	}
	exeName := strings.ToLower(getAppExeName())
	if exeName == "" {
		return false
	}
	return strings.HasPrefix(base, strings.TrimSuffix(exeName, filepath.Ext(exeName))+"_update")
}
