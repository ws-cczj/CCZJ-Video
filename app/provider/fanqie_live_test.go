package provider

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"

	"cczjVideo/app/model"
)

// Opt-in upstream verification: ordinary tests never depend on source uptime.
func TestFanqieLivePublicClassicChapters(t *testing.T) {
	if os.Getenv("CCZJ_FANQIE_LIVE") != "1" {
		t.Skip("set CCZJ_FANQIE_LIVE=1 to check public upstream chapters")
	}
	doc, err := ParseDocument(`{"version":3,"kind":"novel","adapter":"fanqie-web"}`, "novel")
	if err != nil {
		t.Fatal(err)
	}
	pipeline := NewPipeline(doc, NewFetcher(), DefaultLimits())
	for _, row := range []struct{ id, anchor string }{
		{"7345722492855520319", "混沌未分天地乱"},
		{"7345722494013148201", "师父果有些滴㳠"},
		{"7345722495133027382", ""},
	} {
		t.Run(row.id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			text, err := pipeline.Content(ctx, fanqieOrigin, &Chapter{URL: fanqieOrigin + "/reader/" + row.id, Access: model.ChapterAccessAvailable}, "")
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(text.Paragraphs, "\n")
			if len(text.Paragraphs) < 10 || len([]rune(joined)) < 1000 || !strings.Contains(joined, row.anchor) {
				t.Fatalf("incomplete/incorrect public text: %d paragraphs, %d bytes", len(text.Paragraphs), len(joined))
			}
			for _, r := range joined {
				if unicode.Is(unicode.Co, r) {
					t.Fatalf("unconverted private-use character U+%04X", r)
				}
			}
			t.Logf("public chapter: %d paragraphs, %d characters, no private-use glyphs", len(text.Paragraphs), len([]rune(joined)))
		})
	}
}
