// Isolated acceptance host: production HLS transport, temporary logs, no user DB.
package main

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/proxy"
	"cczjVideo/app/service"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	if len(os.Args) != 4 {
		panic("usage: weak-playback-proxy listen vite-origin log-directory")
	}
	if err := applog.Init(os.Args[3]); err != nil {
		panic(err)
	}
	defer applog.Default().Close()
	proxy.SetAllowPrivateTargets(true)
	hls := proxy.NewHLSService()
	defer hls.Close()
	app := service.NewApp()
	vite, err := url.Parse(os.Args[2])
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/__cczj/hls", hls)
	mux.HandleFunc("/audit-log", func(w http.ResponseWriter, r *http.Request) {
		var entry service.LogEntry
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&entry) != nil {
			http.Error(w, "invalid log", 400)
			return
		}
		ok, err := app.WriteLog(entry)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "error": err})
	})
	mux.Handle("/", httputil.NewSingleHostReverseProxy(vite))
	if err := http.ListenAndServe(os.Args[1], mux); err != nil {
		panic(err)
	}
}
