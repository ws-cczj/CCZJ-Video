package library

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

// ---------------------------------------------------------------------------
// 详情与目录
// ---------------------------------------------------------------------------

// Detail 取一部作品与它的目录。
//
// 目录来自本地库；refresh 为真时才去源站刷新一遍。默认不刷新是因为
// 打开一本书就发一轮目录请求，在几十个源的书架上等于每次进应用都打一遍源站。
// Legado 的节流是"剩余不足 3 章且距上次刷新 ≥10 分钟"（ReadBook.kt:864-870），
// 我们把触发条件交给调用方，这里只负责执行。
func (s *Service) Detail(ctx context.Context, itemID int, refresh bool) (*ItemDetail, error) {
	release := s.priority.begin()
	defer release()
	row, err := db.GetLibraryItem(itemID)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取作品失败")
	}
	resolved, resolveErr := s.resolve(row.SourceKey, model.SourceKind(row.Kind))
	if resolveErr != nil {
		// 源被删了或配置坏了，但书还在书架上：仍然返回条目本身，
		// 让界面能显示成"来源已下架"，而不是整页报错。
		// 源被删时 DeleteSource 已经把这些行转成 lifecycle_state='deleted'，
		// 所以 toDTO 自然会把 Available 置成 false，不需要另有一条孤儿分支。
		detail := &ItemDetail{Item: s.toDTO(model.SourceKind(row.Kind), row, map[string]string{})}
		chapters, listErr := s.chaptersOf(itemID)
		if listErr == nil {
			detail.Chapters = chapters
			detail.ChapterCount = len(chapters)
		}
		return detail, nil
	}
	if refresh {
		if err := s.refreshDetailAndToc(ctx, resolved, row); err != nil {
			applog.Warn("[Library] 刷新 %s 的详情失败: %v", row.Title, err)
			// 刷新失败不影响已有数据：目录还在库里，用户至少能接着读。
			// 这与 detail/service.go 的"刷新失败给旧数据续命"是同一个取舍。
		}
		// 刷新之后必须重读这一行：refreshDetailAndToc 已经把新标题、新简介、
		// 新封面写进库了，继续用刷新前那个 row 构造 DTO，界面上看到的就还是旧数据，
		// 而用户刚刚点的是"刷新"。
		if refreshed, err := db.GetLibraryItem(itemID); err == nil {
			row = refreshed
		}
	}
	detail := &ItemDetail{Item: s.toDTO(model.SourceKind(row.Kind), row, map[string]string{})}
	chapters, err := s.chaptersOf(itemID)
	if err != nil {
		return nil, err
	}
	detail.Chapters = chapters
	detail.ChapterCount = len(chapters)
	return detail, nil
}

func (s *Service) chaptersOf(itemID int) ([]*Chapter, error) {
	// 连已下架的一起取：阅读进度可能指着其中一章，界面要能显示成
	// "这一章取不到了"，而不是让进度停在一个看不见的位置上。
	rows, err := db.ListLibraryChapters(itemID, true)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取目录失败")
	}
	chapters := make([]*Chapter, 0, len(rows))
	for _, row := range rows {
		chapters = append(chapters, chapterDTO(row))
	}
	return chapters, nil
}

