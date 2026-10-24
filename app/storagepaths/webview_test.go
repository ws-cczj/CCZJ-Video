package storagepaths

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func legacyProfile(t *testing.T, root, name string, when time.Time) string {
	t.Helper()
	p := filepath.Join(root, name)
	preferences := filepath.Join(p, "EBWebView", "Default", "Preferences")
	if err := os.MkdirAll(filepath.Dir(preferences), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preferences, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(preferences, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileSurvivesRenameAndLaterLegacyLaunch(t *testing.T) {
	root := t.TempDir()
	old := legacyProfile(t, root, "cczjVideo-windows-amd64-2.4.0.exe", time.Now())
	legacyProfile(t, root, "CCZJ-Video-Diagnostics-Preview.exe", time.Now().Add(time.Hour))
	got, err := WindowsWebview(root, "arbitrary-new-name.exe")
	if err != nil || got != old {
		t.Fatalf("old release profile not recovered: %q %v", got, err)
	}
	legacyProfile(t, root, "cczjVideo-2.5.0.exe", time.Now().Add(2*time.Hour))
	got, err = WindowsWebview(root, "cczjVideo-2.5.0.exe")
	if err != nil || got != old {
		t.Fatalf("rename changed selected profile: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(old, "EBWebView", "Default", "Preferences")); err != nil {
		t.Fatal(err)
	}
}

func TestFreshProfileDoesNotDependOnExecutable(t *testing.T) {
	root := t.TempDir()
	first, err := WindowsWebview(root, "first.exe")
	if err != nil {
		t.Fatal(err)
	}
	second, err := WindowsWebview(root, "second.exe")
	if err != nil || second != first || first != filepath.Join(DesktopRoot(root), "WebView2") {
		t.Fatalf("%q %q %v", first, second, err)
	}
}

func TestMissingSelectedProfileDoesNotSilentlyCreateEmptyProfile(t *testing.T) {
	root := t.TempDir()
	p, err := WindowsWebview(root, "first.exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p, p+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := WindowsWebview(root, "second.exe"); err == nil {
		t.Fatal("missing selected data was ignored")
	}
}

func TestInvalidSelectionIsRejected(t *testing.T) {
	for _, data := range []string{`"../outside"`, `"C:\\outside"`, `"nested/profile.exe"`, `"."`, `incomplete`} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(DesktopRoot(root), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(DesktopRoot(root), "webview-profile.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := WindowsWebview(root, "first.exe"); err == nil {
				t.Fatal("invalid selection accepted")
			}
		})
	}
}
