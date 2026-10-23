package danmaku

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"cczjVideo/app/apperror"
	"github.com/andybalholm/brotli"
)

type field struct {
	tag  int
	wire int
	n    uint64
	data []byte
}

func fields(raw []byte) ([]field, error) {
	if len(raw) > MaxPayload {
		return nil, fmt.Errorf("protobuf too large")
	}
	result := make([]field, 0, 16)
	for len(raw) > 0 {
		key, n := binary.Uvarint(raw)
		if n <= 0 || key>>3 == 0 || key>>3 >= 1<<29 {
			return nil, fmt.Errorf("invalid protobuf key")
		}
		raw = raw[n:]
		f := field{tag: int(key >> 3), wire: int(key & 7)}
		switch f.wire {
		case 0:
			f.n, n = binary.Uvarint(raw)
			if n <= 0 {
				return nil, fmt.Errorf("invalid protobuf varint")
			}
			raw = raw[n:]
		case 2:
			var size uint64
			size, n = binary.Uvarint(raw)
			if n <= 0 || size > uint64(len(raw)-n) {
				return nil, fmt.Errorf("truncated protobuf field")
			}
			raw = raw[n:]
			f.data = raw[:int(size)]
			raw = raw[int(size):]
		case 1, 5:
			size := 8
			if f.wire == 5 {
				size = 4
			}
			if len(raw) < size {
				return nil, fmt.Errorf("truncated protobuf fixed field")
			}
			raw = raw[size:]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", f.wire)
		}
		result = append(result, f)
		if len(result) > MaxSegmentItems*4 {
			return nil, fmt.Errorf("too many protobuf fields")
		}
	}
	return result, nil
}
func pnum(fs []field, tag int) uint64 {
	for _, f := range fs {
		if f.tag == tag && f.wire == 0 {
			return f.n
		}
	}
	return 0
}
func ptext(fs []field, tag int) string {
	for _, f := range fs {
		if f.tag == tag && f.wire == 2 && utf8.Valid(f.data) {
			return string(f.data)
		}
	}
	return ""
}
func schemaError(err error) error {
	return apperror.Wrap(apperror.Corrupt, err, "弹幕数据解析失败，请稍后重试或更换来源")
}
func validItem(item Item) bool {
	return item.TimeMS >= 0 && item.TimeMS <= 24*60*60*1000 && strings.TrimSpace(item.Text) != "" && utf8.ValidString(item.Text) && utf8.RuneCountInString(item.Text) <= 500 && (item.Mode == 1 || item.Mode == 4 || item.Mode == 5)
}
func sorted(items []Item) []Item {
	sort.SliceStable(items, func(i, j int) bool { return items[i].TimeMS < items[j].TimeMS })
	return items
}
func rgb(value string) uint32 {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return 0xffffff
	}
	n, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return 0xffffff
	}
	return uint32(n)
}

func DecodeBilibili(raw []byte) ([]Item, int, error) {
	fs, err := fields(raw)
	if err != nil {
		return nil, 0, schemaError(err)
	}
	if pnum(fs, 2) != 0 {
		return nil, 0, apperror.New(apperror.PermissionDenied, "该来源的弹幕已关闭或受限")
	}
	items := make([]Item, 0)
	skipped := 0
	for _, f := range fs {
		if f.tag != 1 || f.wire != 2 {
			continue
		}
		row, err := fields(f.data)
		if err != nil {
			return nil, 0, schemaError(err)
		}
		id := ptext(row, 12)
		if id == "" {
			if n := pnum(row, 1); n != 0 {
				id = strconv.FormatUint(n, 10)
			}
		}
		item := Item{ID: id, TimeMS: int64(pnum(row, 2)), Mode: int(pnum(row, 3)), Color: uint32(pnum(row, 5)) & 0xffffff, FontSize: int(pnum(row, 4)), Text: ptext(row, 7)}
		if item.FontSize == 0 {
			item.FontSize = 25
		}
		if !validItem(item) {
			skipped++
			continue
		}
		items = append(items, item)
		if len(items) > MaxSegmentItems {
			return nil, 0, schemaError(fmt.Errorf("too many items"))
		}
	}
	return sorted(items), skipped, nil
}

