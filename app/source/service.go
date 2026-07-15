package source

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	videos, err := db.ExportAllVideos(sourceKey)
	if err != nil {
		return "", fmt.Errorf("export videos: %w", err)
	}
	types, err := db.ExportAllTypes(sourceKey)
	if err != nil {
		return "", fmt.Errorf("export types: %w", err)
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
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	payload := Payload{Version: 1, Exported: time.Now().Format(time.RFC3339), Source: sourceModel, Videos: videos, Types: types}
	if err := encoder.Encode(payload); err != nil {
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
	applog.InfoFields("source export complete", applog.Fields{"source_key": sourceKey, "video_count": len(videos), "type_count": len(types)})
	return path, nil
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
	if err := db.ImportTypes(sourceKey, payload.Types); err != nil {
		return "", fmt.Errorf("import source types: %w", err)
	}
	if err := db.ImportVideos(sourceKey, payload.Videos); err != nil {
		return "", fmt.Errorf("import source videos: %w", err)
	}
	message := fmt.Sprintf("source %q imported from %s (%d videos, %d types)", sourceKey, origin, len(payload.Videos), len(payload.Types))
	applog.InfoFields("source import complete", applog.Fields{"source_key": sourceKey, "origin": origin, "video_count": len(payload.Videos), "type_count": len(payload.Types)})
	return message, nil
}

func mergeImportedSource(target, imported *model.Source) {
	target.Name = imported.Name
	target.ApiUrl = imported.ApiUrl
	target.UrlTemplate = imported.UrlTemplate
	target.UrlPrefix = imported.UrlPrefix
	target.UrlSuffix = imported.UrlSuffix
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
