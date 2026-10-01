package plugin

import (
	"bytes"
	"encoding/json"
)

// 本地私有的一份 source strategy v2 形状，逐字段对齐
// docs/source-strategy-v2.schema.json。
//
// 这里刻意不 import cczjVideo/app/collect：那边是同一天还在改的实现包，
// schema 才是适配包作者的契约来源；把校验绑在实现结构体上，等于让两个
// 互不相干的发布节奏共用一条错误消息。字段语义变了，schema 得先改，
// 这个结构体跟着 schema 走。

type strategyDoc struct {
	Version      *int               `json:"version"`
	Strategy     string             `json:"strategy"`
	List         *strategyOperation `json:"list"`
	Search       *strategyOperation `json:"search"`
	Detail       *strategyOperation `json:"detail"`
	Response     *strategyResponse  `json:"response"`
	FieldMapping map[string]string  `json:"field_mapping"`
}

type strategyOperation struct {
	Action       string            `json:"action"`
	ActionParam  string            `json:"action_param"`
	PageParam    string            `json:"page_param"`
	LimitParam   string            `json:"limit_param"`
	TypeParam    string            `json:"type_param"`
	KeywordParam string            `json:"keyword_param"`
	HoursParam   string            `json:"hours_param"`
	IDParam      string            `json:"id_param"`
	Extra        map[string]string `json:"extra"`
}

type strategyResponse struct {
	ListPath      string    `json:"list_path"`
	CodePath      string    `json:"code_path"`
	OKCodes       *[]string `json:"ok_codes"`
	MsgPath       string    `json:"msg_path"`
	PagePath      string    `json:"page_path"`
	PageCountPath string    `json:"pagecount_path"`
	TotalPath     string    `json:"total_path"`
}

// validateStrategy 校验原始 JSON，返回归一化（compact）后的同一份文档。
// 形状之外的语义约束：version 恒为 2，strategy 取枚举值，response.ok_codes
// 写了就必须至少一项（不写 response 就走内置默认信封）。
func validateStrategy(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, newPackError(reasonStrategyInvalid, "adapter.strategy 缺失")
	}
	var doc strategyDoc
	if err := decodeStrict(raw, &doc, reasonStrategyInvalid); err != nil {
		return nil, inSection(err, "strategy")
	}
	if doc.Version == nil {
		return nil, newPackError(reasonStrategyInvalid, "strategy.version 必填，且只能是 2")
	}
	if *doc.Version != 2 {
		return nil, newPackError(reasonStrategyInvalid, "strategy.version 只能是 2，当前是 %d", *doc.Version)
	}
	if !strategyNames[doc.Strategy] {
		return nil, newPackError(reasonStrategyInvalid, "strategy.strategy %q 不是 standard_cms|cms_videolist|declarative|custom", doc.Strategy)
	}
	if doc.Response != nil && doc.Response.OKCodes != nil && len(*doc.Response.OKCodes) == 0 {
		return nil, newPackError(reasonStrategyInvalid, "strategy.response.ok_codes 写了就必须至少有一项")
	}
	// 重新压缩一遍再交给界面：包里带了缩进和注释位置的空白也不至于把
	// strategy_config 撑大，同时保证落库的永远是合法 JSON。
	compact, err := compactJSON(raw)
	if err != nil {
		return nil, inSection(err, "strategy")
	}
	return compact, nil
}

func compactJSON(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, newPackError(reasonJSONInvalid, "%v", err)
	}
	out := make(json.RawMessage, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}
