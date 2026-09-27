package db

import (
	"cczjVideo/app/model"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// FilterParams defines the catalog filters shared by browsing and exports.
type FilterParams struct {
	TypeId, Year, Area, Keyword, Sort, Cursor string
	RecentDays                                int
	Page, PageSize                            int
}

func resolveGlobalTypeIdInt(typeName string) int64 {
	if strings.TrimSpace(typeName) == "" {
		return 0
	}
	id, err := GetOrCreateGlobalTypeId(typeName)
	if err != nil {
		return 0
	}
	return id
}

// upsertGlobalVideo maintains only catalog-safe global metadata. Remote detail
// fields (description, cast, crew, playback and download URLs) are deliberately
// not persisted by the catalog path.
func upsertGlobalVideo(v *model.Video) (int64, error) {
	if v == nil || strings.TrimSpace(v.VodName) == "" {
		return 0, fmt.Errorf("vod_name is empty")
	}
	typeID := resolveGlobalTypeIdInt(v.TypeName)
	id, err := GetOrCreateGlobalIDWithMeta(v.VodName, string(v.VodYear), typeID)
	if err != nil {
		return 0, err
	}
	_, err = instance.Exec(`UPDATE global_video SET
		type_id=CASE WHEN ? != 0 THEN ? ELSE type_id END,
		year=CASE WHEN ? != '' THEN ? ELSE year END,
		area=CASE WHEN ? != '' THEN ? ELSE area END,
		pic=CASE WHEN ? != '' THEN ? ELSE pic END,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		typeID, typeID, v.VodYear, v.VodYear, v.VodArea, v.VodArea, v.VodPic, v.VodPic, id)
	return id, err
}

// GetVideoById projects a catalog item into the legacy model used by handlers.
func GetVideoById(sourceKey, vodID string) (*model.Video, error) {
	catalog, err := GetCatalogItem(sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return &model.Video{Id: int(catalog.ID), VodId: model.FlexibleString(catalog.SourceVodID), GlobalId: catalog.GlobalID,
		TypeId: model.FlexibleString(catalog.SourceTypeID), TypeName: catalog.TypeName, VodName: catalog.VodName,
		VodPic: catalog.VodPic, VodRemarks: catalog.VodRemarks, VodYear: catalog.VodYear, VodArea: catalog.VodArea, VodTime: catalog.VodTime}, nil
}

// InsTypeIfNotExist is retained as a catalog type-map upsert for collection callers.
func InsTypeIfNotExist(sourceKey string, typeID model.FlexibleString, typeName string) error {
	id := strings.TrimSpace(typeID.String())
	if id == "" || strings.TrimSpace(typeName) == "" {
		return nil
	}
	globalID := resolveGlobalTypeIdInt(typeName)
	return ImportSourceTypes(sourceKey, []SourceTypeExport{{SourceTypeID: id, GlobalTypeID: globalID, TypeName: typeName}})
}

func GetTypes(sourceKey string) ([]*model.VType, error) {
	rows, err := ExportSourceTypes(sourceKey)
	if err != nil {
		return nil, err
	}
	out := make([]*model.VType, 0, len(rows))
	for _, r := range rows {
		if !isGlobalTypeCollectEnabled(r.GlobalTypeID, r.TypeName) {
			continue
		}
		out = append(out, &model.VType{TypeId: model.FlexibleString(r.SourceTypeID), Name: r.TypeName})
	}
	return out, nil
}

func TableExists(name string) bool {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name)
	return count > 0
}

func GetSetting(key string) (string, error) {
	var value string
	err := instance.Get(&value, `SELECT value FROM settings WHERE key=?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func SetSetting(key, value string) error {
	_, err := instance.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES (?,?)`, key, value)
	return err
}

type TableColumn struct {
	Cid       int    `db:"cid" json:"cid"`
	Name      string `db:"name" json:"name"`
	ColType   string `db:"col_type" json:"col_type"`
	NotNull   int    `db:"notnull" json:"notnull"`
	DfltValue string `db:"dflt_value" json:"dflt_value"`
	Pk        int    `db:"pk" json:"pk"`
}

func GetTableColumns(tableName string) ([]TableColumn, error) {
	// This is only used for fixed schema introspection in the source UI.
	if tableName != "source_videos" && tableName != "global_types" {
		return nil, fmt.Errorf("unsupported table: %s", tableName)
	}
	var cols []TableColumn
	err := instance.Select(&cols, fmt.Sprintf(`SELECT cid,name,COALESCE(type,'') col_type,"notnull",COALESCE(dflt_value,'') dflt_value,pk FROM pragma_table_info('%s')`, tableName))
	return cols, err
}

// TruncateSource removes the source-owned catalog projection and its type map.
func TruncateSource(sourceKey string) error {
	if _, err := instance.Exec(`DELETE FROM source_types WHERE source_key=?`, sourceKey); err != nil {
		return err
	}
	_, err := instance.Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey)
	return err
}

// SourceVideoRef identifies a source catalog entry sharing a global identity.
type SourceVideoRef struct {
	SourceKey string `json:"source_key" db:"source_key"`
	VodId     string `json:"vod_id" db:"vod_id"`
	VodName   string `json:"vod_name" db:"vod_name"`
}

func GetGlobalIdForVideo(sourceKey, vodID string) (int64, error) {
	var id int64
	err := instance.Get(&id, `SELECT global_id FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos"), sourceKey, vodID)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("global_id not found")
	}
	return id, nil
}

func FindSourcesByGlobalId(globalID int64) ([]SourceVideoRef, error) {
	if globalID <= 0 {
		return nil, fmt.Errorf("invalid global_id: %d", globalID)
	}
	var out []SourceVideoRef
	err := instance.Select(&out, `SELECT source_key,source_vod_id AS vod_id,vod_name FROM source_videos WHERE global_id=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos")+` ORDER BY source_key`, globalID)
	return out, err
}
