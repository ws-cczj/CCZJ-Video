package collect

import (
	"bytes"
	"cczjVideo/app/apperror"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ResponseShape mirrors the optional "response" block of a v2 strategy document
// (docs/source-strategy-v2.schema.json). It describes where the payload lives
// inside a non-MAC-CMS envelope instead of assuming the flat
// {code,page,pagecount,limit,total,msg,list} shape.
//
// A nil *ResponseShape means "the MAC-CMS envelope, exactly as before": the
// fetch path keeps its historical behaviour unless the config declares one.
type ResponseShape struct {
	ListPath      string   `json:"list_path,omitempty"`
	CodePath      string   `json:"code_path,omitempty"`
	OKCodes       []string `json:"ok_codes,omitempty"`
	MsgPath       string   `json:"msg_path,omitempty"`
	PagePath      string   `json:"page_path,omitempty"`
	PagecountPath string   `json:"pagecount_path,omitempty"`
	TotalPath     string   `json:"total_path,omitempty"`
}

// MAC-CMS envelope names double as the defaults for a declared response block,
// so {"response":{"list_path":"data.list"}} still reads code/msg/page/... from
// the top level the way every CMS source does.
const (
	macListPath      = "list"
	macCodePath      = "code"
	macMsgPath       = "msg"
	macPagePath      = "page"
	macPagecountPath = "pagecount"
	macTotalPath     = "total"
	macLimitPath     = "limit"
	macOKCode        = "1"
)

// Path resolution is deliberately bounded: a pack document is user input, and a
// 10 MB path must not be walked or split into a million segments.
const (
	maxResponsePathLength   = 512
	maxResponsePathSegments = 64
)

// Path errors are typed so callers can branch on them (a missing code field is
// information, a malformed path is a configuration bug) and so a bad declarative
// config degrades to an error instead of a panic.
var (
	ErrPathSyntax   = errors.New("invalid response path")
	ErrPathNotFound = errors.New("response path not present")
	ErrPathType     = errors.New("response path type mismatch")
)

// PathError says which segment of which configured path could not be resolved.
type PathError struct {
	Path    string
	Segment string
	Err     error
}

func (e *PathError) Error() string {
	if e.Segment == "" {
		return fmt.Sprintf("response path %q: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("response path %q: segment %q: %v", e.Path, e.Segment, e.Err)
}

func (e *PathError) Unwrap() error { return e.Err }

// withDefaults returns a copy of the shape with every unset path and the success
// code list filled in with the MAC-CMS names. The receiver is never mutated, so
// a strategy can hand the same shape to several concurrent fetches.
func (s *ResponseShape) withDefaults() *ResponseShape {
	if s == nil {
		return nil
	}
	out := *s
	out.ListPath = responsePathOrDefault(out.ListPath, macListPath)
	out.CodePath = responsePathOrDefault(out.CodePath, macCodePath)
	out.MsgPath = responsePathOrDefault(out.MsgPath, macMsgPath)
	out.PagePath = responsePathOrDefault(out.PagePath, macPagePath)
	out.PagecountPath = responsePathOrDefault(out.PagecountPath, macPagecountPath)
	out.TotalPath = responsePathOrDefault(out.TotalPath, macTotalPath)
	if len(out.OKCodes) == 0 {
		out.OKCodes = []string{macOKCode}
	} else {
		codes := make([]string, len(out.OKCodes))
		for i, code := range out.OKCodes {
			codes[i] = strings.TrimSpace(code)
		}
		out.OKCodes = codes
	}
	return &out
}

func responsePathOrDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

// acceptsCode reports whether the code the source returned counts as success.
// Codes are compared as text, so 1, "1" and true all survive the trip through
// scalarText; a numeric code and a numeric ok_code also compare numerically, so
// 1.0 still matches the declared "1".
func (s *ResponseShape) acceptsCode(text string) bool {
	for _, want := range s.OKCodes {
		if text == want {
			return true
		}
		if numericEqual(text, want) {
			return true
		}
	}
	return false
}

func numericEqual(a, b string) bool {
	fa, err := strconv.ParseFloat(strings.TrimSpace(a), 64)
	if err != nil {
		return false
	}
	fb, err := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err != nil {
		return false
	}
	return fa == fb
}

// lookupPath walks a dotted JSON path over a decoded document. Objects are
// indexed by key, arrays by a decimal index ("result.0.list"), and any segment
// that cannot be resolved yields a *PathError wrapping ErrPathSyntax,
// ErrPathNotFound or ErrPathType. It never panics and never reflects.
func lookupPath(doc any, path string) (any, error) {
	segments, err := splitResponsePath(path)
	if err != nil {
		return nil, err
	}
	node := doc
	for _, segment := range segments {
		switch current := node.(type) {
		case map[string]any:
			next, ok := current[segment]
			if !ok {
				return nil, &PathError{Path: path, Segment: segment, Err: fmt.Errorf("%w: object has no such key", ErrPathNotFound)}
			}
			node = next
		case []any:
			index, ok := arrayIndex(segment)
			if !ok {
				return nil, &PathError{Path: path, Segment: segment, Err: fmt.Errorf("%w: %q is not an array index", ErrPathType, segment)}
			}
			if index >= len(current) {
				return nil, &PathError{Path: path, Segment: segment, Err: fmt.Errorf("%w: index %d out of range, array holds %d item(s)", ErrPathNotFound, index, len(current))}
			}
			node = current[index]
		default:
			return nil, &PathError{Path: path, Segment: segment, Err: fmt.Errorf("%w: cannot read %q from %s", ErrPathType, segment, jsonKindName(node))}
		}
	}
	return node, nil
}

// splitResponsePath validates the path itself before any document is consulted,
// so a malformed declaration ("data..list", "list.", "../../etc/passwd") always
// reports as a syntax error rather than as an incidental missing key.
func splitResponsePath(path string) ([]string, error) {
	if len(path) > maxResponsePathLength {
		return nil, &PathError{Path: path, Err: fmt.Errorf("%w: longer than %d bytes", ErrPathSyntax, maxResponsePathLength)}
	}
	if path == "" {
		return nil, &PathError{Path: path, Err: fmt.Errorf("%w: empty path", ErrPathSyntax)}
	}
	segments := strings.Split(path, ".")
	if len(segments) > maxResponsePathSegments {
		return nil, &PathError{Path: path, Err: fmt.Errorf("%w: more than %d segments", ErrPathSyntax, maxResponsePathSegments)}
	}
	for i, segment := range segments {
		if segment == "" {
			return nil, &PathError{Path: path, Err: fmt.Errorf("%w: empty segment at index %d", ErrPathSyntax, i)}
		}
	}
	return segments, nil
}

// lookupOptionalPath reads a path that a document may legitimately not carry:
// only "the key is not there" is tolerated, so an envelope without a msg field
// still decodes. A path that cannot be walked at all is a broken pack document
// and must surface instead of being silently dropped on every later page.
func lookupOptionalPath(doc any, path string) (any, bool, error) {
	node, err := lookupPath(doc, path)
	if err == nil {
		return node, true, nil
	}
	if errors.Is(err, ErrPathNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

// arrayIndex accepts only a run of ASCII digits, so "-1", "+1", "1e3" and any
// index too large for int fail as a type error instead of wrapping around.
func arrayIndex(segment string) (int, bool) {
	if segment == "" {
		return 0, false
	}
	for i := 0; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return 0, false
		}
	}
	index, err := strconv.Atoi(segment)
	if err != nil {
		return 0, false
	}
	if index < 0 {
		return 0, false
	}
	return index, true
}

func jsonKindName(node any) string {
	switch node.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, int, int64:
		return "number"
	default:
		return fmt.Sprintf("%T", node)
	}
}

// scalarText normalises a JSON scalar into the text used for ok_codes and msg.
// Objects and arrays are not scalars, so they report ok == false.
func scalarText(node any) (string, bool) {
	switch v := node.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'g', -1, 64), true
	default:
		return "", false
	}
}

