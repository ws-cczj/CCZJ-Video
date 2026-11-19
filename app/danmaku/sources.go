package danmaku

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	CCZJ "cczjVideo/app/cczj"
)

const SourceSettingsKey = CCZJ.SettingDanmakuSources

type SourceSettings struct {
	DefaultProvider string   `json:"defaultProvider"`
	Disabled        []string `json:"disabled"`
}

func (s SourceSettings) Enabled(name string) bool {
	return findProvider(name) != nil && !slices.Contains(s.Disabled, name)
}

func (s SourceSettings) orderedProviders() []string {
	order := make([]string, 0, len(builtinProviders))
	if s.Enabled(s.DefaultProvider) {
		order = append(order, s.DefaultProvider)
	}
	for _, source := range builtinProviders {
		if source.id != s.DefaultProvider && s.Enabled(source.id) {
			order = append(order, source.id)
		}
	}
	return order
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
		if value, ok := fields["mgtvEnabled"]; ok {
			var enabled *bool
			if json.Unmarshal(value, &enabled) != nil || enabled == nil {
				return SourceSettings{}, fmt.Errorf("invalid Mango danmaku source setting")
			}
			if !*enabled {
				s.Disabled = append(s.Disabled, mangoProviderID)
			}
		}
	}

	if s.DefaultProvider != "" && findProvider(s.DefaultProvider) == nil {
		return SourceSettings{}, fmt.Errorf("unknown default danmaku provider")
	}
	seen := map[string]bool{}
	disabled := []string{}
	for _, name := range s.Disabled {
		if findProvider(name) == nil {
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
		for _, source := range builtinProviders {
			if s.Enabled(source.id) {
				s.DefaultProvider = source.id
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
