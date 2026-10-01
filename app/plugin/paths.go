package plugin

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// cleanRelative 是本包路径边界的词法规则，返回清洗后的包内相对路径（正斜杠）：
//
//   - 拒绝空串、绝对路径、盘符、任何 `..` 片段、反斜杠（Windows 逃逸写法）、
//     连续或结尾的 `/`；
//   - filepath.Clean 之后必须仍指到一个具体路径，"." 不算。
//
// resolveInside 与拖放安装（install.go）都走它，越界规则只写一处，免得安装侧的
// 清洗和读取侧的清洗哪天漂移出差别。
func cleanRelative(relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", newPackError(reasonPathInvalid, "路径为空")
	}
	if strings.ContainsRune(relPath, '\\') {
		return "", newPackError(reasonPathInvalid, "路径 %q 含反斜杠，manifest 只接受 / 分隔的包内相对路径", relPath)
	}
	if strings.Contains(relPath, ":") {
		return "", newPackError(reasonPathInvalid, "路径 %q 含盘符或冒号，只接受包内相对路径", relPath)
	}
	if strings.HasPrefix(relPath, "/") || filepath.IsAbs(filepath.FromSlash(relPath)) {
		return "", newPackError(reasonPathInvalid, "路径 %q 是绝对路径，只接受包内相对路径", relPath)
	}
	for _, segment := range strings.Split(relPath, "/") {
		if segment == ".." {
			return "", newPackError(reasonPathInvalid, "路径 %q 含 .. 片段，越出包目录", relPath)
		}
		if segment == "" {
			return "", newPackError(reasonPathInvalid, "路径 %q 含空片段（连续或结尾的 /）", relPath)
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	if cleaned == "." || cleaned == "" {
		return "", newPackError(reasonPathInvalid, "路径 %q 没有指到包内的文件", relPath)
	}
	return cleaned, nil
}

// lexicalInside 在 cleanRelative 之后再用 filepath.Rel 复算一次「仍在基准目录内」，
// 返回拼好的绝对路径。
func lexicalInside(base, relPath string) (string, error) {
	rel, err := cleanRelative(relPath)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, filepath.FromSlash(rel))
	check, err := filepath.Rel(base, full)
	if err != nil || check == "." || !withinParent(check) {
		return "", newPackError(reasonPathInvalid, "路径 %q 解析后越出包目录", relPath)
	}
	return full, nil
}

// resolveInside 把 manifest 里写的相对路径落到包目录内：先走 lexicalInside，
// 再补三条只查磁盘的规则——目标必须存在且是普通文件；最后 EvalSymlinks 一次，
// 符号链接真身越出包目录的一律拒绝，否则 `plugin.json` 里写 `entry: link.glsl`
// 就能把 `%USERPROFILE%\.ssh\id_rsa` 之类的文件端给界面。
//
// 返回的是拼接后的词法路径（调用方按它读取），越界判定用的是解析后的真身。
func resolveInside(packDir, relPath string) (string, error) {
	full, err := lexicalInside(packDir, relPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", newPackError(reasonPathMissing, "引用的文件不存在: %q", relPath)
	}
	if !info.Mode().IsRegular() {
		return "", newPackError(reasonPathMissing, "引用的路径不是普通文件: %q", relPath)
	}

	// 符号链接：真身必须还在包目录里。包目录自己也 EvalSymlinks 一次，
	// 免得两边一个是短名/链接名、一个是真身而比出假阳性。
	realPack, err := filepath.EvalSymlinks(packDir)
	if err != nil {
		return "", newPackError(reasonPathMissing, "无法解析包目录: %v", err)
	}
	realTarget, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", newPackError(reasonPathMissing, "无法解析 %q: %v", relPath, err)
	}
	realRel, err := filepath.Rel(realPack, realTarget)
	if err != nil || realRel == "." || !withinParent(realRel) {
		return "", newPackError(reasonPathInvalid, "路径 %q 经符号链接越出包目录", relPath)
	}
	return full, nil
}

// withinParent 判断 filepath.Rel 的结果是否仍在基准目录之内。
func withinParent(rel string) bool {
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

// readCapped 读取包内文件，超过 capBytes 直接判错，绝不截断（§6）。
// LimitReader 保证一个「声称 8 MiB 其实 5 GB」的文件也只能被读进 cap+1 字节。
func readCapped(path string, capBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, newPackError(reasonPathMissing, "无法打开 %q: %v", filepath.Base(path), err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, capBytes+1))
	if err != nil {
		return nil, newPackError(reasonPathMissing, "读取 %q 失败: %v", filepath.Base(path), err)
	}
	if int64(len(data)) > capBytes {
		return nil, newPackError(reasonPathTooLarge, "%q 超过 %d MiB 上限", filepath.Base(path), capBytes>>20)
	}
	return data, nil
}

// fileWithinCap 用 Stat 做一次廉价体积检查（图片这种「只要存在、内容不进注册表」的文件），
// 真正的读取仍然各自带 LimitReader。
func fileWithinCap(path string, capBytes int64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() <= capBytes
}
