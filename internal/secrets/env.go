package secrets

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Env struct {
	prefix string
}

func NewEnv(prefix string) *Env {
	return &Env{prefix: prefix}
}

func (e *Env) Read(_ context.Context, name string) (string, error) {
	if !variableName.MatchString(name) || !strings.HasPrefix(name, e.prefix) {
		return "", fmt.Errorf("%w: only variables starting with %s", ErrNotAllowed, e.prefix)
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", fmt.Errorf("%w: %s isn't set", ErrMissing, name)
	}
	return value, nil
}