// refreshDetailAndToc 从源站刷新详情与目录，并把结果写回库。
func (s *Service) refreshDetailAndToc(ctx context.Context, resolved *resolvedSource, row *model.LibraryItem) error {
	kind := model.SourceKind(row.Kind)
	detailURL := requestURL(resolved, row.RemoteURL, row.RemoteID)
	if detailURL == "" {
		return apperror.Newf(apperror.Corrupt,
			"这部作品没有可用的详情地址（remote_url 与 remote_id 都推不出请求地址）")
	}
	item, err := resolved.pipeline.Detail(ctx, resolved.source.ApiUrl, detailURL)
	if err != nil {
		return err
	}
	// Search establishes this row's identity. Canonical redirects and a detail
	// page's different ID representation must not create a second item and attach
	// the catalog there, leaving the user's searched/shelved item empty.
	item.RemoteID = row.RemoteID
	// Some detail pages omit a search-only author field. Preserve that evidence
	// only while the returned title still identifies the same work.
	if strings.TrimSpace(item.Author) == "" && matching.Normalize(item.Title) == matching.Normalize(row.Title) {
		item.Author = row.Author
	}
	ids, err := db.UpsertLibraryItems(kind, row.SourceKey, []*model.LibraryItem{{
		RemoteID: item.RemoteID, RemoteURL: item.URL, Title: item.Title, Author: item.Author, Cover: item.Cover,
		Summary: item.Summary, Genres: item.Genres, Status: item.Status,
		Latest: item.Latest, Words: item.Words, RemoteUpdatedAt: item.UpdatedAt,
	}}, true)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "写入详情失败")
	}
	if len(ids) == 0 || ids[0] == 0 {
		return apperror.New(apperror.Internal, "详情写回后没有拿到本地 id")
	}
	itemID := ids[0]

	target := &provider.Item{RemoteID: item.RemoteID, Title: item.Title, URL: item.URL, TocURL: item.TocURL}
	chapters, err := resolved.pipeline.Toc(ctx, resolved.source.ApiUrl, target)
	if err != nil {
		return err
	}
	rows := make([]*model.LibraryChapter, 0, len(chapters))
	for _, chapter := range chapters {
		rows = append(rows, &model.LibraryChapter{
			RemoteID: chapter.RemoteID, RemoteURL: chapter.URL, Title: chapter.Title, Ord: chapter.Ord,
			Volume: chapter.Volume, IsVolume: chapter.IsVolume, Access: chapter.Access,
			RemoteUploadedAt: chapter.UploadedAt,
		})
	}
	added, reused, retired, err := db.ReplaceLibraryChapters(itemID, rows)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "写入目录失败")
	}
	applog.Debug("[Library] %s 目录同步：新增 %d、复用 %d、下架 %d", row.Title, added, reused, retired)
	s.noteTocRefresh(itemID)
	return nil
}

// RefreshReadingToc checks for updates only near the end of a known catalog.
// Failed attempts share the cooldown so an unavailable source is not hit per turn.
func (s *Service) RefreshReadingToc(ctx context.Context, itemID, chapterID int) (*ItemDetail, error) {
	detail, err := s.Detail(ctx, itemID, false)
	if err != nil || !s.claimTocRefresh(itemID, chapterID, detail) {
		return detail, err
	}
	return s.RefreshToc(ctx, itemID)
}

func (s *Service) noteTocRefresh(itemID int) {
	s.tocMu.Lock()
	defer s.tocMu.Unlock()
	s.recordTocAttempt(itemID)
}

func (s *Service) recordTocAttempt(itemID int) {
	if s.tocAttempts == nil {
		s.tocAttempts = make(map[int]time.Time)
	}
	if len(s.tocAttempts) >= 512 {
		var oldest int
		var stamp time.Time
		for id, at := range s.tocAttempts {
			if stamp.IsZero() || at.Before(stamp) {
				oldest, stamp = id, at
			}
		}
		delete(s.tocAttempts, oldest)
	}
	s.tocAttempts[itemID] = time.Now()
}

func (s *Service) claimTocRefresh(itemID, chapterID int, detail *ItemDetail) bool {
	if detail == nil || detail.Item == nil || !detail.Item.Available {
		return false
	}
	index, remaining := -1, 0
	for i, chapter := range detail.Chapters {
		if chapter.ID == chapterID && chapter.Available && !chapter.IsVolume {
			index = i
		}
		if index >= 0 && i > index && chapter.Available && !chapter.IsVolume {
			remaining++
		}
	}
	if index < 0 || remaining >= 3 {
		return false
	}
	return s.claimReadingRefresh(itemID)
}

// rebuildAbsoluteURL 把去域名的 remote_id 还原成一个可请求的地址。
func rebuildAbsoluteURL(apiURL, remoteID string) (string, error) {
	if strings.TrimSpace(remoteID) == "" {
		return "", fmt.Errorf("remote_id 为空")
	}
	base := strings.TrimSpace(apiURL)
	if base == "" {
		return "", fmt.Errorf("源没有接口地址")
	}
	if strings.HasPrefix(remoteID, "http://") || strings.HasPrefix(remoteID, "https://") {
		return remoteID, nil
	}
	scheme, host, err := splitSchemeHost(base)
	if err != nil {
		return "", err
	}
	// remote_id 的形状是 path、path?query 或 ?query（provider.RemoteID）。
	// 三种都直接拼在 scheme://host 后面就是原地址。
	return scheme + "://" + host + "/" + strings.TrimPrefix(remoteID, "/"), nil
}

