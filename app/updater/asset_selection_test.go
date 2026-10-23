package updater

import "testing"

func TestPlatformAssetSelection(t *testing.T) {
	assets := []GitHubAsset{
		{Name: "checksums.txt"},
		{Name: "cczjVideo-windows-arm64-2.4.0.exe"},
		{Name: "cczjVideo-android-amd64-2.4.0.apk"},
		{Name: "cczjVideo-windows-amd64-2.4.0-portable.zip"},
		{Name: "cczjVideo-android-arm64-2.4.0.apk"},
		{Name: "cczjVideo-windows-amd64-2.4.0.exe"},
	}
	for _, tc := range []struct{ os, arch, want string }{
		{"windows", "amd64", "cczjVideo-windows-amd64-2.4.0.exe"},
		{"windows", "arm64", "cczjVideo-windows-arm64-2.4.0.exe"},
		{"android", "arm64", "cczjVideo-android-arm64-2.4.0.apk"},
		{"android", "amd64", "cczjVideo-android-amd64-2.4.0.apk"},
		{"android", "arm", ""}, {"linux", "amd64", ""},
	} {
		got, _, _ := findBestAssetForPlatform(assets, tc.os, tc.arch)
		if got != tc.want {
			t.Errorf("%s/%s got %q, want %q", tc.os, tc.arch, got, tc.want)
		}
	}
}

func TestFallbackMetadataRejectsAnotherReleaseOrRepository(t *testing.T) {
	name := "cczjVideo-windows-amd64-2.4.0.exe"
	for _, address := range []string{
		"https://github.com/other/repo/releases/download/v2.4.0/" + name,
		"https://github.com/ws-cczj/CCZJ-Video/releases/download/v2.3.2/" + name,
	} {
		info := buildUpdateInfoFromVersionInfo(&VersionInfo{Version: "2.4.0", Assets: []GitHubAsset{{Name: name, BrowserDownloadURL: address}}})
		if info.DownloadURL != "" {
			t.Fatal("mismatched fallback accepted", address)
		}
	}
}
