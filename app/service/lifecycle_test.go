package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/outbound"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func startupFailureApp(t *testing.T) (*App, string) {
	t.Helper()
	db.Close()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	a := NewApp()
	t.Cleanup(func() {
		if err := a.rollbackStartup(); err != nil {
			t.Error(err)
		}
		netpolicy.Install(nil)
		outbound.Install(nil)
		db.Close()
		applog.Default().Close()
	})
	return a, filepath.Join(root, "CCZJ Video")
}

func TestStartupFailureRollsBackOwnedRuntime(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"network policy", netpolicy.SettingsKey, "{invalid-json"},
		{"outbound pool after policy install", outbound.SettingsKey, "{invalid-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, dir := startupFailureApp(t)
			if err := db.InitDB(dir); err != nil {
				t.Fatal(err)
			}
			if err := db.SetSetting(tc.key, tc.value); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if err := a.ServiceStartup(context.Background(), application.ServiceOptions{}); err == nil {
				t.Fatal("expected startup failure")
			}
			if db.DB() != nil {
				t.Fatal("failed startup retained the database")
			}
			if a.background.Context().Err() == nil {
				t.Fatal("failed startup did not cancel its owner")
			}
			if len(a.background.ActiveTasks()) != 0 {
				t.Fatal("failed startup retained owned tasks")
			}
		})
	}
}

func TestStartupDatabaseOpenFailureReturnsStorageError(t *testing.T) {
	a, dir := startupFailureApp(t)
	if err := os.MkdirAll(filepath.Join(dir, "cczj_video.db"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.ServiceStartup(context.Background(), application.ServiceOptions{}); !apperror.Is(err, apperror.Storage) {
		t.Fatalf("expected storage error, got %v", err)
	}
	if a.background.Context().Err() == nil {
		t.Fatal("database failure retained background owner")
	}
}

func TestStopOwnedWorkCancelsAllOwnersBeforeWaiting(t *testing.T) {
	a := &App{background: lifecycle.NewGroup(), modeTasks: lifecycle.NewGroup()}
	started := make(chan struct{})
	a.modeTasks.Go("wait for application cancellation", func(ctx context.Context) {
		close(started)
		<-a.background.Context().Done()
		<-ctx.Done()
	})
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.stopOwnedWork(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFailedResourceCloseStillClosesIndependentResources(t *testing.T) {
	a, _ := startupFailureApp(t)
	sentinel := errors.New("comments close failed")
	a.stopComments = func(context.Context) error { return sentinel }
	if err := a.releaseRuntime(); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if a.background.Context().Err() == nil {
		t.Fatal("close failure skipped owner cancellation")
	}
	// Reading groups must already reject late work even though a later closer
	// failed; retrying shutdown can finish once that independent failure clears.
	a.stopComments = nil
	if err := a.releaseRuntime(); err != nil {
		t.Fatal(err)
	}
}
