// Package fileio centralizes local file access for user-provided CLI/TUI paths.
package fileio

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
)

// Open opens a local file path after anchoring the operation to the file's parent directory.
func Open(path string) (*os.File, error) {
	return OpenFile(path, os.O_RDONLY, 0)
}

// OpenFile opens a local file path after anchoring the operation to the file's parent directory.
func OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	cleanPath := filepath.Clean(path)

	root, err := os.OpenRoot(filepath.Dir(cleanPath))
	if err != nil {
		return nil, errors.Wrapf(err, "open parent directory for %s", path)
	}

	defer func() { _ = root.Close() }()

	file, err := root.OpenFile(filepath.Base(cleanPath), flag, perm)
	if err != nil {
		return nil, errors.Wrapf(err, "open file %s", path)
	}

	return file, nil
}

// ReadFile reads a local file path after anchoring the operation to the file's parent directory.
func ReadFile(path string) ([]byte, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.Wrapf(err, "read file %s", path)
	}

	return data, nil
}

// WriteFile writes a local file path after anchoring the operation to the file's parent directory.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	file, err := OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errors.Wrapf(err, "write file %s", path)
	}

	if err := file.Close(); err != nil {
		return errors.Wrapf(err, "close file %s", path)
	}

	return nil
}
