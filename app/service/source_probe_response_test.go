package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"cczjVideo/app/model"
)

func TestSourceProbeDistinguishesResponseFailures(t *testing.T) {
	for _, item := range []struct {
		name, body, kind string
		status           int
		ok               bool
	}{
		{"empty catalogue", `{"code":1,"list":[]}`, "", 200, true},
		{"malformed JSON", `{"code":1,"list":`, "decode", 200, false},
		{"unknown envelope", `{"code":1,"data":[]}`, "decode", 200, false},
		{"verification HTML", `<html>verify</html>`, "decode", 200, false},
		{"API refusal", `{"code":1002,"msg":"not supported","list":"refused"}`, "source", 200, false},
		{"HTTP refused", `<html>denied</html>`, "http", 403, false},
		{"server failure", `not JSON`, "http", 503, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(item.status)
				fmt.Fprint(w, item.body)
			}))
			defer server.Close()
			result := probeSource(context.Background(), &model.Source{SourceKey: "probe-kind", ApiUrl: server.URL, Enabled: 1})
			if result.OK != item.ok || result.StatusCode != item.status || result.FailureKind != item.kind {
				t.Fatalf("probe misclassified response: %+v", result)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := probeSource(ctx, &model.Source{SourceKey: "cancelled", ApiUrl: "https://example.test/api", Enabled: 1})
	if result.StatusCode != 0 || result.FailureKind != "cancelled" {
		t.Fatalf("cancelled request pretended to be a received response: %+v", result)
	}
}
