// Package payload bounds transport input and decoded output before domain validation.
package payload

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/files"
	"github.com/andybalholm/brotli"
)

// Limits separates the wire budget from the decompressed content budget.
type Limits struct{ Input, Output int64 }

// Read rejects rather than silently truncates a payload exceeding maximum.
// The caller owns closing the source; a blocking source must also use ctx for I/O.
func Read(ctx context.Context, source io.Reader, maximum int64) ([]byte, error) {
	if maximum < 1 || maximum >= 1<<40 {
		return nil, apperror.New(apperror.Internal, "无效的数据容量限制")
	}
	data, err := io.ReadAll(io.LimitReader(files.ReaderWithContext(ctx, source), maximum+1))
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "读取数据失败")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, apperror.New(apperror.Validation, "数据超过容量限制")
	}
	return data, nil
}

// Base64 checks the encoded budget before allocating the decoded representation.
func Base64(ctx context.Context, text string, maximum int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maximum < 1 || maximum >= 1<<40 {
		return nil, apperror.New(apperror.Internal, "无效的数据容量限制")
	}
	if int64(len(text)) > ((maximum+2)/3)*4 {
		return nil, apperror.New(apperror.Validation, "编码数据超过容量限制")
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "Base64 数据无效")
	}
	if int64(len(data)) > maximum {
		return nil, apperror.New(apperror.Validation, "数据超过容量限制")
	}
	return data, ctx.Err()
}

// Unpack accepts Brotli/gzip suffixes and gzip magic, preserving the import format.
// Both wire input and decompressed bytes are checked, including compressed trailers.
func Unpack(ctx context.Context, name string, source io.Reader, limits Limits) ([]byte, error) {
	raw, err := Read(ctx, source, limits.Input)
	if err != nil {
		return nil, err
	}
	return UnpackBytes(ctx, name, raw, limits)
}

// UnpackBytes reuses an already owned wire buffer instead of copying it again.
// An uncompressed result aliases raw; consumers must treat it as immutable.
func UnpackBytes(ctx context.Context, name string, raw []byte, limits Limits) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limits.Input < 1 || limits.Output < 1 || int64(len(raw)) > limits.Input {
		return nil, apperror.New(apperror.Validation, "数据超过容量限制")
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".br"):
		reader = brotli.NewReader(reader)
	case strings.HasSuffix(strings.ToLower(name), ".gz") || len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b:
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "gzip 数据无效")
		}
		defer compressed.Close()
		reader = compressed
	default:
		if int64(len(raw)) > limits.Output {
			return nil, apperror.New(apperror.Validation, "数据超过容量限制")
		}
		return raw, nil
	}
	return Read(ctx, reader, limits.Output)
}

// JSON decodes exactly one document; optional field strictness belongs to its contract.
func JSON(ctx context.Context, data []byte, target any, strict bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return apperror.Wrap(apperror.Corrupt, err, "JSON 数据无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON documents")
		}
		return apperror.Wrap(apperror.Corrupt, err, "JSON 包含多余内容")
	}
	return ctx.Err()
}
