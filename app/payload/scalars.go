package payload

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func ScalarInt64(raw json.RawMessage, def int64) (int64, error) {
	if len(raw) == 0 {
		return def, nil
	}
	// 字符串形式："0"
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		if s == "" {
			return def, nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return def, fmt.Errorf("parse %q: %w", s, err)
		}
		return v, nil
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}
func ScalarInt(raw json.RawMessage, def int) (int, error) {
	v, err := ScalarInt64(raw, int64(def))
	if err != nil {
		return def, err
	}
	return int(v), nil
}
func ScalarID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 带引号的字符串
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	// 数字：直接转为字符串
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatInt(n, 10)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatInt(int64(f), 10)
	}
	return ""
}
