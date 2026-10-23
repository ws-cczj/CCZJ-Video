package quality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/provider"
)

type Reader interface {
	Search(context.Context, string, string, int, string) (*provider.ItemPage, error)
	Detail(context.Context, string, string) (*provider.Item, error)
	Toc(context.Context, string, *provider.Item) ([]*provider.Chapter, error)
	Content(context.Context, string, *provider.Chapter, string) (*provider.TextContent, error)
	Pages(context.Context, string, *provider.Chapter) ([]*provider.Page, error)
	ResolveImage(context.Context, string, *provider.Page) (string, error)
}
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}
type Options struct {
	MaxQueries int
	MaxMatches int
	Delay      time.Duration
	Now        func() time.Time
}
type Failure struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Sample struct {
	Position int             `json:"position"`
	Title    string          `json:"title"`
	Access   string          `json:"access"`
	Failure  *Failure        `json:"failure,omitempty"`
	Text     *TextEvidence   `json:"text,omitempty"`
	Pages    int             `json:"pages,omitempty"`
	Images   []ImageEvidence `json:"images,omitempty"`
}
type WorkResult struct {
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	Tags           []string         `json:"tags"`
	Identity       Identity         `json:"identity"`
	MatchedURL     string           `json:"matchedUrl,omitempty"`
	Author         string           `json:"author,omitempty"`
	MetadataFields int              `json:"metadataFields"`
	Catalog        *CatalogEvidence `json:"catalog,omitempty"`
	Samples        []Sample         `json:"samples"`
	Failures       []Failure        `json:"failures"`
	Issues         []Issue          `json:"issues"`
}
type Metric struct {
	Name         string `json:"name"`
	Score        int    `json:"score"`
	Weight       int    `json:"weight"`
	Observations int    `json:"observations"`
}
type Summary struct {
	Status          string         `json:"status"` // healthy, partial, failed, inconclusive
	Score           int            `json:"score"`
	Confidence      string         `json:"confidence"` // insufficient, limited, substantial
	Queries         int            `json:"queries"`
	Matched         int            `json:"matched"`
	Verified        int            `json:"verified"`
	Readable        int            `json:"readable"`
	Attempts        int            `json:"attempts"`
	Restricted      int            `json:"restricted"`
	CatalogCompared int            `json:"catalogCompared"`
	TagCoverage     map[string]int `json:"tagCoverage"`
	Metrics         []Metric       `json:"metrics"`
	Notes           []string       `json:"notes"`
}
type Report struct {
	Version    string       `json:"version"`
	CorpusDate string       `json:"corpusDate"`
	CorpusHash string       `json:"corpusHash"`
	CheckedAt  int64        `json:"checkedAt"`
	Source     Source       `json:"source"`
	Millis     int64        `json:"millis"`
	Summary    Summary      `json:"summary"`
	Works      []WorkResult `json:"works"`
}

func failure(stage string, err error) Failure {
	if errors.Is(err, context.DeadlineExceeded) {
		return Failure{stage, string(apperror.Timeout), "请求超时"}
	}
	if errors.Is(err, context.Canceled) {
		return Failure{stage, string(apperror.Cancelled), "请求取消"}
	}
	e := apperror.Public(err)
	return Failure{stage, string(e.Code), e.Message}
}
func restricted(f *Failure) bool {
	return f != nil && (f.Code == string(apperror.PermissionDenied) || f.Code == string(apperror.AuthenticationRequired))
}

