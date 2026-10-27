package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/netpolicy"
)

func TestSourceProbesUseMiddlewareWithoutCoolingNormalCollectionReads(t *testing.T) {
	setupProbePipeline(t)
	manager := netpolicy.New(netpolicy.NewStore(func() *sql.DB { return db.DB().DB }), context.Background(), netpolicy.Settings{Frequency: "standard"})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	netpolicy.Install(manager)
	t.Cleanup(func() {
		netpolicy.Install(nil)
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"code":1,"list":[{"vod_id":"wanted","vod_name":"目标影片"}]}`)
	}))
	defer server.Close()
	source := &model.Source{SourceKey: "shared-probe-gate", ApiUrl: server.URL, Enabled: 1}
	for i := 0; i < 2; i++ {
		probe := probeSource(context.Background(), source)
		if !probe.OK {
			t.Fatalf("probe failed: %+v", probe)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("source probes bypassed middleware: %d", calls.Load())
	}
	strategy := collect.CreateStrategyFromSource(source)
	ctx := netpolicy.WithScope(context.Background(), source.SourceKey)
	for i := 0; i < 2; i++ {
		if _, err := collect.FetchWithStrategy(ctx, strategy, sourceProbeURL(source)); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("probe cooled normal collection reads: %d", calls.Load())
	}
}
