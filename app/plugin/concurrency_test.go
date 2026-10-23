package plugin

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDisablePreservesBothStates(t *testing.T) {
	service, root, _ := testService(t)
	for _, id := range []string{"first", "second"} {
		dir := writePack(t, root, id, fmt.Sprintf(`{"manifest_version":1,"id":%q,"version":"1.0.0","name":{"en":"Test"},"kind":"script","script":{"entry":"main.js"}}`, id))
		writeFile(t, filepath.Join(dir, "main.js"), "export default () => {}")
	}
	mustScan(t, service)
	var mu sync.Mutex
	states := map[string]bool{}
	loads := 0
	firstLoaded, secondLoaded, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	service.loadStates = func() (map[string]bool, error) {
		mu.Lock()
		copy := make(map[string]bool)
		for k, v := range states {
			copy[k] = v
		}
		loads++
		n := loads
		mu.Unlock()
		if n == 1 {
			close(firstLoaded)
			<-release
		}
		if n == 2 {
			close(secondLoaded)
		}
		return copy, nil
	}
	service.saveStates = func(next map[string]bool) error { mu.Lock(); defer mu.Unlock(); states = next; return nil }
	done := make(chan error, 2)
	go func() { done <- service.SetEnabled("first", false) }()
	<-firstLoaded
	go func() { done <- service.SetEnabled("second", false) }()
	select {
	case <-secondLoaded:
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) != 2 || states["first"] || states["second"] {
		t.Fatalf("concurrent disables lost persisted state: %v", states)
	}
}

func TestStateReadFailureDoesNotOverwriteDisabledPacks(t *testing.T) {
	service, root, _ := testService(t)
	dir := writePack(t, root, "pack", scriptManifest("pack", `{"entry":"main.js"}`))
	writeFile(t, filepath.Join(dir, "main.js"), validScriptSource)
	mustScan(t, service)
	service.loadStates = func() (map[string]bool, error) { return nil, errors.New("database read failed") }
	saves := 0
	service.saveStates = func(map[string]bool) error { saves++; return nil }
	if err := service.SetEnabled("pack", false); err == nil {
		t.Fatal("state read failure must stop the state mutation")
	}
	if saves != 0 {
		t.Fatal("failed state read was replaced with an empty ledger")
	}
}
