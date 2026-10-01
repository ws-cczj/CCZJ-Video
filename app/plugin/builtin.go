package plugin

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

// 内置扩展包：设置页的「动画」「日志」「诊断」三个分组由它们提供（docs/plugins.md §6.4）。
// 面板本体仍然编译在应用里，包只负责把它挂上分组条——于是「要不要这一项」变成了
// 「这个文件夹在不在」，用户删掉就是去掉，应用不替他留着。
//
// all: 是必需的：内嵌目录里将来若出现 `_` 或 `.` 开头的文件，默认规则会静默跳过它们。
//
//go:embed all:builtin
var builtinFS embed.FS

// builtinSeedVersion 是「这一批内置包的形状」的版本号，记在标记文件第一行。
// 改动内置包（补一张图标之类）时把它 +1：下一轮只往在场的包里补缺失的文件，
// 既不复活用户删掉的包，也不覆盖用户改过的文件。
const builtinSeedVersion = "3"

// builtinMarkerName 用点号开头：Scan 那一层跳过点开头的目录，marker 因此不会
// 被当成一个校验失败的扩展包列进注册表。
const builtinMarkerName = ".cczj-builtin-seed"

const builtinSourceRoot = "builtin"

// builtinIDs 是内嵌那批内置包的目录名集合，init 时算一次：扫描要给每一行标「内置」，
// 卸载要按它拒绝删应用自己的东西。判定用目录名而不是「磁盘上有没有 marker」——
// 用户把 logs-panel 删了又想装回来时，那一行照样是内置包，不给卸载按钮。
var builtinIDs = func() map[string]bool {
	set := make(map[string]bool)
	entries, err := fs.ReadDir(builtinFS, builtinSourceRoot)
	if err != nil {
		return set
	}
	for _, entry := range entries {
		if entry.IsDir() {
			set[entry.Name()] = true
		}
	}
	return set
}()

// isBuiltinPackID 报告这个目录名是不是应用自带的扩展包。
func isBuiltinPackID(name string) bool { return builtinIDs[name] }

// SeedBuiltin 把内置扩展包落进 <dataDir>/plugins。
//
// 「种过的不重种」是这条功能的定义而不是实现细节：卸载的语义就是删文件夹，如果每次
// 启动都重种一遍，那个文件夹就永远删不掉。所以标记文件里除了版本号还记着「哪些包已经
// 种过」，只有没记到的包才会被整个写出来；记到过的只往里补缺失的文件（新图标就走这
// 条路），一个字节都不覆盖用户改过的东西。数据目录还没解析出来时静默返回——首次运行
// 早期没有任何扩展包可读，下一轮扫描之前一定会补上。
func (s *Service) SeedBuiltin() error {
	root := s.Directory()
	if strings.TrimSpace(root) == "" {
		return nil
	}
	ids, err := embeddedBuiltinIDs()
	if err != nil {
		return err
	}

	marker := filepath.Join(root, builtinMarkerName)
	version, seeded := readSeedMarker(marker)
	if version == "" {
		// 没有标记 = 这个数据目录从没播种过。
	} else if len(seeded) == 0 {
		// 旧格式只写了版本号，没记包名。这一批一律当作「已经种过」：目录在的补文件，
		// 目录不见了的算用户自己删掉的。从这一版起标记里带着包名，将来不必再猜。
		for _, id := range ids {
			seeded[id] = true
		}
	}
	if version == builtinSeedVersion && allSeeded(seeded, ids) {
		return nil
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "创建扩展包目录失败")
	}

	installed := make([]string, 0, len(ids))
	filledCount := 0
	for _, id := range ids {
		switch {
		case dirExists(filepath.Join(root, id)):
			// 目录在：只往里补缺失的文件（新图标走这条），已存在的一个字节都不动。
			n, err := fillBuiltinPack(root, id)
			if err != nil {
				return err
			}
			filledCount += n
		case !seeded[id]:
			// 没种过也没有同名目录：整个写出来。
			if err := writeBuiltinPack(root, id); err != nil {
				return err
			}
			installed = append(installed, id)
		}
		// 第三种情况——种过但目录不见了——是用户删的，什么都不做。
		seeded[id] = true
	}

	if err := writeSeedMarker(marker, builtinSeedVersion, seeded); err != nil {
		return apperror.Wrap(apperror.Storage, err, "写入内置扩展包标记失败")
	}
	if len(installed) > 0 || filledCount > 0 {
		applog.InfoFields("内置扩展包已落盘", applog.Fields{
			"packs":  strings.Join(installed, ","),
			"filled": filledCount,
			"seed":   builtinSeedVersion,
		})
	}
	return nil
}