func splitSchemeHost(raw string) (string, string, error) {
	withoutScheme := raw
	scheme := "https"
	if i := strings.Index(withoutScheme, "://"); i > 0 {
		scheme = withoutScheme[:i]
		withoutScheme = withoutScheme[i+3:]
	}
	if i := strings.IndexAny(withoutScheme, "/?#"); i >= 0 {
		withoutScheme = withoutScheme[:i]
	}
	if withoutScheme == "" {
		return "", "", fmt.Errorf("源接口地址里没有主机名")
	}
	return scheme, withoutScheme, nil
}

// RefreshToc 只刷目录，不刷详情。追更时用它：一本书的元信息很少变，
// 章节列表才是会变的那一半。
func (s *Service) RefreshToc(ctx context.Context, itemID int) (*ItemDetail, error) {
	release := s.priority.begin()
	defer release()
	row, err := db.GetLibraryItem(itemID)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取作品失败")
	}
	resolved, err := s.resolve(row.SourceKey, model.SourceKind(row.Kind))
	if err != nil {
		return nil, err
	}
	if err := s.refreshDetailAndToc(ctx, resolved, row); err != nil {
		return nil, err
	}
	return s.Detail(ctx, itemID, false)
}

// ---------------------------------------------------------------------------
// 小说正文
// ---------------------------------------------------------------------------

// ChapterText 取一章正文，先查磁盘缓存。
//
// 缓存键含源文档指纹（resolvedSource.version）：改了提取规则就作废旧缓存，
// 否则用户读到的是旧规则解析出来的正文，而界面上看不出来（ADR 0016）。
//
// **失败不缓存**：一次空正文或解析失败如果落了盘，用户下次进来还是空的，
// 而真正的原因（规则失效、需要登录）被缓存盖住了。
func (s *Service) ChapterText(ctx context.Context, chapterID int) (*ChapterText, error) {
	release := s.priority.begin()
	defer release()
	row, err := s.chapterRow(chapterID)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolve(row.sourceKey, model.SourceKind(row.kind))
	if err != nil {
		return nil, err
	}
	key := s.textCacheKey(resolved, row)
	result, err := s.textReads.Do(ctx, key, func(ctx context.Context) (*ChapterText, error) {
		if cached, ok := s.readTextCache(key); ok {
			return &ChapterText{ChapterID: chapterID, Paragraphs: cached.Paragraphs, FromCache: true, Warnings: contentWarnings(cached)}, nil
		}

		chapter := &provider.Chapter{
			RemoteID: row.remoteID, Title: row.title, URL: row.url,
			Ord: row.ord, IsVolume: row.isVolume, Access: model.ChapterAccess(row.access),
		}
		content, err := resolved.pipeline.Content(ctx, resolved.source.ApiUrl, chapter, row.nextChapterURL)
		if err != nil {
			return nil, err
		}
		warnings := contentWarnings(content)
		s.writeTextCache(key, content)
		return &ChapterText{ChapterID: chapterID, Paragraphs: content.Paragraphs, Warnings: warnings}, nil
	})
	if err != nil {
		return nil, err
	}
	copy := *result
	copy.Paragraphs = append([]string(nil), result.Paragraphs...)
	copy.Warnings = append([]string(nil), result.Warnings...)
	if len(copy.Paragraphs) > 0 && len(copy.Warnings) == 0 {
		if err := db.RecordReadingChapter(ctx, model.SourceKind(row.kind), chapterID, resolved.version, "text"); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "保存章节可读证据失败")
		}
	}
	return &copy, nil
}

