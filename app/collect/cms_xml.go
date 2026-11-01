package collect

import (
	"bytes"
	"cczjVideo/app/model"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html/charset"
)

// Legacy CMS RSS is the same catalogue protocol with different field names.
// Normalize it before the shared mapping, identity and pagination validation.
func normalizeCMSXML(body []byte) ([]byte, error) {
	var envelope struct {
		XMLName xml.Name `xml:"rss"`
		Classes []struct {
			ID         string `xml:"id,attr"`
			Parent     string `xml:"pid,attr"`
			TypeParent string `xml:"type_pid,attr"`
			Name       string `xml:",chardata"`
		} `xml:"class>ty"`
		List *struct {
			Page   int `xml:"page,attr"`
			Pages  int `xml:"pagecount,attr"`
			Size   int `xml:"pagesize,attr"`
			Total  int `xml:"recordcount,attr"`
			Videos []struct {
				Fields []struct {
					XMLName xml.Name
					Value   string `xml:",chardata"`
				} `xml:",any"`
				DL struct {
					Lines []struct {
						Flag  string `xml:"flag,attr"`
						Value string `xml:",chardata"`
					} `xml:"dd"`
				} `xml:"dl"`
			} `xml:"video"`
		} `xml:"list"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode CMS XML: %w", err)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode CMS XML trailer: %w", err)
		}
		switch value := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return nil, fmt.Errorf("unexpected data after CMS XML")
			}
		case xml.Comment, xml.ProcInst:
		default:
			return nil, fmt.Errorf("unexpected document after CMS XML")
		}
	}
	classes := make([]Category, 0, len(envelope.Classes))
	for _, row := range envelope.Classes {
		parent := row.Parent
		if parent == "" {
			parent = row.TypeParent
		}
		classes = append(classes, Category{ID: model.FlexibleString(strings.TrimSpace(row.ID)), Name: strings.TrimSpace(row.Name), Parent: model.FlexibleString(parent)})
	}
	if envelope.List == nil {
		if len(classes) > 0 {
			return json.Marshal(map[string]any{"code": 1, "page": 1, "pagecount": 0, "total": 0, "list": []any{}, "class": classes})
		}
		return nil, fmt.Errorf("CMS XML has no list")
	}
	aliases := map[string]string{"id": "vod_id", "tid": "type_id", "name": "vod_name", "type": "type_name", "pic": "vod_pic", "last": "vod_time", "note": "vod_remarks", "des": "vod_content", "actor": "vod_actor", "director": "vod_director", "year": "vod_year", "area": "vod_area", "lang": "vod_lang", "state": "vod_state", "dt": "vod_play_from"}
	rows := make([]map[string]string, 0, len(envelope.List.Videos))
	for _, video := range envelope.List.Videos {
		row := map[string]string{}
		for _, field := range video.Fields {
			row[field.XMLName.Local] = strings.TrimSpace(field.Value)
			if target := aliases[field.XMLName.Local]; target != "" {
				row[target] = strings.TrimSpace(field.Value)
			}
		}
		var lines, names []string
		for i, line := range video.DL.Lines {
			name := strings.TrimSpace(line.Flag)
			if name == "" {
				name = "line" + strconv.Itoa(i+1)
				if i == 0 && row["vod_play_from"] != "" {
					name = row["vod_play_from"]
				}
			}
			if value := strings.TrimSpace(line.Value); value != "" {
				names = append(names, name)
				lines = append(lines, value)
			}
		}
		if len(lines) > 0 {
			row["vod_play_from"], row["vod_play_url"] = strings.Join(names, "$$$"), strings.Join(lines, "$$$")
		}
		rows = append(rows, row)
	}

	return json.Marshal(map[string]any{"code": 1, "page": envelope.List.Page, "pagecount": envelope.List.Pages, "limit": envelope.List.Size, "total": envelope.List.Total, "list": rows, "class": classes})
}
