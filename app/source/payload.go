// Package source owns source import/export payloads and persistence rules.
package source

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// Payload is the versioned portable representation of one source.
type Payload struct {
	Version  int                  `json:"version"`
	Exported string               `json:"exported"`
	Source   *model.Source        `json:"source"`
	Videos   []*db.ExportVideoRow `json:"videos"`
	Types    []*db.ExportTypeRow  `json:"types"`
}
