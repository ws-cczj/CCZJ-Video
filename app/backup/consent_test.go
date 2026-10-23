package backup

import (
	"encoding/json"
	"testing"

	"cczjVideo/app/db"
)

func TestBackupNeverTransfersScriptConsent(t *testing.T) {
	resetUserTables(t)
	for _, key := range []string{"plugin_script_trust", "plugin_permissions"} {
		if err := db.SetSetting(key, "local-consent"); err != nil {
			t.Fatal(err)
		}
	}
	s := NewService()
	payload, err := s.buildPayload(testDirs.data)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range payload.Settings {
		if setting.Key == "plugin_script_trust" || setting.Key == "plugin_permissions" {
			t.Fatal("export must not contain execution consent")
		}
	}
	// An older or manually edited backup must not grant consent either.
	payload.Settings = []db.BackupSetting{
		{Key: "plugin_script_trust", Value: "foreign-consent"},
		{Key: "plugin_permissions", Value: "foreign-permission"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportBytes("old.json", raw, "file"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"plugin_script_trust", "plugin_permissions"} {
		value, readErr := db.GetSetting(key)
		if readErr != nil || value != "local-consent" {
			t.Fatalf("%s was replaced by imported consent: %q, %v", key, value, readErr)
		}
	}
}
