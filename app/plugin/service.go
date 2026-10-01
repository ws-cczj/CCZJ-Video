package plugin

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

// Service 扫描 <dataDir>/plugins、校验每个扩展包，并维护启用/禁用状态。
// 目录只在用到时才解析（dataDir 是惰性函数），所以它可以早于数据库和
// 应用数据目录被构造出来。
type Service struct {
	dataDir func() string

	mu      sync.Mutex
	infos   []Info
	scanned bool
	// scanMu 让并发的手动重扫排队进行，而不是各扫各的、日志交错。
	scanMu sync.Mutex

	// 状态读写在构造时指向 settings KV（SQLite）。app/db 要库打开后才可用，
	// 因此只有 Scan / SetEnabled 会碰它，NewService 不碰。测试注入自己的实现。
	loadStates func() (map[string]bool, error)
	saveStates func(map[string]bool) error
}

// NewService 创建一个扩展包注册表。dataDir 是应用数据目录的取值函数
// （传方法值，不要传调用结果），扩展包根目录是它的 plugins 子目录。
func NewService(dataDir func() string) *Service {
	return &Service{
		dataDir:    dataDir,
		loadStates: loadStatesFromDB,
		saveStates: saveStatesToDB,
	}
}

// Directory 返回扩展包根目录 <dataDir>/plugins，不创建它。
func (s *Service) Directory() string {
	if s.dataDir == nil {
		return ""
	}
	root := s.dataDir()
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, "plugins")
}

// Scan 完整重扫一次并替换缓存的注册表。
//
// 扫描深度只有一层（docs/plugins.md §1）：目录名以 `.` 或 `_` 开头的、
// 以及根本不是目录的条目都跳过；没有 plugin.json 的目录不是扩展包，
// 静默跳过而不是报成 invalid——否则用户放个备份文件夹就多一张错误卡片。
func (s *Service) Scan() ([]Info, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	root := s.Directory()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			// 首次运行还没有 plugins 目录，空注册表不算错误。
			s.replace(nil)
			s.logScan(root, nil)
			return []Info{}, nil
		}
		return nil, apperror.Wrap(apperror.Storage, err, "读取扩展包目录失败")
	}

	infos := make([]Info, 0, len(entries))
	packCount := 0
	for _, entry := range entries {
		// os.ReadDir 已按名字排序，所以「后来的那个包」是确定的：目录名字典序最大的
		// 那个承担 id_duplicate。
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		packDir := filepath.Join(root, name)
		info, found := loadPack(packDir, packCount >= maxPacks)
		if !found {
			continue
		}
		packCount++
		// 统计独立于校验结果：invalid 的那一行更需要这行数字——用户正是要对着
		// 「有几个文件、什么时候改的」来判断自己的文件夹到底是不是他想装的那个。
		info.Files, info.Bytes, info.Updated = packStats(packDir)
		// 按目录名判定内置包：注册表里这一行是不是应用自己种下去的那个。
		info.Builtin = isBuiltinPackID(name)
		infos = append(infos, info)
	}

	markDuplicateIDs(infos)

	states := s.states()
	applyStates(infos, states)
	s.replace(infos)
	s.logScan(root, infos)
	return append([]Info(nil), infos...), nil
}

// List 返回缓存的注册表，第一次调用时触发扫描。
func (s *Service) List() ([]Info, error) {
	return s.snapshot()
}

// SetEnabled 持久化某个包的启用状态。只有注册表里真实存在的包才接受，
// 免得界面拿着一个已经删掉的包 id 写进设置表。
func (s *Service) SetEnabled(id string, enabled bool) error {
	if strings.TrimSpace(id) == "" {
		return apperror.New(apperror.Validation, "扩展包 id 为空")
	}
	infos, err := s.snapshot()
	if err != nil {
		return err
	}
	known := false
	for _, info := range infos {
		if info.ID == id {
			known = true
			break
		}
	}
	if !known {
		return apperror.Newf(apperror.NotFound, "未找到扩展包 %q", id)
	}

	states := s.states()
	if enabled {
		delete(states, id)
	} else {
		states[id] = false
	}
	if err := s.saveStates(states); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存扩展包状态失败")
	}

	// 状态变了，缓存里那一行的 Status 也要跟着变；无效包不受开关影响，
	// 它本来就是 invalid。
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.infos {
		if s.infos[i].ID != id {
			continue
		}
		switch s.infos[i].Status {
		case StatusReady:
			if !enabled {
				s.infos[i].Status = StatusDisabled
			}
		case StatusDisabled:
			if enabled {
				s.infos[i].Status = StatusReady
			}
		}
	}
	return nil
}

