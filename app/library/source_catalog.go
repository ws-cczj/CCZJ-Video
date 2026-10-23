package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

//go:embed source_catalog.json
var bundledSourceCatalog []byte

//go:embed source_library_novel.json
var novelSourceLibrary []byte

//go:embed source_library_comic.json
var comicSourceLibrary []byte

//go:embed source_library_quality.json
var sourceLibraryQuality []byte

const maxCatalogBytes = 32 << 20
const maxCatalogCandidates = 10000

type SourceCandidate struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ApiURL      string         `json:"apiUrl"`
	Kind        string         `json:"kind"`
	Project     string         `json:"project"`
	Origin      string         `json:"origin"`
	Format      string         `json:"format"`
	Compatible  bool           `json:"compatible"`
	Reason      string         `json:"reason"`
	Summary     string         `json:"summary"`
	Imported    bool           `json:"imported"`
	Quality     *SourceQuality `json:"quality,omitempty"`
	document    string
	searchProbe *provider.Document
	language    string
}

type SourcePreview struct {
	ID         string             `json:"id"`
	Origin     string             `json:"origin"`
	Candidates []*SourceCandidate `json:"candidates"`
	ExpiresAt  int64              `json:"expiresAt"`
}

type sourceCatalogSession struct {
	expires    time.Time
	candidates map[string]*SourceCandidate
}

// PreviewSources holds validated documents on the backend; the UI receives
// metadata only. Previewing and probing never installs a source or a binary.
func (s *Service) PreviewSources(ctx context.Context, kind model.SourceKind, input, origin string) (*SourcePreview, error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return nil, apperror.New(apperror.Validation, "请选择小说或漫画模式")
	}
	community := input == "community" || input == "community:refresh"
	refreshCommunity := input == "community:refresh"
	if refreshCommunity {
		input = "https://raw.githubusercontent.com/aoaostar/legado/master/sources/b778fe6b.json"
		if kind == model.SourceKindComic {
			input = "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.json"
		}
	}
	raw := []byte(input)
	if community && !refreshCommunity {
		raw = novelSourceLibrary
		origin = "https://github.com/aoaostar/legado"
		if kind == model.SourceKindComic {
			raw = comicSourceLibrary
			origin = "https://keiyoushi.github.io/docs/guides/getting-started"
		}
	} else if strings.TrimSpace(input) == "" {
		raw = bundledSourceCatalog
		origin = "CCZJ 已适配源"
	} else if strings.HasPrefix(strings.TrimSpace(input), "https://") || strings.HasPrefix(strings.TrimSpace(input), "http://") {
		address := strings.TrimSpace(input)
		response, err := s.fetcher.ScopeMode("source-catalog", string(kind)).FetchSourceList(ctx, address)
		if err != nil {
			return nil, err
		}
		raw = []byte(response.Text)
		origin = response.FinalURL
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	if len(raw) > maxCatalogBytes {
		return nil, apperror.New(apperror.Validation, "源文件超过 32 MiB 上限，请拆分后导入")
	}
	candidates, err := parseSourceCatalog(raw, string(kind), origin)
	if err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "无法读取源列表："+err.Error())
	}
	if !community && strings.TrimSpace(input) == "" {
		ready := candidates[:0]
		for _, row := range candidates {
			if row.Compatible {
				ready = append(ready, row)
			}
		}
		candidates = ready
	}
	if community {
		bundled, parseErr := parseSourceCatalog(bundledSourceCatalog, string(kind), "")
		if parseErr != nil {
			return nil, parseErr
		}
		seen := map[string]bool{}
		combined := make([]*SourceCandidate, 0, len(bundled)+len(candidates))
		for _, row := range append(bundled, candidates...) {
			if !seen[row.ID] {
				seen[row.ID] = true
				combined = append(combined, row)
			}
		}
		candidates = combined
	}
	qualities, err := s.loadSourceQualities()
	if err != nil {
		return nil, err
	}
	imported, err := db.GetSourcesByKind(kind)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取已导入源失败")
	}
	installed := map[string]bool{}
	for _, row := range imported {
		installed[sourceQualityID(string(kind), row.ApiUrl, row.StrategyConfig)] = true
	}
	byID := make(map[string]*SourceCandidate, len(candidates))
	for _, candidate := range candidates {
		candidate.Imported = installed[candidate.ID]
		candidate.Quality = qualities[candidate.ID]
		if candidate.Quality == nil {
			candidate.Quality = bundledQuality(candidate.ID)
		}
		byID[candidate.ID] = candidate
	}
	rankSourceCandidates(candidates)
	id, err := model.NewSourceKey()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(30 * time.Minute)
	s.catalogMu.Lock()
	if s.catalogs == nil {
		s.catalogs = map[string]*sourceCatalogSession{}
	}
	for key, session := range s.catalogs {
		if time.Now().After(session.expires) {
			delete(s.catalogs, key)
		}
	}
	if len(s.catalogs) >= 4 {
		var oldest string
		var at time.Time
		for key, session := range s.catalogs {
			if at.IsZero() || session.expires.Before(at) {
				oldest, at = key, session.expires
			}
		}
		delete(s.catalogs, oldest)
	}
	s.catalogs[id] = &sourceCatalogSession{expires: expires, candidates: byID}
	s.catalogMu.Unlock()
	return &SourcePreview{ID: id, Origin: origin, Candidates: candidates, ExpiresAt: expires.Unix()}, nil
}

