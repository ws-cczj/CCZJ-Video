package collect

import (
	"errors"
	"strings"
	"testing"
)

// decodeDoc is a small helper so the resolver tests describe documents, not the
// decoding ceremony.
func decodeDoc(t *testing.T, body string) any {
	t.Helper()
	doc, err := decodeJSONDocument([]byte(body))
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	return doc
}

func TestLookupPathResolvesPresentSegments(t *testing.T) {
	doc := decodeDoc(t, `{"code":0,"items":["a","b"],"data":{"list":[{"vid":"1"},{"vid":"2"}]},"result":[{"list":{"title":"嵌套"}},{"list":null}],"null_node":null}`)

	cases := []struct {
		name string
		path string
		kind string
	}{
		{"root object key", "code", "number"},
		{"top level array", "items", "array"},
		{"nested object then array", "data.list", "array"},
		{"array index then object key", "result.0.list.title", "string"},
		{"array index of empty object", "result.1.list", "null"},
		{"present but null", "null_node", "null"},
	}
	for _, tc := range cases {
		node, err := lookupPath(doc, tc.path)
		if err != nil {
			t.Errorf("%s: lookupPath(%q) error = %v", tc.name, tc.path, err)
			continue
		}
		if got := jsonKindName(node); got != tc.kind {
			t.Errorf("%s: lookupPath(%q) kind = %q, want %q", tc.name, tc.path, got, tc.kind)
		}
	}
}

func TestLookupPathErrors(t *testing.T) {
	doc := decodeDoc(t, `{"code":7,"page":{"count":3},"items":[{"id":1}],"data":{"list":[]}}`)

	cases := []struct {
		name  string
		path  string
		want  error
		wantS string
	}{
		{"missing key", "data.missing", ErrPathNotFound, "object has no such key"},
		{"missing nested key", "nodata.list", ErrPathNotFound, "object has no such key"},
		{"index out of range", "items.5", ErrPathNotFound, "out of range"},
		{"negative index", "items.-1", ErrPathType, "not an array index"},
		{"word as index", "items.name", ErrPathType, "not an array index"},
		{"float as index", "items.0.0", ErrPathNotFound, ""},
		{"descend into number", "code.list", ErrPathType, "cannot read"},
		{"descend into object array", "items.0.id.x", ErrPathType, "cannot read"},
		{"empty path", "", ErrPathSyntax, "empty path"},
		{"double dot", "data..list", ErrPathSyntax, "empty segment"},
		{"leading dot", ".data", ErrPathSyntax, "empty segment"},
		{"trailing dot", "data.", ErrPathSyntax, "empty segment"},
		{"single dot", ".", ErrPathSyntax, "empty segment"},
		{"overlong path", "data." + strings.Repeat("x", 600), ErrPathSyntax, "longer than"},
		{"too many segments", strings.TrimSuffix(strings.Repeat("a.", maxResponsePathSegments+2), "."), ErrPathSyntax, "more than"},
		{"huge index", "items.99999999999999999999999", ErrPathType, "not an array index"},
	}
	for _, tc := range cases {
		// The resolver must report a typed error instead of panicking, and every
		// branch has to be reachable through errors.Is.
		node, err := lookupPath(doc, tc.path)
		if err == nil {
			t.Errorf("%s: lookupPath(%q) = %v, want error", tc.name, tc.path, node)
			continue
		}
		var pathErr *PathError
		if !errors.As(err, &pathErr) {
			t.Errorf("%s: lookupPath(%q) error %v is not a *PathError", tc.name, tc.path, err)
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: lookupPath(%q) error = %v, want %v", tc.name, tc.path, err, tc.want)
		}
		if tc.wantS != "" && !strings.Contains(err.Error(), tc.wantS) {
			t.Errorf("%s: lookupPath(%q) error = %q, want it to mention %q", tc.name, tc.path, err.Error(), tc.wantS)
		}
	}
}

