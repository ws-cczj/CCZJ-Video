package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cczjVideo/app/apperror"
)

// installFile 是准备写进暂存区的一个文件：包内相对路径 + 已经读好的字节。
type installFile struct {
	rel  string
	data []byte
}

// InstallFromPath 把一次原生拖放装进 <dataDir>/plugins/<manifest id>。
//
// 为什么 Go 直接读磁盘上的那个文件夹：Wails 开了 EnableFileDrop 之后，WebView2 只
// 把被拖东西的绝对路径交给应用（ICoreWebView2File::GetPath），目录里的字节前端根本
// 拿不到。所以「前端读成 base64 再交下来」那条路作废，这里自己走一遍目录树。
//
// 落盘只在 plugin-staging 里进行，校验通过才换进 plugins：拖进来一个写坏的包等于
// 什么都没装，返回的错误带 reason_code，界面能把原因显示出来。换入用两次 rename——
// 同号旧包先挪开、新的挪进来，挪进来失败就把旧的放回原位，用户不会因为一次失败的
// 安装同时失去新旧两份。完成后重扫一次，返回注册表里那条权威 Info。
func (s *Service) InstallFromPath(path string) (*Info, error) {
	root := s.Directory()
	stagingRoot := s.stagingRoot()
	if strings.TrimSpace(root) == "" || strings.TrimSpace(stagingRoot) == "" {
		return nil, apperror.New(apperror.Unavailable, "应用数据目录不可用，无法安装扩展包")
	}
	label := installLabel(path)
	contents, err := readPackTree(path)
	if err != nil {
		return nil, err
	}

	// 整包先写进这个临时目录，最后才 rename 进 plugins：同卷上的目录 rename 是原子的，
	// 比「边写边被扫描」安全——扫描只认 plugins 的一层子目录。
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法创建临时目录 %q", stagingRoot))
	}
	base, err := os.MkdirTemp(stagingRoot, "install-")
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法创建临时安装目录（在 %q 下）", stagingRoot))
	}
	defer os.RemoveAll(base)

	staged := filepath.Join(base, "pack")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法创建临时目录 %q", staged))
	}
	for _, file := range contents {
		target, err := lexicalInside(staged, file.rel)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法创建 %q 的上级目录", file.rel))
		}
		if err := os.WriteFile(target, file.data, 0o644); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法写入 %q", file.rel))
		}
	}
	return s.installStaged(base, label)
}

// installStaged 是安装的后面一半：校验暂存好的那个目录，通过才换进 plugins。
// base 里此刻应当有一个叫 "pack" 的子目录。
func (s *Service) installStaged(base, label string) (*Info, error) {
	root := s.Directory()
	staged := filepath.Join(base, "pack")
	doc, err := readInstallManifest(staged, label)
	if err != nil {
		return nil, err
	}

	// 目录名由 manifest 的 id 决定：拖进来的文件夹叫什么都不算数。先把暂存目录
	// 改名成 id，loadPack 的「id 必须等于目录名」检查才落在最终形状上。
	named := filepath.Join(base, doc.ID)
	if err := os.Rename(staged, named); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法整理临时目录 %q", staged))
	}
	info, found := loadPack(named, false)
	if !found {
		return nil, apperror.Newf(apperror.Validation, "%s 里没有 %s，它不是一个扩展包文件夹", label, manifestFileName)
	}
	if info.Status != StatusReady {
		return nil, newPackError(info.ReasonCode, "%s：校验未通过，没有安装任何东西", info.Reason)
	}

	target := filepath.Join(root, doc.ID)
	replacing := dirExists(target)
	if !replacing {
		// §6 的数量上限本来由扫描报成 invalid 卡片；安装这一步先拦住，免得界面
		// 刚说「已安装」就甩出一张「超出上限」的错误。覆盖同名包不受影响。
		count, err := countInstalledPacks(root)
		if err == nil && count >= maxPacks {
			return nil, newPackError(reasonLimitExceeded,
				"扩展包数量已达上限 %d，请先删掉不用的包再拖进来", maxPacks)
		}
	}

	previous := filepath.Join(base, "previous")
	hasPrevious := false
	if replacing {
		if err := os.Rename(target, previous); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法移开同名旧包 %q", doc.ID))
		}
		hasPrevious = true
	}
	if err := os.Rename(named, target); err != nil {
		if hasPrevious {
			if restoreErr := os.Rename(previous, target); restoreErr != nil {
				// 装失败 + 回位也失败：两个错误只能带一个 Cause，把回位的原文写进消息，
				// 否则用户照着「请手工放回」去挪目录时，连为什么没自动放回都不知道。
				return nil, apperror.Wrap(apperror.Storage, err,
					fmt.Sprintf("安装 %q 失败，且旧包没能自动放回（%v），请把 %q 挪回扩展包目录", doc.ID, restoreErr, previous))
			}
		}
		return nil, apperror.Wrap(apperror.Storage, err, "无法安装到扩展包目录")
	}

	infos, err := s.Scan()
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if infos[i].ID == doc.ID {
			return &infos[i], nil
		}
	}
	installed := info
	installed.Dir = target
	return &installed, nil
}

