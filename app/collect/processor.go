package collect

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

func CleanHTML(html string) string {
	if html == "" {
		return html
	}
	applog.Info("[CleanHTML] 输入: len=%d, content=%s", len(html), truncate(html, 100))
	html = strings.TrimPrefix(strings.TrimSuffix(html, "`"), "`")
	html = strings.Trim(html, "`\"' ")

	brRegex := regexp.MustCompile(`(?i)<br\s*/?>`)
	text := brRegex.ReplaceAllString(html, "\n")

	tagRegex := regexp.MustCompile(`<[^>]+>`)
	text = tagRegex.ReplaceAllString(text, "")

	entityMap := map[string]string{
		"&nbsp;": " ", "&amp;": "&", "&lt;": "<", "&gt;": ">",
		"&quot;": "\"", "&#39;": "'",
	}
	for e, r := range entityMap {
		text = strings.ReplaceAll(text, e, r)
	}
	unknownEntityRegex := regexp.MustCompile(`&[a-z0-9]+;`)
	text = unknownEntityRegex.ReplaceAllString(text, "")

	whitespaceRegex := regexp.MustCompile(`\s+`)
	text = whitespaceRegex.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	if len(text) > 1800 {
		text = text[:1800] + "..."
	}
	applog.Info("[CleanHTML] 输出: len=%d, content=%s", len(text), truncate(text, 100))
	return text
}

// cleanField 清理字段值前后的反引号、引号、空格等包裹字符
func cleanField(s string) string {
	if s == "" {
		return s
	}
	return strings.Trim(s, "`\"' \t\n\r")
}

// DefaultFieldAliases 内置默认字段别名映射（兼容常见源站字段名）
// 基于分析多个采集源返回格式总结，包含所有常见变体
var DefaultFieldAliases = map[string][]string{
	"vod_id":   {"vod_id", "id", "video_id", "vid", "videoId", "VideoId"},
	"vod_name": {"vod_name", "title", "vod_title", "name", "vodname", "VideoName"},
	// 注意：vod_pic_screenshot / vod_pic_thumb / vod_pic_slide 不放在此处，
	// 因为 API 返回这些字段可能为空字符串，会因 map 随机迭代顺序覆盖有效的 vod_pic。
	// 它们的回退逻辑在 ParseVideoWithMapping 的 "vod_pic 回退" 部分单独处理。
	"vod_pic":          {"vod_pic", "poster", "vod_poster", "thumb", "vod_thumb", "cover", "vod_cover", "img", "vod_img", "pic", "image"},
	"vod_actor":        {"vod_actor", "actor", "actors", "vod_actors"},
	"vod_director":     {"vod_director", "director", "directors", "vod_directors"},
	"vod_content":      {"vod_content", "content", "desc", "description", "vod_desc", "vod_description", "detail", "vod_detail", "summary", "vod_blurb"},
	"vod_year":         {"vod_year", "year", "vodyear", "release_year"},
	"vod_area":         {"vod_area", "area", "vodarea", "country"},
	"vod_lang":         {"vod_lang", "lang", "language", "vodlanguage"},
	"vod_class":        {"vod_class", "class", "category"},
	"type_id":          {"type_id", "typeid", "category_id", "class_id", "type_id_1"},
	"type_name":        {"type_name", "typename", "category_name", "class_name", "type"},
	"vod_play_url":     {"vod_play_url", "play_url", "playurl", "url", "vodurl", "play_urls", "source"},
	"vod_down_url":     {"vod_down_url", "down_url", "download_url", "downurl"},
	"vod_remarks":      {"vod_remarks", "remarks", "vodremark", "note", "vod_note"},
	"vod_tag":          {"vod_tag", "tag", "tags", "keywords", "vod_keywords", "vod_tags"},
	"vod_en":           {"vod_en", "vod_enname", "enname", "en_name", "english_name"},
	"vod_douban_id":    {"vod_douban_id", "douban_id", "doubanid", "db_id"},
	"vod_douban_score": {"vod_douban_score", "douban_score", "doubanid_score"},
	"vod_sub":          {"vod_sub", "sub", "subtitle", "vod_subtitle"},
	"vod_status":       {"vod_status", "status"},
	"vod_letter":       {"vod_letter", "letter"},
	"vod_total":        {"vod_total", "total", "episode_count"},
	"vod_pubdate":      {"vod_pubdate", "pubdate", "release_date"},
	"vod_duration":     {"vod_duration", "duration"},
	"vod_hits":         {"vod_hits", "hits", "views", "vod_hits_total"},
	"vod_hits_day":     {"vod_hits_day", "hits_day"},
	"vod_hits_week":    {"vod_hits_week", "hits_week"},
	"vod_hits_month":   {"vod_hits_month", "hits_month"},
	"vod_score":        {"vod_score", "score", "rating", "vod_rating"},
	"vod_score_all":    {"vod_score_all", "score_all"},
	"vod_score_num":    {"vod_score_num", "score_num"},
	"vod_isend":        {"vod_isend", "isend", "is_ended"},
	"vod_time":         {"vod_time", "time", "update_time"},
	"vod_play_from":    {"vod_play_from", "play_from", "play_source"},
	"vod_play_server":  {"vod_play_server", "play_server"},
	"vod_play_note":    {"vod_play_note", "play_note"},
	"vod_author":       {"vod_author", "author"},
}

