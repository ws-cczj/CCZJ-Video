package source

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/files"
	"cczjVideo/app/model"
	"context"
	"errors"

	"encoding/json"

	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
)

// WorkOwner coordinates source changes with accepted collection work.
type WorkOwner interface {
	Quiesce(context.Context, string) (func(), error)
	Forget(string)
}

// Service owns source-management business rules independently of Wails.
type Service struct {
	work    WorkOwner
	publish func(string, any)
	changed func(string)
}

func NewServiceWithRuntime(work WorkOwner, publish func(string, any)) *Service {
	return &Service{work: work, publish: publish}
}

// SetChangeObserver is wired before startup and runs only after a committed change.
func (s *Service) SetChangeObserver(changed func(string)) { s.changed = changed }
func (s *Service) sourceChanged(key string) {
	if s.changed != nil {
		s.changed(key)
	}
}
func (s *Service) quiesce(ctx context.Context, key string) (func(), error) {
	if s.work == nil {
		return func() {}, ctx.Err()
	}
	return s.work.Quiesce(ctx, key)
}
func (s *Service) Add(ctx context.Context, value *model.Source) error {
	if err := AddSourceContext(ctx, value); err != nil {
		return err
	}
	s.sourceChanged(value.SourceKey)
	return nil
}
func (s *Service) Update(ctx context.Context, value *model.Source) error {
	if value == nil {
		return apperror.New(apperror.Validation, "source is required")
	}
	release, err := s.quiesce(ctx, value.SourceKey)
	if err != nil {
		return err
	}
	defer release()
	if err := UpdateSourceContext(ctx, value); err != nil {
		return err
	}
	s.sourceChanged(value.SourceKey)
	return nil
}
func (s *Service) Delete(ctx context.Context, key string) error {
	release, err := s.quiesce(ctx, key)
	if err != nil {
		return err
	}
	defer release()
	if err := DeleteSourceContext(ctx, key); err != nil {
		return err
	}
	if s.work != nil {
		s.work.Forget(key)
	}
	s.sourceChanged(key)
	return nil
}
func (s *Service) Clear(ctx context.Context, key string) error {
	release, err := s.quiesce(ctx, key)
	if err != nil {
		return err
	}
	defer release()
	if err := db.TruncateSourceContext(ctx, key); err != nil {
		return err
	}
	cache.InvalidateSource(key, "源目录已退役")
	return nil
}
func (s *Service) Strategy(ctx context.Context, key, config string) (err error) {
	defer func() { logSourceChange(ctx, "strategy", &model.Source{SourceKey: key, StrategyConfig: config}, err) }()
	release, err := s.quiesce(ctx, key)
	if err != nil {
		return err
	}
	defer release()
	if err := db.SetSourceStrategyConfigContext(ctx, key, config); err != nil {
		return err
	}
	cache.InvalidateSource(key, "采集源策略已修改")
	return nil
}

// NewService creates a source service using the configured database package.
func NewService() *Service { return &Service{} }

// Export writes one source payload as a Brotli-compressed JSON file under
// dataDir/exports and returns its absolute path.
func (s *Service) Export(dataDir, sourceKey string) (string, error) {
	return s.ExportContext(context.Background(), dataDir, sourceKey)
}

// ExportContext owns the output file until a complete, closed export is published.
func (s *Service) ExportContext(ctx context.Context, dataDir, sourceKey string) (string, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return "", err
	}
	sourceModel, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "read source")
	}
	sourceTypes, err := db.ExportSourceTypesContext(ctx, sourceKey)
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "export source types")
	}
	types := make([]TypePayload, 0, len(sourceTypes))
	for _, t := range sourceTypes {
		types = append(types, TypePayload{TypeID: t.SourceTypeID, GlobalTypeID: t.GlobalTypeID, TypeName: t.TypeName})
	}

	exportDir := filepath.Join(dataDir, "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "create export directory")
	}
	videoCount := 0
	path, err := files.WriteUnique(ctx, exportDir, fmt.Sprintf("source_%s_%s_*.json.br", sourceKey, time.Now().Format("20060102_150405")), func(output io.Writer) error {
		writer := brotli.NewWriterLevel(output, 6)
		var err error
		videoCount, err = writeExportPayloadContext(ctx, writer, sourceModel, types, sourceKey)
		return errors.Join(err, writer.Close())
	})
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "export source")
	}
	applog.InfoFields("source export complete", applog.Fields{"source_key": sourceKey, "video_count": videoCount, "type_count": len(types)})
	return path, nil
}

