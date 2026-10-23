// Package danmaku owns public-provider discovery, validated episode bindings,
// bounded segment reads and durable cache policy. It never controls playback.
package danmaku

import (
	"context"
	"regexp"
	"strings"

	"cczjVideo/app/apperror"
)

const DecoderVersion = "ordinary-v1"
const MaxPayload = 16 << 20
const MaxSegmentItems = 30000

type Item struct {
	ID       string `json:"id"`
	TimeMS   int64  `json:"timeMs"`
	Text     string `json:"text"`
	Mode     int    `json:"mode"`
	Color    uint32 `json:"color"`
	FontSize int    `json:"fontSize"`
}

type Candidate struct {
	Provider string `json:"provider"`
	Ref      string `json:"ref"`
	Title    string `json:"title"`
	Year     string `json:"year"`
	Kind     string `json:"kind"`
}

type Episode struct {
	Provider   string `json:"provider"`
	SeriesID   string `json:"seriesId"`
	EpisodeID  string `json:"episodeId"`
	AID        string `json:"aid"`
	Title      string `json:"title"`
	Number     string `json:"number"`
	DurationMS int64  `json:"durationMs"`
	PageURL    string `json:"pageUrl"`
}

type Binding struct {
	LocalKey  string  `json:"localKey"`
	Episode   Episode `json:"episode"`
	OffsetMS  int64   `json:"offsetMs"`
	Confirmed bool    `json:"confirmed"`
}

type Segment struct {
	Key     string `json:"key"`
	StartMS int64  `json:"startMs"`
	EndMS   int64  `json:"endMs"`
}

type Index struct {
	Segments []Segment `json:"segments"`
}

type Window struct {
	Items     []Item   `json:"items"`
	StartMS   int64    `json:"startMs"`
	EndMS     int64    `json:"endMs"`
	Provider  string   `json:"provider"`
	EpisodeID string   `json:"episodeId"`
	Cached    bool     `json:"cached"`
	Stale     bool     `json:"stale"`
	Warnings  []string `json:"warnings"`
	Skipped   int      `json:"skipped"`
}

// Provider varies at the actual protocol seam, rather than leaking upstream
// JSON/protobuf fields into the player or persistence.
type Provider interface {
	Search(context.Context, string) ([]Candidate, error)
	Episodes(context.Context, string) ([]Episode, error)
	Index(context.Context, Episode) (Index, error)
	Segment(context.Context, Episode, Segment) ([]Item, int, error)
}

var decimalID = regexp.MustCompile(`^[0-9]{1,24}$`)
var qqID = regexp.MustCompile(`^[A-Za-z0-9]{5,32}$`)

func ValidateBinding(binding Binding) error {
	e := binding.Episode
	valid := false
	switch e.Provider {
	case "bilibili":
		valid = decimalID.MatchString(e.EpisodeID) && (e.AID == "" || decimalID.MatchString(e.AID))
	case "qq":
		valid = qqID.MatchString(e.EpisodeID)
	case "iqiyi":
		valid = decimalID.MatchString(e.EpisodeID)
	}
	if !valid || !binding.Confirmed || strings.TrimSpace(binding.LocalKey) == "" || len(binding.LocalKey) > 512 || len(e.Title) > 1024 || binding.OffsetMS < -3600000 || binding.OffsetMS > 3600000 || e.DurationMS < 0 || e.DurationMS > 24*60*60*1000 {
		return apperror.New(apperror.Validation, "请确认具体单集和有效的弹幕来源")
	}
	return nil
}
