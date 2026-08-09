package storagefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalFS struct {
	baseDir string
}

func NewLocalFS(baseDir string) *LocalFS {
	return &LocalFS{baseDir: baseDir}
}

func (l *LocalFS) resolve(key string) (string, error) {
	// Reject path traversal — keys are meant to be trusted-shaped (UUID
	// segments), but this is cheap insurance since keys can embed filenames.
	clean := filepath.Clean("/" + key)
	if strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return filepath.Join(l.baseDir, clean), nil
}

func (l *LocalFS) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	path, err := l.resolve(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, r)
}

func (l *LocalFS) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (l *LocalFS) Delete(ctx context.Context, key string) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
