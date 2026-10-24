// Package backup moves user data (settings, favorites, watch history and the
// metadata they point at) between databases without ever replacing one.
package backup

import (
	"cczjVideo/app/files"
	"cczjVideo/app/payload"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/model"

	"github.com/andybalholm/brotli"
)

const (
	// payloadKind 让备份文件和采集源导出文件互不冒充：
	// 两种 JSON 形状不同，喂错入口只会得到「缺少 source 信息」这种看不懂的报错。
	payloadKind     = "cczj.backup"
	payloadVersion  = 2
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
	Reading   *db.BackupReading    `json:"reading,omitempty"`
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
	ReadingApplied   int `json:"reading_applied"`
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
	// 旧数据目录的记账（已迁移/已合并、发生在哪天）说的是这台机器上的库搬到哪儿了。
	// 跟着备份走到另一台机器，就会把那边还没找回来的旧库判成「已经处理过」。
	"legacy_data_status": true,
	"legacy_data_at":     true,
	// 同意条款是对「这台机器上的这份安装」作出的意思表示。跟着备份导入走，
	// 一份已同意的备份就能替别人跳过首启确认，闸门形同虚设。
	"license_terms_version": true,
	// Trust and permissions are consent on this installation, never transferable.
	"plugin_script_trust": true,
	"plugin_permissions":  true,
}

// exportableSources 决定备份里是否带上源定义。源带着 API 地址，收藏与历史
// 的「从哪打开」坐标才有意义；目录本体仍走采集源导出，不塞进这里。
func (s *Service) buildPayload(dataDir string) (Payload, error) {
	return s.buildPayloadContext(context.Background(), dataDir)
}
func (s *Service) buildPayloadContext(ctx context.Context, dataDir string) (Payload, error) {
	snapshot, err := db.ReadBackupSnapshot(ctx, db.DB())
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Storage, err, "read backup snapshot")
	}
	settings := make([]db.BackupSetting, 0, len(snapshot.Settings))
	for _, setting := range snapshot.Settings {
		if !machineLocalSettings[setting.Key] {
			settings = append(settings, setting)
		}
	}
	return Payload{Kind: payloadKind, Version: payloadVersion, Exported: time.Now().Format(time.RFC3339), App: filepath.Base(dataDir), Settings: settings, Sources: snapshot.Sources, Videos: snapshot.Videos, Favorites: snapshot.Favorites, History: snapshot.History, Reading: snapshot.Reading}, nil
}