func TestLookupPathNumericKeysAreArrayIndexes(t *testing.T) {
	// Arrays index by digits; objects keep "0" as an ordinary key.
	doc := decodeDoc(t, `{"zero":{"0":"对象里的键"},"arr":["数组第零项"]}`)
	if got, err := lookupPath(doc, "zero.0"); err != nil {
		t.Fatalf("object numeric key: %v", err)
	} else if got != "对象里的键" {
		t.Fatalf("object numeric key = %v", got)
	}
	if got, err := lookupPath(doc, "arr.0"); err != nil {
		t.Fatalf("array index: %v", err)
	} else if got != "数组第零项" {
		t.Fatalf("array index = %v", got)
	}
}

func TestDecodeJSONDocumentRejectsTrailingData(t *testing.T) {
	if _, err := decodeJSONDocument([]byte(`{"code":1} trailing`)); err == nil {
		t.Fatal("trailing data accepted as one document")
	}
	if _, err := decodeJSONDocument([]byte(``)); err == nil {
		t.Fatal("empty body accepted")
	}
	doc, err := decodeJSONDocument([]byte(`{"id":12345678901234567890}`))
	if err != nil {
		t.Fatalf("valid body rejected: %v", err)
	}
	// UseNumber keeps long ids exact: float64 would round this one.
	if got, err := lookupPath(doc, "id"); err != nil {
		t.Fatalf("id: %v", err)
	} else if text, _ := scalarText(got); text != "12345678901234567890" {
		t.Fatalf("id text = %q", text)
	}
}

func TestScalarTextNormalizesCodeValues(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		path     string
		wantText string
		wantOK   bool
	}{
		{"number", `{"code":1}`, "code", "1", true},
		{"string number", `{"code":"1"}`, "code", "1", true},
		{"boolean true", `{"ok":true}`, "ok", "true", true},
		{"boolean false", `{"ok":false}`, "ok", "false", true},
		{"text", `{"code":"ERR_AUTH"}`, "code", "ERR_AUTH", true},
		{"zero", `{"code":0}`, "code", "0", true},
		{"null", `{"code":null}`, "code", "", false},
		{"object", `{"code":{"a":1}}`, "code", "", false},
		{"array", `{"code":[1]}`, "code", "", false},
	}
	for _, tc := range cases {
		node, err := lookupPath(decodeDoc(t, tc.body), tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		text, ok := scalarText(node)
		if text != tc.wantText || ok != tc.wantOK {
			t.Errorf("%s: scalarText = (%q, %v), want (%q, %v)", tc.name, text, ok, tc.wantText, tc.wantOK)
		}
	}
}

func TestResponseShapeAcceptsCode(t *testing.T) {
	cases := []struct {
		shape ResponseShape
		text  string
		want  bool
	}{
		{ResponseShape{OKCodes: []string{"0"}}, "0", true},
		{ResponseShape{OKCodes: []string{"0"}}, "1", false},
		{ResponseShape{OKCodes: []string{"1"}}, "1.0", true},
		{ResponseShape{OKCodes: []string{"1"}}, "true", false},
		{ResponseShape{OKCodes: []string{"true", "ok"}}, "ok", true},
		{ResponseShape{}, "1", true}, // ok_codes defaults to the MAC-CMS code
		{ResponseShape{OKCodes: []string{}}, "", false},
	}
	for i, tc := range cases {
		shape := tc.shape.withDefaults()
		if got := shape.acceptsCode(tc.text); got != tc.want {
			t.Errorf("case %d: acceptsCode(%q) = %v, want %v", i, tc.text, got, tc.want)
		}
	}
}

func TestResponseShapeDefaultsDoNotMutateConfig(t *testing.T) {
	declared := &ResponseShape{ListPath: "data.list", OKCodes: []string{"0"}}
	resolved := declared.withDefaults()
	if declared.ListPath != "data.list" || declared.CodePath != "" || len(declared.OKCodes) != 1 {
		t.Fatalf("receiver changed: %+v", *declared)
	}
	wants := []struct{ got, want string }{
		{resolved.ListPath, "data.list"},
		{resolved.CodePath, "code"},
		{resolved.MsgPath, "msg"},
		{resolved.PagePath, "page"},
		{resolved.PagecountPath, "pagecount"},
		{resolved.TotalPath, "total"},
	}
	for _, want := range wants {
		if want.got != want.want {
			t.Errorf("default path = %q, want %q", want.got, want.want)
		}
	}
	// The defaulted copy must not alias the caller's slice.
	resolved.OKCodes[0] = "mutated"
	if declared.OKCodes[0] != "0" {
		t.Fatalf("ok_codes slice is shared")
	}
}