func parseSourceCatalog(raw []byte, kind, origin string) ([]*SourceCandidate, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		var envelope map[string]json.RawMessage
		if err = json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("需要 JSON 对象或数组")
		}
		if modern := envelope["extensionList"]; len(modern) > 0 {
			var list struct {
				Extensions []json.RawMessage `json:"extensions"`
			}
			if err = json.Unmarshal(modern, &list); err != nil {
				return nil, err
			}
			rows = list.Extensions
		} else if sources := envelope["sources"]; len(sources) > 0 && envelope["pkg"] == nil {
			if err = json.Unmarshal(sources, &rows); err != nil {
				return nil, err
			}
		} else {
			rows = []json.RawMessage{raw}
		}
	}
	if len(rows) > maxCatalogCandidates {
		return nil, fmt.Errorf("列表超过 %d 个候选，请拆分源文件", maxCatalogCandidates)
	}
	out := make([]*SourceCandidate, 0, len(rows))
	seen := map[string]bool{}
	for index, row := range rows {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(row, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("第 %d 项不是源对象", index+1)
		}
		candidate := &SourceCandidate{Kind: kind, Origin: validCatalogOrigin(origin), Format: "cczj-v3", Project: "CCZJ"}
		var document string
		switch {
		case fields["bookSourceUrl"] != nil:
			candidate.Name = stringValue(fields["bookSourceName"])
			candidate.ApiURL = stringValue(fields["bookSourceUrl"])
			candidate.Format = "legado"
			candidate.Project = "Legado"
			var sourceType int
			_ = json.Unmarshal(fields["bookSourceType"], &sourceType)
			if sourceType != 0 && sourceType != 2 {
				continue
			}
			actualKind := "novel"
			if sourceType == 2 {
				actualKind = "comic"
			}
			if actualKind != kind {
				continue
			}
			candidate.language = "zh"
			candidate.searchProbe, _ = legacySearchProbe(fields, kind)
			if adapter := bundledAdapter(kind, candidate.ApiURL); adapter != nil {
				document = string(adapter.Document)
				candidate.Summary = adapter.Summary
				candidate.Format = "adapted"
				candidate.ApiURL = adapter.ApiURL
			} else {
				var err error
				document, err = convertLegado(fields, kind)
				if err != nil {
					candidate.Reason = err.Error()
				}
			}
		case fields["sources"] != nil && (fields["pkg"] != nil || fields["packageName"] != nil):
			// A Mihon index describes Kotlin/APK executables, not extraction rules.
			var sources []struct {
				ID       json.RawMessage `json:"id"`
				Name     string          `json:"name"`
				BaseURL  string          `json:"baseUrl"`
				Lang     string          `json:"lang"`
				HomeURL  string          `json:"homeUrl"`
				Language string          `json:"language"`
			}
			if err := json.Unmarshal(fields["sources"], &sources); err != nil {
				return nil, err
			}
			if kind != "comic" {
				continue
			}
			rowDigest := sha256.Sum256(row)
			for _, entry := range sources {
				if entry.BaseURL == "" {
					entry.BaseURL = entry.HomeURL
				}
				c := &SourceCandidate{Name: entry.Name, ApiURL: entry.BaseURL, Kind: kind, Origin: candidate.Origin, Format: "mihon", Project: "Mihon", Reason: "此扩展包含 Kotlin/APK 代码，需要对应站点适配；不能直接执行为阅读源"}
				c.language = entry.Lang
				if c.language == "" {
					c.language = entry.Language
				}
				if adapter := bundledAdapter(kind, entry.BaseURL); adapter != nil {
					c.document = string(adapter.Document)
					c.ApiURL = adapter.ApiURL
					c.Compatible = true
					c.Reason = ""
					c.Summary = adapter.Summary
					c.Format = "adapted"
				}
				// Metadata-only extension entries may share a host and display name
				// across languages. Keep remote identity; hash the large parent once.
				c.ID = sourceQualityID(kind, c.ApiURL, hex.EncodeToString(rowDigest[:])+"\x00"+string(entry.ID)+"\x00"+entry.Name+"\x00"+c.language)
				if c.Compatible {
					c.ID = sourceQualityID(kind, c.ApiURL, c.document)
				}
				if !seen[c.ID] {
					seen[c.ID] = true
					out = append(out, c)
				}
			}
			continue
		default:
			if declared := stringValue(fields["kind"]); declared != "" && declared != kind {
				continue
			}
			candidate.Name = stringValue(fields["name"])
			candidate.ApiURL = stringValue(fields["apiUrl"])
			if p := stringValue(fields["project"]); p != "" {
				candidate.Project = p
			}
			if f := stringValue(fields["format"]); f != "" {
				candidate.Format = f
			}
			if link := validCatalogOrigin(stringValue(fields["origin"])); link != "" {
				candidate.Origin = link
			}
			candidate.Reason = stringValue(fields["reason"])
			candidate.Summary = stringValue(fields["summary"])
			if d := fields["document"]; len(d) > 0 && string(d) != "null" {
				document = stringValue(d)
				if document == "" {
					document = string(d)
				}
			}
		}
		if candidate.Name == "" {
			candidate.Name = candidate.ApiURL
		}
		if candidate.Name == "" {
			candidate.Name = fmt.Sprintf("源 %d", index+1)
		}
		if len(document) > 256<<10 {
			candidate.Reason = "单个源文档超过 256 KiB 上限"
			document = ""
		}
		if candidate.Reason == "" {
			if err := model.ValidateSourceURL(candidate.ApiURL); err != nil {
				candidate.Reason = "缺少合法 HTTP(S) 源地址"
			} else if parsed, err := provider.ParseDocument(document, kind); err != nil {
				candidate.Reason = err.Error()
			} else if candidate.Format == "legado" && !legacyDocumentHasKeyword(parsed) {
				candidate.Reason = "固定书单没有关键词搜索能力"
			} else {
				encoded, _ := json.Marshal(parsed)
				document = string(encoded)
				candidate.Compatible = true
			}
		}
		candidate.document = document
		candidate.ID = sourceQualityID(kind, candidate.ApiURL, document)
		if document == "" {
			candidate.ID = sourceQualityID(kind, candidate.ApiURL, string(row))
		}
		if !seen[candidate.ID] {
			seen[candidate.ID] = true
			out = append(out, candidate)
		}
	}
	if len(out) > maxCatalogCandidates {
		return nil, fmt.Errorf("展开扩展后超过 %d 个候选", maxCatalogCandidates)
	}
	return out, nil
}