// Export writes the full backup as a Brotli-compressed JSON file. An empty
// destination puts it under dataDir/exports, same as the source export does.
func (s *Service) Export(dataDir, destination string) (string, error) {
	return s.ExportContext(context.Background(), dataDir, destination)
}
func (s *Service) ExportContext(ctx context.Context, dataDir, destination string) (string, error) {
	payload, err := s.buildPayloadContext(ctx, dataDir)
	if err != nil {
		return "", err
	}
	if err := checkItemLimit(payload); err != nil {
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
	if len(encoded) > maxBackupBytes {
		return "", apperror.New(apperror.Validation, "backup exceeds import size limit")
	}
	if strings.TrimSpace(destination) == "" {
		path, err = files.WriteUnique(ctx, filepath.Dir(path), "backup_"+time.Now().Format(defaultExportAt)+"_*.json.br", func(writer io.Writer) error { return compressBackup(writer, encoded) })
	} else {
		err = writeCompressedContext(ctx, path, encoded)
	}
	if err != nil {
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
	return writeCompressedContext(context.Background(), path, encoded)
}
func writeCompressedContext(ctx context.Context, path string, encoded []byte) error {
	return apperror.Wrap(apperror.Storage, files.WriteAtomic(ctx, path, func(writer io.Writer) error { return compressBackup(writer, encoded) }), "write backup file")
}
func compressBackup(destination io.Writer, encoded []byte) error {
	writer := brotli.NewWriterLevel(destination, 6)
	_, err := writer.Write(encoded)
	return errors.Join(err, writer.Close())
}

// ImportBase64 decodes a backup chosen in the browser and merges it.
func (s *Service) ImportBase64(filename, b64Content string) (Result, error) {
	return s.ImportBase64Context(context.Background(), filename, b64Content)
}
func (s *Service) ImportBase64Context(ctx context.Context, filename, b64Content string) (Result, error) {
	if strings.TrimSpace(b64Content) == "" {
		return Result{}, apperror.New(apperror.Validation, "backup content is empty")
	}
	raw, err := payload.Base64(ctx, b64Content, maxBackupBytes)
	if err != nil {
		return Result{}, apperror.Wrap(apperror.Validation, err, "decode base64")
	}
	return s.ImportBytesContext(ctx, filename, raw, "base64")
}

// ImportBytes decompresses by filename suffix (falling back to gzip magic) and
// merges the payload.
func (s *Service) ImportBytes(filename string, raw []byte, origin string) (Result, error) {
	return s.ImportBytesContext(context.Background(), filename, raw, origin)
}
func (s *Service) ImportBytesContext(ctx context.Context, filename string, raw []byte, origin string) (Result, error) {
	if len(raw) > maxBackupBytes {
		return Result{}, apperror.Newf(apperror.Validation, "backup too large: limit %d MiB", maxBackupBytes>>20)
	}
	decoded, err := payload.UnpackBytes(ctx, filename, raw, payload.Limits{Input: maxBackupBytes, Output: maxBackupBytes})
	if err != nil {
		return Result{}, err
	}
	var imported Payload
	if err := payload.JSON(ctx, decoded, &imported, false); err != nil {
		return Result{}, apperror.Wrap(apperror.Corrupt, err, "parse backup JSON")
	}
	return s.ImportContext(ctx, imported, origin)
}

func decompress(filename string, raw []byte) ([]byte, error) {
	return payload.UnpackBytes(context.Background(), filename, raw, payload.Limits{Input: maxBackupBytes, Output: maxBackupBytes})
}

// Import merges a payload into the live database. Nothing here deletes local
// rows: a backup is an addition, and the user's own data always wins a conflict.
func (s *Service) Import(payload Payload, origin string) (Result, error) {
	return s.ImportContext(context.Background(), payload, origin)
}

// ImportContext commits all categories together and never treats storage failure as a skipped record.
func (s *Service) ImportContext(ctx context.Context, payload Payload, origin string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if payload.Kind != payloadKind {
		return Result{}, apperror.Newf(apperror.Corrupt, "not a CCZJ backup file (kind=%q)", payload.Kind)
	}
	if payload.Version != 1 && payload.Version != payloadVersion {
		return Result{}, apperror.Newf(apperror.Unsupported, "unsupported backup version %d (this build reads %d)", payload.Version, payloadVersion)
	}
	if err := checkItemLimit(payload); err != nil {
		return Result{}, err
	}
	var result Result
	change := db.CatalogChange{}
	err := db.WriteBackup(ctx, db.DB(), func(writer *db.BackupWriter) error {
		applied, err := writer.MergeReading(payload.Reading)
		if err != nil {
			return err
		}
		result.ReadingApplied = applied
		if err := s.mergeSources(writer, payload.Sources, &result); err != nil {
			return err
		}
		if err := s.mergeSettings(writer, payload.Settings, &result); err != nil {
			return err
		}
		ids, err := s.resolveIdentities(writer, payload)
		if err != nil {
			return err
		}
		if err := s.mergeMeta(writer, payload.Videos, ids, &result); err != nil {
			return err
		}
		if err := s.mergeFavorites(writer, payload.Favorites, ids, &result); err != nil {
			return err
		}
		if err := s.mergeHistory(writer, payload.History, ids, &result); err != nil {
			return err
		}
		change.Affected = result.SourcesAdded + result.MetaApplied + result.FavoritesAdded + result.HistoryApplied
		if change.Affected > 0 {
			change.Sources, err = writer.SourceKeys()
			return err
		}
		return nil
	})
	if err != nil {
		return Result{}, apperror.Wrap(apperror.Storage, err, "恢复备份失败")
	}
	cache.PublishCatalogChange(change, "backup restored")
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

// Export and import must agree: a published backup must fit the reader's limits.
func checkItemLimit(payload Payload) error {
	items := len(payload.Favorites) + len(payload.History) + len(payload.Videos) + len(payload.Settings) + len(payload.Sources)
	if payload.Reading != nil {
		items += len(payload.Reading.Records) + len(payload.Reading.Items) + len(payload.Reading.Chapters)
	}
	if items > maxBackupItems {
		return apperror.Newf(apperror.Validation, "backup holds %d entries, limit is %d", items, maxBackupItems)
	}
	return nil
}

func (s *Service) mergeSources(writer *db.BackupWriter, sources []*model.Source, result *Result) error {
	for _, source := range sources {
		if source == nil {
			result.SourcesSkipped++
			continue
		}
		key := strings.TrimSpace(source.SourceKey)
		if model.ValidateSourceKey(key) != nil || model.ValidateSourceKind(source.GetKind()) != nil {
			result.SourcesSkipped++
			continue
		}
		_, err := writer.Source(key)
		if err == nil {
			result.SourcesSkipped++
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		copy := *source
		copy.SourceKey = key
		if err := writer.AddSource(&copy); err != nil {
			return err
		}
		result.SourcesAdded++
	}
	return nil
}

func (s *Service) mergeSettings(writer *db.BackupWriter, settings []db.BackupSetting, result *Result) error {
	for _, setting := range settings {
		key := strings.TrimSpace(setting.Key)
		if key == "" || machineLocalSettings[key] {
			result.SettingsSkipped++
			continue
		}
		if key == "default_source_key" && strings.TrimSpace(setting.Value) != "" {
			if _, err := writer.Source(setting.Value); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					result.SettingsSkipped++
					continue
				}
				return err
			}
		}
		existing, err := writer.Setting(key)
		if err != nil {
			return err
		}
		if existing == setting.Value {
			result.SettingsSkipped++
			continue
		}
		if err := writer.SetSetting(key, setting.Value); err != nil {
			return err
		}
		result.SettingsApplied++
	}
	return nil
}

// identityKey 是「标题+年份」的查表键，与 db 侧身份解析的输入一一对应。
func identityKey(vodName, year string) string {
	return strings.TrimSpace(vodName) + "\x00" + strings.TrimSpace(year)
}

// resolveIdentities resolves every title the payload mentions in one batch, so
// a large history does not turn into one full-table read per row.
func (s *Service) resolveIdentities(writer *db.BackupWriter, payload Payload) (map[string]int64, error) {
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
	ids, err := writer.ResolveIdentities(identities)
	lookup := make(map[string]int64, len(ids))
	if err != nil {
		return nil, err
	}
	for i, identity := range identities {
		lookup[identityKey(identity.VodName, identity.Year)] = ids[i]
	}
	return lookup, nil
}

func (s *Service) mergeMeta(writer *db.BackupWriter, videos []db.BackupVideoMeta, ids map[string]int64, result *Result) error {
	for _, meta := range videos {
		globalID := ids[identityKey(meta.VodName, meta.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		applied, err := writer.MergeMeta(globalID, meta)
		if err != nil {
			return err
		}
		if applied {
			result.MetaApplied++
		}
	}
	return nil
}

func (s *Service) mergeFavorites(writer *db.BackupWriter, favorites []db.BackupFavorite, ids map[string]int64, result *Result) error {
	for _, item := range favorites {
		globalID := ids[identityKey(item.VodName, item.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		added, err := writer.MergeFavorite(globalID, strings.TrimSpace(item.SourceKey), strings.TrimSpace(item.VodID), item.CreatedAt)
		if err != nil {
			return err
		}
		if added {
			result.FavoritesAdded++
		} else {
			result.FavoritesSkipped++
		}
	}
	return nil
}

func (s *Service) mergeHistory(writer *db.BackupWriter, history []db.BackupHistory, ids map[string]int64, result *Result) error {
	for _, item := range history {
		globalID := ids[identityKey(item.VodName, item.Year)]
		if globalID <= 0 {
			result.Unresolved++
			continue
		}
		applied, err := writer.MergeHistory(globalID, strings.TrimSpace(item.SourceKey), strings.TrimSpace(item.VodID),
			item.EpNum, item.Position, item.UpdatedAt)
		if err != nil {
			return err
		}
		if applied {
			result.HistoryApplied++
		} else {
			result.HistorySkipped++
		}
	}
	return nil
}