// Evaluate never writes user libraries, progresses or remote source rules. Its
// report can be rerun for a new adapter with exactly the same corpus version.
func Evaluate(ctx context.Context, reader Reader, source Source, corpus Corpus, opts Options, probe ImageProbe) (*Report, error) {
	if err := corpus.Validate(); err != nil {
		return nil, err
	}
	if source.Kind != "novel" && source.Kind != "comic" {
		return nil, fmt.Errorf("unsupported source kind")
	}
	if opts.MaxQueries <= 0 {
		opts.MaxQueries = 24
	}
	if opts.MaxMatches <= 0 {
		opts.MaxMatches = 5
	}
	if opts.MaxQueries > 200 || opts.MaxMatches > 20 || opts.Delay < 0 {
		return nil, fmt.Errorf("invalid benchmark limits")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	start := time.Now()
	now := opts.Now()
	report := &Report{Version: Version, CorpusDate: corpus.UpdatedAt, CheckedAt: now.Unix(), Source: source, Works: []WorkResult{}}
	encoded, _ := json.Marshal(corpus)
	digest := sha256.Sum256(encoded)
	report.CorpusHash = hex.EncodeToString(digest[:])
	next := time.Time{}
	pace := func() error {
		if delay := time.Until(next); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		next = time.Now().Add(opts.Delay)
		return ctx.Err()
	}
	matched := 0
	usedURLs := map[string]bool{}
	for _, w := range diverseWorks(corpus.Works, source.Kind) {
		if len(report.Works) >= opts.MaxQueries || matched >= opts.MaxMatches {
			break
		}
		if err := pace(); err != nil {
			return nil, err
		}
		wr := WorkResult{ID: w.ID, Title: w.Title, Tags: w.Tags, Samples: []Sample{}, Failures: []Failure{}, Issues: []Issue{}}
		page, err := reader.Search(ctx, source.URL, w.Title, 1, "")
		if err != nil {
			wr.Identity = Identity{"absent", "检索失败，不等于未收录"}
			wr.Failures = append(wr.Failures, failure("search", err))
			report.Works = append(report.Works, wr)
			if apperror.Public(err).Code == apperror.RateLimited {
				break
			}
			continue
		}
		var chosen *provider.Item
		var identity Identity
		best := 0
		ambiguous := false
		sawWrong := false
		for _, item := range page.Items {
			id := Match(w, item)
			rank := 0
			if id.Status == "verified" {
				rank = 2
			}
			if id.Status == "provisional" {
				rank = 1
			}
			if id.Status == "mismatch" {
				for _, title := range append([]string{w.Title}, w.Aliases...) {
					if Normalize(item.Title) == Normalize(title) {
						sawWrong = true
					}
				}
			}
			if rank == 0 {
				continue
			}
			if rank == best && chosen != nil && canonicalURL(item.URL) != canonicalURL(chosen.URL) {
				ambiguous = true
			}
			if rank > best {
				chosen, identity, best, ambiguous = item, id, rank, false
			}
		}
		if chosen == nil {
			wr.Identity = Identity{"absent", "未检索到同版作品"}
			if sawWrong {
				wr.Identity = Identity{"mismatch", "仅返回同名异作者作品"}
			}
			report.Works = append(report.Works, wr)
			continue
		}
		if ambiguous {
			wr.Identity = Identity{"ambiguous", "多个同等候选，未自动选书"}
			report.Works = append(report.Works, wr)
			continue
		}
		wr.Identity = identity
		wr.MatchedURL = chosen.URL
		if usedURLs[canonicalURL(chosen.URL)] {
			wr.Issues = append(wr.Issues, Issue{"fixed_search_target", "error", "不同作品查询指向相同作品地址"})
			report.Works = append(report.Works, wr)
			continue
		}
		usedURLs[canonicalURL(chosen.URL)] = true
		matched++
		if err := pace(); err != nil {
			return nil, err
		}
		book, err := reader.Detail(ctx, source.URL, chosen.URL)
		if err != nil {
			wr.Failures = append(wr.Failures, failure("detail", err))
			book = chosen
		} else {
			actual := Match(w, book)
			if actual.Status == "mismatch" {
				wr.Identity = actual
				wr.Issues = append(wr.Issues, Issue{"detail_identity_mismatch", "error", "搜索结果与详情作品不一致"})
				report.Works = append(report.Works, wr)
				continue
			}
			wr.Identity = actual
		}
		wr.Author = book.Author
		for _, v := range []string{book.Title, book.Author, book.Cover, book.Summary} {
			if strings.TrimSpace(v) != "" {
				wr.MetadataFields++
			}
		}
		if len([]rune(book.Author)) > 100 {
			wr.Issues = append(wr.Issues, Issue{"corrupt_author", "warning", "作者字段混入其他文本"})
			wr.Author = "[字段超长]"
		}
		if err := pace(); err != nil {
			return nil, err
		}
		chapters, err := reader.Toc(ctx, source.URL, book)
		if err != nil {
			wr.Failures = append(wr.Failures, failure("toc", err))
			report.Works = append(report.Works, wr)
			continue
		}
		ce := AnalyzeCatalog(w, chapters, now)
		wr.Catalog = &ce
		usable := []*provider.Chapter{}
		for _, c := range chapters {
			if c != nil && !c.IsVolume {
				usable = append(usable, c)
			}
		}
		fingerprints := map[string]string{}
		imageFingerprints := map[string]string{}
		positions := positions(len(usable))
		for _, pos := range positions {
			if err := pace(); err != nil {
				return nil, err
			}
			c := usable[pos]
			sample := Sample{Position: pos, Title: c.Title, Access: string(c.Access)}
			if source.Kind == "novel" {
				nextURL := ""
				if pos+1 < len(usable) {
					nextURL = usable[pos+1].URL
				}
				body, err := reader.Content(ctx, source.URL, c, nextURL)
				if err != nil {
					f := failure("content", err)
					sample.Failure = &f
				} else {
					e := AnalyzeText(body)
					sample.Text = &e
					if first, ok := fingerprints[e.Fingerprint]; ok && e.Runes > 100 {
						e.Issues = append(e.Issues, Issue{"repeated_chapter_body", "error", "不同章节正文相同：" + first + " / " + c.Title})
					}
					fingerprints[e.Fingerprint] = c.Title
				}
			} else {
				pages, err := reader.Pages(ctx, source.URL, c)
				if err != nil {
					f := failure("pages", err)
					sample.Failure = &f
				} else {
					sample.Pages = len(pages)
					for _, index := range positionsForImages(len(pages)) {
						if err := pace(); err != nil {
							return nil, err
						}
						p := pages[index]
						address := p.ImageURL
						if address == "" {
							address, err = reader.ResolveImage(ctx, source.URL, p)
						}
						var img ImageEvidence
						if err != nil {
							f := failure("image", err)
							img.Failure = &f
						} else if probe == nil {
							img.Failure = &Failure{"image", "UNSUPPORTED", "未配置实际图片校验"}
						} else {
							img = probe(ctx, address, p.Referer)
						}
						img.Index = index
						sample.Images = append(sample.Images, img)
						if img.Failure == nil && img.Fingerprint != "" {
							if first, ok := imageFingerprints[img.Fingerprint]; ok {
								wr.Issues = append(wr.Issues, Issue{"repeated_image_review", "info", "抽样重复图片，可能为公共扉页或广告，待核对：" + first + " / " + c.Title})
							}
							imageFingerprints[img.Fingerprint] = c.Title
						}
					}
				}
			}
			wr.Samples = append(wr.Samples, sample)
		}
		report.Works = append(report.Works, wr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report.Millis = time.Since(start).Milliseconds()
	report.Summary = Summarize(report.Works, source.Kind)
	return report, nil
}
func positions(n int) []int {
	if n <= 0 {
		return nil
	}
	out := []int{0}
	for _, i := range []int{n / 2, n - 1} {
		if i != out[len(out)-1] {
			out = append(out, i)
		}
	}
	return out
}
func positionsForImages(n int) []int { return positions(n) }
func diverseWorks(works []Work, kind string) []Work {
	groups := map[string][]Work{}
	keys := []string{}
	for _, w := range works {
		if w.Kind != kind {
			continue
		}
		key := w.Tags[0]
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], w)
	}
	out := []Work{}
	for len(keys) > 0 {
		next := keys[:0]
		for _, key := range keys {
			out = append(out, groups[key][0])
			groups[key] = groups[key][1:]
			if len(groups[key]) > 0 {
				next = append(next, key)
			}
		}
		keys = next
	}
	return out
}
func issuePenalty(issues []Issue) int {
	p := 0
	seen := map[string]bool{}
	for _, i := range issues {
		if seen[i.Code] {
			continue
		}
		seen[i.Code] = true
		if i.Severity == "error" {
			p += 45
		}
		if i.Severity == "warning" {
			p += 20
		}
	}
	return min(p, 100)
}

func Summarize(works []WorkResult, kind string) Summary {
	s := Summary{Status: "inconclusive", Confidence: "insufficient", TagCoverage: map[string]int{}, Metrics: []Metric{}, Notes: []string{}}
	identity, meta, catalog, readable, clean := 0, 0, 0, 0, 0
	catalogN, cleanN := 0, 0
	defects := false
	for _, w := range works {
		s.Queries++
		if w.Identity.Status != "verified" && w.Identity.Status != "provisional" {
			continue
		}
		s.Matched++
		for _, tag := range w.Tags {
			s.TagCoverage[tag]++
		}
		if w.Identity.Status == "verified" {
			s.Verified++
			identity += 100
		} else {
			identity += 50
		}
		meta += w.MetadataFields * 25
		if len(w.Failures) > 0 {
			defects = true
		}
		if issuePenalty(w.Issues) > 0 {
			defects = true
		}
		meta -= min(w.MetadataFields*25, issuePenalty(w.Issues))
		if w.Catalog != nil {
			catalogN++
			catalog += 100 - issuePenalty(w.Catalog.Issues)
			if issuePenalty(w.Catalog.Issues) > 0 {
				defects = true
			}
			if w.Catalog.ReferenceComparable {
				s.CatalogCompared++
			}
		}
		for _, sample := range w.Samples {
			s.Attempts++
			if sample.Failure != nil {
				if restricted(sample.Failure) {
					s.Restricted++
				} else {
					defects = true
				}
				continue
			}
			if sample.Text != nil {
				if sample.Text.Runes == 0 {
					defects = true
					continue
				}
				s.Readable++
				readable += 100 - issuePenalty(sample.Text.Issues)
				cleanN++
				clean += 100 - issuePenalty(sample.Text.Issues)
				if issuePenalty(sample.Text.Issues) > 0 {
					defects = true
				}
			} else if sample.Pages > 0 && len(sample.Images) > 0 {
				ok := 0
				for _, img := range sample.Images {
					if img.Failure == nil && img.Width > 0 && img.Height > 0 {
						ok++
					}
				}
				if ok > 0 {
					s.Readable++
				}
				readable += ok * 100 / len(sample.Images)
				if ok != len(sample.Images) {
					defects = true
				}
			}
		}
	}
	if s.Matched == 0 {
		s.Notes = append(s.Notes, "没有同版样本，不能给内容质量下结论；查看未收录、身份不符与请求失败的分别记录")
		return s
	}
	s.Metrics = append(s.Metrics, Metric{"identity", identity / s.Matched, 15, s.Matched}, Metric{"metadata", meta / s.Matched, 10, s.Matched})
	if catalogN > 0 {
		s.Metrics = append(s.Metrics, Metric{"catalog", catalog / catalogN, 30, catalogN})
	}
	if s.Attempts > 0 {
		s.Metrics = append(s.Metrics, Metric{"reading", readable / s.Attempts, 35, s.Attempts})
	}
	if cleanN > 0 {
		s.Metrics = append(s.Metrics, Metric{"text_cleanliness", clean / cleanN, 10, cleanN})
	}
	weight := 0
	for _, m := range s.Metrics {
		s.Score += m.Score * m.Weight
		weight += m.Weight
	}
	if weight > 0 {
		s.Score /= weight
	}
	capScore := 59
	if s.Verified >= 2 && s.Attempts >= 6 && s.Readable >= 3 {
		s.Confidence = "limited"
		capScore = 84
	}
	if s.Verified >= 3 && s.Attempts >= 9 && s.Readable >= 6 && s.CatalogCompared >= 2 {
		s.Confidence = "substantial"
		capScore = 100
	}
	// Confidence scales the evidence-based result rather than flattening all
	// imperfect sources to the same cap (which would hide duplicate-body bugs).
	s.Score = s.Score * capScore / 100
	s.Status = "partial"
	if s.Readable == 0 {
		s.Status = "failed"
	}
	if !defects && s.Restricted == 0 && s.Confidence != "insufficient" && s.Score >= 75 {
		s.Status = "healthy"
	}
	s.Notes = append(s.Notes, "未收录样本仅影响覆盖面，不扣内容质量；分数与置信度必须一起看", "这是一轮抽检，不能表示长期稳定或全站完整")
	if kind == "comic" {
		s.Notes = append(s.Notes, "已检验图片响应、格式、尺寸和重复指纹；未做 OCR/人工逐页审核，不能宣称图片无广告或图序全部正确")
	}
	if s.CatalogCompared == 0 {
		s.Notes = append(s.Notes, "未满足多锚点同版比较条件，章节完整度尚未证实")
	}
	if s.Restricted > 0 {
		s.Notes = append(s.Notes, "付费或登录限制单独记录，影响当前可读范围，不判作正文损坏")
	}
	return s
}

// Rank reports within a medium; unknown and low-evidence sources cannot win
// solely because a single first chapter happened to respond quickly.
func Rank(reports []*Report) {
	sort.SliceStable(reports, func(i, j int) bool {
		a, b := reports[i], reports[j]
		rank := func(s Summary) int {
			if s.Status == "inconclusive" {
				return 1
			}
			if s.Status == "failed" {
				return 0
			}
			if s.Confidence == "insufficient" {
				return 2
			}
			if s.Status == "healthy" {
				return 4
			}
			return 3
		}
		if rank(a.Summary) != rank(b.Summary) {
			return rank(a.Summary) > rank(b.Summary)
		}
		if a.Summary.Score != b.Summary.Score {
			return a.Summary.Score > b.Summary.Score
		}
		if a.Summary.Matched != b.Summary.Matched {
			return a.Summary.Matched > b.Summary.Matched
		}
		if a.Millis != b.Millis {
			return a.Millis < b.Millis
		}
		return a.Source.ID < b.Source.ID
	})
}

func imageHost(address string) string {
	u, _ := url.Parse(address)
	if u == nil {
		return ""
	}
	return u.Hostname()
}