func sourceQualityID(kind, address, document string) string {
	var parsed provider.Document
	if json.Unmarshal([]byte(document), &parsed) == nil && parsed.Version == 3 {
		if encoded, err := json.Marshal(parsed); err == nil {
			document = string(encoded)
		}
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + strings.TrimRight(address, "/") + "\x00" + document))
	return hex.EncodeToString(sum[:])
}

type bundledSourceAdapter struct {
	ApiURL   string          `json:"apiUrl"`
	Kind     string          `json:"kind"`
	Summary  string          `json:"summary"`
	Document json.RawMessage `json:"document"`
	Quality  *SourceQuality  `json:"quality"`
}

var adapterOnce sync.Once
var sourceAdapters []bundledSourceAdapter
var qualityOnce sync.Once
var shippedQualities map[string]*SourceQuality

func bundledAdapter(kind, address string) *bundledSourceAdapter {
	adapterOnce.Do(func() {
		_ = json.Unmarshal([]byte(strings.TrimPrefix(string(bundledSourceCatalog), "\ufeff")), &sourceAdapters)
	})
	target := strings.TrimRight(strings.ToLower(address), "/")
	for _, adapter := range sourceAdapters {
		if adapter.Kind == kind && len(adapter.Document) > 0 && target == strings.TrimRight(strings.ToLower(adapter.ApiURL), "/") {
			return &adapter
		}
	}
	return nil
}