// contentWarnings 把"内容拿到了但有件事该说"的情况翻成给用户看的话。
//
// 三条都不是错误，所以不阻断显示：判错了至少用户还能看到内容，
// 而判对了用户才知道该去改编码、该换一个源、或者这一章本来就不完整。
func contentWarnings(content *provider.TextContent) []string {
	var warnings []string
	if content.Truncated {
		warnings = append(warnings, "这一章的正文超过了单章上限，可能不完整")
	}
	if content.Garbled {
		warnings = append(warnings, "正文里有大量无法解码的字符；试试在源设置里把编码改成 GBK")
	}
	if content.FontObfuscated {
		// 字体码位混淆（reports/06 §6）：站点下发一份码位↔字形被打乱的 WebFont，
		// 浏览器渲染正常，抽出来的却是码点有效、字义错误的汉字。
		// 不提示的话用户看到的是一屏"正常汉字组成的胡话"，完全无法自查。
		warnings = append(warnings, "这个源可能用了字体加密，本项目不支持解密，正文可能是错字")
	}
	return warnings
}

// textCacheEntry 是正文缓存的落盘形状。
type textCacheEntry struct {
	Version        int      `json:"version"`
	Paragraphs     []string `json:"paragraphs"`
	Truncated      bool     `json:"truncated,omitempty"`
	Garbled        bool     `json:"garbled,omitempty"`
	FontObfuscated bool     `json:"fontObfuscated,omitempty"`
}

func (s *Service) textCacheKey(resolved *resolvedSource, row *chapterRow) string {
	// JSON encodes component boundaries, including identities containing delimiters.
	// A policy change must also invalidate previously truncated content.
	key, _ := json.Marshal([]any{"content-v2", resolved.source.SourceKey, resolved.version, row.itemRemoteID, row.remoteID, s.limits.MaxContentPages, s.limits.MaxContentBytes})
	return string(key)
}

func (s *Service) readTextCache(key string) (*provider.TextContent, bool) {
	if s.textCache == nil {
		return nil, false
	}
	raw, contentType, ok := s.textCache.Read(key)
	if !ok || !strings.HasPrefix(contentType, "application/json") {
		return nil, false
	}
	var entry textCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}
	// 空正文不进缓存也不该从缓存里被端出来：那意味着上次写入时判错了，
	// 而现在这一次本可以重新抓。
	if entry.Version != 2 || len(entry.Paragraphs) == 0 {
		return nil, false
	}
	return &provider.TextContent{Paragraphs: entry.Paragraphs, Truncated: entry.Truncated, Garbled: entry.Garbled, FontObfuscated: entry.FontObfuscated}, true
}

func (s *Service) writeTextCache(key string, content *provider.TextContent) {
	if s.textCache == nil || content == nil || len(content.Paragraphs) == 0 {
		return
	}
	raw, err := json.Marshal(textCacheEntry{Version: 2, Paragraphs: content.Paragraphs, Truncated: content.Truncated, Garbled: content.Garbled, FontObfuscated: content.FontObfuscated})
	if err != nil {
		return
	}
	s.textCache.Write(key, raw, "application/json; charset=utf-8")
}

// chapterRow 是取一章所需的全部上下文。
//
// 一次查询里把作品与相邻章一起取出来：正文提取需要"下一章的地址"来防止
// 把下一章吞进本章（provider.Pipeline.Content 的 nextChapterURL 参数）。
type chapterRow struct {
	chapterID      int
	itemID         int
	itemRemoteID   string
	kind           string
	sourceKey      string
	remoteID       string
	title          string
	url            string
	ord            int
	isVolume       bool
	access         string
	nextChapterURL string
}