// embeddedBuiltinIDs 列出内嵌那批内置包的目录名，排序是为了让标记文件的内容稳定可比。
func embeddedBuiltinIDs() ([]string, error) {
	entries, err := fs.ReadDir(builtinFS, builtinSourceRoot)
	if err != nil {
		return nil, apperror.Wrap(apperror.Internal, err, "读取内置扩展包失败")
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func allSeeded(seeded map[string]bool, ids []string) bool {
	for _, id := range ids {
		if !seeded[id] {
			return false
		}
	}
	return true
}

// readSeedMarker 读标记文件：第一行是清单版本，其余每行一个「已经种过」的包名。
// 文件不存在或读不出来返回空版本，让调用方按「从没播种过」处理。
func readSeedMarker(path string) (string, map[string]bool) {
	seeded := make(map[string]bool)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", seeded
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", seeded
	}
	for _, line := range lines[1:] {
		if name := strings.TrimSpace(line); name != "" {
			seeded[name] = true
		}
	}
	return strings.TrimSpace(lines[0]), seeded
}

func writeSeedMarker(path, version string, seeded map[string]bool) error {
	names := make([]string, 0, len(seeded))
	for name := range seeded {
		names = append(names, name)
	}
	sort.Strings(names)
	var body strings.Builder
	body.WriteString(version)
	body.WriteByte('\n')
	for _, name := range names {
		body.WriteString(name)
		body.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(body.String()), 0o644)
}

// builtinPackFiles 把一个内嵌包目录读成「目标绝对路径 + 内容」列表。
// 先收集再落盘：中途读失败（内嵌文件被改坏之类）时不会留下半个包目录，否则那个残缺
// 目录会被 Scan 报成校验失败，看起来像是包本身写坏了。
func builtinPackFiles(root, id string) ([]pendingFile, error) {
	sourceDir := builtinSourceRoot + "/" + id
	target := filepath.Join(root, id)
	var files []pendingFile
	walkErr := fs.WalkDir(builtinFS, sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, sourceDir+"/")
		full, err := lexicalInside(target, rel)
		if err != nil {
			return err
		}
		data, readErr := builtinFS.ReadFile(path)
		if readErr != nil {
			return apperror.Wrap(apperror.Internal, readErr, fmt.Sprintf("读取内嵌文件 %s 失败", path))
		}
		files = append(files, pendingFile{path: full, data: data})
		return nil
	})
	if walkErr != nil {
		// 越界那类原因码（reason_path_invalid）要原样传出去：外面再套一层码会把
		// 界面认得的 reason_code 盖掉。其余（内嵌文件读不出）才是包本身不可用。
		var pe *packError
		if errors.As(walkErr, &pe) {
			return nil, walkErr
		}
		return nil, apperror.Wrap(apperror.Internal, walkErr, fmt.Sprintf("内置扩展包 %q 不可用", id))
	}
	return files, nil
}

type pendingFile struct {
	path string
	data []byte
}

// writeBuiltinPack 把一个内置包目录整个写到 <root>/<id>。目标路径仍走本包的词法
// 边界（lexicalInside）：内嵌内容是我们自己编译进去的，但越界规则不该在这里再抄一遍。
func writeBuiltinPack(root, id string) error {
	target := filepath.Join(root, id)
	files, err := builtinPackFiles(root, id)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			os.RemoveAll(target)
			return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("创建 %q 失败", filepath.Dir(file.path)))
		}
		// 0o644 而不是只读：内置包同样允许用户改，改坏了由校验和扩展包面板接住。
		if err := os.WriteFile(file.path, file.data, 0o644); err != nil {
			os.RemoveAll(target)
			return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("写入 %q 失败", file.path))
		}
	}
	return nil
}

// fillBuiltinPack 只往已有的包目录里补「内嵌有、磁盘上没有」的文件，返回补了几个。
// 内置包后来加的图标就靠这一步进到已经存在的安装里；已存在的文件一律不碰，
// 用户改过的 manifest、删掉的入口文件都是他的决定，重种一次不该替他推翻。
func fillBuiltinPack(root, id string) (int, error) {
	files, err := builtinPackFiles(root, id)
	if err != nil {
		return 0, err
	}
	filled := 0
	for _, file := range files {
		if _, statErr := os.Stat(file.path); statErr == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			return filled, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("创建 %q 失败", filepath.Dir(file.path)))
		}
		if err := os.WriteFile(file.path, file.data, 0o644); err != nil {
			return filled, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("写入 %q 失败", file.path))
		}
		filled++
	}
	return filled, nil
}