// stagingRoot 是安装过程的工作目录，放在 plugins 的旁边：扫描只认 plugins 的一层
// 子目录，所以暂存中的包绝不会被扫进注册表。
func (s *Service) stagingRoot() string {
	if s.dataDir == nil {
		return ""
	}
	root := s.dataDir()
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, "plugin-staging")
}

// readPackTree 走一遍被拖进来的目录，清洗成「包内相对路径 + 字节」。
//
// 拖进来的常常不是包本身而是装着它的那一层（解压下载包最常见），所以根下没有
// manifest 时允许往下走一层：恰好一个子目录、且那个子目录里有 manifest 才认，
// 多个子目录或都没有都直接报错——猜错方向的安装比装不上更难查。
func readPackTree(path string) ([]installFile, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, newPackError(reasonEntryEmpty, "拖进来的路径是空的，请拖整个扩展包文件夹")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		// 拖完之后文件夹被挪走/改名是常见情况，和「磁盘读不动」分两码说：
		// 前者界面该提示重新拖一次，后者才是真的装不了。
		if errors.Is(err, fs.ErrNotExist) {
			return nil, apperror.Wrap(apperror.NotFound, err, fmt.Sprintf("%q 已经不在了，请重新拖一次", filepath.Base(trimmed)))
		}
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("读取 %q 失败", filepath.Base(trimmed)))
	}
	if !info.IsDir() {
		return nil, apperror.Newf(apperror.Validation, "%q 是一个文件，请拖整个扩展包文件夹而不是单个文件", filepath.Base(trimmed))
	}
	// 空目录单独说一句：它和「有东西但没有 manifest」是两回事，用户对着空文件夹
	// 读到「没有 plugin.json」会先怀疑自己拖错了地方。
	if entries, err := os.ReadDir(trimmed); err == nil && len(entries) == 0 {
		return nil, newPackError(reasonEntryEmpty, "%s 是空的，里面没有任何文件", filepath.Base(trimmed))
	}
	if _, err := os.Stat(filepath.Join(trimmed, manifestFileName)); err != nil {
		child, found := singlePackChild(trimmed)
		if !found {
			return nil, apperror.Newf(apperror.Validation, "%s 里没有 %s，请一次拖一个扩展包文件夹", filepath.Base(trimmed), manifestFileName)
		}
		return readPackTree(child)
	}
	return walkPackTree(trimmed)
}

// singlePackChild 找出「唯一那个装着 manifest 的子目录」。
func singlePackChild(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var found string
	for _, entry := range entries {
		if !entry.IsDir() || installSkipped(entry.Name()) {
			continue
		}
		child := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(child, manifestFileName)); err != nil {
			continue
		}
		if found != "" {
			return "", false
		}
		found = child
	}
	return found, found != ""
}

