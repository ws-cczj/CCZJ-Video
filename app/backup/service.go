// Package backup moves user data (settings, favorites, watch history and the
// metadata they point at) between databases without ever replacing one.
package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"

	"github.com/andybalholm/brotli"
)

const (
	// payloadKind 让备份文件和采集源导出文件互不冒充：
	// 两种 JSON 形状不同，喂错入口只会得到「缺少 source 信息」这种看不懂的报错。
	payloadKind     = "cczj.backup"
	payloadVersion  = 1
	maxBackupBytes  = 64 << 20
	maxBackupItems  = 200000
	defaultExportAt = "20060102_150405"
)

// Service owns backup file formats and merge rules independently of Wails.
type Service struct{}

// NewService creates a backup service using the configured database package.
func NewService() *Service { return &Service{} }

// Payload 是备份文件的全部内容。收藏/历史/元数据都以「标题+年份」而不是
// 本机 global_id 的形式落盘，所以文件在别的机器、别的库上也能对上号。
type Payload struct {
	Kind      string               `json:"kind"`
	Version   int                  `json:"version"`
	Exported  string               `json:"exported"`
	App       string               `json:"app"`
	Settings  []db.BackupSetting   `json:"settings"`
	Sources   []*model.Source      `json:"sources,omitempty"`
	Videos    []db.BackupVideoMeta `json:"videos"`
	Favorites []db.BackupFavorite  `json:"favorites"`
	History   []db.BackupHistory   `json:"history"`
}

// Result 是一次导入的逐类计数，界面按这些数字讲结果，不翻译后端文案。
type Result struct {
	SettingsApplied  int `json:"settings_applied"`
	SettingsSkipped  int `json:"settings_skipped"`
	SourcesAdded     int `json:"sources_added"`
	SourcesSkipped   int `json:"sources_skipped"`
	MetaApplied      int `json:"meta_applied"`
	FavoritesAdded   int `json:"favorites_added"`
	FavoritesSkipped int `json:"favorites_skipped"`
	HistoryApplied   int `json:"history_applied"`
	HistorySkipped   int `json:"history_skipped"`
	Unresolved       int `json:"unresolved"`
}

// machineLocalSettings 永远不进导入：这些键描述的是这台机器的窗口、上次启动的
// 版本和升级复位记账。把旧机器的那份盖过来，最坏情况会让升级/复位流程判断失误，
// 那是会丢数据的一类错误。
var machineLocalSettings = map[string]bool{
	"window_width":                        true,
	"window_height":                       true,
	"window_size_key":                     true,
	"last_start_version":                  true,
	"database_reset_version":              true,
	"database_reset_generation":           true,
	"collect.schedule.last_exit_unix_sec": true,
}

// exportableSources 决定备份里是否带上源定义。源带着 API 地址，收藏与历史
// 的「从哪打开」坐标才有意义；目录本体仍走采集源导出，不塞进这里。
func (s *Service) buildPayload(dataDir string) (Payload, error) {
	settings, err := db.ReadAllSettings(db.DB())
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read settings")
	}
	favorites, err := db.ReadFavorites(db.DB())
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read favorites")
	}
	history, err := db.ReadHistory(db.DB())
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read history")
	}
	videos, err := db.ReadLinkedVideoMeta(db.DB())
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read linked metadata")
	}
	sources, err := db.GetAllSources()
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read sources")
	}
	return Payload{
		Kind:      payloadKind,
		Version:   payloadVersion,
		Exported:  time.Now().Format(time.RFC3339),
		App:       filepath.Base(dataDir),
		Settings:  settings,
		Sources:   sources,
		Videos:    videos,
		Favorites: favorites,
		History:   history,
	}, nil
}

// Export writes the full backup as a Brotli-compressed JSON file. An empty
// destination puts it under dataDir/exports, same as the source export does.
func (s *Service) Export(dataDir, destination string) (string, error) {
	payload, err := s.buildPayload(dataDir)
	if err != nil {
		return "", err
	}
	path, err := s.destinationPath(dataDir, destination)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", apperror.Wrap(apperror.Internal, err, "encode backup")
	}
	if err := writeCompressed(path, encoded); err != nil {
		return "", err
	}
	applog.InfoFields("backup exported", applog.Fields{
		"path":      path,
		"favorites": len(payload.Favorites),
		"history":   len(payload.History),
		"settings":  len(payload.Settings),
		"sources":   len(payload.Sources),
		"raw_bytes": len(encoded),
	})
	return path, nil
}