func DecodeQQ(raw []byte) ([]Item, int, error) {
	root, err := object(raw)
	if err != nil {
		return nil, 0, err
	}
	rows, ok := root["barrage_list"].([]any)
	if !ok {
		return nil, 0, schemaError(fmt.Errorf("missing barrage_list"))
	}
	if len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("too many items"))
	}
	items := make([]Item, 0, len(rows))
	skipped := 0
	for _, value := range rows {
		row := obj(value)
		style := map[string]any{}
		if raw := text(row["content_style"]); raw != "" {
			if parsed, err := object([]byte(raw)); err == nil {
				style = parsed
			}
		}
		color := text(style["color"])
		if colors := arr(style["gradient_colors"]); len(colors) > 0 {
			color = text(colors[0])
		}
		mode := 1
		switch number(style["position"]) {
		case 2:
			mode = 5
		case 3:
			mode = 4
		}
		timeValue := text(row["time_offset"])
		at, err := strconv.ParseInt(timeValue, 10, 64)
		item := Item{ID: text(row["id"]), TimeMS: at, Text: text(row["content"]), Mode: mode, Color: rgb(color), FontSize: 25}
		if err != nil || !validItem(item) {
			skipped++
			continue
		}
		items = append(items, item)
	}
	if len(rows) > 0 && len(items) == 0 {
		return nil, skipped, schemaError(fmt.Errorf("all QQ items invalid"))
	}
	return sorted(items), skipped, nil
}

func DecodeIQiyi(compressed []byte) ([]Item, int, error) {
	raw, err := io.ReadAll(io.LimitReader(brotli.NewReader(bytes.NewReader(compressed)), MaxPayload+1))
	if err != nil || len(raw) > MaxPayload {
		if err == nil {
			err = fmt.Errorf("inflated payload too large")
		}
		return nil, 0, schemaError(err)
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if bytes.HasPrefix(trimmed, []byte("<")) {
		return decodeIQXML(trimmed)
	}
	return decodeIQProto(raw)
}
func secondsMS(value string) (int64, error) {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || v < 0 || v > 86400 {
		return 0, fmt.Errorf("invalid showTime")
	}
	return int64(v*1000 + .5), nil
}
func decodeIQProto(raw []byte) ([]Item, int, error) {
	fs, err := fields(raw)
	if err != nil {
		return nil, 0, schemaError(err)
	}
	code := ptext(fs, 1)
	if code != "A00000" {
		return nil, 0, schemaError(fmt.Errorf("IQ status %q", code))
	}
	items := make([]Item, 0)
	skipped := 0
	for _, f := range fs {
		if f.tag != 6 || f.wire != 2 {
			continue
		}
		block, err := fields(f.data)
		if err != nil {
			return nil, 0, schemaError(err)
		}
		for _, b := range block {
			if b.tag != 2 || b.wire != 2 {
				continue
			}
			row, err := fields(b.data)
			if err != nil {
				return nil, 0, schemaError(err)
			}
			at := ptext(row, 6)
			if at == "" {
				at = ptext(block, 1)
			}
			ms, err := secondsMS(at)
			item := Item{ID: ptext(row, 1), TimeMS: ms, Text: html.UnescapeString(ptext(row, 2)), Mode: 1, Color: rgb(ptext(row, 8)), FontSize: 25}
			if err != nil || !validItem(item) {
				skipped++
				continue
			}
			items = append(items, item)
			if len(items) > MaxSegmentItems {
				return nil, 0, schemaError(fmt.Errorf("too many items"))
			}
		}
	}
	if len(items) == 0 && skipped > 0 {
		return nil, skipped, schemaError(fmt.Errorf("all IQ items invalid"))
	}
	return sorted(items), skipped, nil
}
func decodeIQXML(raw []byte) ([]Item, int, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	items := make([]Item, 0)
	skipped := 0
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, schemaError(err)
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, 0, schemaError(fmt.Errorf("XML declarations not supported"))
		case xml.StartElement:
			if !rootSeen {
				rootSeen = true
				if t.Name.Local != "danmu" {
					return nil, 0, schemaError(fmt.Errorf("unexpected XML root"))
				}
			}
			if t.Name.Local != "bulletInfo" {
				continue
			}
			var row struct {
				ID    string `xml:"contentId"`
				Text  string `xml:"content"`
				Time  string `xml:"showTime"`
				Color string `xml:"color"`
			}
			if err := decoder.DecodeElement(&row, &t); err != nil {
				return nil, 0, schemaError(err)
			}
			ms, err := secondsMS(row.Time)
			item := Item{ID: row.ID, TimeMS: ms, Text: row.Text, Mode: 1, Color: rgb(row.Color), FontSize: 25}
			if err != nil || !validItem(item) {
				skipped++
				continue
			}
			items = append(items, item)
			if len(items) > MaxSegmentItems {
				return nil, 0, schemaError(fmt.Errorf("too many items"))
			}
		}
	}
	if !rootSeen || (len(items) == 0 && skipped > 0) {
		return nil, skipped, schemaError(fmt.Errorf("invalid IQ XML items"))
	}
	return sorted(items), skipped, nil
}

// Cache decoding is validated independently from TypeScript or SQL types.
func decodeItems(raw []byte) ([]Item, error) {
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if len(items) > MaxSegmentItems {
		return nil, fmt.Errorf("too many cached items")
	}
	for _, item := range items {
		if !validItem(item) {
			return nil, fmt.Errorf("invalid cached item")
		}
	}
	return items, nil
}
