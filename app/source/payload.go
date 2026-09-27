// Package source owns source import/export payloads and persistence rules.
package source

import "cczjVideo/app/model"

// Payload is the versioned portable representation of one source.
type Payload struct {
	Version  int             `json:"version"`
	Exported string          `json:"exported"`
	Source   *model.Source   `json:"source"`
	Videos   []*CatalogVideo `json:"videos"`
	Types    []TypePayload   `json:"types"`
}

// CatalogVideo is the v2 portable directory projection. VodID accepts v1
// payloads; all legacy detail fields are ignored during import.
type CatalogVideo struct {
	SourceVodID  string `json:"source_vod_id,omitempty"`
	VodID        string `json:"vod_id,omitempty"`
	SourceTypeID string `json:"source_type_id,omitempty"`
	TypeID       string `json:"type_id,omitempty"`
	TypeName     string `json:"type_name"`
	VodName      string `json:"vod_name"`
	VodPic       string `json:"vod_pic,omitempty"`
	VodRemarks   string `json:"vod_remarks,omitempty"`
	VodYear      string `json:"vod_year,omitempty"`
	VodArea      string `json:"vod_area,omitempty"`
	VodTime      string `json:"vod_time,omitempty"`
}
type TypePayload struct {
	TypeID       string `json:"type_id"`
	GlobalTypeID int64  `json:"global_type_id,omitempty"`
	TypeName     string `json:"type_name"`
}
