package files

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

type contextWriter struct {
	ctx         context.Context
	destination io.Writer
}

func (w contextWriter) Write(buffer []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.destination.Write(buffer)
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}

// ReaderWithContext checks cancellation between reads. A blocking network reader
// must also receive ctx when it is opened; the caller retains ownership of Close.
func ReaderWithContext(ctx context.Context, source io.Reader) io.Reader {
	return contextReader{ctx, source}
}

// CopyContext cooperatively cancels file copies and hashing without buffering the file.
func CopyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	n, err := io.CopyBuffer(destination, ReaderWithContext(ctx, source), make([]byte, 64<<10))
	if err != nil {
		return n, err
	}
	return n, ctx.Err()
}

// WriteAtomic publishes a complete file in its destination directory. Failure
// closes and removes only the owned temporary file, preserving any previous file.
func WriteAtomic(ctx context.Context, path string, write func(io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.part")
	if err != nil {
		return err
	}
	return publishFile(ctx, file, path, write)
}

// WriteUnique allocates a unique final name from pattern and publishes it only
// after all writes, flushing and closing succeed.
func WriteUnique(ctx context.Context, directory, pattern string, write func(io.Writer) error) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, pattern+".part")
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(file.Name(), ".part")
	if err := publishFile(ctx, file, path, write); err != nil {
		return "", err
	}
	return path, nil
}

func publishFile(ctx context.Context, file *os.File, path string, write func(io.Writer) error) error {
	published := false
	defer func() {
		file.Close()
		if !published {
			_ = os.Remove(file.Name())
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := write(contextWriter{ctx, file}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	published = true
	return nil
}