func (s *Service) chapterRow(chapterID int) (*chapterRow, error) {
	chapter, err := db.GetLibraryChapter(chapterID)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取章节失败")
	}
	item, err := db.GetLibraryItem(chapter.ItemID)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取章节所属作品失败")
	}
	row := &chapterRow{
		chapterID: chapterID, itemID: item.Id, itemRemoteID: item.RemoteID,
		kind: item.Kind, sourceKey: item.SourceKey, remoteID: chapter.RemoteID,
		title: chapter.Title, ord: chapter.Ord, isVolume: chapter.IsVolume,
		access: string(chapter.Access),
	}
	resolved, err := s.resolve(item.SourceKey, model.SourceKind(item.Kind))
	if err == nil {
		row.url = requestURL(resolved, chapter.RemoteURL, chapter.RemoteID)
		// 下一章的地址：按 ord 找下一行。找不到就算了——它只是一个额外的
		// 防吞保护，没有它正文提取照样工作，只是"下一页"规则写得太宽时
		// 可能把下一章的开头带进来。
		if next, nextErr := db.GetLibraryChapterByOrd(item.Id, chapter.Ord+1); nextErr == nil && next != nil {
			row.nextChapterURL = requestURL(resolved, next.RemoteURL, next.RemoteID)
		}
	}
	return row, nil
}

// requestURL 返回一条记录该请求的绝对地址。
//
// 优先用库里存的原始地址。只有它为空时（源没给出 url 字段）才退回用
// api_url + remote_id 重建。重建对"remote_id 来自 URL"的源是准的，
// 但源文档声明了显式 id 字段时 remote_id 是 "12345" 这种值，
// 拼出来的地址是纯垃圾；而详情页也可能在另一个域上，从 api_url 根本推不回来。
// 所以 remote_url 是主路径，重建只是兜底。
func requestURL(resolved *resolvedSource, remoteURL, remoteID string) string {
	if trimmed := strings.TrimSpace(remoteURL); trimmed != "" {
		return trimmed
	}
	absolute, err := rebuildAbsoluteURL(resolved.source.ApiUrl, remoteID)
	if err != nil {
		return ""
	}
	return absolute
}

// ---------------------------------------------------------------------------
// 漫画图片列表
// ---------------------------------------------------------------------------

// ChapterPages 取一话的图片列表，先查磁盘缓存。
//
// Page-list cache follows the source revision. Explicit retry bypasses it to
// reacquire signed image URLs; it does not bypass image validation or policy.
func (s *Service) ChapterPages(ctx context.Context, chapterID int) (*ChapterPages, error) {
	return s.chapterPages(ctx, chapterID, false)
}

func (s *Service) RefreshChapterPages(ctx context.Context, chapterID int) (*ChapterPages, error) {
	return s.chapterPages(ctx, chapterID, true)
}

func (s *Service) chapterPages(ctx context.Context, chapterID int, refresh bool) (*ChapterPages, error) {
	release := s.priority.begin()
	defer release()
	row, err := s.chapterRow(chapterID)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolve(row.sourceKey, model.SourceKind(row.kind))
	if err != nil {
		return nil, err
	}
	key := s.textCacheKey(resolved, row)
	s.pageMu.Lock()
	if refresh {
		s.pageEpoch++
	}
	epoch := s.pageEpoch
	s.pageMu.Unlock()
	flightKey := fmt.Sprintf("%s:%d", key, epoch)
	result, err := s.pageReads.Do(ctx, flightKey, func(ctx context.Context) (*ChapterPages, error) {
		if cached, ok := s.readPagesCache(key); ok && !refresh {
			return &ChapterPages{ChapterID: chapterID, Pages: s.proxyPages(cached, row, resolved), FromCache: true}, nil
		}

		chapter := &provider.Chapter{
			RemoteID: row.remoteID, Title: row.title, URL: row.url,
			Ord: row.ord, IsVolume: row.isVolume, Access: model.ChapterAccess(row.access),
		}
		found, err := resolved.pipeline.Pages(ctx, resolved.source.ApiUrl, chapter)
		if err != nil {
			return nil, err
		}
		// 两步解析：只给出解析页地址的那些页，还要再请求一次才拿到图片最终地址。
		// reports/03:95 明确"不能默认 page URL 就是最终图片 URL"。
		resolvedPages := make([]*provider.Page, 0, len(found))
		for _, page := range found {
			if page.ImageURL != "" {
				resolvedPages = append(resolvedPages, page)
				continue
			}
			if resolved.document.Operations.Image == nil {
				// 源没有声明第二步，这一页拿不到图片。报出来比放一个空 URL 进去好：
				// 空 URL 在界面上是一张永远加载不出来的图，看不出是配置漏了一步。
				return nil, apperror.New(apperror.Unsupported,
					"这一话只给出了解析页地址，但源配置里没有 image 操作来做第二步解析")
			}
			imageURL, err := resolved.pipeline.ResolveImage(ctx, resolved.source.ApiUrl, page)
			if err != nil {
				return nil, err
			}
			page.ImageURL = imageURL
			resolvedPages = append(resolvedPages, page)
		}
		// A slower request started before an explicit refresh must not restore an
		// expired URL after the refreshed page list has been published.
		s.pageMu.Lock()
		if s.pageEpoch == epoch {
			s.writePagesCache(key, resolvedPages)
		}
		s.pageMu.Unlock()
		return &ChapterPages{ChapterID: chapterID, Pages: s.proxyPages(resolvedPages, row, resolved)}, nil
	})
	if err != nil {
		return nil, err
	}
	copy := *result
	copy.Pages = make([]*Page, len(result.Pages))
	for i, page := range result.Pages {
		if page != nil {
			value := *page
			copy.Pages[i] = &value
		}
	}
	copy.Warnings = append([]string(nil), result.Warnings...)
	if len(copy.Pages) > 0 {
		if err := db.RecordReadingChapter(ctx, model.SourceKind(row.kind), chapterID, resolved.version, "page_list"); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "保存漫画页表证据失败")
		}
	}
	return &copy, nil
}