func (s *Service) destinationPath(dataDir, destination string) (string, error) {
	if strings.TrimSpace(destination) == "" {
		exportDir := filepath.Join(dataDir, exportDirName)
		if err := os.MkdirAll(exportDir, 0755); err != nil {
			return "", apperror.Wrap(apperror.Storage, err, "create export directory")
		}
		return filepath.Join(exportDir, "backup_"+time.Now().Format(defaultExportAt)+".json.br"), nil
	}
	if _, err := os.Stat(destination); err == nil {
		info, statErr := os.Stat(destination)
		if statErr == nil && info.IsDir() {
			return "", apperror.Newf(apperror.Validation, "destination is a directory: %s", destination)
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "create destination directory")
	}
	return destination, nil
}

func writeCompressed(path string, encoded []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "create backup file")
	}
	writer := brotli.NewWriterLevel(file, 6)
	if _, err := writer.Write(encoded); err != nil {
		_ = writer.Close()
		_ = file.Close()
		_ = os.Remove(path)
		return apperror.Wrap(apperror.Storage, err, "write backup file")
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return apperror.Wrap(apperror.Storage, err, "close backup file")
	}
	if err := file.Close(); err != nil {
		return apperror.Wrap(apperror.Storage, err, "close backup file")
	}
	return nil
}

// ImportBase64 decodes a backup chosen in the browser and merges it.
func (s *Service) ImportBase64(filename, b64Content string) (Result, error) {
	if strings.TrimSpace(b64Content) == "" {
		return Result{}, apperror.New(apperror.Validation, "backup content is empty")
	}
	raw, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return Result{}, apperror.Wrap(apperror.Validation, err, "decode base64")
	}
	return s.ImportBytes(filename, raw, "base64")
}

// ImportBytes decompresses by filename suffix (falling back to gzip magic) and
// merges the payload.
func (s *Service) ImportBytes(filename string, raw []byte, origin string) (Result, error) {
	if len(raw) > maxBackupBytes {
		return Result{}, apperror.Newf(apperror.Validation, "backup too large: limit %d MiB", maxBackupBytes>>20)
	}
	decoded, err := decompress(filename, raw)
	if err != nil {
		return Result{}, err
	}
	var payload Payload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return Result{}, apperror.Wrap(apperror.Corrupt, err, "parse backup JSON")
	}
	return s.Import(payload, origin)
}

func decompress(filename string, raw []byte) ([]byte, error) {
	source := bytes.NewReader(raw)
	var reader io.Reader = source
	switch {
	case strings.HasSuffix(strings.ToLower(filename), ".br"):
		reader = brotli.NewReader(source)
	case strings.HasSuffix(strings.ToLower(filename), ".gz"):
		gzReader, err := gzip.NewReader(source)
		if err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "gunzip backup")
		}
		defer gzReader.Close()
		reader = gzReader
	default:
		if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
			gzReader, err := gzip.NewReader(source)
			if err != nil {
				return nil, apperror.Wrap(apperror.Corrupt, err, "gunzip backup")
			}
			defer gzReader.Close()
			reader = gzReader
		}
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxBackupBytes+1))
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "read backup")
	}
	if len(decoded) > maxBackupBytes {
		return nil, apperror.Newf(apperror.Validation, "backup too large after decompression: limit %d MiB", maxBackupBytes>>20)
	}
	return decoded, nil
}

// Import merges a payload into the live database. Nothing here deletes local
// rows: a backup is an addition, and the user's own data always wins a conflict.
func (s *Service) Import(payload Payload, origin string) (Result, error) {
	if payload.Kind != payloadKind {
		return Result{}, apperror.Newf(apperror.Corrupt, "not a CCZJ backup file (kind=%q)", payload.Kind)
	}
	if payload.Version != payloadVersion {
		return Result{}, apperror.Newf(apperror.Unsupported, "unsupported backup version %d (this build reads %d)", payload.Version, payloadVersion)
	}
	items := len(payload.Favorites) + len(payload.History) + len(payload.Videos)
	if items > maxBackupItems {
		return Result{}, apperror.Newf(apperror.Validation, "backup holds %d entries, limit is %d", items, maxBackupItems)
	}
	var result Result
	s.mergeSources(payload.Sources, &result)
	s.mergeSettings(payload.Settings, &result)
	ids := s.resolveIdentities(payload)
	s.mergeMeta(payload.Videos, ids, &result)
	s.mergeFavorites(payload.Favorites, ids, &result)
	s.mergeHistory(payload.History, ids, &result)
	applog.InfoFields("backup imported", applog.Fields{
		"origin":           origin,
		"settings_applied": result.SettingsApplied,
		"sources_added":    result.SourcesAdded,
		"favorites_added":  result.FavoritesAdded,
		"history_applied":  result.HistoryApplied,
		"unresolved":       result.Unresolved,
	})
	return result, nil
}