func flexFromNode(node any) FlexInt {
	text, ok := scalarText(node)
	if !ok {
		return 0
	}
	return FlexInt(intFromText(text))
}

// intFromText reads an integer out of a JSON scalar without ever failing:
// pagination counters that cannot be understood read as 0, which the engine's
// page loop treats as "one page only" rather than as an unbounded run.
func intFromText(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	if n, err := strconv.Atoi(trimmed); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil && f > 0 {
		// A counter reported as 7.0 is still page count 7; a negative or huge
		// value clamps to 0 so the page loop cannot run away.
		if f >= float64(maxIntAsFloat) {
			return 0
		}
		return int(f)
	}
	return 0
}

const maxIntAsFloat = 1 << 30

// decodeJSONDocument parses a body into map[string]any / []any while keeping
// numbers exact, so a vod_id of 12345678901234567890 survives as text.
func decodeJSONDocument(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var doc any
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unexpected data after JSON document")
	}
	return doc, nil
}

// decodeShapedEnvelope reads one response body through a declared envelope.
//
// The success verdict is decided before the list is read: an error page that
// carries no list at all must still report the source's own message instead of a
// path error about a missing list.
func decodeShapedEnvelope(body []byte, shape *ResponseShape, fieldMapping map[string]string) (*FetchResult, error) {
	if shape == nil {
		return nil, fmt.Errorf("response shape is nil")
	}
	shape = shape.withDefaults()

	doc, err := decodeJSONDocument(body)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, fmt.Sprintf("parse json with mapping (body preview: %s)", bodyPreview(body)))
	}

	result := &FetchResult{}
	codeNode, err := lookupPath(doc, shape.CodePath)
	switch {
	case err == nil && codeNode != nil:
		text, ok := scalarText(codeNode)
		if !ok {
			return nil, &PathError{Path: shape.CodePath, Err: fmt.Errorf("%w: code must be a scalar, got %s", ErrPathType, jsonKindName(codeNode))}
		}
		result.CodeText = text
		result.Code = intFromText(text)
		result.OK = shape.acceptsCode(text)
	case err == nil || errors.Is(err, ErrPathNotFound):
		// A document that carries no code field, or a null where the shape says
		// the code lives, cannot contradict ok_codes: the request already answered
		// HTTP 200, so treat it as success rather than inventing a failure the
		// source never reported.
		result.OK = true
	default:
		// A malformed configured path is a broken pack document, not a source
		// outage: say so instead of parsing every later page against it.
		return nil, err
	}

	if node, found, err := lookupOptionalPath(doc, shape.MsgPath); err == nil && found {
		if text, ok := scalarText(node); ok {
			result.Msg = text
		}
	} else if err != nil {
		return nil, err
	}
	if node, found, err := lookupOptionalPath(doc, shape.PagePath); err == nil && found {
		result.Page = flexFromNode(node)
	} else if err != nil {
		return nil, err
	}
	if node, found, err := lookupOptionalPath(doc, shape.PagecountPath); err == nil && found {
		result.Pagecount = flexFromNode(node)
	} else if err != nil {
		return nil, err
	}
	if node, found, err := lookupOptionalPath(doc, shape.TotalPath); err == nil && found {
		result.Total = flexFromNode(node)
	} else if err != nil {
		return nil, err
	}
	// limit has no configurable path (the schema does not carry one), so it is
	// only ever read from the MAC-CMS top level and stays informational.
	if limitNode, err := lookupPath(doc, macLimitPath); err == nil {
		result.Limit = flexFromNode(limitNode)
	}

	if !result.OK {
		return result, nil
	}

	listNode, err := lookupPath(doc, shape.ListPath)
	if err != nil {
		return nil, fmt.Errorf("list path %q: %w", shape.ListPath, err)
	}
	videos, err := parseVideosFromValue(listNode, fieldMapping)
	if err != nil {
		return nil, fmt.Errorf("list path %q: %w", shape.ListPath, err)
	}
	result.List = videos
	return result, nil
}
