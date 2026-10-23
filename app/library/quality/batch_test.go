package quality

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/provider"
)

func TestDispatchThousandsKeepsFiveActiveAndBoundedGoroutines(t *testing.T) {
	tasks := make([]int, 10000)
	for i := range tasks {
		tasks[i] = i
	}
	base := runtime.NumGoroutine()
	var peakG atomic.Int64
	seen := map[int]bool{}
	stats, err := Dispatch(context.Background(), tasks, 5, func(ctx context.Context, i int) int {
		n := int64(runtime.NumGoroutine())
		for old := peakG.Load(); n > old; old = peakG.Load() {
			if peakG.CompareAndSwap(old, n) {
				break
			}
		}
		return i
	}, func(i, r int) error {
		if i != r || seen[i] {
			return fmt.Errorf("duplicate or foreign result")
		}
		seen[i] = true
		return nil
	})
	if err != nil || stats.Completed != 10000 || stats.Submitted != 10000 || stats.PeakActive > 5 || stats.QueueCapacity != 5 || peakG.Load() > int64(base+20) {
		t.Fatal(stats, peakG.Load(), base, err)
	}
	if _, err = Dispatch(context.Background(), tasks, 6, func(context.Context, int) int { return 0 }, func(int, int) error { return nil }); err == nil {
		t.Fatal("accepted more than five workers")
	}
}
func TestDispatchCancellationAndWriterFailureDrainWorkers(t *testing.T) {
	for _, name := range []string{"cancel", "writer"} {
		t.Run(name, func(t *testing.T) {
			tasks := make([]int, 1000)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("disk full")
			var active atomic.Int64
			if name == "cancel" {
				time.AfterFunc(10*time.Millisecond, cancel)
			}
			_, err := Dispatch(ctx, tasks, 5, func(ctx context.Context, i int) int {
				active.Add(1)
				defer active.Add(-1)
				if name == "cancel" {
					<-ctx.Done()
				}
				return i
			}, func(int, int) error {
				if name == "writer" {
					return sentinel
				}
				return nil
			})
			if active.Load() != 0 {
				t.Fatal("workers leaked")
			}
			if name == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if name == "writer" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
		})
	}
}

type immediateTransport struct{}

func (immediateTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("image body")), Request: r}, nil
}
func TestNetworkGateHoldsSlotsUntilResponseBodiesClose(t *testing.T) {
	g := NewNetworkGate()
	client := http.Client{Transport: g.Wrap(immediateTransport{})}
	responses := make(chan *http.Response, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get("https://source.example/page")
			if err != nil {
				t.Error(err)
				return
			}
			responses <- resp
		}()
	}
	wg.Wait()
	if g.Stats().PeakInFlight != 5 {
		t.Fatal(g.Stats())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://source.example/six", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Do(req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("sixth request entered while five bodies remained open", err)
	}
	close(responses)
	for r := range responses {
		if err = r.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if err = r.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if g.active.Load() != 0 || len(g.slots) != 0 {
		t.Fatal("response slots leaked")
	}
}
func TestIndexSeparatesModesAndUsesRealTranslatedQueryTitles(t *testing.T) {
	c := testCorpus()
	w := c.Works[0]
	w.ID = "comic"
	w.Kind = "comic"
	w.Layout = "page"
	w.Title = "ダンジョン飯"
	w.Aliases = []string{"迷宫饭", "Delicious in Dungeon"}
	w.Language = "ja"
	w.SearchTitles = map[string]string{"ja": "ダンジョン飯", "zh": "迷宫饭", "en": "Delicious in Dungeon"}
	c.Works = append(c.Works, w)
	x, err := Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	if seed := x.QuerySeeds("comic", "zh", "", 1); len(seed) != 1 || seed[0] != "迷宫饭" {
		t.Fatal(seed)
	}
	item := &provider.Item{Title: "迷宫饭", Author: "作者"}
	if got := x.Lookup("comic", item); len(got) != 1 || got[0].Work.ID != "comic" {
		t.Fatal(got)
	}
	if got := x.Lookup("novel", item); len(got) != 0 {
		t.Fatal("matched comic in novel bucket", got)
	}
}

func TestLocalizedMetadataTitlesAreIndexedWithoutDuplicatingAliases(t *testing.T) {
	c := testCorpus()
	c.Works[0].SearchTitles = map[string]string{"en": "A Real Localized Title"}
	x, err := Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	got := x.Lookup("novel", &provider.Item{Title: "A Real Localized Title", Author: "作者"})
	if len(got) != 1 || got[0].Work.ID != "0" || got[0].Identity.Status != "verified" {
		t.Fatal(got)
	}
}

