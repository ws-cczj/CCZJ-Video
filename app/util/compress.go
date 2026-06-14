package util

import (
	"bytes"
	"encoding/base64"

	"github.com/andybalholm/brotli"
)

func CompressData(input []byte) (string, error) {
	var buf bytes.Buffer
	writer := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	_, err := writer.Write(input)
	if err != nil {
		return "", err
	}
	writer.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func DecompressData(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	reader := brotli.NewReader(bytes.NewReader(data))
	var buf bytes.Buffer
	_, err = buf.ReadFrom(reader)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func CompressIfLong(text string) string {
	if len(text) < 100 {
		return text
	}
	data, err := CompressData([]byte(text))
	if err != nil || len(text) <= len(data) {
		return text
	}
	return "Br-" + data
}

func DecompressIfNeeded(text string) string {
	if len(text) < 3 || text[:3] != "Br-" {
		return text
	}
	data, err := DecompressData(text[3:])
	if err != nil {
		return text
	}
	return string(data)
}
