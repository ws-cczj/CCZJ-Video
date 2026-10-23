// source-quality evaluates reading adapters against a versioned work corpus.
// It defaults to an isolated database, never importing sources or read progress.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
)

type runReport struct {
	Version    string            `json:"version"`
	CorpusDate string            `json:"corpusDate"`
	StartedAt  string            `json:"startedAt"`
	Completed  bool              `json:"completed"`
	Results    []*quality.Report `json:"results"`
	Skipped    []string          `json:"skipped"`
	Failures   []runFailure      `json:"failures"`
}
type runFailure struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	kind := flag.String("kind", "novel", "novel or comic")
	sources := flag.String("sources", "", "comma-separated source-name filters")
	all := flag.Bool("all", false, "evaluate every compatible candidate (can take a long time)")
	input := flag.String("input", "community", "community, source JSON file, or catalog URL")
	corpusPath := flag.String("corpus", "", "custom benchmark JSON; defaults to embedded corpus")
	workIDs := flag.String("works", "", "optional comma-separated benchmark work IDs")
	queries := flag.Int("queries", 24, "maximum diverse work queries per source")
	matches := flag.Int("matches", 5, "maximum matched works to read per source")
	delay := flag.Duration("delay", 350*time.Millisecond, "minimum interval between operations")
	timeout := flag.Duration("timeout", 5*time.Minute, "per-source timeout")
	out := flag.String("out", "reports/source-quality/latest.json", "JSON evidence/ranking output")
	dataDir := flag.String("data-dir", "", "explicit database directory; omitted uses a disposable isolated DB")
	persist := flag.Bool("persist", false, "save benchmark summaries to --data-dir for the app's quality sorting")
	routed := flag.Bool("search-routing", false, "verify actual builtin multi-source searches in an isolated database")
	flag.Parse()
	if *kind != "novel" && *kind != "comic" {
		return fmt.Errorf("--kind must be novel or comic")
	}
	if !*routed && strings.TrimSpace(*sources) == "" && !*all {
		return fmt.Errorf("specify --sources or explicitly opt into --all")
	}
	if *persist && *dataDir == "" {
		return fmt.Errorf("--persist requires an explicit --data-dir")
	}
	if *routed && (*persist || *dataDir != "") {
		return fmt.Errorf("--search-routing requires a disposable database; omit --persist and --data-dir")
	}
	if *queries < 1 || *queries > 200 || *matches < 1 || *matches > 20 || *delay < 0 || *timeout <= 0 {
		return fmt.Errorf("invalid benchmark limits")
	}
	corpus, err := quality.DefaultCorpus()
	if err != nil {
		return err
	}
	if *corpusPath != "" {
		raw, e := os.ReadFile(*corpusPath)
		if e != nil {
			return e
		}
		corpus, err = quality.ParseCorpus(raw)
		if err != nil {
			return err
		}
	}
	if *workIDs != "" {
		selected := map[string]bool{}
		for _, id := range strings.Split(*workIDs, ",") {
			selected[strings.TrimSpace(id)] = true
		}
		works := []quality.Work{}
		for _, w := range corpus.Works {
			if selected[w.ID] {
				works = append(works, w)
				delete(selected, w.ID)
			}
		}
		if len(selected) > 0 || len(works) == 0 {
			return fmt.Errorf("unknown benchmark work ID")
		}
		corpus.Works = works
	}
	foundKind := false
	for _, w := range corpus.Works {
		foundKind = foundKind || w.Kind == *kind
	}
	if !foundKind {
		return fmt.Errorf("no benchmark works for this kind")
	}
	root := *dataDir
	if root == "" {
		root, err = os.MkdirTemp("", "cczj-quality-")
		if err != nil {
			return err
		}
		// The resolved target is the exact directory created by MkdirTemp, never
		// a supplied app path. Closing SQLite precedes this deferred cleanup.
		defer func() {
			if err := os.RemoveAll(root); err != nil {
				fmt.Fprintln(os.Stderr, "remove temporary benchmark database:", err)
			}
		}()
	}
	if err = db.InitDB(root); err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	svc := library.NewServiceWithRuntime(func() string { return root }, ctx, nil)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = svc.Close(closeCtx)
	}()
	if *routed {
		svc.EnableBuiltinRouting()
		return verifyRoutedSearches(ctx, svc, corpus, *kind, *queries, *matches, *timeout, *out)
	}
	catalogInput := *input
	if raw, e := os.ReadFile(catalogInput); e == nil {
		catalogInput = string(raw)
	}
	preview, err := svc.PreviewSources(ctx, model.SourceKind(*kind), catalogInput, "")
	if err != nil {
		return err
	}
	run := runReport{Version: quality.Version, CorpusDate: corpus.UpdatedAt, StartedAt: time.Now().Format(time.RFC3339), Results: []*quality.Report{}, Skipped: []string{}, Failures: []runFailure{}}
	filters := strings.Split(*sources, ",")
	selected := 0
	for _, candidate := range preview.Candidates {
		include := *all
		for _, filter := range filters {
			filter = strings.TrimSpace(filter)
			if filter != "" && (strings.Contains(strings.ToLower(candidate.Name), strings.ToLower(filter)) || candidate.ID == filter) {
				include = true
			}
		}
		if !include {
			continue
		}
		selected++
		if !candidate.Compatible {
			run.Skipped = append(run.Skipped, candidate.Name+"："+candidate.Reason)
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A source run may outlive a preview's UI session. Reload only the same
		// input, retaining the stable candidate identity and validated rules.
		if time.Now().Unix()+int64(timeout.Seconds()) >= preview.ExpiresAt {
			preview, err = svc.PreviewSources(ctx, model.SourceKind(*kind), catalogInput, "")
			if err != nil {
				return err
			}
		}
		fmt.Printf("CHECK %s\n", candidate.Name)
		call, cancel := context.WithTimeout(ctx, *timeout)
		r, e := svc.BenchmarkCandidate(call, preview.ID, candidate.ID, corpus, quality.Options{MaxQueries: *queries, MaxMatches: *matches, Delay: *delay}, *persist)
		cancel()
		if e != nil {
			run.Failures = append(run.Failures, runFailure{candidate.Name, e.Error()})
			fmt.Printf("UNFINISHED %s: %v\n", candidate.Name, e)
		} else {
			run.Results = append(run.Results, r)
			fmt.Printf("RESULT %s: %s, score=%d, confidence=%s, matched=%d, readable=%d/%d\n", candidate.Name, r.Summary.Status, r.Summary.Score, r.Summary.Confidence, r.Summary.Matched, r.Summary.Readable, r.Summary.Attempts)
		}
		quality.Rank(run.Results)
		if err = writeReport(*out, run); err != nil {
			return err
		}
	}
	if selected == 0 {
		return fmt.Errorf("no source matched the selection")
	}
	run.Completed = ctx.Err() == nil
	if err = writeReport(*out, run); err != nil {
		return err
	}
	fmt.Printf("REPORT %s (completed=%t; unfinished=%d; unsupported=%d)\n", *out, run.Completed, len(run.Failures), len(run.Skipped))
	return ctx.Err()
}
func writeReport(path string, r any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".quality-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() {
		if err := os.Remove(temp); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "remove temporary benchmark report:", err)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