// Only shipped, reviewed observations can seed an untested local installation.
// A downloaded catalog cannot promote itself by supplying invented quality JSON.
func bundledQuality(id string) *SourceQuality {
	qualityOnce.Do(func() { _ = json.Unmarshal(sourceLibraryQuality, &shippedQualities) })
	if quality := shippedQualities[id]; quality != nil {
		copy := *quality
		copy.Evidence = "bundled"
		return &copy
	}
	adapterOnce.Do(func() {
		_ = json.Unmarshal([]byte(strings.TrimPrefix(string(bundledSourceCatalog), "\ufeff")), &sourceAdapters)
	})
	for _, adapter := range sourceAdapters {
		if len(adapter.Document) > 0 && sourceQualityID(adapter.Kind, adapter.ApiURL, string(adapter.Document)) == id && adapter.Quality != nil {
			copy := *adapter.Quality
			copy.Evidence = "bundled"
			return &copy
		}
	}
	return nil
}

func (s *Service) catalogCandidate(previewID, id string) (*SourceCandidate, error) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	session := s.catalogs[previewID]
	if session == nil || time.Now().After(session.expires) {
		return nil, apperror.New(apperror.Conflict, "源预览已过期，请重新载入")
	}
	candidate := session.candidates[id]
	if candidate == nil {
		return nil, apperror.New(apperror.NotFound, "此候选不在当前源列表中")
	}
	copy := *candidate
	return &copy, nil
}

func (s *Service) CheckSourceCandidate(ctx context.Context, previewID, id, keyword string) (*SourceCandidate, error) {
	candidate, err := s.catalogCandidate(previewID, id)
	if err != nil {
		return nil, err
	}
	if !candidate.Compatible {
		return candidate, nil
	}
	select {
	case s.searchSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.searchSlots }()
	doc, err := provider.ParseDocument(candidate.document, candidate.Kind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keyword) != "" {
		doc.CheckKeyword = strings.TrimSpace(keyword)
	}
	owner := ctx
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	resolved := &resolvedSource{source: &model.Source{SourceKey: candidate.ID, ApiUrl: candidate.ApiURL, Kind: candidate.Kind}, document: doc, pipeline: provider.NewPipeline(doc, s.fetcher.ScopeMode(candidate.ID, candidate.Kind), s.limits)}
	outcome := s.testResolvedSource(ctx, candidate.ID, resolved)
	if owner.Err() != nil {
		return nil, owner.Err()
	}
	quality, err := s.saveSourceQuality(candidate.ID, outcome)
	if err != nil {
		return nil, err
	}
	candidate.Quality = quality
	return candidate, nil
}