// ReadFile 返回包内某个文本文件的内容（UTF-8）。只认 §6 的体积上限，
// 路径一律走 resolveInside，越出包目录的直接拒绝。
func (s *Service) ReadFile(id, relPath string) (string, error) {
	packDir, err := s.packDir(id)
	if err != nil {
		return "", err
	}
	if !textExtensions[strings.ToLower(filepath.Ext(relPath))] {
		return "", apperror.Newf(apperror.Unsupported, "%q 不是可读取的文本类型（json/txt/md/glsl/vert/frag/yaml/yml/csv/js/mjs/css）", relPath)
	}
	path, resolveErr := resolveInside(packDir, relPath)
	if resolveErr != nil {
		return "", resolveErr
	}
	data, readErr := readCapped(path, maxTextReadBytes)
	if readErr != nil {
		return "", readErr
	}
	if !utf8.Valid(data) {
		return "", apperror.Newf(apperror.Unsupported, "%q 不是 UTF-8 文本", relPath)
	}
	return string(data), nil
}

// ReadAsset 把包内图片读成 data:<mime>;base64,...，给界面当 <img src> 用。
// 图片只按 §6 的 8 MiB 上限走，扩展名不在白名单里的一律拒绝。
func (s *Service) ReadAsset(id, relPath string) (string, error) {
	packDir, err := s.packDir(id)
	if err != nil {
		return "", err
	}
	mime, ok := imageMimes[strings.ToLower(filepath.Ext(relPath))]
	if !ok {
		return "", apperror.Newf(apperror.Unsupported, "%q 不是支持的图片类型（png/jpg/jpeg/webp/gif/bmp/avif）", relPath)
	}
	path, resolveErr := resolveInside(packDir, relPath)
	if resolveErr != nil {
		return "", resolveErr
	}
	data, readErr := readCapped(path, maxImageBytes)
	if readErr != nil {
		return "", readErr
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// ---------- 内部 ----------

func (s *Service) snapshot() ([]Info, error) {
	s.mu.Lock()
	if s.scanned {
		out := append([]Info(nil), s.infos...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	return s.Scan()
}

func (s *Service) replace(infos []Info) {
	s.mu.Lock()
	s.infos = infos
	s.scanned = true
	s.mu.Unlock()
}

func (s *Service) packDir(id string) (string, error) {
	infos, err := s.snapshot()
	if err != nil {
		return "", err
	}
	for _, info := range infos {
		if info.ID == id {
			if strings.TrimSpace(info.Dir) == "" {
				return "", apperror.Newf(apperror.Internal, "扩展包 %q 没有可用目录", id)
			}
			return info.Dir, nil
		}
	}
	return "", apperror.Newf(apperror.NotFound, "未找到扩展包 %q", id)
}

// states 读启用状态。设置值坏了不该让整个注册表消失：报一条警告，按全启用继续。
func (s *Service) states() map[string]bool {
	states, err := s.loadStates()
	if err != nil {
		applog.Warn("解析 %s 设置失败，按全部启用处理: %v", settingsKey, err)
		return make(map[string]bool)
	}
	if states == nil {
		return make(map[string]bool)
	}
	return states
}

func applyStates(infos []Info, states map[string]bool) {
	for i := range infos {
		if infos[i].Status != StatusReady {
			continue
		}
		if enabled, recorded := states[infos[i].ID]; recorded && !enabled {
			infos[i].Status = StatusDisabled
		}
	}
}

// markDuplicateIDs 在按目录名排序的注册表上做一次去重：后来的那个包判
// id_duplicate，先出现的保持原样。已经无效的包不参与——它的错误更靠前。
func markDuplicateIDs(infos []Info) {
	seen := make(map[string]bool, len(infos))
	for i := range infos {
		if infos[i].Status == StatusInvalid {
			continue
		}
		if seen[infos[i].ID] {
			infos[i].Status = StatusInvalid
			infos[i].ReasonCode = reasonIDDuplicate
			infos[i].Reason = fmt.Sprintf("扩展包 id %q 已被前面的包占用", infos[i].ID)
			infos[i].Source = nil
			infos[i].Shader = nil
			infos[i].Theme = nil
			infos[i].Script = nil
			continue
		}
		seen[infos[i].ID] = true
	}
}

func (s *Service) logScan(root string, infos []Info) {
	ready, disabled, invalid := 0, 0, 0
	for _, info := range infos {
		switch info.Status {
		case StatusReady:
			ready++
		case StatusDisabled:
			disabled++
		case StatusInvalid:
			invalid++
			applog.WarnFields("扩展包校验未通过", applog.Fields{
				"id":          info.ID,
				"dir":         info.Dir,
				"reason_code": info.ReasonCode,
				"reason":      info.Reason,
			})
		}
	}
	applog.InfoFields("扩展包扫描完成", applog.Fields{
		"root":     root,
		"packs":    len(infos),
		"ready":    ready,
		"disabled": disabled,
		"invalid":  invalid,
	})
}
