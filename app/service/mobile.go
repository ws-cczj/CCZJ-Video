package service

import (
	"encoding/json"
	"net/http"
)

// serveMobileMediaEndpoint provides mobile hosts a small endpoint record;
// video bytes themselves are streamed over its private loopback connection.
func (a *App) serveMobileMediaEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || a.mobileMedia == nil {
		http.NotFound(w, r)
		return
	}
	endpoint, err := a.mobileMedia.Endpoint()
	if err != nil {
		http.Error(w, "mobile media unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]string{"endpoint": endpoint}); err != nil {
		return
	}
}
