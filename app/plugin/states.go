package plugin

import (
	"encoding/json"
	"errors"
	"strings"

	"cczjVideo/app/db"
)

// 启用/禁用状态落在既有的 settings KV 表里（键 plugin_states），值是一个只写
// 「被关掉」的包的对象：{"<pack-id>": false}。缺省即可用，所以删包不需要清账，
// 老数据里出现 true 也只会被理解成「启用」。

// decodeStates 是纯函数：把设置值解成 map，便于在没有数据库句柄时单测。
func decodeStates(value string) (map[string]bool, error) {
	states := make(map[string]bool)
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return states, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &states); err != nil {
		return nil, err
	}
	return states, nil
}

// encodeStates 只持久化 false 项（禁用），使设置值保持最小、可读。
func encodeStates(states map[string]bool) (string, error) {
	disabled := make(map[string]bool)
	for id, enabled := range states {
		if !enabled {
			disabled[id] = false
		}
	}
	if len(disabled) == 0 {
		// 全启用时留空串而不是 "{}"：设置页里这一项没内容就等于「没人关过」。
		return "", nil
	}
	encoded, err := json.Marshal(disabled)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// loadStatesFromDB 读设置表。数据库没打开时（尚未 InitDB，或在测试里）返回空状态：
// 扫描本身只依赖磁盘，不该因为库不在就连坐。
func loadStatesFromDB() (map[string]bool, error) {
	if db.DB() == nil {
		return make(map[string]bool), nil
	}
	value, err := db.GetSetting(settingsKey)
	if err != nil {
		return nil, err
	}
	return decodeStates(value)
}

func saveStatesToDB(states map[string]bool) error {
	if db.DB() == nil {
		return errors.New("plugin: 数据库尚未打开，无法保存扩展包启用状态")
	}
	value, err := encodeStates(states)
	if err != nil {
		return err
	}
	return db.SetSetting(settingsKey, value)
}
