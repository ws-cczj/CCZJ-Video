package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const MaxSourceCategories = 2048

func SetManualShortDramaTypesContext(ctx context.Context, sourceKey string, ids []string) error {
	if len(ids) > MaxSourceCategories {
		return fmt.Errorf("source category limit exceeded")
	}
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var key string
	if err = tx.GetContext(ctx, &key, `SELECT source_key FROM sources WHERE source_key=? AND (kind='video' OR kind='')`, sourceKey); err != nil {
		return err
	}
	var existing []string
	if err = tx.SelectContext(ctx, &existing, `SELECT source_type_id FROM source_types WHERE source_key=?`, sourceKey); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, id := range existing {
		known[id] = true
	}
	for _, id := range ids {
		if !known[id] {
			return sql.ErrNoRows
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE source_types SET manual_short_drama=0 WHERE source_key=?`, sourceKey); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE source_types SET manual_short_drama=1 WHERE source_key=? AND source_type_id=?`, sourceKey, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MergeSourceCategoriesContext refreshes protocol metadata without changing
// user-maintained global mappings or removing historical catalog identities.
func MergeSourceCategoriesContext(ctx context.Context, sourceKey string, rows []SourceTypeExport) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > MaxSourceCategories {
		return fmt.Errorf("source category limit exceeded")
	}
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range rows {
		if strings.TrimSpace(row.SourceTypeID) == "" || strings.TrimSpace(row.TypeName) == "" {
			continue
		}
		parent := row.ParentTypeID
		if parent == "0" {
			parent = ""
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO source_types(source_key,source_type_id,parent_type_id,type_name,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(source_key,source_type_id) DO UPDATE SET parent_type_id=CASE WHEN ?<>'' THEN excluded.parent_type_id ELSE source_types.parent_type_id END,type_name=excluded.type_name,updated_at=CURRENT_TIMESTAMP`, sourceKey, row.SourceTypeID, parent, row.TypeName, row.ParentTypeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UNION removes repeated IDs, so malformed cyclic parent links cannot recurse
// forever. Parent matching stays inside one source's identity namespace.
func GetShortDramaSourceTypesContext(ctx context.Context, sourceKey string, includeManual bool) ([]SourceTypeExport, error) {
	var rows []SourceTypeExport
	manual := ""
	if includeManual {
		manual = " OR st.manual_short_drama=1"
	}
	err := instance.SelectContext(ctx, &rows, `WITH RECURSIVE related(type_id) AS (
	SELECT st.source_type_id FROM source_types st LEFT JOIN global_types gt ON gt.id=st.global_type_id
	WHERE st.source_key=? AND st.source_type_id<>'' AND (st.type_name LIKE '%短剧%' OR st.type_name LIKE '%短劇%' OR st.type_name LIKE '%短片剧%' OR gt.type_name LIKE '%短剧%' OR gt.type_name LIKE '%短劇%' OR gt.type_name LIKE '%短片剧%'`+manual+`)
	UNION
	SELECT st.source_type_id FROM source_types st JOIN related r ON st.parent_type_id=r.type_id WHERE st.source_key=? AND st.parent_type_id<>'' AND st.parent_type_id<>'0' AND st.source_type_id<>''
	)
	SELECT source_type_id,parent_type_id,manual_short_drama,COALESCE(global_type_id,0) global_type_id,type_name FROM source_types
	WHERE source_key=? AND source_type_id IN (SELECT type_id FROM related) ORDER BY source_type_id LIMIT 2049`, sourceKey, sourceKey, sourceKey)
	if err == nil && len(rows) > MaxSourceCategories {
		return nil, fmt.Errorf("short drama category limit exceeded")
	}
	return rows, err
}

func shortDramaCatalogFilter(ctx context.Context, sourceKey string) (string, []any, error) {
	rows, err := GetShortDramaSourceTypesContext(ctx, sourceKey, false)
	if err != nil {
		return "", nil, err
	}
	if len(rows) == 0 {
		return catalogShortDramaClause, nil, nil
	}
	args := make([]any, 0, len(rows))
	marks := make([]string, 0, len(rows))
	for _, row := range rows {
		args = append(args, row.SourceTypeID)
		marks = append(marks, "?")
	}
	return "(" + catalogShortDramaClause + " OR source_type_id IN (" + strings.Join(marks, ",") + "))", args, nil
}
