// Package library 是小说与漫画模式的资源载入层。
//
// Shared acquisition/storage is assembled here. Text-chapter matching and novel
// routing belong to novel; episode/volume matching and manga routing to comic.
//
// Content acquisition and caches live here; durable personal reading state is
// owned by readingstate and its SQLite adapter.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
	"cczjVideo/app/netbudget"
	"cczjVideo/app/provider"
	"cczjVideo/app/proxy"
	"cczjVideo/app/workflight"
)

const (
	// textCacheTTL 是正文在磁盘上还直接端得出去的年龄上限，按最后被读到的时刻算。
	// 正文不会自己变（变的只是源改版后解析方式），所以 TTL 的作用是淘汰
	// "再也不会被读到的那些章"，不是保证新鲜。30 天与收藏/历史图片同一档。
	textCacheTTL = 30 * 24 * time.Hour
	// textCacheMaxBytes 是正文缓存目录的上限。一章几 KB 到几十 KB，
	// 256MB 装得下上万章，而数据目录在系统盘上。
	textCacheMaxBytes = 256 << 20
	// searchConcurrency bounds active source searches across this Service.
	// This is not a budget for playback, image requests or other host traffic.
	searchConcurrency = 5
)

// Service 是阅读模式的资源载入服务。
type Service struct {
	fetcher        *provider.Fetcher
	textCache      *proxy.BlobStore
	limits         provider.Limits
	searchSlots    chan struct{}
	continuations  searchContinuations
	textReads      *workflight.Group[*ChapterText]
	pageReads      *workflight.Group[*ChapterPages]
	trendReads     *workflight.Group[*ComicTrendPage]
	pageMu         sync.Mutex
	pageEpoch      uint64
	tocMu          sync.Mutex
	tocAttempts    map[int]time.Time
	catalogMu      sync.Mutex
	catalogs       map[string]*sourceCatalogSession
	qualityMu      sync.Mutex
	browseMu       sync.Mutex
	routingEnabled atomic.Bool
	auditRunning   atomic.Bool
	registryMu     sync.Mutex
	registry       map[string][]*BatchSource
	readingGate    *quality.NetworkGate
	priority       readingPriority

	// imageURL 把上游图片地址换成同源代理地址。由服务层注入，
	// 因为代理实例挂在 App 上，而这个包不该知道 App 的存在。
	imageURL func(upstream, referer, sourceKey string) string
}

// NewService 建一个阅读资源服务。dataDir 延迟求值：启动早期数据目录可能还没迁移到位。
func NewService(dataDir func() string) *Service {
	return NewServiceWithRuntime(dataDir, context.Background(), nil)
}

func NewServiceWithRuntime(dataDir func() string, owner context.Context, budget *netbudget.Budget) *Service {
	fetcher := provider.NewFetcher()
	fetcher.ConfigureBudget(budget)
	gate := quality.NewNetworkGate()
	// A fresh fetcher has no scopes and gate.Wrap is non-nil: both preconditions
	// of WrapTransport are guaranteed here, before publication.
	_ = fetcher.WrapTransport(gate.Wrap)
	return &Service{
		readingGate: gate,
		textReads:   workflight.New[*ChapterText](owner, 64),
		pageReads:   workflight.New[*ChapterPages](owner, 64),
		trendReads:  workflight.New[*ComicTrendPage](owner, 4),
		fetcher:     fetcher,
		textCache:   proxy.NewBlobStore(dataDir, "textcache", textCacheTTL, textCacheMaxBytes),
		limits:      provider.DefaultLimits(),
		searchSlots: make(chan struct{}, searchConcurrency),
	}
}

func (s *Service) Close(ctx context.Context) error {
	first := s.textReads.Close(ctx)
	second := s.pageReads.Close(ctx)
	third := s.trendReads.Close(ctx)
	s.fetcher.Close()
	if first != nil {
		return first
	}
	if second != nil {
		return second
	}
	return third
}

func (s *Service) SetMaintenanceLauncher(launch func(string, func(context.Context)) bool) {
	s.textCache.SetMaintenanceLauncher(launch)
}

// SetImageURLBuilder 注入"上游图片地址 → 同源代理地址"的换算。
func (s *Service) SetImageURLBuilder(fn func(upstream, referer, sourceKey string) string) {
	s.imageURL = fn
}

