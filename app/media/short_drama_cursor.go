package media

import (
	"cczjVideo/app/apperror"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const shortDramaCursorPrefix = "scope1:"

type dramaCategoryCursor struct {
	ID   string `json:"i"`
	Page int    `json:"p"`
	Done bool   `json:"d,omitempty"`
}
type dramaCursor struct {
	Signature string                `json:"s"`
	Page      int                   `json:"p"`
	Offset    int                   `json:"o"`
	Types     []dramaCategoryCursor `json:"t"`
}

func readDramaCursor(req ShortDramaPageReq, apiURL string, ids []string) (*dramaCursor, error) {
	sort.Slice(ids, func(i, j int) bool {
		a, ea := strconv.ParseUint(ids[i], 10, 64)
		b, eb := strconv.ParseUint(ids[j], 10, 64)
		if ea == nil && eb == nil {
			if a != b {
				return a < b
			}
			return ids[i] < ids[j]
		}
		if (ea == nil) != (eb == nil) {
			return ea == nil
		}
		return ids[i] < ids[j]
	})
	if len(ids) > 128 {
		return nil, apperror.New(apperror.Corrupt, "短剧分类数量超出范围")
	}
	raw, err := json.Marshal([]any{req.SourceKey, apiURL, req.Keyword, req.RecentDays, ids})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	signature := hex.EncodeToString(digest[:])
	cursor := &dramaCursor{Signature: signature, Page: req.Page}
	for _, id := range ids {
		cursor.Types = append(cursor.Types, dramaCategoryCursor{ID: id, Page: 1})
	}
	if req.CategoryID == "" {
		return cursor, nil
	}
	if !strings.HasPrefix(req.CategoryID, shortDramaCursorPrefix) {
		for _, id := range ids {
			if id == req.CategoryID {
				cursor.Types = []dramaCategoryCursor{{ID: id, Page: req.Page}}
				return cursor, nil
			}
		}
		return nil, apperror.New(apperror.Validation, "来源短剧分类已变化，请刷新")
	}
	if len(req.CategoryID) > 64<<10 {
		return nil, apperror.New(apperror.Validation, "短剧游标超出范围")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(req.CategoryID, shortDramaCursorPrefix))
	if err == nil {
		err = json.Unmarshal(data, &cursor)
	}
	if err != nil || cursor == nil || cursor.Signature != signature || cursor.Page != req.Page || (len(cursor.Types) == 0 || len(cursor.Types) > len(ids)) || cursor.Offset < 0 || cursor.Offset >= len(cursor.Types) {
		return nil, apperror.New(apperror.Validation, "短剧分页已变化，请刷新")
	}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	seen := map[string]bool{}
	for _, row := range cursor.Types {
		if !known[row.ID] || seen[row.ID] || row.Page < 1 || row.Page > 1001 {
			return nil, apperror.New(apperror.Validation, "短剧分页无效")
		}
		seen[row.ID] = true
	}

	return cursor, nil
}

func (c *dramaCursor) next() (string, bool, error) {
	more := false
	for _, row := range c.Types {
		if !row.Done {
			more = true
			break
		}
	}
	c.Page++
	raw, err := json.Marshal(c)
	return shortDramaCursorPrefix + base64.RawURLEncoding.EncodeToString(raw), more, err
}
