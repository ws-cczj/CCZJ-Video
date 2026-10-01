package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// updatedLayout 精确到分钟就够回答「我刚改的文件生效了吗」。多出来的秒数只会把
// 扩展包列表那一行拉长，而那一行本来就要和文件名、体积挤在一起。
const updatedLayout = "2006-01-02 15:04"

// defaultIconNames 是「不写 manifest 也有图标」的那几个约定名。手工做的包十个有八九
// 就叫这些名字，扫到就用，省掉「图标准备好了怎么还是不显示」这一整类问题。
var defaultIconNames = []string{"icon.png", "icon.jpg", "icon.jpeg", "icon.webp", "icon.gif"}

// findDefaultIcon 在包根目录里找那张约定名的图。找不到、太大、是个目录都算「没有图标」
// 而不是错误：约定的东西不该反过来把包判死。
func findDefaultIcon(packDir string) string {
	for _, name := range defaultIconNames {
		path := filepath.Join(packDir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || !fileWithinCap(path, maxIconBytes) {
			continue
		}
		return name
	}
	return ""
}

// resolveIcon 校验 manifest.icon：扩展名要落在 imageMimes（ReadAsset 只认那几种），
// 路径要在包内，体积另有比背景图小得多的上限。空串是「这个包没声明图标」，不是错误，
// 于是去包根找一张约定名的图。
// 出错就整包判 invalid——「图标读不出来就当没有」会让一个写错的 manifest 看起来
// 像我们的渲染坏了，而这个包其它段其实也没被检查过第二轮。
func resolveIcon(packDir, relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return findDefaultIcon(packDir), nil
	}
	if _, isImage := imageMimes[strings.ToLower(filepath.Ext(relPath))]; !isImage {
		return "", newPackError(reasonPathInvalid, "icon %q 不是支持的图片扩展名", relPath)
	}
	path, err := resolveInside(packDir, relPath)
	if err != nil {
		return "", inSection(err, "icon")
	}
	if !fileWithinCap(path, maxIconBytes) {
		return "", newPackError(reasonPathTooLarge, "icon 超过 %d KiB 上限", maxIconBytes/1024)
	}
	return filepath.ToSlash(relPath), nil
}

// packStats 数出包目录里实际的文件数与总字节，以及最后一次改动时间。
//
// 杂物过滤复用 installSkipped：`.git` 里那两千个文件不该出现在「这个扩展包有
// 2031 个文件」这句话里——那行数字是给用户核对「我自己的包内容」的。
// 读不到的单个条目只是不计入统计（正在被删、权限不够），绝不让整包扫描失败。
func packStats(packDir string) (files int, bytes int64, updated string) {
	var latest time.Time
	walkErr := filepath.WalkDir(packDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(packDir, path)
		if relErr != nil || installSkipped(filepath.ToSlash(rel)) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		files++
		bytes += info.Size()
		if mtime := info.ModTime(); mtime.After(latest) {
			latest = mtime
		}
		return nil
	})
	if walkErr != nil {
		return 0, 0, ""
	}
	if latest.IsZero() {
		return files, bytes, ""
	}
	return files, bytes, latest.Format(updatedLayout)
}
