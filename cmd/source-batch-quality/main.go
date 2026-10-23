// source-batch-quality schedules whole source catalogs with a shared capacity of
// five and writes one append-only evidence record per source configuration.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/library"
	"cczjVideo/app/library/quality"
)

type manifest struct {
	Catalogs []catalogInput `json:"catalogs"`
}

const batchAlgorithm = "indexed-discovery.v2"

type catalogInput struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type header struct {
	Type          string `json:"type"`
	RunHash       string `json:"runHash"`
	CorpusHash    string `json:"corpusHash"`
	StartedAt     string `json:"startedAt"`
	Sources       int    `json:"sources"`
	ParentRunHash string `json:"parentRunHash,omitempty"`
	Algorithm     string `json:"algorithm,omitempty"`
}
type entry struct {
	Type   string              `json:"type"`
	Result library.BatchResult `json:"result"`
}
type snapshot struct {
	Header           header                    `json:"header"`
	Completed        bool                      `json:"completed"`
	CorpusWorks      int                       `json:"corpusWorks"`
	TotalSources     int                       `json:"totalSources"`
	Processed        int                       `json:"processed"`
	Resumed          int                       `json:"resumed"`
	CanRead          int                       `json:"canRead"`
	CanSearch        int                       `json:"canSearch"`
	SourcesByKind    map[string]int            `json:"sourcesByKind"`
	States           map[string]int            `json:"states"`
	EndpointStates   map[string]int            `json:"endpointStates"`
	DiscoveryQueries int                       `json:"discoveryQueries"`
	IndexLookups     int                       `json:"indexLookups"`
	MatchedWorks     int                       `json:"matchedWorks"`
	HomepagePairs    int                       `json:"homepagePairs"`
	Network          quality.GateStats         `json:"network"`
	RunNetwork       quality.GateStats         `json:"runNetwork"`
	Endpoints        library.BatchNetworkStats `json:"endpoints"`
	RunEndpoints     library.BatchNetworkStats `json:"runEndpoints"`
	Dispatch         quality.DispatchStats     `json:"dispatch"`
	Rankings         []*quality.Report         `json:"rankings"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (err error) {
	manifestPath := flag.String("manifest", "reports/source-quality/catalog-inputs/manifest.json", "downloaded catalog manifest")
	corpusPath := flag.String("corpus", "reports/source-quality/bulk-corpus.v1.json", "downloaded work benchmark")
	out := flag.String("out", "reports/source-quality/bulk-run-20261021", "evidence directory")
	workers := flag.Int("workers", 5, "global workers, maximum five")
	queries := flag.Int("queries", 3, "bounded discovery queries per searchable source")
	matches := flag.Int("matches", 2, "matched works to deep sample per source")
	maxDeep := flag.Int("max-deep", 30, "maximum sources admitted to deep sampling")
	probeTimeout := flag.Duration("probe-timeout", 1500*time.Millisecond, "short-pass endpoint deadline; timeout is unknown")
	operationTimeout := flag.Duration("operation-timeout", 3*time.Second, "per discovery request deadline")
	sourceTimeout := flag.Duration("source-timeout", 60*time.Second, "per-source job deadline")
	delay := flag.Duration("delay", 350*time.Millisecond, "minimum interval between a source's operations")
	resume := flag.Bool("resume", false, "resume identical inputs/options from the evidence journal")
	limit := flag.Int("limit", 0, "optional source limit; zero means all")
	inspectHome := flag.Bool("inspect-homepages", false, "match bounded homepage anchor titles against the corpus")
	homeOnly := flag.Bool("homepages-only", false, "inspect homepages without search/deep requests")
	recheckHTTP := flag.String("recheck-http", "", "select only 2xx source endpoints from an earlier evidence journal")
	readableOnly := flag.Bool("readable-only", false, "select all full-reading adapters for targeted deep regression")
	searchableOnly := flag.Bool("searchable-only", false, "select full adapters and validated static search-only adapters")
	flag.Parse()
	if *workers < 1 || *workers > quality.MaxChannels || *sourceTimeout <= 0 || *limit < 0 {
		return fmt.Errorf("invalid worker/source limits")
	}
	raw, err := readLimited(*corpusPath, 64<<20)
	if err != nil {
		return err
	}
	corpus, err := quality.ParseCorpus(raw)
	if err != nil {
		return err
	}
	index, err := quality.Compile(corpus)
	if err != nil {
		return err
	}
	options := library.BatchOptions{ProbeTimeout: *probeTimeout, OperationTimeout: *operationTimeout, Queries: *queries, Matches: *matches, Delay: *delay, MaxDeepSources: *maxDeep}
	options.InspectHomepage = *inspectHome || *homeOnly
	options.HomepagesOnly = *homeOnly
	sources, err := loadSources(*manifestPath)
	if err != nil {
		return err
	}
	parentHash := ""
	if *readableOnly {
		selected := sources[:0]
		for _, s := range sources {
			if s.CanRead {
				selected = append(selected, s)
			}
		}
		sources = selected
	}
	if *searchableOnly {
		selected := sources[:0]
		for _, s := range sources {
			if s.CanSearch {
				selected = append(selected, s)
			}
		}
		sources = selected
	}
	if *recheckHTTP != "" {
		allIDs := map[string]bool{}
		for _, s := range sources {
			allIDs[s.ID] = true
		}
		prior, rows, _, e := readJournal(*recheckHTTP, "", allIDs)
		if e != nil {
			return e
		}
		parentHash = prior.RunHash
		selected := sources[:0]
		for _, s := range sources {
			if r, ok := rows[s.ID]; ok && r.Probe.HTTPStatus >= 200 && r.Probe.HTTPStatus < 300 {
				selected = append(selected, s)
			}
		}
		sources = selected
	}
	if *limit > 0 && *limit < len(sources) {
		sources = sources[:*limit]
	}
	if len(sources) == 0 {
		return fmt.Errorf("no sources")
	}
	fingerprint, err := runHash(index.Hash(), sources, options, *sourceTimeout)
	if err != nil {
		return err
	}
	h := header{Type: "header", RunHash: fingerprint, CorpusHash: index.Hash(), StartedAt: time.Now().Format(time.RFC3339), Sources: len(sources)}
	h.ParentRunHash = parentHash
	h.Algorithm = batchAlgorithm
	if err = os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	journalPath := filepath.Join(*out, "sources.jsonl")
	known := map[string]bool{}
	for _, s := range sources {
		known[s.ID] = true
	}
	previous := map[string]library.BatchResult{}
	priorNetwork := quality.GateStats{}
	priorEndpoints := library.BatchNetworkStats{}
	if *resume {
		var good int64
		h, previous, good, err = readJournal(journalPath, fingerprint, known)
		if err != nil {
			return err
		}
		if err = os.Truncate(journalPath, good); err != nil {
			return err
		}
		if raw, e := os.ReadFile(filepath.Join(*out, "summary.json")); e == nil {
			var prior snapshot
			if e = json.Unmarshal(raw, &prior); e != nil {
				return e
			}
			if prior.Header.RunHash != fingerprint {
				return fmt.Errorf("resume summary hash mismatch")
			}
			priorNetwork = prior.Network
			priorEndpoints = prior.Endpoints
		}
	} else if info, e := os.Stat(journalPath); e == nil && info.Size() > 0 {
		return fmt.Errorf("journal exists; use --resume or a new --out directory")
	}
	engineOptions := options
	for _, r := range previous {
		if r.Quality != nil {
			engineOptions.MaxDeepSources = max(0, engineOptions.MaxDeepSources-1)
		}
	}
	engine, err := library.NewBatchEngine(index, engineOptions)
	if err != nil {
		return err
	}
	defer engine.Close()
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, journal.Close()) }()
	buffer := bufio.NewWriterSize(journal, 256<<10)
	encoder := json.NewEncoder(buffer)
	if !*resume {
		if err = encoder.Encode(h); err != nil {
			return err
		}
	}
	state := snapshot{Header: h, CorpusWorks: index.Size(), TotalSources: len(sources), States: map[string]int{}, EndpointStates: map[string]int{}, SourcesByKind: map[string]int{}, Rankings: []*quality.Report{}}
	for _, s := range sources {
		state.SourcesByKind[s.Kind]++
		if s.CanRead {
			state.CanRead++
		}
		if s.CanSearch {
			state.CanSearch++
		}
	}
	account := func(r library.BatchResult) {
		state.Processed++
		state.States[r.State]++
		state.EndpointStates[r.Probe.Status]++
		state.HomepagePairs += len(r.HomepageMatches)
		if r.Discovery != nil {
			state.DiscoveryQueries += r.Discovery.Queries
			state.IndexLookups += r.Discovery.Lookups
			state.MatchedWorks += len(r.Discovery.MatchedIDs)
		}
		if r.Quality != nil {
			state.Rankings = append(state.Rankings, r.Quality)
		}
	}
	pending := make([]*library.BatchSource, 0, len(sources))
	for _, s := range sources {
		if r, ok := previous[s.ID]; ok && r.State != "cancelled" && r.State != "discovery_unfinished" && r.State != "deep_unfinished" {
			account(r)
			state.Resumed++
		} else {
			pending = append(pending, s)
		}
	}
	fmt.Printf("START sources=%d pending=%d works=%d readAdapters=%d searchable=%d workers=%d queueCapacity=%d\n", len(sources), len(pending), index.Size(), state.CanRead, state.CanSearch, *workers, quality.MaxChannels)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	lastWrite := time.Now()
	writeSnapshot := func() error {
		if err := buffer.Flush(); err != nil {
			return err
		}
		state.RunNetwork = engine.NetworkStats()
		state.Network = state.RunNetwork
		state.Network.Requests += priorNetwork.Requests
		state.Network.PeakInFlight = max(state.Network.PeakInFlight, priorNetwork.PeakInFlight)
		state.RunEndpoints = engine.Stats()
		state.Endpoints = cumulativeEndpoints(priorEndpoints, state.RunEndpoints)
		quality.Rank(state.Rankings)
		return writeJSON(filepath.Join(*out, "summary.json"), state)
	}
	if err = writeSnapshot(); err != nil {
		return err
	}
	stats, runErr := quality.Dispatch(ctx, pending, *workers, func(owner context.Context, s *library.BatchSource) library.BatchResult {
		call, cancel := context.WithTimeout(owner, *sourceTimeout)
		defer cancel()
		return engine.Execute(call, s)
	}, func(s *library.BatchSource, r library.BatchResult) error {
		if err := encoder.Encode(entry{Type: "result", Result: r}); err != nil {
			return err
		}
		account(r)
		if state.Processed%100 == 0 || time.Since(lastWrite) > 5*time.Second {
			if err := writeSnapshot(); err != nil {
				return err
			}
			lastWrite = time.Now()
			fmt.Printf("PROGRESS %d/%d states=%v HTTPpeak=%d queries=%d indexedItems=%d\n", state.Processed, state.TotalSources, state.States, state.Network.PeakInFlight, state.DiscoveryQueries, state.IndexLookups)
		}
		return nil
	})
	state.Dispatch = stats
	state.Completed = runErr == nil && state.Processed == state.TotalSources
	if err = writeSnapshot(); err != nil {
		return err
	}
	if err = journal.Sync(); err != nil {
		return err
	}
	fmt.Printf("FINISH completed=%t processed=%d/%d peakHTTP=%d endpointRequests=%d reused=%d elapsed=%dms\n", state.Completed, state.Processed, state.TotalSources, state.Network.PeakInFlight, state.Endpoints.Requests, state.Endpoints.Reused, stats.Millis)
	return runErr
}

func cumulativeEndpoints(prior, current library.BatchNetworkStats) library.BatchNetworkStats {
	current.Requests += prior.Requests
	current.Reused += prior.Reused
	current.PeakHTTP = max(prior.PeakHTTP, current.PeakHTTP)
	current.DeepStarted += prior.DeepStarted
	current.HomepageLookups += prior.HomepageLookups
	return current
}

func loadSources(path string) ([]*library.BatchSource, error) {
	raw, err := readLimited(path, 4<<20)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if len(m.Catalogs) == 0 || len(m.Catalogs) > 32 {
		return nil, fmt.Errorf("invalid catalog count")
	}
	all := []*library.BatchSource{}
	seen := map[string]bool{}
	add := func(rows []*library.BatchSource) {
		for _, s := range rows {
			if !seen[s.ID] {
				seen[s.ID] = true
				all = append(all, s)
			}
		}
	}
	bundled, err := library.BundledBatchSources()
	if err != nil {
		return nil, err
	}
	add(bundled)
	for _, c := range m.Catalogs {
		if c.Kind != "novel" && c.Kind != "comic" {
			return nil, fmt.Errorf("invalid catalog kind")
		}
		p := c.Path
		if _, e := os.Stat(p); e != nil {
			p = filepath.Join(filepath.Dir(path), filepath.Base(c.Path))
		}
		data, e := readLimited(p, 32<<20)
		if e != nil {
			return nil, e
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), c.SHA256) {
			return nil, fmt.Errorf("catalog SHA256 mismatch: %s", c.Path)
		}
		rows, e := library.ParseBatchSources(data, c.Kind, c.URL)
		if e != nil {
			return nil, e
		}
		add(rows)
	}
	return library.InterleaveOrigins(all), nil
}
func runHash(corpus string, sources []*library.BatchSource, opts library.BatchOptions, timeout time.Duration) (string, error) {
	ids := make([]string, len(sources))
	for i, s := range sources {
		ids[i] = s.ID
	}
	sort.Strings(ids)
	raw, err := json.Marshal(struct {
		Algorithm string
		Corpus    string
		Sources   []string
		Options   library.BatchOptions
		Timeout   time.Duration
	}{batchAlgorithm, corpus, ids, opts, timeout})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func readLimited(path string, limit int64) (data []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds input bound: %s", path)
	}
	return data, nil
}
func writeJSON(path string, value any) (err error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".summary-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// A torn final line is discarded only for an explicit --resume of identical
// inputs. Completed records are never regenerated or silently re-scored.
func readJournal(path, expected string, known map[string]bool) (h header, rows map[string]library.BatchResult, good int64, err error) {
	rows = map[string]library.BatchResult{}
	f, err := os.Open(path)
	if err != nil {
		return h, rows, 0, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	r := bufio.NewReaderSize(f, 256<<10)
	first := true
	for {
		line, e := r.ReadBytes('\n')
		if len(line) > 16<<20 {
			return h, rows, good, fmt.Errorf("journal record exceeds bound")
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return h, rows, good, e
		}
		if first {
			if err = json.Unmarshal(line, &h); err != nil {
				return h, rows, good, err
			}
			if h.Type != "header" || expected != "" && h.RunHash != expected {
				return h, rows, good, fmt.Errorf("resume input/options hash mismatch")
			}
			first = false
		} else {
			var row entry
			if err = json.Unmarshal(line, &row); err != nil {
				return h, rows, good, err
			}
			if row.Type != "result" || row.Result.Source == nil || !known[row.Result.Source.ID] {
				return h, rows, good, fmt.Errorf("foreign or invalid journal source")
			}
			rows[row.Result.Source.ID] = row.Result
		}
		good += int64(len(line))
	}
	if first {
		return h, rows, good, fmt.Errorf("missing complete journal header")
	}
	return h, rows, good, nil
}