func TestSourceSeedRotationSpreadsCoverageAndRemainsReproducible(t *testing.T) {
	x, err := Compile(performanceCorpus(1000))
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("source-%d", i)
		seeds := x.querySeeds("novel", "zh", "configured-title", 3, id)
		again := x.querySeeds("novel", "zh", "configured-title", 3, id)
		if len(seeds) != 3 || seeds[0] != "configured-title" || strings.Join(seeds, ",") != strings.Join(again, ",") {
			t.Fatal(seeds, again)
		}
		for _, title := range seeds[1:] {
			titles[title] = true
		}
	}
	if len(titles) < 100 {
		t.Fatal("source batch concentrated on the same few titles", len(titles))
	}
}

func TestApplicationAuditCorporaKeepModesAndOnlineSamplesSeparate(t *testing.T) {
	for _, kind := range []string{"novel", "comic"} {
		c, err := AuditCorpus(kind)
		if err != nil {
			t.Fatal(err)
		}
		minimum := 1600
		if kind == "novel" {
			minimum = 5200
		}
		if len(c.Works) < minimum {
			t.Fatal("online corpus missing", kind, len(c.Works))
		}
		for _, w := range c.Works {
			if w.Kind != kind {
				t.Fatal("cross-mode audit corpus", w)
			}
		}
	}
}

func TestDiscoveryDoesNotCountAmbiguousHomonymsAsTwoWorks(t *testing.T) {
	c := testCorpus()
	c.Works[1].Title = c.Works[0].Title
	c.Works[1].Authors = []string{"another author"}
	x, err := Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	r := &homonymReader{item: &provider.Item{Title: c.Works[0].Title}}
	d, err := Discover(context.Background(), r, Source{Kind: "novel"}, x, "zh", c.Works[0].Title, 1, 2, 0)
	if err != nil || len(d.MatchedIDs) != 0 || d.AmbiguousTitles != 1 {
		t.Fatal(d, err)
	}
	r.item.Author = "another author"
	d, err = Discover(context.Background(), r, Source{Kind: "novel"}, x, "zh", c.Works[0].Title, 1, 2, 0)
	if err != nil || len(d.MatchedIDs) != 1 || d.MatchedIDs[0] != "1" {
		t.Fatal(d, err)
	}
}

type homonymReader struct {
	fakeReader
	item *provider.Item
}

func (r *homonymReader) Search(context.Context, string, string, int, string) (*provider.ItemPage, error) {
	return &provider.ItemPage{Items: []*provider.Item{r.item}}, nil
}

type discoveryReader struct{ fakeReader }

func (r *discoveryReader) Search(ctx context.Context, base, q string, p int, n string) (*provider.ItemPage, error) {
	r.calls++
	return &provider.ItemPage{Items: []*provider.Item{{Title: "作品0", Author: "作者", URL: "https://mirror.example/作品0"}, {Title: "作品1", Author: "作者", URL: "https://mirror.example/作品1"}}}, nil
}
func TestDiscoveryIndexesAllReturnedTitlesAndDeepReadReusesSearch(t *testing.T) {
	x, err := Compile(testCorpus())
	if err != nil {
		t.Fatal(err)
	}
	r := &discoveryReader{}
	d, err := Discover(context.Background(), r, Source{Kind: "novel"}, x, "zh", "not-the-target-title", 2, 2, 0)
	if err != nil || d.Queries != 1 || d.Lookups != 2 || len(d.Hits) != 2 {
		t.Fatal(d, err)
	}
	report, err := EvaluateHits(context.Background(), r, Source{Kind: "novel"}, x, d, Options{}, nil)
	if err != nil || report.CorpusHash != x.Hash() || r.calls != 1 || report.Summary.Readable != 6 {
		t.Fatal(report, r.calls, err)
	}
}
func performanceCorpus(n int) Corpus {
	c := testCorpus()
	template := c.Works[0]
	c.Works = make([]Work, n)
	for i := range c.Works {
		w := template
		w.ID = fmt.Sprint(i)
		w.Title = fmt.Sprintf("benchmark-book-%08d", i)
		c.Works[i] = w
	}
	return c
}
func BenchmarkIndexLookup(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			x, err := Compile(performanceCorpus(n))
			if err != nil {
				b.Fatal(err)
			}
			item := &provider.Item{Title: fmt.Sprintf("benchmark-book-%08d", n-1), Author: "作者"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(x.Lookup("novel", item)) != 1 {
					b.Fatal("missing")
				}
			}
		})
	}
}
func BenchmarkLinearTitleLookup(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			c := performanceCorpus(n)
			target := Normalize(c.Works[n-1].Title)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, w := range c.Works {
					if Normalize(w.Title) == target {
						break
					}
				}
			}
		})
	}
}