func (s *Service) mergeSources(sources []*model.Source, result *Result) {
	for _, source := range sources {
		if source == nil {
			continue
		}
		key := strings.TrimSpace(source.SourceKey)
		if model.ValidateSourceKey(key) != nil {
			result.SourcesSkipped++
			continue
		}
		if existing, err := db.GetSourceByKey(key); err == nil && existing != nil {
			// 本机已有的源保持原样：备份里的地址可能是旧机器的临时可用镜像。
			result.SourcesSkipped++
			continue
		}
		if err := db.AddSource(source); err != nil {
			result.SourcesSkipped++
			continue
		}
		result.SourcesAdded++
	}
}

func (s *Service) mergeSettings(settings []db.BackupSetting, result *Result) {
	for _, setting := range settings {
		key := strings.TrimSpace(setting.Key)
		if key == "" || machineLocalSettings[key] {
			result.SettingsSkipped++
			continue
		}
		if key == "default_source_key" && strings.TrimSpace(setting.Value) != "" {
			// 默认源指向一个本机不存在的源会让界面开在空列表上，宁可不搬。
			if existing, err := db.GetSourceByKey(setting.Value); err != nil || existing == nil {
				result.SettingsSkipped++
				continue
			}
		}
		if existing, err := db.GetSetting(key); err == nil && existing == setting.Value {
			// 值没变就不算「写入」：否则把自己导出的备份再导回去会报出几项改动，
			// 用户看到的计数和实际发生的写入对不上。
			result.SettingsSkipped++
			continue
		}
		if err := db.SetSetting(key, setting.Value); err != nil {
			result.SettingsSkipped++
			continue
		}
		result.SettingsApplied++
	}
}

// identityKey 是「标题+年份」的查表键，与 db 侧身份解析的输入一一对应。
func identityKey(vodName, year string) string {
	return strings.TrimSpace(vodName) + "\x00" + strings.TrimSpace(year)
}

// resolveIdentities resolves every title the payload mentions in one batch, so
// a large history does not turn into one full-table read per row.
func (s *Service) resolveIdentities(payload Payload) map[string]int64 {
	seen := make(map[string]bool, len(payload.Favorites)+len(payload.History)+len(payload.Videos))
	identities := make([]db.BackupIdentity, 0, len(payload.Favorites)+len(payload.History)+len(payload.Videos))
	collect := func(vodName, year string) {
		if strings.TrimSpace(vodName) == "" {
			return
		}
		key := identityKey(vodName, year)
		if seen[key] {
			return
		}
		seen[key] = true
		identities = append(identities, db.BackupIdentity{VodName: vodName, Year: year})
	}
	for _, item := range payload.Videos {
		collect(item.VodName, item.Year)
	}
	for _, item := range payload.Favorites {
		collect(item.VodName, item.Year)
	}
	for _, item := range payload.History {
		collect(item.VodName, item.Year)
	}
	ids, err := db.ResolveBackupIdentities(identities)
	lookup := make(map[string]int64, len(ids))
	if err != nil {
		applog.Error("[backup] 身份解析失败，收藏与历史将只按已能解析的条目合并: %v", err)
		return lookup
	}
	for i, identity := range identities {
		lookup[identityKey(identity.VodName, identity.Year)] = ids[i]
	}
	return lookup
}

func (s *Service) mergeMeta(videos []db.BackupVideoMeta, ids map[string]int64, result *Result) {
	for _, meta := range videos {
		globalID := ids[identityKey(meta.VodName, meta.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		applied, err := db.MergeVideoMetaFromBackup(globalID, meta)
		if err != nil {
			applog.Warn("[backup] 合并全局元数据失败 global_id=%d: %v", globalID, err)
			continue
		}
		if applied {
			result.MetaApplied++
		}
	}
}

func (s *Service) mergeFavorites(favorites []db.BackupFavorite, ids map[string]int64, result *Result) {
	for _, item := range favorites {
		globalID := ids[identityKey(item.VodName, item.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		added, err := db.MergeFavoriteFromBackup(globalID, strings.TrimSpace(item.SourceKey), strings.TrimSpace(item.VodID), item.CreatedAt)
		if err != nil {
			applog.Warn("[backup] 合并收藏失败 %q: %v", item.VodName, err)
			result.FavoritesSkipped++
			continue
		}
		if added {
			result.FavoritesAdded++
		} else {
			result.FavoritesSkipped++
		}
	}
}

func (s *Service) mergeHistory(history []db.BackupHistory, ids map[string]int64, result *Result) {
	for _, item := range history {
		globalID := ids[identityKey(item.VodName, item.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		applied, err := db.MergeHistoryFromBackup(globalID, strings.TrimSpace(item.SourceKey), strings.TrimSpace(item.VodID),
			item.EpNum, item.Position, item.UpdatedAt)
		if err != nil {
			applog.Warn("[backup] 合并历史失败 %q ep=%d: %v", item.VodName, item.EpNum, err)
			result.HistorySkipped++
			continue
		}
		if applied {
			result.HistoryApplied++
		} else {
			result.HistorySkipped++
		}
	}
}
