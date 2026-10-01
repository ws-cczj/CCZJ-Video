package service

import (
	"bytes"
	"cczjVideo/app/apperror"
	sourceservice "cczjVideo/app/source"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/andybalholm/brotli"
)

// ======================== 数据源导入 / 导出（含 Brotli 压缩） ========================

// sourceExportPayload 导出的 JSON 结构（写入 Brotli 压缩文件）
type sourceExportPayload = sourceservice.Payload

// ExportSource 导出某个源的所有数据到 .json.br 文件（Brotli 压缩），返回绝对路径
// 前端拿到文件路径后可以在系统文件管理器中复制/传给他人
func (a *App) ExportSource(sourceKey string) (string, error) {
	return sourceservice.NewService().Export(a.getDataDir(), sourceKey)
}

func (a *App) ImportSource(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", apperror.New(apperror.Validation, "文件路径为空")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return "", apperror.Wrap(apperror.NotFound, err, "无法访问文件")
	}
	if info.IsDir() {
		return "", apperror.New(apperror.Validation, "路径是目录，不是文件")
	}
	if info.Size() > maxSourceImportBytes {
		return "", apperror.Newf(apperror.Validation, "导入文件过大：最大允许 %d MiB", maxSourceImportBytes>>20)
	}

	f, err := os.Open(trimmed)
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "打开文件失败")
	}
	defer f.Close()

	// 根据文件扩展名检测压缩格式
	lower := strings.ToLower(trimmed)
	var reader io.Reader = f
	if strings.HasSuffix(lower, ".br") {
		reader = brotli.NewReader(f)
	} else if strings.HasSuffix(lower, ".gz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return "", apperror.Wrap(apperror.Corrupt, err, "解压 gzip 失败")
		}
		defer gzr.Close()
		reader = gzr
	} else {
		// 无扩展名或 .json：尝试检测 gzip magic bytes
		magic := make([]byte, 2)
		_, err = f.Read(magic)
		if err != nil {
			return "", apperror.Wrap(apperror.Storage, err, "读取文件失败")
		}
		_, _ = f.Seek(0, 0)
		if magic[0] == 0x1f && magic[1] == 0x8b {
			gzr, err := gzip.NewReader(f)
			if err != nil {
				return "", apperror.Wrap(apperror.Corrupt, err, "解压 gzip 失败")
			}
			defer gzr.Close()
			reader = gzr
		}
	}

	var payload sourceExportPayload
	dec := json.NewDecoder(io.LimitReader(reader, maxSourceImportBytes+1))
	if err := dec.Decode(&payload); err != nil {
		return "", apperror.Wrap(apperror.Corrupt, err, "解析 JSON 失败")
	}
	if payload.Source == nil {
		return "", apperror.New(apperror.Corrupt, "文件中缺少 source 信息")
	}
	return a.persistImportedSource(payload, "file")
}

// ImportSourceFromBase64 imports source data provided by the browser.
func (a *App) ImportSourceFromBase64(filename string, b64Content string) (string, error) {
	if strings.TrimSpace(b64Content) == "" {
		return "", apperror.New(apperror.Validation, "文件内容为空")
	}
	if len(b64Content) > maxSourceImportBase64Bytes {
		return "", apperror.Newf(apperror.Validation, "导入内容过大：最大允许 %d MiB", maxSourceImportBytes>>20)
	}
	raw, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return "", apperror.Wrap(apperror.Corrupt, err, "解码 base64 失败")
	}

	// 根据文件名扩展名检测压缩格式
	var reader io.Reader = bytes.NewReader(raw)
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".br") {
		reader = brotli.NewReader(bytes.NewReader(raw))
	} else if strings.HasSuffix(lower, ".gz") {
		gzr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", apperror.Wrap(apperror.Corrupt, err, "解压 gzip 失败")
		}
		defer gzr.Close()
		reader = gzr
	} else {
		// 无扩展名或 .json：尝试检测 gzip magic bytes
		if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
			gzr, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return "", apperror.Wrap(apperror.Corrupt, err, "解压 gzip 失败")
			}
			defer gzr.Close()
			reader = gzr
		}
	}

	var payload sourceExportPayload
	dec := json.NewDecoder(io.LimitReader(reader, maxSourceImportBytes+1))
	if err := dec.Decode(&payload); err != nil {
		return "", apperror.Wrap(apperror.Corrupt, err, "解析 JSON 失败")
	}
	if payload.Source == nil {
		return "", apperror.New(apperror.Corrupt, "文件中缺少 source 信息")
	}
	return a.persistImportedSource(payload, "base64")
}

func (a *App) persistImportedSource(payload sourceExportPayload, origin string) (string, error) {
	return sourceservice.NewService().Import(payload, origin)
}
