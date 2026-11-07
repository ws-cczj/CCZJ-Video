package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"

	"cczjVideo/app/apperror"
	CCZJ "cczjVideo/app/cczj"
)

// Only image-bearing theme JSON uses this versioned binary envelope. Public
// settings and portable backups continue to expose the original JSON bytes.
const themeSettingEnvelope = "CCZJ:THEME:GZIP:1:"
const maxThemeSettingBytes = 60 << 20
const minThemeImageCompressionBytes = 1 << 20
const minThemeSettingSavingPercent = 5

// The gate uses individual image bytes, excluding the data URL header and
// base64 expansion. Multiple small images never add up to one eligible image.
func hasLargeThemeImage(ctx context.Context, value string) (bool, error) {
	if len(value) < minThemeImageCompressionBytes*4/3 {
		return false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return false, nil
	}
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var images struct {
			Background string `json:"backgroundImage"`
			Original   string `json:"backgroundOriginal"`
		}
		if err := decoder.Decode(&images); err != nil {
			return false, nil
		}
		if themeImageBytes(images.Background) >= minThemeImageCompressionBytes || themeImageBytes(images.Original) >= minThemeImageCompressionBytes {
			return true, nil
		}
	}
	return false, nil
}
func themeImageBytes(value string) int {
	if !strings.HasPrefix(value, "data:image/") {
		return 0
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 || !strings.HasSuffix(value[:comma], ";base64") {
		return 0
	}
	content := value[comma+1:]
	if len(content)%4 != 0 {
		return 0
	}
	size := len(content) / 4 * 3
	if strings.HasSuffix(content, "=") {
		size--
	}
	if strings.HasSuffix(content, "==") {
		size--
	}
	return size
}

func encodeSettingValue(ctx context.Context, key, value string) (any, error) {
	if key != CCZJ.SettingThemeCustoms {
		return value, nil
	}
	if len(value) > maxThemeSettingBytes {
		return nil, apperror.New(apperror.Validation, "主题数据总量超过 60 MiB，请减少主题或裁剪图片")
	}
	eligible, err := hasLargeThemeImage(ctx, value)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return value, nil
	}
	var output bytes.Buffer
	output.WriteString(themeSettingEnvelope)
	writer, err := gzip.NewWriterLevel(&output, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	// Check cancellation between bounded chunks, including restore transactions.
	for offset := 0; offset < len(value); {
		if err := ctx.Err(); err != nil {
			writer.Close()
			return nil, err
		}
		end := min(offset+64<<10, len(value))
		if _, err = io.WriteString(writer, value[offset:end]); err != nil {
			writer.Close()
			return nil, err
		}
		offset = end
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if len(value)-output.Len() < (len(value)*minThemeSettingSavingPercent+99)/100 {
		return value, nil
	}
	return output.Bytes(), nil
}

func decodeSettingValue(key, value string) (string, error) {
	if key != CCZJ.SettingThemeCustoms || !strings.HasPrefix(value, themeSettingEnvelope) {
		return value, nil
	}
	reader, err := gzip.NewReader(strings.NewReader(value[len(themeSettingEnvelope):]))
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "主题数据解压失败")
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, maxThemeSettingBytes+1))
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "主题数据解压失败")
	}
	if len(decoded) > maxThemeSettingBytes {
		return "", apperror.New(apperror.Storage, "主题数据解压后超出容量限制")
	}
	return string(decoded), nil
}