func TestDecodeShapedEnvelopeTable(t *testing.T) {
	shape := &ResponseShape{
		ListPath:      "data.list",
		CodePath:      "code",
		OKCodes:       []string{"0"},
		MsgPath:       "data.message",
		PagePath:      "data.pn",
		PagecountPath: "data.pages",
		TotalPath:     "data.total",
	}

	t.Run("nested success", func(t *testing.T) {
		result, err := decodeShapedEnvelope([]byte(`{"code":"0","data":{"message":"ok","pn":2,"pages":4,"total":118,"list":[{"vid":"a1","name":"影片甲"},{"vid":"a2","name":"影片乙"}]}}`), shape, map[string]string{"vid": "vod_id", "name": "vod_name"})
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !result.OK || result.CodeText != "0" || result.Msg != "ok" {
			t.Fatalf("envelope fields = %+v", result)
		}
		if result.Page.Int() != 2 || result.Pagecount.Int() != 4 || result.Total.Int() != 118 {
			t.Fatalf("paging = %+v", result)
		}
		if len(result.List) != 2 || result.List[0].VodId.String() != "a1" || result.List[1].VodName != "影片乙" {
			t.Fatalf("list = %+v", result.List)
		}
	})

	t.Run("numeric boolean and string codes all match", func(t *testing.T) {
		for _, body := range []string{`{"code":0,"data":{"list":[]}}`, `{"code":"0","data":{"list":[]}}`, `{"code":"00","data":{"list":[]}}`} {
			result, err := decodeShapedEnvelope([]byte(body), shape, nil)
			if err != nil {
				t.Fatalf("%s: %v", body, err)
			}
			if !result.OK || len(result.List) != 0 {
				t.Fatalf("%s: %+v", body, result)
			}
		}
		booleanShape := &ResponseShape{ListPath: "data", CodePath: "ok", OKCodes: []string{"true"}}
		result, err := decodeShapedEnvelope([]byte(`{"ok":true,"data":[{"name":"布尔码"}]}`), booleanShape, map[string]string{"name": "vod_name"})
		if err != nil {
			t.Fatalf("boolean code: %v", err)
		}
		if !result.OK || result.CodeText != "true" || result.Code != 0 {
			// Code stays the numeric field it always was: a boolean code has no
			// integer form, so only CodeText carries it.
			t.Fatalf("boolean code result = %+v", result)
		}
	})

	t.Run("failure surfaces configured msg", func(t *testing.T) {
		result, err := decodeShapedEnvelope([]byte(`{"code":"500","data":{"message":"签名失效","list":[]}}`), shape, nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if result.OK {
			t.Fatalf("code 500 accepted as success")
		}
		if result.Msg != "签名失效" {
			t.Fatalf("msg = %q", result.Msg)
		}
	})

	t.Run("missing code field is not a failure", func(t *testing.T) {
		result, err := decodeShapedEnvelope([]byte(`{"data":{"list":[{"name":"无码状态"}]}}`), shape, map[string]string{"name": "vod_name"})
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !result.OK || result.CodeText != "" || len(result.List) != 1 {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("null code is not a failure", func(t *testing.T) {
		result, err := decodeShapedEnvelope([]byte(`{"code":null,"data":{"list":[]}}`), shape, nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !result.OK {
			t.Fatalf("null code rejected")
		}
	})

	t.Run("single object under list path is one record", func(t *testing.T) {
		single := &ResponseShape{ListPath: "data.video"}
		result, err := decodeShapedEnvelope([]byte(`{"code":1,"data":{"video":{"vod_id":"9","vod_name":"单对象"}}}`), single, nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(result.List) != 1 || result.List[0].VodName != "单对象" {
			t.Fatalf("list = %+v", result.List)
		}
	})

	t.Run("null list is an empty page", func(t *testing.T) {
		result, err := decodeShapedEnvelope([]byte(`{"code":0,"data":{"list":null}}`), shape, nil)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(result.List) != 0 {
			t.Fatalf("list = %+v", result.List)
		}
	})

	t.Run("non scalar code errors", func(t *testing.T) {
		_, err := decodeShapedEnvelope([]byte(`{"code":{"v":1},"data":{"list":[]}}`), shape, nil)
		if !errors.Is(err, ErrPathType) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("list of wrong type errors", func(t *testing.T) {
		_, err := decodeShapedEnvelope([]byte(`{"code":"0","data":{"list":"不是数组"}}`), shape, nil)
		if !errors.Is(err, ErrPathType) {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(err.Error(), "data.list") {
			t.Fatalf("error must name the configured path: %v", err)
		}
	})

	t.Run("missing list path errors", func(t *testing.T) {
		_, err := decodeShapedEnvelope([]byte(`{"code":"0","data":{"message":"没有列表"}}`), shape, nil)
		if !errors.Is(err, ErrPathNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDecodeShapedEnvelopeSkipsUnusableRecords(t *testing.T) {
	// A malformed record must not sink the whole page, matching the tolerance of
	// the historical ParseVideosWithMapping loop.
	body := `{"code":1,"list":[{"vod_name":"可用"},"不是对象",123]}`
	result, err := decodeShapedEnvelope([]byte(body), &ResponseShape{}, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.List) != 1 || result.List[0].VodName != "可用" {
		t.Fatalf("list = %+v", result.List)
	}
}

func TestParseVideosFromValueAppliesMappingToArray(t *testing.T) {
	doc := decodeDoc(t, `{"items":[{"headline":"映射标题","thumb":"https://c/1.jpg"},{"vod_name":"别名标题","vod_pic":"https://c/2.jpg"}]}`)
	node, err := lookupPath(doc, "items")
	if err != nil {
		t.Fatal(err)
	}
	videos, err := parseVideosFromValue(node, map[string]string{"headline": "vod_name", "thumb": "vod_pic"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(videos) != 2 {
		t.Fatalf("videos = %d, want 2", len(videos))
	}
	if videos[0].VodName != "映射标题" || videos[0].VodPic != "https://c/1.jpg" {
		t.Fatalf("mapped record = %+v", videos[0])
	}
	if videos[1].VodName != "别名标题" || videos[1].VodPic != "https://c/2.jpg" {
		t.Fatalf("alias record = %+v", videos[1])
	}
}

func TestCanonicalizeFieldMappingOnlyRotatesUnambiguousPairs(t *testing.T) {
	cases := []struct {
		name  string
		given map[string]string
		want  map[string]string
	}{
		{"declared direction wins", map[string]string{"headline": "vod_name"}, map[string]string{"headline": "vod_name"}},
		{"pack direction rotates", map[string]string{"vod_name": "headline"}, map[string]string{"headline": "vod_name"}},
		{"both canonical stays declared", map[string]string{"vod_name": "vod_actor"}, map[string]string{"vod_name": "vod_actor"}},
		{"neither canonical stays", map[string]string{"foo": "bar"}, map[string]string{"foo": "bar"}},
		{"empty target dropped", map[string]string{"vod_name": " "}, map[string]string{}},
		{"alias spelling is not canonical", map[string]string{"vod_name": "title"}, map[string]string{"title": "vod_name"}},
	}
	for _, tc := range cases {
		got := canonicalizeFieldMapping(tc.given)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
	if canonicalizeFieldMapping(nil) != nil {
		t.Error("nil mapping must stay nil-shaped for the caller")
	}
	// The canonical set is exactly what model.Video exposes as JSON names.
	for _, name := range []string{"vod_name", "vod_pic", "vod_id", "vod_play_url", "type_name", "vod_class"} {
		if !canonicalVideoFields[name] {
			t.Errorf("%q must be a canonical field", name)
		}
	}
	if canonicalVideoFields["title"] || canonicalVideoFields["cover"] {
		t.Error("source-side spellings are not canonical fields")
	}
}
