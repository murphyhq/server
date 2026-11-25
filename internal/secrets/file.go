package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxFileSize = 64 << 10

type File struct {
	directory string
}

func NewFile(directory string) *File {
	return &File{directory: directory}
}

func (f *File) Read(_ context.Context, name string) (string, error) {
	notAllowed := fmt.Errorf("%w: only regular files in %s", ErrNotAllowed, f.directory)
	if !filepath.IsAbs(name) {
		return "", notAllowed
	}
	directory, err := filepath.EvalSymlinks(f.directory)
	if err != nil {
		return "", fmt.Errorf("%w: %s doesn't exist", ErrMissing, f.directory)
	}
	path, err := filepath.EvalSymlinks(name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s doesn't exist", ErrMissing, name)
	}
	if err != nil {
		return "", notAllowed
	}
	if relative, err := filepath.Rel(directory, path); err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return "", notAllowed
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return "", notAllowed
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", notAllowed
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > maxFileSize {
		return "", fmt.Errorf("%w: %s is larger than %d KiB", ErrNotAllowed, name, maxFileSize>>10)
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if value == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrMissing, name)
	}
	return value, nil
}
