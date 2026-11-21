package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

type EnvResolver struct{}

func (EnvResolver) Resolve(_ context.Context, ref string) (string, error) {
	name := strings.TrimPrefix(ref, "env://")
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return value, nil
}

type FileResolver struct{}

func (FileResolver) Resolve(_ context.Context, ref string) (string, error) {
	path := strings.TrimPrefix(ref, "file://")
	if path == "" {
		return "", errors.New("file path is empty")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimRight(string(content), "\r\n"), nil
}
