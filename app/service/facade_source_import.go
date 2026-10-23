package service

import (
	"bytes"
	"cczjVideo/app/apperror"
	"cczjVideo/app/payload"
	sourceservice "cczjVideo/app/source"
	"context"
	"io"
	"os"
	"strings"
)

// ======================== 数据源导入 / 导出（含 Brotli 压缩） ========================

// sourceExportPayload 导出的 JSON 结构（写入 Brotli 压缩文件）
type sourceExportPayload = sourceservice.Payload

// ExportSource 导出某个源的所有数据到 .json.br 文件（Brotli 压缩），返回绝对路径
// 前端拿到文件路径后可以在系统文件管理器中复制/传给他人
func (a *App) ExportSource(sourceKey string) (string, error) {
	return sourceservice.NewService().Export(a.getDataDir(), sourceKey)
}

func (a *App) ImportSource(ctx context.Context, filePath string) (string, error) {
	return runRequest(a, ctx, "source.ImportSource", func(ctx context.Context) (string, error) {
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

		return a.decodeSourceImport(ctx, trimmed, f, "file")
	})
}

// ImportSourceFromBase64 accepts the same bounded formats as file import.
func (a *App) ImportSourceFromBase64(ctx context.Context, filename string, b64Content string) (string, error) {
	return runRequest(a, ctx, "source.ImportSourceFromBase64", func(ctx context.Context) (string, error) {
		if strings.TrimSpace(b64Content) == "" {
			return "", apperror.New(apperror.Validation, "文件内容为空")
		}
		raw, err := payload.Base64(ctx, b64Content, maxSourceImportBytes)
		if err != nil {
			return "", err
		}
		return a.decodeSourceImport(ctx, filename, bytes.NewReader(raw), "base64")
	})
}

func (a *App) decodeSourceImport(ctx context.Context, name string, reader io.Reader, origin string) (string, error) {
	decoded, err := payload.Unpack(ctx, name, reader, payload.Limits{Input: maxSourceImportBytes, Output: maxSourceImportBytes})
	if err != nil {
		return "", err
	}
	var imported sourceExportPayload
	if err = payload.JSON(ctx, decoded, &imported, false); err != nil {
		return "", err
	}
	if imported.Source == nil {
		return "", apperror.New(apperror.Corrupt, "文件中缺少 source 信息")
	}
	return a.persistImportedSource(imported, origin)
}

func (a *App) persistImportedSource(payload sourceExportPayload, origin string) (string, error) {
	return sourceservice.NewService().Import(payload, origin)
}
