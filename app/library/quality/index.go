package quality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/provider"
)

// Index compiles a corpus once. Novel/comic and language buckets prevent a
// larger foreign-language catalog from crowding out Chinese search samples.
// Lookup is O(title length + same-title candidates), not O(corpus size).
type Index struct {
	corpus Corpus
	hash   string
	titles map[string][]int
	seeds  map[string][]string
}

func Compile(c Corpus) (*Index, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	x := &Index{corpus: c, hash: hex.EncodeToString(sum[:]), titles: make(map[string][]int, len(c.Works)*2), seeds: map[string][]string{}}
	for i, w := range c.Works {
		seen := map[string]bool{}
		indexTitle := func(title string) {
			key := w.Kind + "\x00" + Normalize(title)
			if !seen[key] {
				x.titles[key] = append(x.titles[key], i)
				seen[key] = true
			}
		}
		indexTitle(w.Title)
		for _, title := range w.Aliases {
			indexTitle(title)
		}
		for _, title := range w.SearchTitles {
			indexTitle(title)
		}
	}
	// Round-robin by genre once; each source selects only a bounded prefix.
	seenSeeds := map[string]bool{}
	for _, kind := range []string{"novel", "comic"} {
		for _, w := range diverseWorks(c.Works, kind) {
			localized := map[string]string{}
			langs := make([]string, 0, len(w.SearchTitles))
			for lang := range w.SearchTitles {
				langs = append(langs, lang)
			}
			sort.Strings(langs)
			for _, lang := range langs {
				title := w.SearchTitles[lang]
				if title != "" {
					key := languageKey(lang)
					if localized[key] == "" || lang == key || lang == "zh-cn" {
						localized[key] = title
					}
				}
			}
			if len(localized) == 0 {
				localized[languageKey(w.Language)] = w.Title
			}
			for lang, title := range localized {
				key := kind + "\x00" + lang
				identity := key + "\x00" + Normalize(title)
				if !seenSeeds[identity] {
					x.seeds[key] = append(x.seeds[key], title)
					seenSeeds[identity] = true
				}
			}
		}
	}
	return x, nil
}
func languageKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Split(strings.ReplaceAll(s, "_", "-"), "-")[0]
	if s == "" {
		s = "zh"
	}
	return s
}
func (x *Index) Hash() string { return x.hash }
func (x *Index) Date() string { return x.corpus.UpdatedAt }
func (x *Index) Size() int    { return len(x.corpus.Works) }

type Hit struct {
	Work     Work           `json:"work"`
	Item     *provider.Item `json:"-"`
	Identity Identity       `json:"identity"`
}

func (x *Index) Lookup(kind string, item *provider.Item) []Hit {
	hits, _ := x.lookup(kind, item)
	return hits
}
func (x *Index) lookup(kind string, item *provider.Item) ([]Hit, bool) {
	if item == nil {
		return nil, false
	}
	indices := x.titles[kind+"\x00"+Normalize(item.Title)]
	if len(indices) > 16 {
		return nil, true
	} // Adversarial homonym groups remain ambiguous.
	out := make([]Hit, 0, len(indices))
	for _, i := range indices {
		w := x.corpus.Works[i]
		id := Match(w, item)
		if id.Status == "verified" || id.Status == "provisional" {
			out = append(out, Hit{Work: w, Item: item, Identity: id})
		}
	}
	return out, false
}
func (x *Index) QuerySeeds(kind, language, configured string, limit int) []string {
	return x.querySeeds(kind, language, configured, limit, "")
}

func (x *Index) querySeeds(kind, language, configured string, limit int, sourceID string) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	add := func(s string) {
		key := Normalize(s)
		if key != "" && !seen[key] && len(out) < limit {
			seen[key] = true
			out = append(out, s)
		}
	}
	add(configured)
	seeds := x.seeds[kind+"\x00"+languageKey(language)]
	offset := 0
	if sourceID != "" && len(seeds) > 0 {
		h := fnv.New64a()
		_, _ = h.Write([]byte(sourceID))
		offset = int(h.Sum64() % uint64(len(seeds)))
	}
	// Stable source-specific rotation spreads a batch over the corpus without
	// multiplying every source by every work or changing a resumed selection.
	for i := 0; i < len(seeds); i++ {
		add(seeds[(offset+i)%len(seeds)])
		if len(out) == limit {
			break
		}
	}
	return out
}

