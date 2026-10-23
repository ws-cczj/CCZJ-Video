// Package readingstate owns the durable reading-state protocol independently of
// its SQLite adapter and the novel/comic presentation stores.
package readingstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrInvalidRecord = errors.New("invalid reading record")

func invalidRecord(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecord, fmt.Sprintf(format, args...))
}

type Record struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

type Change struct {
	Record
	Delete bool `json:"delete"`
}

func ValidateKind(kind string) error {
	if kind != "novel" && kind != "comic" {
		return invalidRecord("unknown reading kind %q", kind)
	}
	return nil
}

func ValidateKey(kind, bucket, key string) error {
	if err := ValidateKind(kind); err != nil {
		return err
	}
	switch bucket {
	case "shelf", "progress":
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 || parts[0] != kind || parts[1] == "" || parts[2] == "" || len(key) > 16384 {
			return invalidRecord("invalid reading identity")
		}
	case "preferences":
		if key != "default" {
			return invalidRecord("invalid preferences identity")
		}
	default:
		return invalidRecord("unknown reading bucket %q", bucket)
	}
	return nil
}

// Validate checks the stable persisted shape, while permitting future optional
// presentation fields. A missing source is not invalid: offline history survives.
func (r Record) Validate(kind string) error {
	if err := ValidateKey(kind, r.Bucket, r.Key); err != nil {
		return err
	}
	if len(r.Value) > 2<<20 {
		return invalidRecord("reading record exceeds size limit")
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(r.Value), &value); err != nil || value == nil {
		return invalidRecord("reading record must contain a JSON object")
	}
	integer := func(field string, min float64) bool {
		n, ok := value[field].(float64)
		return ok && !math.IsInf(n, 0) && !math.IsNaN(n) && n >= min && n <= 9007199254740991 && math.Trunc(n) == n
	}
	number := func(field string, min, max float64) bool {
		n, ok := value[field].(float64)
		return ok && !math.IsInf(n, 0) && !math.IsNaN(n) && n >= min && n <= max
	}
	switch r.Bucket {
	case "shelf":
		if !integer("itemId", 1) || !number("addedAt", 0, 9007199254740991) {
			return invalidRecord("invalid shelf identity or timestamp")
		}
		ref := value
		if kind == "novel" {
			nested, ok := value["item"].(map[string]any)
			if !ok {
				return invalidRecord("missing novel snapshot")
			}
			ref = nested
			if nested["id"] != value["itemId"] {
				return invalidRecord("shelf snapshot ID mismatch")
			}
			if value["key"] != r.Key {
				return invalidRecord("shelf identity mismatch")
			}
		}
		if ref["kind"] != kind {
			return invalidRecord("shelf kind mismatch")
		}
		source, sourceOK := ref["sourceKey"].(string)
		remote, remoteOK := ref["remoteId"].(string)
		if !sourceOK || !remoteOK || kind+"\x00"+source+"\x00"+remote != r.Key {
			return invalidRecord("shelf identity mismatch")
		}
		if _, ok := ref["title"].(string); !ok {
			return invalidRecord("missing shelf title")
		}
	case "progress":
		if !integer("itemId", 1) || !number("updatedAt", 0, 9007199254740991) {
			return invalidRecord("invalid progress identity or timestamp")
		}
		if kind == "novel" {
			if !integer("chapterId", 0) || !integer("chapterOrd", 0) || !number("ratio", 0, 1) {
				return invalidRecord("invalid novel progress")
			}
		} else {
			if !integer("episodeId", 0) || !integer("episodeOrd", 0) || !integer("page", 0) || !number("offset", 0, 1) {
				return invalidRecord("invalid comic progress")
			}
		}
	case "preferences":
		// Preferences are normalized by each mode; enforce object/size here so
		// future readers may preserve fields they do not yet understand.
	}
	return nil
}
