package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cczjVideo/app/library"
)

func TestResumeRecoversOnlyCompleteKnownRecordsAndRejectsChangedInputs(t *testing.T) {
	h := header{Type: "header", RunHash: "hash", Sources: 1}
	first, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(entry{Type: "result", Result: library.BatchResult{Source: &library.BatchSource{ID: "one"}, State: "adapter_required"}})
	if err != nil {
		t.Fatal(err)
	}
	good := append(append(first, '\n'), append(raw, '\n')...)
	data := append(append([]byte{}, good...), []byte(`{"type":"resul`)...)
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, rows, n, err := readJournal(path, "hash", map[string]bool{"one": true})
	if err != nil || len(rows) != 1 || n != int64(len(good)) {
		t.Fatal(rows, n, err)
	}
	if _, _, _, err = readJournal(path, "other-hash", map[string]bool{"one": true}); err == nil {
		t.Fatal("resumed a different corpus/source configuration")
	}
	if _, _, _, err = readJournal(path, "hash", map[string]bool{"foreign": true}); err == nil {
		t.Fatal("accepted a foreign source")
	}
}

func TestManifestAcceptsPowerShellDigestCaseAndRejectsChangedSourceBytes(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`[{"bookSourceName":"fixture","bookSourceUrl":"https://example.org"}]`)
	sourcePath := filepath.Join(dir, "new-sources.json")
	if err := os.WriteFile(sourcePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	raw, err := json.Marshal(manifest{Catalogs: []catalogInput{{Kind: "novel", Path: sourcePath, SHA256: strings.ToUpper(hex.EncodeToString(sum[:]))}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadSources(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(sourcePath, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadSources(path); err == nil {
		t.Fatal("changed input bypassed digest validation")
	}
}

func TestCompletedResumeRetainsPriorNetworkEvidence(t *testing.T) {
	prior := library.BatchNetworkStats{Requests: 25, Reused: 70, PeakHTTP: 5, DeepStarted: 2, HomepageLookups: 400}
	if got := cumulativeEndpoints(prior, library.BatchNetworkStats{}); got != prior {
		t.Fatal("empty resume erased earlier network evidence", got)
	}
	got := cumulativeEndpoints(prior, library.BatchNetworkStats{Requests: 3, Reused: 2, PeakHTTP: 1, DeepStarted: 1, HomepageLookups: 10})
	if got.Requests != 28 || got.Reused != 72 || got.PeakHTTP != 5 || got.DeepStarted != 3 || got.HomepageLookups != 410 {
		t.Fatal(got)
	}
}