func writeExportPayload(writer io.Writer, sourceModel *model.Source, types []TypePayload, sourceKey string) (int, error) {
	return writeExportPayloadContext(context.Background(), writer, sourceModel, types, sourceKey)
}
func writeExportPayloadContext(ctx context.Context, writer io.Writer, sourceModel *model.Source, types []TypePayload, sourceKey string) (int, error) {
	exported, err := json.Marshal(time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	source, err := json.Marshal(sourceModel)
	if err != nil {
		return 0, err
	}
	if _, err := io.WriteString(writer, `{"version":2,"exported":`+string(exported)+`,"source":`+string(source)+`,"videos":[`); err != nil {
		return 0, err
	}
	first := true
	count := 0
	if err := db.StreamCatalogExportContext(ctx, sourceKey, func(row db.CatalogExportRow) error {
		video := CatalogVideo{
			SourceVodID:  row.SourceVodID,
			SourceTypeID: row.SourceTypeID,
			TypeName:     row.TypeName,
			VodName:      row.VodName,
			VodPic:       row.VodPic,
			VodRemarks:   row.VodRemarks,
			VodYear:      row.VodYear,
			VodArea:      row.VodArea,
			VodTime:      row.VodTime,
		}
		encoded, err := json.Marshal(video)
		if err != nil {
			return err
		}
		if !first {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		first = false
		if _, err := writer.Write(encoded); err != nil {
			return err
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	encodedTypes, err := json.Marshal(types)
	if err != nil {
		return 0, err
	}
	if _, err := io.WriteString(writer, `],"types":`+string(encodedTypes)+"}\n"); err != nil {
		return 0, err
	}
	return count, nil
}

// Import persists a validated portable source payload.
func (s *Service) Import(payload Payload, origin string) (string, error) {
	return s.ImportContext(context.Background(), payload, origin)
}
func (s *Service) ImportContext(ctx context.Context, payload Payload, origin string) (string, error) {
	if payload.Source == nil {
		return "", apperror.New(apperror.Validation, "source payload is required")
	}
	key := strings.TrimSpace(payload.Source.SourceKey)
	if err := model.ValidateSourceKey(key); err != nil {
		return "", apperror.Wrap(apperror.Validation, err, "来源标识无效")
	}
	if payload.Version < 0 || payload.Version > 2 {
		return "", apperror.New(apperror.Unsupported, "来源备份格式版本不受支持")
	}
	if err := model.ValidateSourceURL(strings.TrimSpace(payload.Source.ApiUrl)); err != nil {
		return "", apperror.Wrap(apperror.Validation, err, "来源地址无效")
	}
	if payload.Source.GetKind() != model.SourceKindVideo && (len(payload.Videos) > 0 || len(payload.Types) > 0) {
		return "", apperror.New(apperror.Validation, "阅读来源不能导入视频目录和分类")
	}
	release, err := s.quiesce(ctx, key)
	if err != nil {
		return "", err
	}
	defer release()
	sourceKey := key

	videos := make([]*model.Video, 0, len(payload.Videos))
	for _, v := range payload.Videos {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if v == nil {
			return "", apperror.New(apperror.Validation, "来源目录中存在空条目")
		}
		id := v.SourceVodID
		if id == "" {
			id = v.VodID
		}
		if id != "" {
			videos = append(videos, &model.Video{VodId: model.FlexibleString(id), TypeId: model.FlexibleString(firstNonEmpty(v.SourceTypeID, v.TypeID)), TypeName: v.TypeName, VodName: v.VodName, VodPic: v.VodPic, VodRemarks: v.VodRemarks, VodYear: v.VodYear, VodArea: v.VodArea, VodTime: v.VodTime})
		}
	}
	types := make([]db.SourceTypeExport, 0, len(payload.Types))
	for _, t := range payload.Types {
		types = append(types, db.SourceTypeExport{SourceTypeID: t.TypeID, GlobalTypeID: t.GlobalTypeID, TypeName: t.TypeName})
	}
	err = db.ImportSourceBundle(ctx, key, func(existing *model.Source) (*model.Source, error) {
		copy := *payload.Source
		copy.SourceKey = key
		copy.ApiUrl = strings.TrimSpace(copy.ApiUrl)
		if existing != nil {
			if existing.GetKind() != copy.GetKind() {
				return nil, apperror.New(apperror.Conflict, "导入不能改变既有来源的内容模式")
			}
			mergeImportedSource(existing, &copy)
			return existing, nil
		}
		return &copy, nil
	}, videos, types)
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "导入来源失败")
	}
	cache.InvalidateSource(key, "来源已导入")
	s.sourceChanged(key)
	message := fmt.Sprintf("source %q imported from %s (%d videos, %d types)", sourceKey, origin, len(payload.Videos), len(payload.Types))
	applog.InfoFields("source import complete", applog.Fields{"source_key": key, "origin": origin, "video_count": len(payload.Videos), "type_count": len(payload.Types)})
	return message, nil
}

func mergeImportedSource(target, imported *model.Source) {
	target.Name = imported.Name
	target.ApiUrl = imported.ApiUrl
	target.AdvConfigRaw = imported.AdvConfigRaw
	target.ScheduleCfgRaw = imported.ScheduleCfgRaw
	config := target.GetAdvConfig()
	if imported.CollectLimit > 0 && config.CollectLimit == 0 {
		config.CollectLimit = imported.CollectLimit
	}
	if imported.CollectHours > 0 && config.CollectHours == 0 {
		config.CollectHours = imported.CollectHours
	}
	target.SetAdvConfig(config)
}
