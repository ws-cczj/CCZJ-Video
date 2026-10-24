package source

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"
	"database/sql"
	"errors"
	"strings"
)

// GetAllSources 返回**视频**源列表。
//
// 刻意不含小说/漫画源：这个函数的两个下游——设置页的采集源管理与后台采集调度
// （app/collection/scheduler_service.go:123）——都按视频语义处理返回值，
// 一个书源出现在这里就会被界面上那个"采集"按钮当成 CMS 接口去拉分页。
// 需要跨模式的全量列表（备份、诊断）请直接走 db.GetAllSources。
func GetAllSources() ([]*model.Source, error) {
	return db.GetSourcesByKind(model.SourceKindVideo)
}

// GetSourcesByKind 返回某个模式下的全部源，含停用的。阅读模式的源管理走这里。
func GetSourcesByKind(kind model.SourceKind) ([]*model.Source, error) {
	if err := model.ValidateSourceKind(kind); err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "invalid source kind")
	}
	return db.GetSourcesByKind(kind)
}

func GetEnabledSources() ([]*model.Source, error) {
	return db.GetEnabledSources()
}

// deriveKey is the legacy alias derivation, retained for existing callers.
func deriveKey(rawURL string) string {
	return model.DeriveSourceKey(rawURL)
}

func AddSource(s *model.Source) error { return AddSourceContext(context.Background(), s) }

func AddSourceContext(ctx context.Context, s *model.Source) (err error) {
	defer func() { logSourceChange(ctx, "add", s, err) }()
	if s == nil {
		return apperror.New(apperror.Validation, "source is required")
	}
	s.ApiUrl = strings.TrimSpace(s.ApiUrl)
	if s.ApiUrl == "" {
		return apperror.New(apperror.Validation, "api_url is required")
	}
	if err := model.ValidateSourceURL(s.ApiUrl); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid api_url")
	}
	if s.SourceKey == "" {
		key, err := model.NewSourceKey()
		if err != nil {
			return apperror.Wrap(apperror.Internal, err, "分配来源标识失败")
		}
		s.SourceKey = key
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	if s.Name == "" {
		s.Name = deriveKey(s.ApiUrl)
		if s.Name == "" {
			s.Name = s.SourceKey
		}
	}

	existing, lookupErr := db.GetSourceByKeyContext(ctx, s.SourceKey)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return apperror.Wrap(apperror.Storage, lookupErr, "读取来源失败")
	}
	if existing != nil {
		return apperror.Newf(apperror.Conflict, "来源 %s 已存在", s.SourceKey)
	}
	if err := db.CreateSourceWithDefault(ctx, s); err != nil {
		return err
	}
	return nil
}

func UpdateSource(s *model.Source) error { return UpdateSourceContext(context.Background(), s) }

func UpdateSourceContext(ctx context.Context, s *model.Source) (err error) {
	defer func() { logSourceChange(ctx, "update", s, err) }()
	if s == nil {
		return apperror.New(apperror.Validation, "source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	s.ApiUrl = strings.TrimSpace(s.ApiUrl)
	if err := model.ValidateSourceURL(s.ApiUrl); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid api_url")
	}
	if err := db.UpdateSourceMetadataContext(ctx, s); err != nil {
		return err
	}

	cache.InvalidateSource(s.SourceKey, "采集源配置已修改")
	return nil
}

func DeleteSource(key string) error { return DeleteSourceContext(context.Background(), key) }

func DeleteSourceContext(ctx context.Context, key string) (err error) {
	defer func() { logSourceChange(ctx, "delete", &model.Source{SourceKey: key}, err) }()
	if err := model.ValidateSourceKey(key); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	if err := db.DeleteSourceContext(ctx, key); err != nil {
		return err
	}
	cache.InvalidateSource(key, "采集源已删除")
	return nil
}

func GetSourceStats() ([]model.SourceStat, error) {
	return GetSourceStatsContext(context.Background())
}
func GetSourceStatsContext(ctx context.Context) ([]model.SourceStat, error) {
	rows, err := db.GetSourceStatsContext(ctx)
	return rows, apperror.Wrap(apperror.Storage, err, "读取来源统计失败")
}

// ======================== 数据源详情 / 操作 ========================

// SourceTableSummary 描述该源下某一张表的信息
type SourceTableSummary struct {
	TableName string           `json:"table_name"`
	Role      string           `json:"role"` // video / episode / type
	RowCount  int              `json:"row_count"`
	Columns   []db.TableColumn `json:"columns"`
}

// SourceDetail 返回该源的"字段 + 示例"，用于设置页面展示
type SourceDetail struct {
	SourceKey string               `json:"source_key"`
	Name      string               `json:"name"`
	ApiUrl    string               `json:"api_url"`
	Tables    []SourceTableSummary `json:"tables"`
	Samples   []*model.Video       `json:"sample_videos"`
	Episodes  []*model.Episode     `json:"sample_episodes"`
}

func GetSourceDetail(sourceKey string) (*SourceDetail, error) {
	return GetSourceDetailContext(context.Background(), sourceKey)
}
func GetSourceDetailContext(ctx context.Context, sourceKey string) (*SourceDetail, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "来源标识无效")
	}
	src, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取来源失败")
	}
	counts, err := db.ReadSourceInspectionCounts(ctx, sourceKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取来源目录统计失败")
	}

	tables := []SourceTableSummary{
		{TableName: "source_videos", Role: "catalog", RowCount: counts.Catalog},
		{TableName: "global_types", Role: "type", RowCount: counts.Types},
	}
	for i := range tables {
		t := &tables[i]
		cols, cErr := db.GetTableColumnsContext(ctx, t.TableName)
		if cErr != nil {
			return nil, apperror.Wrap(apperror.Storage, cErr, "读取来源字段失败")
		}
		t.Columns = cols
	}

	samples, err := db.GetCatalogVideoPageContext(ctx, sourceKey, db.FilterParams{Page: 1, PageSize: 5})
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取来源目录示例失败")
	}
	episodes := []*model.Episode{}

	return &SourceDetail{
		SourceKey: src.SourceKey,
		Name:      src.Name,
		ApiUrl:    src.ApiUrl,
		Tables:    tables,
		Samples:   samples.Videos,
		Episodes:  episodes,
	}, nil
}

// Log identity and outcome without exposing API tokens or response bodies.
func logSourceChange(ctx context.Context, action string, source *model.Source, err error) {
	fields := operation.Fields(ctx, applog.Fields{"action": action})
	if _, ok := fields["operation"]; !ok {
		fields["operation"] = "source." + action
	}
	if source != nil {
		fields["source_key"] = source.SourceKey
		if source.ApiUrl != "" {
			fields["name"], fields["enabled"], fields["kind"] = source.Name, source.Enabled, source.GetKind()
			fields["endpoint"] = applog.SafeURL(source.ApiUrl)
			fields["config_sha256"] = applog.Fingerprint([]byte(source.ApiUrl + "\x00" + source.StrategyConfig + "\x00" + source.AdvConfigRaw))
		}
		if action == "strategy" {
			fields["strategy_sha256"] = applog.Fingerprint([]byte(source.StrategyConfig))
		}
	}
	if err != nil {
		fields["code"] = apperror.Public(err).Code
		fields["error"] = apperror.Diagnostic(err)
		applog.WarnFields("source operation failed", fields)
		return
	}
	applog.InfoFields("source operation complete", fields)
}
