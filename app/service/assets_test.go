package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetRoutesDoNotFallThroughToApplicationHTML(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, mobile := range []bool{false, true} {
		handler := AssetMiddleware(&App{}, mobile)(next)
		for _, tt := range []struct {
			path   string
			status int
		}{
			{"/__cczj/hls", 503}, {"/__cczj/img", 503}, {"/__cczj/android-media", 404},
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
