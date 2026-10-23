package library

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The product auditor uses the same frozen inputs as the completed batch run.
// Compression only reduces executable data size; no downloaded code is run.
//
//go:embed audit_catalogs/*
var auditCatalogFiles embed.FS

func auditCatalogs(kind string) ([][]byte, error) {
	raw, err := auditCatalogFiles.ReadFile("audit_catalogs/manifest.json")
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Catalogs []struct {
			File   string   `json:"file"`
			Kinds  []string `json:"kinds"`
			SHA256 string   `json:"sha256"`
		} `json:"catalogs"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	result := [][]byte{}
	for _, row := range manifest.Catalogs {
		selected := false
		for _, k := range row.Kinds {
			selected = selected || k == kind
		}
		if !selected {
			continue
		}
		compressed, err := auditCatalogFiles.ReadFile("audit_catalogs/" + row.File)
		if err != nil {
			return nil, err
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxCatalogBytes+1))
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		if len(data) > maxCatalogBytes {
			return nil, fmt.Errorf("builtin audit catalog exceeds limit")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != row.SHA256 {
			return nil, fmt.Errorf("builtin audit catalog digest mismatch")
		}
		result = append(result, data)
	}
	return result, nil
}