// ParseVideoWithMapping 通用视频解析函数：支持自定义字段映射
// 逻辑：遍历原始 JSON 的所有字段，尝试映射到 Video 结构
// Alias precedence is declared explicitly because Go map iteration order is
// intentionally random. Configuration mapping always wins; then an exact
// target name wins; finally the first alias in this stable list wins.
var defaultFieldAliasOrder = []string{
	"vod_id", "vod_name", "vod_pic", "type_id", "type_name", "vod_class",
	"vod_year", "vod_area", "vod_lang", "vod_remarks", "vod_score",
	"vod_douban_score", "vod_douban_id", "vod_actor", "vod_director",
	"vod_content", "vod_play_url", "vod_down_url", "vod_tag", "vod_en",
	"vod_sub", "vod_status", "vod_letter", "vod_total", "vod_pubdate",
	"vod_duration", "vod_hits", "vod_hits_day", "vod_hits_week",
	"vod_hits_month", "vod_score_all", "vod_score_num", "vod_isend",
	"vod_time", "vod_play_from", "vod_play_server", "vod_play_note", "vod_author",
}

type fieldAliasTarget struct {
	field string
	rank  int
}

var defaultFieldAliasIndex = buildDefaultFieldAliasIndex()

func buildDefaultFieldAliasIndex() map[string]fieldAliasTarget {
	index := make(map[string]fieldAliasTarget)
	rank := 0
	for _, target := range defaultFieldAliasOrder {
		for _, alias := range DefaultFieldAliases[target] {
			if _, alreadyAssigned := index[alias]; !alreadyAssigned {
				index[alias] = fieldAliasTarget{field: target, rank: rank}
			}
			rank++
		}
	}
	return index
}

type fieldCandidate struct {
	source   string
	target   string
	value    interface{}
	priority int
}

func collectFieldCandidates(raw map[string]interface{}, fieldMapping map[string]string) []fieldCandidate {
	candidates := make([]fieldCandidate, 0, len(raw))
	for source, value := range raw {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && text == "" {
			continue
		}
		if mapped, ok := fieldMapping[source]; ok && mapped != "" {
			candidates = append(candidates, fieldCandidate{source: source, target: mapped, value: value, priority: 0})
			continue
		}
		alias, ok := defaultFieldAliasIndex[source]
		if !ok {
			continue
		}
		priority := 100 + alias.rank
		if source == alias.field {
			priority = 1
		}
		candidates = append(candidates, fieldCandidate{source: source, target: alias.field, value: value, priority: priority})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].target != candidates[j].target {
			return candidates[i].target < candidates[j].target
		}
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].source < candidates[j].source
	})
	return candidates
}

