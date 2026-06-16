package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	Root string
}

func NewLocal(root string) LocalStorage {
	return LocalStorage{Root: root}
}

func (s LocalStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	path := s.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s LocalStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return os.Open(s.path(key))
}

func (s LocalStorage) Delete(ctx context.Context, key string) error {
	return os.Remove(s.path(key))
}

func (s LocalStorage) path(key string) string {
	key = strings.TrimPrefix(filepath.Clean("/"+key), "/")
	return filepath.Join(s.Root, key)
}
