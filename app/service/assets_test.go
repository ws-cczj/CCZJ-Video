package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cczjVideo/app/proxy"
)

func TestMobileMediaDiscoveryKeepsAndroidAliasAndRejectsDesktop(t *testing.T) {
	bridge := proxy.NewMediaBridge(context.Background(), http.NotFoundHandler())
	defer bridge.Close()
	app := &App{mobileMedia: bridge}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("media discovery fell through to HTML") })
	var endpoint string
	for _, path := range []string{"/__cczj/mobile-media", "/__cczj/android-media"} {
		response := httptest.NewRecorder()
		AssetMiddleware(app, true)(next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("media discovery failed: %d", response.Code)
		}
		var data map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data["endpoint"] == "" || (endpoint != "" && endpoint != data["endpoint"]) {
			t.Fatal("aliases do not share the application media bridge")
		}
		endpoint = data["endpoint"]
		response = httptest.NewRecorder()
		AssetMiddleware(app, false)(next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatal("desktop exposed mobile media discovery")
		}
	}
}

func TestAssetRoutesDoNotFallThroughToApplicationHTML(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, mobile := range []bool{false, true} {
		handler := AssetMiddleware(&App{}, mobile)(next)
		for _, tt := range []struct {
			path   string
			status int
		}{
			{"/__cczj/hls", 503}, {"/__cczj/img", 503}, {"/__cczj/android-media", 404},
			{"/__cczj/mobile-media", 404},
			{"/__cczj/missing", 404}, {"/", 204}, {"/@vite/client", 204},
		} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != tt.status {
				t.Errorf("mobile=%t path=%s: %d", mobile, tt.path, response.Code)
			}
			if tt.path == "/__cczj/img" {
				id := response.Header().Get("X-CCZJ-Request-ID")
				if id == "" || response.Header().Get("X-CCZJ-Pipeline") != "http.image" {
					t.Fatal(response.Header())
				}
				another := httptest.NewRecorder()
				handler.ServeHTTP(another, httptest.NewRequest(http.MethodGet, tt.path, nil))
				if another.Header().Get("X-CCZJ-Request-ID") == id {
					t.Fatal("different HTTP requests shared an invocation identity")
				}
			}
		}
	}
}