// proxyPages 把上游图片地址换成同源代理地址。
//
// Referer 用这一页被发现时所在的页面地址（provider.Page.Referer），
// 而不是章节页或源首页：真实漫画 CDN 校验的往往就是那个地址。
// 源声明的头由代理按 source_key 反查（provider.SourcesHeaders），不走 URL 参数。
func (s *Service) proxyPages(pages []*provider.Page, row *chapterRow, resolved *resolvedSource) []*Page {
	out := make([]*Page, 0, len(pages))
	for _, page := range pages {
		if page == nil || reviewedComicPromotion(resolved.source.ApiUrl, page.ImageURL) {
			continue
		}
		url := page.ImageURL
		if s.imageURL != nil && url != "" {
			referer := page.Referer
			if referer == "" {
				referer = row.url
			}
			url = s.imageURL(url, referer, resolved.source.SourceKey)
			if proxyURL, err := neturl.Parse(url); err == nil && proxyURL.Path == "/__cczj/img" {
				q := proxyURL.Query()
				q.Set("rv", resolved.version)
				proxyURL.RawQuery = q.Encode()
				url = proxyURL.String()
			}
		}
		out = append(out, &Page{Index: len(out), URL: url})
	}
	return out
}

// cachedPage 是页表的落盘形状。
//
// 存的是**上游地址**而不是代理地址：代理地址里编着 Referer 与 source_key，
// 缓存下来会让"换了一个源"或"改了 Referer"之后仍然端出旧地址。
type cachedPage struct {
	Index    int    `json:"index"`
	ImageURL string `json:"imageUrl"`
	Referer  string `json:"referer"`
}

func (s *Service) readPagesCache(key string) ([]*provider.Page, bool) {
	if s.textCache == nil {
		return nil, false
	}
	raw, contentType, ok := s.textCache.Read(key + "#pages")
	if !ok || !strings.HasPrefix(contentType, "application/json") {
		return nil, false
	}
	var entries []cachedPage
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return nil, false
	}
	pages := make([]*provider.Page, 0, len(entries))
	for _, entry := range entries {
		pages = append(pages, &provider.Page{Index: entry.Index, ImageURL: entry.ImageURL, Referer: entry.Referer})
	}
	return pages, true
}

func (s *Service) writePagesCache(key string, pages []*provider.Page) {
	if s.textCache == nil || len(pages) == 0 {
		return
	}
	entries := make([]cachedPage, 0, len(pages))
	for _, page := range pages {
		entries = append(entries, cachedPage{Index: page.Index, ImageURL: page.ImageURL, Referer: page.Referer})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return
	}
	// 页表与正文共用一个 store，所以键要加后缀区分：同一章的两种内容
	// 用同一个键会互相覆盖，而覆盖是静默的。
	s.textCache.Write(key+"#pages", raw, "application/json; charset=utf-8")
}