// Discovery uses a constant per-source query budget and indexes every returned
// title, rather than issuing a request for every source/work combination.
type Discovery struct {
	Queries         int       `json:"queries"`
	QueryTitles     []string  `json:"queryTitles,omitempty"`
	Items           int       `json:"items"`
	Lookups         int       `json:"lookups"`
	Hits            []Hit     `json:"-"`
	MatchedIDs      []string  `json:"matchedIds"`
	Failures        []Failure `json:"failures"`
	AmbiguousTitles int       `json:"ambiguousTitles"`
}

func Discover(ctx context.Context, reader Reader, source Source, x *Index, language, configured string, queries, maxMatches int, delay time.Duration) (Discovery, error) {
	d := Discovery{MatchedIDs: []string{}, Failures: []Failure{}}
	if queries < 1 || queries > 64 || maxMatches < 1 || maxMatches > 20 {
		return d, fmt.Errorf("invalid discovery budget")
	}
	seen := map[string]bool{}
	consecutiveNetwork := 0
	for _, seed := range x.querySeeds(source.Kind, language, configured, queries, source.ID) {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		if d.Queries > 0 && delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return d, ctx.Err()
			case <-timer.C:
			}
		}
		d.Queries++
		d.QueryTitles = append(d.QueryTitles, seed)
		page, err := reader.Search(ctx, source.URL, seed, 1, "")
		if err != nil {
			f := failure("search", err)
			d.Failures = append(d.Failures, f)
			if f.Code == "RATE_LIMITED" {
				break
			}
			if f.Code == "TIMEOUT" || f.Code == "UNAVAILABLE" {
				consecutiveNetwork++
			} else {
				consecutiveNetwork = 0
			}
			if consecutiveNetwork >= 2 {
				break
			}
			continue
		}
		consecutiveNetwork = 0
		if page == nil {
			continue
		}
		for _, item := range page.Items {
			d.Items++
			d.Lookups++
			hits, ambiguous := x.lookup(source.Kind, item)
			if len(hits) > 1 {
				verified := []Hit{}
				for _, hit := range hits {
					if hit.Identity.Status == "verified" {
						verified = append(verified, hit)
					}
				}
				if len(verified) == 1 {
					hits = verified
				} else {
					ambiguous = true
				}
			}
			if ambiguous {
				d.AmbiguousTitles++
				continue
			}
			for _, hit := range hits {
				if seen[hit.Work.ID] {
					continue
				}
				seen[hit.Work.ID] = true
				d.Hits = append(d.Hits, hit)
				d.MatchedIDs = append(d.MatchedIDs, hit.Work.ID)
				if len(d.Hits) >= maxMatches {
					return d, nil
				}
			}
		}
	}
	return d, ctx.Err()
}

// EvaluateHits reuses discoveries; no second search is needed for each hit.
// The full index hash stays attached although only a few works are deep-read.
func EvaluateHits(ctx context.Context, reader Reader, source Source, x *Index, d Discovery, opts Options, probe ImageProbe) (*Report, error) {
	if len(d.Hits) == 0 {
		return nil, fmt.Errorf("no identity-matched works")
	}
	c := Corpus{Version: Version, UpdatedAt: x.corpus.UpdatedAt}
	cached := &discoveredReader{Reader: reader, items: map[string]*provider.Item{}}
	for _, hit := range d.Hits {
		c.Works = append(c.Works, hit.Work)
		cached.items[hit.Work.Title] = hit.Item
	}
	opts.MaxQueries = len(c.Works)
	opts.MaxMatches = len(c.Works)
	r, err := Evaluate(ctx, cached, source, c, opts, probe)
	if err != nil {
		return nil, err
	}
	r.CorpusHash = x.hash
	return r, nil
}

type discoveredReader struct {
	Reader
	items map[string]*provider.Item
}

func (r *discoveredReader) Search(ctx context.Context, base, q string, page int, cursor string) (*provider.ItemPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if item, ok := r.items[q]; ok {
		return &provider.ItemPage{Items: []*provider.Item{item}}, nil
	}
	return r.Reader.Search(ctx, base, q, page, cursor)
}