type SourceImportResult struct {
	ID        string `json:"id"`
	SourceKey string `json:"sourceKey"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (s *Service) ImportSourceCandidates(ctx context.Context, previewID string, ids []string) ([]*SourceImportResult, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, apperror.New(apperror.Validation, "每次请选择 1–100 个源")
	}
	candidates := make([]*SourceCandidate, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		candidate, err := s.catalogCandidate(previewID, id)
		if err != nil {
			return nil, err
		}
		if !candidate.Compatible {
			return nil, apperror.New(apperror.Unsupported, candidate.Reason)
		}
		if !seen[id] {
			seen[id] = true
			candidates = append(candidates, candidate)
		}
	}
	out := make([]*SourceImportResult, 0, len(candidates))
	for _, candidate := range candidates {
		result := &SourceImportResult{ID: candidate.ID, Name: candidate.Name, Status: "failed"}
		if ctx.Err() != nil {
			result.Message = "导入已取消"
			out = append(out, result)
			continue
		}
		key := "src_" + candidate.ID[:32]
		existing, err := db.GetSourceByKey(key)
		if err == nil {
			result.SourceKey = existing.SourceKey
			result.Status = "existing"
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, apperror.Wrap(apperror.Storage, err, "读取已导入源失败")
		} else {
			// Also recognize an identical source that was added manually.
			sources, readErr := db.GetSourcesByKind(model.SourceKind(candidate.Kind))
			if readErr != nil {
				return out, readErr
			}
			for _, source := range sources {
				if sourceQualityID(candidate.Kind, source.ApiUrl, source.StrategyConfig) == candidate.ID {
					result.SourceKey = source.SourceKey
					result.Status = "existing"
					break
				}
			}
			if result.Status != "existing" {
				row := &model.Source{SourceKey: key, Name: candidate.Name, ApiUrl: candidate.ApiURL, Kind: candidate.Kind, StrategyConfig: candidate.document, Enabled: 1}
				if err = db.AddSource(row); err != nil {
					result.Message = "写入源失败"
				} else {
					result.SourceKey = key
					result.Status = "imported"
				}
			}
		}
		out = append(out, result)
		if result.Status == "imported" || result.Status == "existing" {
			s.catalogMu.Lock()
			if session := s.catalogs[previewID]; session != nil {
				if current := session.candidates[candidate.ID]; current != nil {
					current.Imported = true
				}
			}
			s.catalogMu.Unlock()
		}
	}
	return out, nil
}

func rankSourceCandidates(candidates []*SourceCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		ar, br := candidateRank(a), candidateRank(b)
		if ar != br {
			return ar > br
		}
		if a.Quality != nil && b.Quality != nil {
			if a.Quality.Score != b.Quality.Score {
				return a.Quality.Score > b.Quality.Score
			}
			if a.Quality.Millis != b.Quality.Millis {
				return a.Quality.Millis < b.Quality.Millis
			}
		}
		return a.Name < b.Name
	})
}

func candidateRank(c *SourceCandidate) int {
	if !c.Compatible {
		return 0
	}
	if c.Quality == nil || time.Since(time.Unix(c.Quality.CheckedAt, 0)) > 24*time.Hour {
		return 2
	}
	switch c.Quality.Status {
	case "healthy":
		if strings.HasPrefix(c.Quality.Evidence, "benchmark:") {
			return 6
		}
		return 4
	case "partial":
		if strings.HasPrefix(c.Quality.Evidence, "benchmark:") && c.Quality.Score >= 60 {
			return 5
		}
		return 3
	default:
		return 1
	}
}
