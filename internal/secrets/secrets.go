package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

var (
	ErrMissing    = errors.New("secret not found")
	ErrNotAllowed = errors.New("secret outside what Murphy may read")
)

type Source interface {
	Read(ctx context.Context, name string) (string, error)
}

type Reader struct {
	sources map[string]Source
}

func NewReader(env, file, ssm Source) *Reader {
	return &Reader{sources: map[string]Source{"env": env, "file": file, "aws-ssm": ssm}}
}

func (r *Reader) Read(ctx context.Context, reference string) (string, error) {
	ref, err := url.Parse(reference)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not a secret reference", ErrNotAllowed, reference)
	}
	source, ok := r.sources[ref.Scheme]
	if !ok || source == nil {
		return "", fmt.Errorf("%w: Murphy can't read %s:// references", ErrNotAllowed, ref.Scheme)
	}
	name := ref.Path
	if ref.Scheme == "env" {
		name = ref.Host
	}
	value, err := source.Read(ctx, name)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", reference, err)
	}
	return value, nil
}
