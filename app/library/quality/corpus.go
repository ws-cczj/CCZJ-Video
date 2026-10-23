// Package quality owns reproducible reading-source identity, catalog and content
// checks. Coverage is reported separately: a specialist source is not defective
// merely because it does not carry a popular title from another genre.
package quality

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const Version = "reading-quality.v1"

//go:embed benchmark.v1.json
var bundled []byte

//go:embed bulk.v1.json
var bulk []byte

// AuditCorpus combines the frozen online metadata with curated catalog anchors,
// scoped to exactly one mode. It is independent of CLI benchmark denominators.
func AuditCorpus(kind string) (Corpus, error) {
	c, err := ParseCorpus(bulk)
	if err != nil {
		return c, err
	}
	curated, err := DefaultCorpus()
	if err != nil {
		return c, err
	}
	out := Corpus{Version: Version, UpdatedAt: c.UpdatedAt}
	seen := map[string]bool{}
	for _, list := range [][]Work{curated.Works, c.Works} {
		for _, w := range list {
			if w.Kind != kind {
				continue
			}
			key := w.Kind + "\x00" + Normalize(w.Title) + "\x00" + strings.Join(w.Authors, "\x00")
			if !seen[key] {
				seen[key] = true
				out.Works = append(out.Works, w)
			}
		}
	}
	return out, out.Validate()
}

type Corpus struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updatedAt"`
	Works     []Work `json:"works"`
}
type Work struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"`
	Title           string            `json:"title"`
	Aliases         []string          `json:"aliases"`
	Authors         []string          `json:"authors"`
	Tags            []string          `json:"tags"`
	Language        string            `json:"language"`
	SearchLanguages []string          `json:"searchLanguages,omitempty"`
	SearchTitles    map[string]string `json:"searchTitles,omitempty"`
	Layout          string            `json:"layout"`
	Edition         string            `json:"edition"`
	Selection       string            `json:"selection"`
	References      []Reference       `json:"references"`
	Catalog         *Catalog          `json:"catalog,omitempty"`
}
type Reference struct {
	URL       string `json:"url"`
	CheckedAt string `json:"checkedAt"`
	Role      string `json:"role"`
}
type Catalog struct {
	Unit       string   `json:"unit"`
	Count      int      `json:"count,omitempty"`
	Scope      string   `json:"scope"`
	Status     string   `json:"status"`
	VerifiedAt string   `json:"verifiedAt"`
	URL        string   `json:"url"`
	Anchors    []Anchor `json:"anchors"`
}
type Anchor struct {
	Number float64 `json:"number"`
	Title  string  `json:"title"`
	Volume string  `json:"volume,omitempty"`
}

func DefaultCorpus() (Corpus, error) { return ParseCorpus(bundled) }
func ParseCorpus(raw []byte) (Corpus, error) {
	var c Corpus
	if len(raw) > 64<<20 {
		return c, fmt.Errorf("benchmark exceeds 64 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("benchmark contains trailing data")
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c Corpus) Validate() error {
	if c.Version != Version || len(c.Works) == 0 || len(c.Works) > 50000 {
		return fmt.Errorf("invalid benchmark version or work count")
	}
	if _, err := time.Parse(time.DateOnly, c.UpdatedAt); err != nil {
		return fmt.Errorf("invalid benchmark date")
	}
	seen := map[string]bool{}
	for _, w := range c.Works {
		if w.ID == "" || seen[w.ID] || w.Title == "" || len(w.Title) > 600 || len(w.Tags) == 0 || len(w.References) == 0 || w.Selection == "" || w.Edition == "" {
			return fmt.Errorf("incomplete benchmark work %q", w.ID)
		}
		seen[w.ID] = true
		if w.Kind != "novel" && w.Kind != "comic" {
			return fmt.Errorf("invalid kind for %s", w.ID)
		}
		if (w.Kind == "novel" && w.Layout != "text") || (w.Kind == "comic" && w.Layout != "strip" && w.Layout != "page") {
			return fmt.Errorf("invalid layout for %s", w.ID)
		}
		if w.Language == "" {
			return fmt.Errorf("missing language for %s", w.ID)
		}
		for _, r := range w.References {
			if !publicURL(r.URL) {
				return fmt.Errorf("invalid reference for %s", w.ID)
			}
			if _, err := time.Parse(time.DateOnly, r.CheckedAt); err != nil {
				return fmt.Errorf("invalid reference date for %s", w.ID)
			}
			if r.Role != "metadata" && r.Role != "catalog" && r.Role != "ranking" {
				return fmt.Errorf("invalid reference role")
			}
		}
		if cat := w.Catalog; cat != nil {
			if cat.Unit != "chapter" && cat.Unit != "episode" && cat.Unit != "section" {
				return fmt.Errorf("invalid catalog unit for %s", w.ID)
			}
			if cat.Scope != "complete" && cat.Scope != "public" && cat.Scope != "snapshot" {
				return fmt.Errorf("invalid catalog scope")
			}
			if cat.Status != "completed" && cat.Status != "ongoing" && cat.Status != "unknown" {
				return fmt.Errorf("invalid catalog status")
			}
			if cat.Count < 0 || !publicURL(cat.URL) {
				return fmt.Errorf("invalid catalog reference")
			}
			if _, err := time.Parse(time.DateOnly, cat.VerifiedAt); err != nil {
				return fmt.Errorf("invalid catalog date")
			}
			for _, a := range cat.Anchors {
				if a.Title == "" || a.Number < 0 {
					return fmt.Errorf("invalid catalog anchor")
				}
			}
		}
	}
	return nil
}
func publicURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

// Normalize ignores typography only. It deliberately preserves subtitles,
// numerals and adaptation labels; Douluo II must not match Douluo I.
func Normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFKC.String(s))
}
