package db

import (
	"cczjVideo/app/apperror"
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
		// 源站偶尔会把 type_name 给成一串空格（库里已经存下来的历史行也在）。这种名字到了
		// 界面上就是一个说不出是什么、又点得动的空标签，所以读的时候一并修：trim 后为空就不出。
		name := strings.TrimSpace(r.TypeName)
		if name == "" {
			continue
		}
		if !isGlobalTypeCollectEnabled(r.GlobalTypeID, name) {
			continue
		}
		out = append(out, &model.VType{TypeId: model.FlexibleString(r.SourceTypeID), Name: name})
	}
	return out, nil
}

func TableExists(name string) bool {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name)
	return count > 0
}

func GetSetting(key string) (string, error) {
	if instance == nil {
		return "", ErrDBNotOpen
	}
	var value string
	err := instance.Get(&value, `SELECT value FROM settings WHERE key=?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func SetSetting(key, value string) error {
	if instance == nil {
		return ErrDBNotOpen
	}
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
// 清空目录必须连水位线一起作废并整体成功：只删了一半却留着旧游标，下一轮增量
// 会认为自己已经覆盖过这段时间，被清掉的数据就永远补不回来了。
func TruncateSource(sourceKey string) error {
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin source truncate: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM source_types WHERE source_key=?`, sourceKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		return err
	}
	if err := ResetCollectCursor(tx, sourceKey); err != nil {
		return err
	}
	return tx.Commit()
}

// SourceVideoRef identifies a source catalog entry sharing a global identity.
type SourceVideoRef struct {
	SourceKey string `json:"source_key" db:"source_key"`
	VodId     string `json:"vod_id" db:"vod_id"`
	VodName   string `json:"vod_name" db:"vod_name"`
}

// GetGlobalIdForVideo 取某源某集对应的 global_id。
//
// 「没有这一条」和「查询失败」必须分开：以前两者都回一句 global_id not found，
// 库锁了、列缺了都会被当成"这条还没采集"，界面显示空结果，日志里什么都没有。
func GetGlobalIdForVideo(sourceKey, vodID string) (int64, error) {
	var id int64
	err := instance.Get(&id, `SELECT global_id FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos"), sourceKey, vodID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperror.Newf(apperror.NotFound, "global_id not found: %s/%s", sourceKey, vodID)
	}
	if err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "查询 global_id 失败")
	}
	if id == 0 {
		return 0, apperror.Newf(apperror.NotFound, "global_id not found: %s/%s", sourceKey, vodID)
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
