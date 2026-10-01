package model

import "testing"

func TestValidateSourceKey(t *testing.T) {
	valid := []string{"source", "source_1", "a", "a123", "a_b_c"}
	for _, key := range valid {
		if err := ValidateSourceKey(key); err != nil {
			t.Fatalf("ValidateSourceKey(%q) returned %v", key, err)
		}
	}

	invalid := []string{"", " Source", "source ", "Source", "source-key", "source;drop", "1" + string(make([]byte, 64))}
	for _, key := range invalid {
		if err := ValidateSourceKey(key); err == nil {
			t.Fatalf("ValidateSourceKey(%q) unexpectedly succeeded", key)
		}
	}
}

// 高级配置有两个落点：adv_config 列和 collect_limit/collect_hours 两个平列。
// 读的时候 JSON 优先、平列兜底，写的时候两边必须同步 —— 否则编辑采集源会把
// 老配置读成 0（就是过去「编辑一次源，采集条数上限没了」那个缺陷）。
func TestSourceAdvConfigRoundTrip(t *testing.T) {
	fallback := Source{CollectLimit: 20, CollectHours: 6}
	if cfg := fallback.GetAdvConfig(); cfg.CollectLimit != 20 || cfg.CollectHours != 6 {
		t.Fatalf("flat columns must back an empty adv_config, got %+v", cfg)
	}

	explicit := Source{AdvConfigRaw: `{"collect_limit":50,"collect_hours":3,"field_mapping":{"title":"vod_name"}}`, CollectLimit: 20}
	cfg := explicit.GetAdvConfig()
	if cfg.CollectLimit != 50 || cfg.CollectHours != 3 || cfg.FieldMapping["title"] != "vod_name" {
		t.Fatalf("adv_config must win over the flat columns, got %+v", cfg)
	}

	source := Source{}
	source.SetAdvConfig(AdvConfig{CollectLimit: 30, CollectHours: 12, FieldMapping: map[string]string{"name": "vod_name"}})
	if source.CollectLimit != 30 || source.CollectHours != 12 {
		t.Fatalf("SetAdvConfig left the flat columns at %d/%d", source.CollectLimit, source.CollectHours)
	}
	after := source.GetAdvConfig()
	if after.CollectLimit != 30 || after.CollectHours != 12 || after.FieldMapping["name"] != "vod_name" {
		t.Fatalf("round trip = %+v", after)
	}
}

// 空的 schedule_config 表示「跟全局默认」，所以 nil 才是合法读数；坏 JSON 也按 nil 处理，
// 不能让一个写坏的列把整个源卡住。SetScheduleConfig(nil) 必须写空串而不是 "null"。
func TestSourceScheduleConfigNilMeansGlobalDefault(t *testing.T) {
	if (&Source{}).GetScheduleConfig() != nil {
		t.Fatal("empty schedule_config must read as nil")
	}
	if (&Source{ScheduleCfgRaw: `not json`}).GetScheduleConfig() != nil {
		t.Fatal("unparsable schedule_config must read as nil")
	}

	source := Source{ScheduleCfgRaw: `{"enabled":true}`}
	source.SetScheduleConfig(nil)
	if source.ScheduleCfgRaw != "" {
		t.Fatalf("clearing must blank the column, got %q", source.ScheduleCfgRaw)
	}

	source.SetScheduleConfig(&ScheduleConfig{Enabled: true, Mode: CollectModeIncremental, IntervalMin: 10})
	cfg := source.GetScheduleConfig()
	if cfg == nil || !cfg.Enabled || cfg.Mode != CollectModeIncremental || cfg.IntervalMin != 10 {
		t.Fatalf("round trip = %+v", cfg)
	}
}
