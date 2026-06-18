package model

import (
	"cczjVideo/app/applog"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// FlexibleString 统一把字符串 / 数字 / null 解析为字符串。
// 同时实现了 sql.Scanner 和 driver.Valuer，以便 SQLite 中数字/字符串都能正常读写。
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}
	// 带引号的字符串
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexibleString(s)
		return nil
	}
	// 数字：先用 json.Number 避免科学计数，再转成字符串
	var num json.Number
	if err := json.Unmarshal(b, &num); err == nil {
		*f = FlexibleString(num.String())
		return nil
	}
	// 其他（bool 等）直接字符串化
	*f = FlexibleString(strings.Trim(string(b), "\" \t\n\r"))
	return nil
}

func (f FlexibleString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}

func (f FlexibleString) String() string { return string(f) }

// Scan 实现 database/sql.Scanner：支持 int64 / float64 / string / []byte / nil
func (f *FlexibleString) Scan(value interface{}) error {
	if f == nil {
		return fmt.Errorf("model.FlexibleString: Scan on nil pointer")
	}
	if value == nil {
		*f = ""
		return nil
	}
	switch v := value.(type) {
	case string:
		*f = FlexibleString(v)
	case []byte:
		*f = FlexibleString(string(v))
	case int:
		*f = FlexibleString(fmt.Sprintf("%d", v))
	case int32:
		*f = FlexibleString(fmt.Sprintf("%d", v))
	case int64:
		*f = FlexibleString(fmt.Sprintf("%d", v))
	case float32:
		*f = FlexibleString(fmt.Sprintf("%g", v))
	case float64:
		*f = FlexibleString(fmt.Sprintf("%g", v))
	case bool:
		if v {
			*f = "1"
		} else {
			*f = "0"
		}
	default:
		// sqlx/numeric 有时会把自定义 number 包一层
		rv := reflect.ValueOf(value)
		for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
			if rv.IsNil() {
				*f = ""
				return nil
			}
			rv = rv.Elem()
		}
		switch rv.Kind() {
		case reflect.String:
			*f = FlexibleString(rv.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			*f = FlexibleString(fmt.Sprintf("%d", rv.Int()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			*f = FlexibleString(fmt.Sprintf("%d", rv.Uint()))
		case reflect.Float32, reflect.Float64:
			*f = FlexibleString(fmt.Sprintf("%g", rv.Float()))
		case reflect.Bool:
			if rv.Bool() {
				*f = "1"
			} else {
				*f = "0"
			}
		default:
			*f = FlexibleString(fmt.Sprintf("%v", value))
		}
	}
	return nil
}

// Value 实现 driver.Valuer，写入数据库时统一为字符串
func (f FlexibleString) Value() (driver.Value, error) {
	return string(f), nil
}

type Video struct {
	// 内部主键（可选，sqlite 自增）
	Id int `json:"id,omitempty" db:"id"`
	// 上游资源站提供的原生视频 ID（唯一标识，用于 ON CONFLICT）
	VodId FlexibleString `json:"vod_id" db:"vod_id"`
	// 上游资源站提供的分类 ID（用于 type 过滤）
	TypeId   FlexibleString `json:"type_id" db:"type_id"`
	TypeName string         `json:"type_name" db:"type_name"`

	VodName     string `json:"vod_name" db:"vod_name"`
	GlobalId    int64  `json:"global_id" db:"global_id"`
	VodClass    string `json:"vod_class" db:"vod_class"`
	VodLang     string `json:"vod_lang" db:"vod_lang"`
	VodActor    string `json:"vod_actor" db:"vod_actor"`
	VodArea     string `json:"vod_area" db:"vod_area"`
	VodContent  string `json:"vod_content" db:"vod_content"`
	VodPic      string `json:"vod_pic" db:"vod_pic"`
	VodDirector string `json:"vod_director" db:"vod_director"`
	VodFrom     string `json:"vod_from" db:"vod_from"`
	VodRemarks  string `json:"vod_remarks" db:"vod_remarks"`
	VodTime     string `json:"vod_time" db:"vod_time"`
	VodYear     string `json:"vod_year" db:"vod_year"`
	VodPlayUrl  string `json:"vod_play_url" db:"vod_play_url"`
	VodDownUrl  string `json:"vod_down_url" db:"vod_down_url"`

	// 扩展字段：豆瓣评分/热度/状态等（来自上游采集接口，不同源站可能返回数字或字符串）
	VodDoubanId    FlexibleString `json:"vod_douban_id" db:"vod_douban_id"`
	VodDoubanScore FlexibleString `json:"vod_douban_score" db:"vod_douban_score"`
	VodHits        FlexibleString `json:"vod_hits" db:"vod_hits"`
	VodHitsDay     FlexibleString `json:"vod_hits_day" db:"vod_hits_day"`
	VodHitsWeek    FlexibleString `json:"vod_hits_week" db:"vod_hits_week"`
	VodHitsMonth   FlexibleString `json:"vod_hits_month" db:"vod_hits_month"`
	VodPubdate     FlexibleString `json:"vod_pubdate" db:"vod_pubdate"`
	VodVersion     FlexibleString `json:"vod_version" db:"vod_version"`
	VodState       FlexibleString `json:"vod_state" db:"vod_state"`
	VodScore       FlexibleString `json:"vod_score" db:"vod_score"`
	VodScoreAll    FlexibleString `json:"vod_score_all" db:"vod_score_all"`
	VodScoreNum    FlexibleString `json:"vod_score_num" db:"vod_score_num"`
	VodIsEnd       FlexibleString `json:"vod_isend" db:"vod_isend"`
	VodPlayFrom    string `json:"vod_play_from" db:"vod_play_from"`
	VodPlayNote    string `json:"vod_play_note" db:"vod_play_note"`
	VodLetter      string `json:"vod_letter" db:"vod_letter"`
	VodTag         string `json:"vod_tag" db:"vod_tag"`
	VodSub         string `json:"vod_sub" db:"vod_sub"`
	VodEn          string `json:"vod_en" db:"vod_en"`
}

// UnmarshalJSON 自定义反序列化：兼容不同源站返回的字段别名
// 常用别名映射：
//   vod_enname        → vod_en
//   vod_title         → vod_name
//   vod_poster        → vod_pic
//   vod_thumb         → vod_pic
//   vod_img           → vod_pic
//   vod_cover         → vod_pic
//   vod_keywords      → vod_tag
//   vod_tags          → vod_tag
//   vod_detail        → vod_content
//   vod_description   → vod_content
//   vod_desc          → vod_content
//   vod_authors       → vod_actor
//   vod_actors        → vod_actor
//   vod_directors     → vod_director
//   vod_total         → vod_hits
//   vod_hits_total    → vod_hits
//   vod_playfrom      → vod_play_from
//   vod_playnote      → vod_play_note
func (v *Video) UnmarshalJSON(data []byte) error {
	applog.Info("[Video.UnmarshalJSON] 原始数据: %s", truncate(string(data), 500))

	type Alias Video
	aux := &struct {
		VodEnname        string `json:"vod_enname"`
		VodTitle         string `json:"vod_title"`
		VodPoster        string `json:"vod_poster"`
		VodThumb         string `json:"vod_thumb"`
		VodImg           string `json:"vod_img"`
		VodCover         string `json:"vod_cover"`
		VodKeywords      string `json:"vod_keywords"`
		VodTags          string `json:"vod_tags"`
		VodDetail        string `json:"vod_detail"`
		VodDescription   string `json:"vod_description"`
		VodDesc          string `json:"vod_desc"`
		VodAuthors       string `json:"vod_authors"`
		VodActors        string `json:"vod_actors"`
		VodDirectors     string `json:"vod_directors"`
		VodTotal         string `json:"vod_total"`
		VodHitsTotal     string `json:"vod_hits_total"`
		VodPlayfrom      string `json:"vod_playfrom"`
		VodPlaynote      string `json:"vod_playnote"`
		*Alias
	}{
		Alias: (*Alias)(v),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	applog.Info("[Video.UnmarshalJSON] 反序列化后 - vod_id: %s, vod_name: %s, vod_actor: %s, vod_director: %s, vod_content: %s, vod_pic: %s",
		v.VodId.String(), v.VodName, v.VodActor, v.VodDirector, truncate(v.VodContent, 100), v.VodPic)

	if v.VodEn == "" && aux.VodEnname != "" { v.VodEn = aux.VodEnname }
	if v.VodName == "" && aux.VodTitle != "" { v.VodName = aux.VodTitle }
	if v.VodPic == "" && aux.VodPoster != "" { v.VodPic = aux.VodPoster }
	if v.VodPic == "" && aux.VodThumb != "" { v.VodPic = aux.VodThumb }
	if v.VodPic == "" && aux.VodImg != "" { v.VodPic = aux.VodImg }
	if v.VodPic == "" && aux.VodCover != "" { v.VodPic = aux.VodCover }
	if v.VodTag == "" && aux.VodKeywords != "" { v.VodTag = aux.VodKeywords }
	if v.VodTag == "" && aux.VodTags != "" { v.VodTag = aux.VodTags }
	if v.VodContent == "" && aux.VodDetail != "" { v.VodContent = aux.VodDetail }
	if v.VodContent == "" && aux.VodDescription != "" { v.VodContent = aux.VodDescription }
	if v.VodContent == "" && aux.VodDesc != "" { v.VodContent = aux.VodDesc }
	if v.VodActor == "" && aux.VodAuthors != "" { v.VodActor = aux.VodAuthors }
	if v.VodActor == "" && aux.VodActors != "" { v.VodActor = aux.VodActors }
	if v.VodDirector == "" && aux.VodDirectors != "" { v.VodDirector = aux.VodDirectors }
	if v.VodHits.String() == "" && aux.VodTotal != "" { v.VodHits = FlexibleString(aux.VodTotal) }
	if v.VodHits.String() == "" && aux.VodHitsTotal != "" { v.VodHits = FlexibleString(aux.VodHitsTotal) }
	if v.VodPlayFrom == "" && aux.VodPlayfrom != "" { v.VodPlayFrom = aux.VodPlayfrom }
	if v.VodPlayNote == "" && aux.VodPlaynote != "" { v.VodPlayNote = aux.VodPlaynote }

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

type VType struct {
	Id        int            `json:"id" db:"id"`
	TypeId    FlexibleString `json:"type_id" db:"type_id"`
	Name      string         `json:"name" db:"name"`
	ParentId  int            `json:"parent_id" db:"parent_id"`
	Sort      int            `json:"sort" db:"sort"`
	SourceKey string         `json:"source_key" db:"source_key"`
}

type Episode struct {
	VodId  FlexibleString `json:"vod_id" db:"vod_id"`
	EpNum  int            `json:"ep_num" db:"ep_num"`
	EpName string         `json:"ep_name" db:"ep_name"`
	EpUrl  string         `json:"ep_url" db:"ep_url"`
}

type Favorite struct {
	Id        int    `json:"id"`
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
	VodName   string `json:"vod_name"`
	VodPic    string `json:"vod_pic"`
	CreatedAt string `json:"created_at"`
}

type WatchHistory struct {
	Id        int     `json:"id"`
	SourceKey string  `json:"source_key"`
	VodId     string  `json:"vod_id"`
	VodName   string  `json:"vod_name"`
	VodPic    string  `json:"vod_pic"`
	EpNum     int     `json:"ep_num"`
	Position  float64 `json:"position"`
	UpdatedAt string  `json:"updated_at"`
}
