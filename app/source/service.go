package source

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
)

// Service owns source-management business rules independently of Wails.
type Service struct{}

// NewService creates a source service using the configured database package.
func NewService() *Service { return &Service{} }

// Export writes one source payload as a Brotli-compressed JSON file under
// dataDir/exports and returns its absolute path.
func (s *Service) Export(dataDir, sourceKey string) (string, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return "", err
	}
	sourceModel, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	sourceTypes, err := db.ExportSourceTypes(sourceKey)
	if err != nil {
		return "", fmt.Errorf("export source types: %w", err)
	}
	types := make([]TypePayload, 0, len(sourceTypes))
	for _, t := range sourceTypes {
		types = append(types, TypePayload{TypeID: t.SourceTypeID, GlobalTypeID: t.GlobalTypeID, TypeName: t.TypeName})
	}

	exportDir := filepath.Join(dataDir, "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}
	path := filepath.Join(exportDir, fmt.Sprintf("source_%s_%s.json.br", sourceKey, time.Now().Format("20060102_150405")))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("create export file: %w", err)
	}
	defer file.Close()

	writer := brotli.NewWriterLevel(file, 6)
	videoCount, err := writeExportPayload(writer, sourceModel, types, sourceKey)
	if err != nil {
		_ = writer.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("encode source export: %w", err)
	}
	if err := writer.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close source export: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close export file: %w", err)
	}
	applog.InfoFields("source export complete", applog.Fields{"source_key": sourceKey, "video_count": videoCount, "type_count": len(types)})
	return path, nil
}

func writeExportPayload(writer io.Writer, sourceModel *model.Source, types []TypePayload, sourceKey string) (int, error) {
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
	if err := db.StreamCatalogExport(sourceKey, func(row db.CatalogExportRow) error {
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
	if payload.Source == nil {
		return "", fmt.Errorf("source payload is required")
	}
	sourceKey := strings.TrimSpace(payload.Source.SourceKey)
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return "", fmt.Errorf("validate imported source key: %w", err)
	}
	existing, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("read existing source: %w", err)
		}
		if err := db.AddSource(payload.Source); err != nil {
			return "", fmt.Errorf("create imported source: %w", err)
		}
	} else {
		mergeImportedSource(existing, payload.Source)
		if err := db.UpdateSource(existing); err != nil {
			return "", fmt.Errorf("update imported source: %w", err)
		}
	}
	videos := make([]*model.Video, 0, len(payload.Videos))
	for _, v := range payload.Videos {
		id := v.SourceVodID
		if id == "" {
			id = v.VodID
		}
		if id != "" {
			videos = append(videos, &model.Video{VodId: model.FlexibleString(id), TypeId: model.FlexibleString(firstNonEmpty(v.SourceTypeID, v.TypeID)), TypeName: v.TypeName, VodName: v.VodName, VodPic: v.VodPic, VodRemarks: v.VodRemarks, VodYear: v.VodYear, VodArea: v.VodArea, VodTime: v.VodTime})
		}
	}
	// Importing a catalogue file is a deliberate user action, so deleted rows
	// are restored regardless of the catalog_revive_deleted setting.
	if err := db.UpsertCatalogItemsWithRevival(sourceKey, videos); err != nil {
		return "", fmt.Errorf("import catalog videos: %w", err)
	}
	types := make([]db.SourceTypeExport, 0, len(payload.Types))
	for _, t := range payload.Types {
		types = append(types, db.SourceTypeExport{SourceTypeID: t.TypeID, GlobalTypeID: t.GlobalTypeID, TypeName: t.TypeName})
	}
	if err := db.ImportSourceTypes(sourceKey, types); err != nil {
		return "", fmt.Errorf("import source types: %w", err)
	}
	message := fmt.Sprintf("source %q imported from %s (%d videos, %d types)", sourceKey, origin, len(payload.Videos), len(payload.Types))
	applog.InfoFields("source import complete", applog.Fields{"source_key": sourceKey, "origin": origin, "video_count": len(payload.Videos), "type_count": len(payload.Types)})
	return message, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