// walkPackTree 读一个包目录的全部内容：杂物丢掉、个数与总量上限复算、符号链接
// 不跟。上限在读字节之前判，免得为一个谎称合法的目录先把 16 MiB 灌进内存。
func walkPackTree(dir string) ([]installFile, error) {
	var out []installFile
	var total int
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return apperror.Wrap(apperror.Storage, walkErr, fmt.Sprintf("读取 %q 失败", filepath.Base(dir)))
		}
		if entry.IsDir() {
			// 符号链接目录：WalkDir 不会跟进目录链接本身，但链接指向的位置在 Windows
			// 上可能以「目录」形态出现在枚举里。整棵外面的东西不该被装进包里。
			if path != dir {
				info, statErr := os.Lstat(path)
				if statErr != nil {
					return apperror.Wrap(apperror.Storage, statErr, fmt.Sprintf("读取 %q 失败", filepath.Base(path)))
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return filepath.SkipDir
				}
			}
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return apperror.Wrap(apperror.Storage, statErr, fmt.Sprintf("读取文件信息 %q 失败", path))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return apperror.Wrap(apperror.Internal, relErr, fmt.Sprintf("%q 算不出包内相对路径", path))
		}
		rel = filepath.ToSlash(rel)
		if installSkipped(rel) {
			return nil
		}
		if len(rel) > maxInstallPathChars {
			return newPackError(reasonPathInvalid, "%q 的路径过长（最多 %d 字符）", rel, maxInstallPathChars)
		}
		if len(out)+1 > maxInstallFiles {
			return newPackError(reasonLimitExceeded, "一个扩展包最多 %d 个文件", maxInstallFiles)
		}
		if total+int(info.Size()) > maxInstallTotalBytes {
			return newPackError(reasonPathTooLarge,
				"扩展包文件夹超过 %d MiB 上限", maxInstallTotalBytes>>20)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("无法读取 %q", rel))
		}
		total += len(data)
		if total > maxInstallTotalBytes {
			return newPackError(reasonPathTooLarge,
				"扩展包文件夹超过 %d MiB 上限", maxInstallTotalBytes>>20)
		}
		out = append(out, installFile{rel: rel, data: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, newPackError(reasonEntryEmpty, "%s 里只有系统杂物文件，没有扩展包内容", filepath.Base(dir))
	}
	return out, nil
}

// installJunk 是拖放时会跟着进来的系统杂物，按小写名字匹配。
var installJunk = map[string]bool{
	"thumbs.db":       true,
	"desktop.ini":     true,
	"zone.identifier": true,
}

// installSkipped 逐段看路径：任何一段是点号开头的隐藏项（.DS_Store、.git 里的
// 整个工作区，用户从源码目录直接拖包时最常见）、上面那张表里的系统杂物，或
// 解压产物 __MACOSX，整个文件就丢掉——留着只会白占 §6 的个数与体积上限。
func installSkipped(rel string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		lower := strings.ToLower(segment)
		if strings.HasPrefix(lower, ".") || installJunk[lower] || lower == "__macosx" {
			return true
		}
	}
	return false
}

// readInstallManifest 只解码到拿 id 为止：id 决定目录名，而 loadPack 要求两者
// 一致，所以必须在校验之前先读到它。其余字段一个都不看，全部交给 loadPack。
func readInstallManifest(packDir, label string) (*manifestDoc, error) {
	raw, err := readManifest(packDir)
	if err != nil {
		if errors.Is(err, errManifestAbsent) {
			return nil, apperror.Newf(apperror.Validation, "%s 里没有 %s，请一次拖一个扩展包文件夹", label, manifestFileName)
		}
		return nil, err
	}
	var doc manifestDoc
	if err := decodeStrict(raw, &doc, reasonJSONInvalid); err != nil {
		return nil, inSection(err, manifestFileName)
	}
	if !packIDPattern.MatchString(doc.ID) {
		return nil, newPackError(reasonIDInvalid, "id %q 不符合 ^[a-z0-9][a-z0-9_-]{0,63}$", doc.ID)
	}
	return &doc, nil
}

// countInstalledPacks 数一遍根下有多少个「会被扫描认作扩展包」的目录，
// 规则与 Scan 的入选条件一致（目录、名字不以 . 或 _ 开头、里面有 plugin.json）。
func countInstalledPacks(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, name, manifestFileName)); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// installLabel 用被拖文件夹的名字说话：失败的时候用户认识的只有那个名字，
// manifest 里的 id 他可能还没打开看过。
func installLabel(path string) string {
	name := filepath.Base(filepath.FromSlash(strings.TrimSpace(path)))
	if name == "" || name == "." || name == string(filepath.VolumeName(name)) {
		return "拖进来的文件夹"
	}
	return fmt.Sprintf("文件夹 %q", name)
}
