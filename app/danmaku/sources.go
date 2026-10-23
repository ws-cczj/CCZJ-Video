package danmaku

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const SourceSettingsKey = "danmaku_sources_v1"

type SourceSettings struct {
	DefaultProvider string   `json:"defaultProvider"`
	Disabled        []string `json:"disabled"`
}

func builtinProvider(name string) bool {
	return name == "bilibili" || name == "qq" || name == "iqiyi"
}

func (s SourceSettings) Enabled(name string) bool {
	if !builtinProvider(name) {
		return false
	}
	for _, disabled := range s.Disabled {
		if disabled == name {
			return false
		}
	}
	return true
}

// Defaults preserve existing bindings. Disabling a source never removes them.
func DecodeSourceSettings(raw string) (SourceSettings, error) {
	s := SourceSettings{DefaultProvider: "bilibili", Disabled: []string{}}
	if raw != "" {
		var fields map[string]json.RawMessage
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
			return SourceSettings{}, fmt.Errorf("invalid danmaku source settings")
		}
		if value, ok := fields["defaultProvider"]; ok {
			var name *string
			if json.Unmarshal(value, &name) != nil || name == nil {
				return SourceSettings{}, fmt.Errorf("invalid default danmaku provider")
			}
			s.DefaultProvider = *name
		}
		if value, ok := fields["disabled"]; ok {
			if json.Unmarshal(value, &s.Disabled) != nil || s.Disabled == nil {
				return SourceSettings{}, fmt.Errorf("invalid disabled danmaku providers")
			}
		}
	}

	if s.DefaultProvider != "" && !builtinProvider(s.DefaultProvider) {
		return SourceSettings{}, fmt.Errorf("unknown default danmaku provider")
	}
	seen := map[string]bool{}
	disabled := []string{}
	for _, name := range s.Disabled {
		if !builtinProvider(name) {
			return SourceSettings{}, fmt.Errorf("unknown disabled danmaku provider")
		}
		if !seen[name] {
			disabled = append(disabled, name)
			seen[name] = true
		}
	}
	s.Disabled = disabled
	if !s.Enabled(s.DefaultProvider) {
		s.DefaultProvider = ""
		for _, name := range []string{"bilibili", "qq", "iqiyi"} {
			if s.Enabled(name) {
				s.DefaultProvider = name
				break
			}
		}
	}
	return s, nil
}

func (s *Store) SourceSettings(ctx context.Context) (SourceSettings, error) {
	database, err := s.db()
	if err != nil {
		return SourceSettings{}, err
	}
	var raw string
	err = database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, SourceSettingsKey).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SourceSettings{}, err
	}
	return DecodeSourceSettings(raw)
}
