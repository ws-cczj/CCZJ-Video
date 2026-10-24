package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
)

func TestDownloadFallsBackAndOnlyPublishesVerifiedBytes(t *testing.T) {
	for _, failure := range []string{"truncated", "wrong checksum"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			payload := strings.Repeat("verified new release", 128)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bad" {
					if failure == "truncated" {
						w.Header().Set("Content-Length", "9000")
					}
					_, _ = fmt.Fprint(w, "incomplete or corrupt bytes")
					return
				}
				_, _ = fmt.Fprint(w, payload)
			}))
			defer server.Close()
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
			var progressCalls int
			got, err := downloadFromSources(context.Background(), dir, "https://github.com/ws-cczj/CCZJ-Video/releases/download/v9.1.0/app.exe", digest,
				[]speedTestResult{{url: server.URL + "/bad", source: "bad"}, {url: server.URL + "/good", source: "good"}}, server.Client(),
				func(downloaded, total int64, speed float64) { progressCalls++ })
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyRecordedArtifact(got); err != nil {
				t.Fatal(err)
			}
			if progressCalls < 2 {
				t.Fatal("missing start/final progress")
			}
			if path, version := scanDownloadedArtifacts(dir, "app.exe", "9.1.0"); path != got || version != "9.1.0" {
				t.Fatalf("scan failed: %q %q", path, version)
			}
			if path, _ := scanDownloadedArtifacts(dir, filepath.Base(got), "9.1.0"); path != "" {
				t.Fatal("running update offered as an install package")
			}
			if path, _ := scanDownloadedArtifacts(dir, "app.exe", "9.2.0"); path != "" {
				t.Fatal("older package offered as latest")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("failed source left partial bytes: %v", entries)
			}
		})
	}
}

func TestDownloadChecksumFailureCleansPartialArtifact(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "bad payload") }))
	defer server.Close()
	_, err := downloadFromSources(context.Background(), dir, "https://github.com/ws-cczj/CCZJ-Video/releases/download/v9.1.0/app.exe", strings.Repeat("a", 64),
		[]speedTestResult{{url: server.URL, source: "bad"}}, server.Client(), nil)
	if apperror.CodeOf(err) != apperror.Corrupt {
		t.Fatalf("lost checksum failure: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unverified bytes remain: %v", entries)
	}
}

func TestScanIgnoresPartialAndUnknownLegacyArtifacts(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{stagingPrefix + ".exe", stagingPrefix + ".9.1.0.123.part"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(strings.Repeat("x", 2048)), 0644); err != nil {
			t.Fatal(err)
		}
		if err := recordDigest(path, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := scanDownloadedArtifacts(dir, "app.exe", "9.1.0"); got != "" {
		t.Fatalf("uninstallable package restored: %q", got)
	}
}
