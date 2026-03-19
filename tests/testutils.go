package testutils

import (
	"os"
	"path/filepath"
)

func CreateTempDir(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, err
	}

	cleanup := func() {
		os.RemoveAll(dir)
	}

	return dir, cleanup, nil
}

func CreateTempFile(dir, name string, content []byte) (string, error) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", err
	}
	return path, nil
}
