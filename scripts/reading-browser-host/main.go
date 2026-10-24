// Local test bridge: isolated SQLite + production reading services. The browser
// replaces only Wails RPC transport; this is not a native desktop/Android host.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/library"
	"cczjVideo/app/model"
	"cczjVideo/app/proxy"
	"cczjVideo/app/readingstate"
)

func main() {
	port := flag.Int("port", 9296, "loopback test port")
	relativeImages := flag.Bool("relative-images", false, "use production-relative image URLs behind a browser route")
	managed := flag.Bool("managed-sources", false, "enable the builtin reading directory and staged routing")
	flag.Parse()
	root, err := os.MkdirTemp("", "cczj-reading-browser-")
	must(err)
	defer os.RemoveAll(root)
	must(applog.Init(filepath.Join(root, "logs")))
	defer applog.Default().Close()
	must(db.InitDB(filepath.Join(root, "data")))
	defer db.Close()
	owner, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	service := library.NewServiceWithRuntime(func() string { return root }, owner, nil)
	if *managed {
		service.EnableBuiltinRouting()
	}
	defer service.Close(context.Background())
	images := proxy.NewImageProxyWithRuntime(func() string { return root }, owner, nil)
	images.ConfigureReadingRuntime(service.WrapReadingTransport, service.BeginReadingForeground)
	defer images.Close(context.Background())
	images.SetHeaderResolver(library.SourcesHeaders)
	origin := fmt.Sprintf("http://127.0.0.1:%d", *port)
	service.SetImageURLBuilder(func(upstream, referer, key string) string {
		if *relativeImages {
			return images.URL(upstream, referer, key)
		}
		return origin + images.URL(upstream, referer, key)
	})
	state := db.NewReadingStateStore(db.DB())
	mux := http.NewServeMux()
	mux.Handle("/__cczj/img", images)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ready") })
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var call struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&call); err != nil {
			http.Error(w, "invalid RPC", 400)
			return
		}
		str := func(index int) string {
			var value string
			if index < len(call.Args) {
				_ = json.Unmarshal(call.Args[index], &value)
			}
			return value
		}
		num := func(index int) int {
			var value int
			if index < len(call.Args) {
				_ = json.Unmarshal(call.Args[index], &value)
			}
			return value
		}
		var result any
		var err error
		switch call.Method {
		case "NovelSourceDirectory":
			result, err = service.SourceDirectory(r.Context(), model.SourceKindNovel)
		case "ComicSourceDirectory":
			result, err = service.SourceDirectory(r.Context(), model.SourceKindComic)
		case "NovelSourceAlternatives":
			result, err = service.SourceAlternatives(r.Context(), model.SourceKindNovel, num(0), str(1))
		case "ComicSourceAlternatives":
			result, err = service.SourceAlternatives(r.Context(), model.SourceKindComic, num(0), str(1))
		case "NovelChapterMatch":
			result, err = service.MatchChapter(r.Context(), model.SourceKindNovel, num(0), num(1), num(2))
		case "ComicEpisodeMatch":
			result, err = service.MatchChapter(r.Context(), model.SourceKindComic, num(0), num(1), num(2))
		case "ComicBrowseChannels":
			result = service.ComicBrowseChannels()
		case "ComicBrowsePage":
			var refresh bool
			if len(call.Args) > 1 {
				_ = json.Unmarshal(call.Args[1], &refresh)
			}
			result, err = service.BrowseComics(r.Context(), str(0), refresh)
		case "NovelBrowseChannels":
			result = service.NovelBrowseChannels()
		case "NovelBrowsePage":
			var refresh bool
			if len(call.Args) > 2 {
				_ = json.Unmarshal(call.Args[2], &refresh)
			}
			result, err = service.BrowseNovels(r.Context(), str(0), str(1), refresh)
		case "LibraryPreviewSources":
			result, err = service.PreviewSources(r.Context(), model.SourceKind(str(0)), str(1), str(2))
		case "LibraryCheckSourceCandidate":
			result, err = service.CheckSourceCandidate(r.Context(), str(0), str(1), str(2))
		case "LibraryImportSources":
			var ids []string
			err = json.Unmarshal(call.Args[1], &ids)
			if err == nil {
				result, err = service.ImportSourceCandidates(r.Context(), str(0), ids)
			}
		case "LibrarySources":
			result, err = service.ListSources(model.SourceKind(str(0)))
		case "LibraryTestSource":
			result, err = service.TestSource(r.Context(), str(0))
		case "LibrarySetSourceEnabled":
			var enabled bool
			_ = json.Unmarshal(call.Args[1], &enabled)
			err = service.SetSourceEnabled(str(0), enabled)
		case "LibraryDeleteSource":
			err = service.DeleteSource(str(0))
		case "LibrarySearchPage", "LibrarySearchStream":
			result, err = service.SearchPage(r.Context(), model.SourceKind(str(0)), str(1), str(2))
		case "LibrarySearch":
			result, err = service.Search(r.Context(), model.SourceKind(str(0)), str(1), num(2), nil)
		case "LibraryItem":
			var refresh bool
			_ = json.Unmarshal(call.Args[1], &refresh)
			result, err = service.Detail(r.Context(), num(0), refresh)
		case "LibraryRefreshToc":
			result, err = service.RefreshToc(r.Context(), num(0))
		case "LibraryComicContext", "LibraryRefreshComicContext":
			var location library.ReadingLocation
			err = json.Unmarshal(call.Args[0], &location)
			if err == nil {
				if call.Method == "LibraryComicContext" {
					result, err = service.ComicContext(r.Context(), location)
				} else {
					result, err = service.RefreshComicContext(r.Context(), location)
				}
			}
		case "ComicTrending":
			result, err = service.TrendingComics(r.Context(), str(0), false)
		case "LibraryNovelContext", "LibraryRefreshNovelContext":
			var location library.NovelReadingLocation
			err = json.Unmarshal(call.Args[0], &location)
			if err == nil {
				if call.Method == "LibraryRefreshNovelContext" {
					result, err = service.RefreshNovelContext(r.Context(), location)
				} else {
					result, err = service.NovelContext(r.Context(), location)
				}
			}
		case "LibraryRefreshReadingToc":
			result, err = service.RefreshReadingToc(r.Context(), num(0), num(1))
		case "LibraryChapterText":
			result, err = service.ChapterText(r.Context(), num(0))
		case "LibraryChapterPages":
			result, err = service.ChapterPages(r.Context(), num(0))
		case "LibraryRefreshChapterPages":
			result, err = service.RefreshChapterPages(r.Context(), num(0))
		case "LibraryCatalogPage":
			var query db.LibraryPageQuery
			err = json.Unmarshal(call.Args[0], &query)
			if err == nil {
				result, err = service.CatalogPage(r.Context(), query)
			}
		case "LibraryResolveRefs":
			var refs []db.LibraryRef
			err = json.Unmarshal(call.Args[0], &refs)
			if err == nil {
				result, err = service.ResolveRefs(r.Context(), refs)
			}
		case "ReadingStateLoad":
			var records []readingstate.Record
			err = json.Unmarshal([]byte(str(1)), &records)
			if err == nil {
				result, err = state.Load(r.Context(), str(0), records)
			}
		case "ReadingStateApply":
			var changes []readingstate.Change
			err = json.Unmarshal([]byte(str(1)), &changes)
			if err == nil {
				err = state.Apply(r.Context(), str(0), changes)
			}
		default:
			result = ""
		}
		if err != nil {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(apperror.Public(err))
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-owner.Done(); _ = server.Shutdown(context.Background()) }()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		must(err)
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
