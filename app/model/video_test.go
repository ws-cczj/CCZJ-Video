package model

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
)

func decodeFlexible(t *testing.T, payload string) FlexibleString {
	t.Helper()
	var f FlexibleString
	if err := f.UnmarshalJSON([]byte(payload)); err != nil {
		t.Fatalf("UnmarshalJSON(%s) = %v", payload, err)
	}
	return f
}

// 上游 CMS 对同一个字段有时给字符串、有时给数字。这里钉住两件事：数字不能变成
// 1e+09 这种科学计数（会被当成片名写进库），以及 null 必须落成空串而不是 "<nil>"。
func TestFlexibleStringUnmarshalJSON(t *testing.T) {
	cases := []struct {
		payload string
		want    string
	}{
		{`"12"`, "12"},
		{`12`, "12"},
		{`12.5`, "12.5"},
		{`1000000000`, "1000000000"},
		{`null`, ""},
		{`""`, ""},
		{`true`, "true"},
	}
	for _, c := range cases {
		if got := decodeFlexible(t, c.payload).String(); got != c.want {
			t.Errorf("payload %s -> %q, want %q", c.payload, got, c.want)
		}
	}
	if got := decodeFlexible(t, "").String(); got != "" {
		t.Errorf("empty payload -> %q", got)
	}
	if got := decodeFlexible(t, `"带引号"`).String(); got != "带引号" {
		t.Fatalf("quoted string -> %q", got)
	}
}

func TestFlexibleStringMarshalScanValue(t *testing.T) {
	raw, err := json.Marshal(FlexibleString("12"))
	if err != nil || string(raw) != `"12"` {
		t.Fatalf("MarshalJSON = %s, %v", raw, err)
	}
	if FlexibleString("abc").String() != "abc" {
		t.Fatal("String() must return the underlying text")
	}

	value, err := FlexibleString("abc").Value()
	if err != nil || value != driver.Value("abc") {
		t.Fatalf("Value() = %#v, %v", value, err)
	}

	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "s", "s"},
		{"bytes", []byte("b"), "b"},
		{"int", 7, "7"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"float32", float32(1.5), "1.5"},
		{"float64", float64(1000000000), "1e+09"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
		{"pointer to string", func() *string { s := "p"; return &s }(), "p"},
		{"nil pointer", (*string)(nil), ""},
		{"unknown kind", struct{ A int }{A: 1}, "{1}"},
	}
	for _, c := range cases {
		var f FlexibleString
		if err := f.Scan(c.input); err != nil {
			t.Fatalf("Scan(%s) = %v", c.name, err)
		}
		if f.String() != c.want {
			t.Errorf("Scan(%s) -> %q, want %q", c.name, f.String(), c.want)
		}
	}

	// 反射拿不到值（nil receiver）必须是结构化错误，不能 panic。
	var nilTarget *FlexibleString
	if err := nilTarget.Scan("x"); err == nil {
		t.Fatal("Scan on a nil pointer returned no error")
	} else if apperror.CodeOf(err) != apperror.Internal {
		t.Fatalf("Scan on a nil pointer code = %s", apperror.CodeOf(err))
	}
}

// 别名字段只在正式字段为空时填空，且顺序固定。源站同时给 vod_pic 和 vod_poster 时
// 必须留正式字段；否则一次 map 迭代顺序不同就会换封面。（Go 的 map 迭代是随机的。）
func TestVideoUnmarshalAliasPrecedence(t *testing.T) {
	var video Video
	if err := json.Unmarshal([]byte(`{"vod_pic":"formal","vod_poster":"poster","vod_thumb":"thumb","vod_img":"img","vod_cover":"cover"}`), &video); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if video.VodPic != "formal" {
		t.Fatalf("vod_pic = %q, want formal", video.VodPic)
	}

	video = Video{}
	if err := json.Unmarshal([]byte(`{"vod_thumb":"thumb","vod_img":"img","vod_cover":"cover"}`), &video); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if video.VodPic != "thumb" {
		t.Fatalf("alias order must be thumb > img > cover, got %q", video.VodPic)
	}

	video = Video{}
	if err := json.Unmarshal([]byte(`{"vod_cover":"cover"}`), &video); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if video.VodPic != "cover" {
		t.Fatalf("vod_cover -> vod_pic = %q", video.VodPic)
	}
}

func TestVideoUnmarshalAliasesFillEmptyFields(t *testing.T) {
	payload := `{"vod_title":"标题","vod_enname":"EN","vod_keywords":"kw","vod_tags":"tag",
		"vod_detail":"d1","vod_description":"d2","vod_desc":"d3",
		"vod_authors":"a1","vod_actors":"a2","vod_directors":"dir",
		"vod_total":42,"vod_hits":"7","vod_playfrom":"line","vod_playnote":"note"}`
	var video Video
	if err := json.Unmarshal([]byte(payload), &video); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if video.VodName != "标题" || video.VodEn != "EN" {
		t.Fatalf("name/en = %q/%q", video.VodName, video.VodEn)
	}
	if video.VodTag != "kw" {
		t.Fatalf("vod_keywords must win when vod_tag is empty, got %q", video.VodTag)
	}
	if video.VodContent != "d1" {
		t.Fatalf("vod_detail must win over later aliases, got %q", video.VodContent)
	}
	if video.VodActor != "a1" || video.VodDirector != "dir" {
		t.Fatalf("actor/director = %q/%q", video.VodActor, video.VodDirector)
	}
	// vod_hits 已有值，vod_total 不得覆盖。
	if video.VodHits.String() != "7" {
		t.Fatalf("vod_hits = %q, want 7", video.VodHits.String())
	}
	if video.VodPlayFrom != "line" || video.VodPlayNote != "note" {
		t.Fatalf("playfrom/playnote = %q/%q", video.VodPlayFrom, video.VodPlayNote)
	}

	// vod_hits 空时按 vod_total > vod_hits_total 顺序回填，数字保持十进制。
	video = Video{}
	if err := json.Unmarshal([]byte(`{"vod_hits_total":1000000000}`), &video); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if video.VodHits.String() != "1000000000" {
		t.Fatalf("vod_hits = %q", video.VodHits.String())
	}

	if err := json.Unmarshal([]byte(`{"vod_id":`), &video); err == nil {
		t.Fatal("malformed payload accepted")
	}
}

// 日志里截断原始报文用的，越界不能 panic —— 采集到的超长 vod_content 会走到这里。
func TestTruncateKeepsShortStrings(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("truncate(short) = %q", got)
	}
	if got := truncate(strings.Repeat("x", 10), 5); got != "xxxxx..." {
		t.Fatalf("truncate(long) = %q", got)
	}
	if got := truncate("中文一二三", 6); got != "中文..." {
		t.Fatalf("truncate by bytes = %q", got)
	}
}
