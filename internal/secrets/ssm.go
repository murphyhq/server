package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type ParameterStore interface {
	GetParameter(ctx context.Context, input *ssm.GetParameterInput, options ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

type SSM struct {
	store ParameterStore
	paths []string
}

func NewSSM(store ParameterStore, paths []string) *SSM {
	return &SSM{store: store, paths: paths}
}

func (s *SSM) Read(ctx context.Context, name string) (string, error) {
	if !s.allows(name) {
		return "", fmt.Errorf("%w: only parameters under %s", ErrNotAllowed, strings.Join(s.paths, ", "))
	}
	output, err := s.store.GetParameter(ctx, &ssm.GetParameterInput{Name: sdkaws.String(name), WithDecryption: sdkaws.Bool(true)})
	var notFound *types.ParameterNotFound
	if errors.As(err, &notFound) {
		return "", fmt.Errorf("%w: %s doesn't exist", ErrMissing, name)
	}
	if err != nil {
		return "", fmt.Errorf("get parameter %s: %w", name, err)
	}
	value := sdkaws.ToString(output.Parameter.Value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrMissing, name)
	}
	return value, nil
}

func (s *SSM) allows(name string) bool {
	if !strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/")[1:], "") || slices.ContainsFunc(strings.Split(name, "/"), func(part string) bool { return part == "." || part == ".." }) {
		return false
	}
	return slices.ContainsFunc(s.paths, func(path string) bool { return strings.HasPrefix(name, strings.TrimSuffix(path, "/")+"/") })
}