func ParseVideoWithMapping(rawData []byte, fieldMapping map[string]string) (*model.Video, error) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(rawData, &rawMap); err != nil {
		return nil, err
	}

	v := &model.Video{}
	vType := reflect.ValueOf(v).Elem()
	vTypeStruct := vType.Type()

	assigned := make(map[string]bool)
	for _, candidate := range collectFieldCandidates(rawMap, fieldMapping) {
		targetFieldName := candidate.target
		if assigned[targetFieldName] {
			continue
		}
		srcValue := candidate.value

		for i := 0; i < vType.NumField(); i++ {
			field := vType.Field(i)
			fieldType := vTypeStruct.Field(i)
			jsonTag := fieldType.Tag.Get("json")
			if jsonTag == "" || jsonTag == "-" {
				continue
			}
			jsonName := strings.Split(jsonTag, ",")[0]
			if jsonName == targetFieldName {
				// 跳过空字符串值，防止后遍历的空别名覆盖已设置的有效值
				if s, ok := srcValue.(string); ok && s == "" {
					break
				}
				setFieldValue(field, srcValue)
				assigned[targetFieldName] = true
				break
			}
		}
	}

	applog.Debug("[FieldMapping] 解析结果 - vod_id: %s, vod_name: %s, vod_pic: %s, vod_actor: %s, vod_director: %s, vod_content: %s",
		v.VodId.String(), v.VodName, v.VodPic, v.VodActor, v.VodDirector, truncate(v.VodContent, 50))

	// vod_pic 回退：列表 API 的 vod_pic 可能为空，但 vod_pic_thumb/vod_pic_screenshot/vod_pic_slide 可能有值
	if v.VodPic == "" {
		for _, fallback := range []string{"vod_pic_thumb", "vod_pic_screenshot", "vod_pic_slide", "vod_pic_screenshot"} {
			if pic, ok := rawMap[fallback]; ok && pic != nil {
				if s := fmt.Sprintf("%v", pic); s != "" && s != "<nil>" {
					v.VodPic = s
					break
				}
			}
		}
	}

	return v, nil
}

// findTargetField 根据源字段名查找目标字段名
func findTargetField(srcKey string) string {
	if target, ok := defaultFieldAliasIndex[srcKey]; ok {
		return target.field
	}
	return ""
}

// getKeys 返回 map 的所有键
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ParseVideosWithMapping 批量解析视频（兼容 FetchResult 结构）
func ParseVideosWithMapping(rawData []byte, fieldMapping map[string]string) ([]*model.Video, error) {
	var result struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(rawData, &result); err != nil {
		return nil, err
	}

	videos := make([]*model.Video, 0, len(result.List))
	for i, item := range result.List {
		v, err := ParseVideoWithMapping(item, fieldMapping)
		if err != nil {
			applog.Warn("[FieldMapping] 解析第 %d 条视频失败: %v", i, err)
			continue
		}
		videos = append(videos, v)
	}
	return videos, nil
}

func setFieldValue(field reflect.Value, value interface{}) {
	if !field.CanSet() {
		return
	}

	switch v := value.(type) {
	case string:
		if field.Kind() == reflect.String {
			field.SetString(v)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(v)
			field.Set(reflect.ValueOf(fs))
		}
	case int:
		s := fmt.Sprintf("%d", v)
		if field.Kind() == reflect.String {
			field.SetString(s)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(s)
			field.Set(reflect.ValueOf(fs))
		}
	case int64:
		s := fmt.Sprintf("%d", v)
		if field.Kind() == reflect.String {
			field.SetString(s)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(s)
			field.Set(reflect.ValueOf(fs))
		}
	case float64:
		// 避免科学计数法：整数部分用 %d，小数用 %g
		s := fmt.Sprintf("%g", v)
		if v == float64(int64(v)) && v >= 0 {
			s = fmt.Sprintf("%d", int64(v))
		}
		if field.Kind() == reflect.String {
			field.SetString(s)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(s)
			field.Set(reflect.ValueOf(fs))
		}
	case bool:
		s := "0"
		if v {
			s = "1"
		}
		if field.Kind() == reflect.String {
			field.SetString(s)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(s)
			field.Set(reflect.ValueOf(fs))
		}
	default:
		s := fmt.Sprintf("%v", value)
		if field.Kind() == reflect.String {
			field.SetString(s)
		} else if field.Type() == reflect.TypeOf(model.FlexibleString("")) {
			fs := model.FlexibleString(s)
			field.Set(reflect.ValueOf(fs))
		}
	}
}