// ---------------------------------------------------------------------------
// 对前端的形状
// ---------------------------------------------------------------------------

// Item 是一部作品。字段名用 camelCase：这是给前端的 DTO，不是 DB 行。
type Item struct {
	ID         int    `json:"id"`
	Kind       string `json:"kind"`
	SourceKey  string `json:"sourceKey"`
	SourceName string `json:"sourceName"`
	RemoteID   string `json:"remoteId"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	// Cover 已经是同源代理地址，前端直接当 <img src> 用。
	Cover   string `json:"cover"`
	Summary string `json:"summary"`
	Genres  string `json:"genres"`
	Status  string `json:"status"`
	Latest  string `json:"latest"`
	Words   string `json:"words"`
	// UpdatedAt 是源给的原文，不换算单位（reports/04:24：秒与毫秒混着猜会差一千倍）。
	UpdatedAt string `json:"updatedAt"`
	// Available 为假表示这个条目已经从源目录里消失或源被删了。
	// 它仍然被返回，因为书架指着它——界面要显示成"来源已下架"，
	// 而不是让用户的书架凭空少一行（reports/00:109）。
	Available bool `json:"available"`
}

// Chapter 是目录里的一章。
type Chapter struct {
	ID int `json:"id"`
	// RemoteID 是这一章的稳定身份（规范化后的去域名路径，或源声明的显式 id）。
	//
	// 前端记阅读进度要用它而不是 ID：ID 是本地自增主键，换库或重装就变，
	// 而进度是按 {kind, sourceKey, itemRemoteId} 存的，章也得有一个同样稳定的键，
	// 否则"这本书读到第几章"在源改版之后就对不上了。
	RemoteID string `json:"remoteId"`
	Ord      int    `json:"ord"`
	Title    string `json:"title"`
	Volume   string `json:"volume"`
	IsVolume bool   `json:"isVolume"`
	// Access 是源**声明**的访问条件（available / login / subscription / unknown）。
	// 它是元数据，不等于内容已经拿到手：标着 available 的章也可能因为源改版取不到。
	Access string `json:"access"`
	// Available 为假表示这一章已经从源目录里消失。进度可能还指着它，
	// 所以不能直接不返回。
	Available bool `json:"available"`
}

// ItemDetail 是一部作品加它的目录。
type ItemDetail struct {
	Item     *Item      `json:"item"`
	Chapters []*Chapter `json:"chapters"`
	// ChapterCount 与 Chapters 是同一批行，包含已下架的（每行用 Available 标注）。
	// 界面上"共 N 章"直接数列表即可，这个字段是给调用方省一次遍历的。
	ChapterCount int `json:"chapterCount"`
}

// ChapterText 是一章正文。
type ChapterText struct {
	ChapterID  int      `json:"chapterId"`
	Paragraphs []string `json:"paragraphs"`
	// FromCache 报告这一份是从磁盘缓存端出来的。
	FromCache bool `json:"fromCache"`
	// Warnings 是"内容拿到了，但有件事该让用户知道"：正文可能被截断、
	// 编码像是错了、疑似字体码位混淆。它们不是错误，所以不阻断显示。
	Warnings []string `json:"warnings"`
}

// Page 是漫画的一页。URL 已经是同源代理地址。
type Page struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
}

// ChapterPages 是一话的图片列表。
type ChapterPages struct {
	ChapterID int      `json:"chapterId"`
	Pages     []*Page  `json:"pages"`
	FromCache bool     `json:"fromCache"`
	Warnings  []string `json:"warnings"`
}

// SourceResult 是一个源在一次聚合搜索里的结果。
//
// 每个源各自成功或失败（reports/00:131）：一个源挂了不该让整屏搜索变空，
// 而"空列表"必须只代表一次成功的"没有结果"，不能承担所有错误。
type SourceResult struct {
	SourceKey  string  `json:"sourceKey"`
	SourceName string  `json:"sourceName"`
	Items      []*Item `json:"items"`
	// NextURL 是这个源自己的下一页游标。分页状态按源独立保存，
	// 因为有的源用 page、有的用 offset、有的只给一个"下一页"链接。
	NextURL string `json:"nextUrl"`
	HasNext bool   `json:"hasNext"`
	// ErrorCode 与 ErrorMessage 只在失败时非空。前端按码决定处置
	// （超时值得重试、需要登录不该重试、配置写错该去改配置）。
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// SearchOutcome 是一次聚合搜索的全部结果。
type SearchOutcome struct {
	Keyword         string          `json:"keyword"`
	Page            int             `json:"page"`
	Results         []*SourceResult `json:"results"`
	SearchedSources int             `json:"searchedSources"`
	PendingSources  int             `json:"pendingSources"`
	// Total 是各源成功返回的条目数之和。它不含失败源可能有的结果，
	// 所以界面上要同时显示"有 N 个源没搜成"。
	Total       int `json:"total"`
	FailedCount int `json:"failedCount"`
	// NextCursor is an opaque, short-lived continuation owned by this Service.
	// It retains each source's independent cursor and configuration revision.
	NextCursor string `json:"nextCursor"`
}

// ---------------------------------------------------------------------------
// 源文档
// ---------------------------------------------------------------------------

// resolvedSource 是一个源加它解析好的管线。
type resolvedSource struct {
	source   *model.Source
	document *provider.Document
	pipeline *provider.Pipeline
	// version 是源文档的指纹，进正文缓存键：改了提取规则就该作废旧缓存，
	// 否则用户读到的是旧规则解析出来的正文，而界面上看不出来（ADR 0016）。
	version string
}

// SourcesHeaders 给图片代理反查一个源声明的请求头。
//
// 头不走 URL 参数：让 WebView 能在查询串里塞任意请求头，等于把"用本机的网络身份
// 发任意请求"开放给了页面脚本。这里按 source_key 反查用户自己配的内容。
func SourcesHeaders(sourceKey string) map[string]string {
	source, err := db.GetSourceByKey(sourceKey)
	if err != nil || source == nil {
		return nil
	}
	document, err := provider.ParseDocument(source.StrategyConfig, string(source.GetKind()))
	if err != nil {
		return nil
	}
	return document.Headers
}

// resolve 取出一个源并解析它的文档。
func (s *Service) resolve(sourceKey string, kind model.SourceKind) (*resolvedSource, error) {
	source, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, fmt.Sprintf("读取来源 %s 失败", sourceKey))
	}
	if source.GetKind() != kind {
		return nil, apperror.Newf(apperror.Validation,
			"来源 %s 是%s源，不能当%s源用", sourceKey, source.GetKind(), kind)
	}
	document, err := provider.ParseDocument(source.StrategyConfig, string(kind))
	if err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "这个源的配置不合法")
	}
	return &resolvedSource{
		source:   source,
		document: document,
		pipeline: provider.NewPipeline(document, s.fetcher.ScopeMode(source.SourceKey+":"+sourceRevision(source), string(kind)), s.limits),
		version:  sourceRevision(source),
	}, nil
}

func sourceRevision(source *model.Source) string {
	encoded, _ := json.Marshal([]string{source.ApiUrl, source.StrategyConfig, string(source.GetKind())})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// 已入库内容的浏览
// ---------------------------------------------------------------------------

// Items 列出一个模式下所有已入库的作品（跨源）。
//
// 书城与分类页要它：那里展示的是"已经入库的全部作品"，不隶属任何单个源。
// includeRetired 为真时连来源已下架的一起返回——书架指着它们，
// 界面上要能显示成"来源已下架"，而不是让用户的书架凭空少一行。
func (s *Service) Items(kind model.SourceKind, includeRetired bool) ([]*Item, error) {
	if err := model.ValidateSourceKind(kind); err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "invalid source kind")
	}
	rows, err := db.ListLibraryItemsByKind(kind, includeRetired)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取作品列表失败")
	}
	// 源名要按 source_key 查一次；同一个源的几十个作品不该查几十遍。
	names := map[string]string{}
	out := make([]*Item, 0, len(rows))
	for _, row := range rows {
		item := s.toDTO(kind, row, names)
		out = append(out, item)
	}
	return out, nil
}

// ResolveRef 按自然键把一个条目取回来。
//
// 书架存的是 {kind, sourceKey, remoteId} 而不是本地 id：id 是自增主键，
// 换库或重装就变了，而书架是要跨这些活下来的。所以每次进应用都得能
// 按自然键把条目重新解析成本地 id，解析不到就标成来源不可用。
func (s *Service) ResolveRef(kind model.SourceKind, sourceKey, remoteID string) (*Item, error) {
	if err := model.ValidateSourceKind(kind); err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "invalid source kind")
	}
	row, err := db.GetLibraryItemByRemote(kind, sourceKey, remoteID)
	if err != nil {
		return nil, apperror.Wrap(apperror.NotFound, err, "这个条目不在库里")
	}
	names := map[string]string{}
	return s.toDTO(kind, row, names), nil
}

// toDTO 把一行库记录转成前端 DTO。names 是 source_key → 源名的缓存，
// 由调用方持有，避免一次列表查询里对同一个源反复查库。
func (s *Service) toDTO(kind model.SourceKind, row *model.LibraryItem, names map[string]string) *Item {
	name, ok := names[row.SourceKey]
	if !ok {
		if source, err := db.GetSourceByKey(row.SourceKey); err == nil && source != nil {
			name = source.Name
		}
		names[row.SourceKey] = name
	}
	cover := row.Cover
	if s.imageURL != nil && cover != "" {
		// 封面也用这一页自己的地址当 Referer：防盗链严格的源上，
		// 一整页共用一个引用页会让只有第一张封面能出来、其余全 403。
		cover = s.imageURL(cover, row.RemoteURL, row.SourceKey)
	}
	return &Item{
		ID: row.Id, Kind: row.Kind, SourceKey: row.SourceKey, SourceName: name,
		RemoteID: row.RemoteID, Title: row.Title, Author: row.Author, Cover: cover,
		Summary: row.Summary, Genres: row.Genres, Status: row.Status,
		Latest: row.Latest, Words: row.Words, UpdatedAt: row.RemoteUpdatedAt,
		Available: row.LifecycleState == "active",
	}
}

// ---------------------------------------------------------------------------
// 搜索
// ---------------------------------------------------------------------------

// Search 在所有启用的源上搜一遍，每个源各自成功或失败。
//
// 不等最慢的源：每个源一有结果就通过 onResult 回调发出去（服务层把它转成 Wails
// 事件），返回值是最终汇总。reports/00:131 要求"按 provider 逐个发回成功结果，
// 不等待最慢源完成再全屏刷新"。
func (s *Service) Search(ctx context.Context, kind model.SourceKind, keyword string, page int, onResult func(*SourceResult)) (*SearchOutcome, error) {
	if err := validateSearch(kind, keyword); err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	release := s.priority.begin()
	defer release()
	if err := s.ensureBuiltinSources(ctx, kind); err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	sources, err := db.GetEnabledSourcesByKind(kind)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取源列表失败")
	}
	plans := make([]sourceSearchState, 0, len(sources))
	byKey := make(map[string]*model.Source, len(sources))
	for _, source := range sources {
		plans = append(plans, sourceSearchState{Key: source.SourceKey, Page: page})
		byKey[source.SourceKey] = source
	}
	return s.searchPlans(ctx, kind, keyword, page, plans, byKey, onResult)
}

func (s *Service) searchSourcePage(ctx context.Context, kind model.SourceKind, source *model.Source, keyword string, state sourceSearchState) *SourceResult {
	requestCtx := ctx
	if s.routingEnabled.Load() {
		var cancel context.CancelFunc
		deadline := novel.SearchDeadline
		if kind == model.SourceKindComic {
			deadline = comic.SearchDeadline
		}
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}
	result := &SourceResult{SourceKey: source.SourceKey, SourceName: source.Name, Items: []*Item{}}
	defer func() {
		// User cancellation, local storage errors and stale cursors say nothing
		// about upstream health. A per-source deadline is an actual failed attempt.
		if requestCtx.Err() != nil || !s.routingEnabled.Load() {
			return
		}
		switch result.ErrorCode {
		case string(apperror.Cancelled), string(apperror.Storage), string(apperror.Conflict):
			return
		}
		if err := db.RecordReadingSearchHealth(requestCtx, kind, sourceQualityID(string(kind), source.ApiUrl, source.StrategyConfig), result.ErrorCode); err != nil {
			applog.Warn("[Library] 保存搜索实测失败: %v", err)
		}
	}()
	resolved, err := s.resolve(source.SourceKey, kind)
	if err != nil {
		result.ErrorCode, result.ErrorMessage = splitError(err)
		return result
	}
	if state.Version != "" && state.Version != resolved.version {
		result.ErrorCode, result.ErrorMessage = splitError(apperror.New(apperror.Conflict, "来源配置已变化，请重新搜索"))
		return result
	}
	found, err := resolved.pipeline.Search(ctx, resolved.source.ApiUrl, keyword, state.Page, state.URL)
	if err != nil {
		result.ErrorCode, result.ErrorMessage = splitError(err)
		return result
	}
	result.NextURL, result.HasNext = found.NextURL, found.HasNext
	if len(found.Items) == 0 {
		// 一次成功的"没有结果"。刻意不写成错误：空列表只能是这个意思
		// （reports/00:135），否则"源挂了"与"这本书真没有"在界面上分不开。
		return result
	}
	// 搜索结果落库：详情与目录要用本地 id 关联，而书架存的是
	// {kind, sourceKey, remoteId}——不落库就没有 id 可给。
	rows := make([]*model.LibraryItem, 0, len(found.Items))
	for _, item := range found.Items {
		rows = append(rows, &model.LibraryItem{
			RemoteID: item.RemoteID, RemoteURL: item.URL, Title: item.Title, Author: item.Author,
			// Cover 必须一起落库：搜索结果里显示的是它，而书架是从库里读的，
			// 少写这一列的话从书架进详情页封面就是空的，看着像图片代理坏了。
			Cover:   item.Cover,
			Summary: item.Summary, Genres: item.Genres, Status: item.Status,
			Latest: item.Latest, Words: item.Words, RemoteUpdatedAt: item.UpdatedAt,
		})
	}
	if err := ctx.Err(); err != nil {
		result.ErrorCode, result.ErrorMessage = splitError(err)
		return result
	}
	ids, err := db.UpsertLibraryItems(kind, source.SourceKey, rows, true)
	if err != nil {
		applog.Warn("[Library] 搜索结果入库失败 kind=%s source=%s count=%d: %v", kind, source.SourceKey, len(rows), err)
		result.ErrorCode, result.ErrorMessage = splitError(apperror.Wrap(apperror.Storage, err, "写入搜索结果失败"))
		return result
	}
	for i, item := range found.Items {
		if i >= len(ids) || ids[i] == 0 {
			// 没有本地 id 的条目（缺标题或缺身份）在 UpsertLibraryItems 里被跳过了。
			// 不返回它：一个没有 id 的条目点进去什么也做不了。
			continue
		}
		cover := item.Cover
		if s.imageURL != nil && cover != "" {
			// Referer 用**这一条自己的**详情地址。早先这里写的是
			// found.Items[0].URL（第一条的地址），一整页搜索结果会共用同一个
			// 引用页——防盗链严格的源上只有第一张封面能出来，其余全 403，
			// 而现象看起来像"这个源的封面时好时坏"。
			cover = s.imageURL(cover, item.URL, source.SourceKey)
		}
		result.Items = append(result.Items, &Item{
			ID: ids[i], Kind: string(kind), SourceKey: source.SourceKey, SourceName: source.Name,
			RemoteID: item.RemoteID, Title: item.Title, Author: item.Author, Cover: cover,
			Summary: item.Summary, Genres: item.Genres, Status: item.Status,
			Latest: item.Latest, Words: item.Words, UpdatedAt: item.UpdatedAt, Available: true,
		})
	}
	result.NextURL = found.NextURL
	result.HasNext = found.HasNext
	evidence := make([]db.ReadingWorkEvidence, 0, len(result.Items))
	for _, item := range result.Items {
		evidence = append(evidence, db.ReadingWorkEvidence{ItemID: item.ID, TitleKey: matching.Normalize(item.Title), AuthorKey: matching.Normalize(item.Author)})
	}
	if err := db.RecordReadingWorks(ctx, kind, source.SourceKey, resolved.version, evidence); err != nil {
		result.ErrorCode, result.ErrorMessage = splitError(apperror.Wrap(apperror.Storage, err, "保存来源覆盖记录失败"))
	}
	return result
}

// splitError 把一个 error 拆成码与消息，供 DTO 使用。
//
// 码从 apperror 取；没有码的（包内 fmt.Errorf）归到 INTERNAL，
// 因为那意味着走到了一条没预料到的分支，而不是用户能处置的情况。
func splitError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	response := apperror.Public(err)
	return string(response.Code), response.Message
}
