package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/config"
)

type nested struct {
	Addr string `env:"ADDR"`
}

type settings struct {
	Name    string        `env:"NAME" required:"true"`
	Port    int           `env:"PORT" default:"8080"`
	Debug   bool          `env:"DEBUG"`
	Timeout time.Duration `env:"TIMEOUT" default:"5s"`
	Region  string        `env:"AWS_REGION"`
	Nested  nested
}

func TestLoadAppliesValuesAndDefaults(t *testing.T) {
	t.Setenv("MURPHY_NAME", "murphy")
	t.Setenv("MURPHY_DEBUG", "true")
	t.Setenv("MURPHY_ADDR", ":9000")
	t.Setenv("AWS_REGION", "eu-west-1")

	var s settings
	require.NoError(t, config.Load(&s))

	assert.Equal(t, settings{
		Name:    "murphy",
		Port:    8080,
		Debug:   true,
		Timeout: 5 * time.Second,
		Region:  "eu-west-1",
		Nested:  nested{Addr: ":9000"},
	}, s)
}

func TestLoadReportsEveryProblem(t *testing.T) {
	t.Setenv("MURPHY_PORT", "eighty")
	t.Setenv("MURPHY_TIMEOUT", "soon")

	var s settings
	err := config.Load(&s)

	require.Error(t, err)
	assert.ErrorContains(t, err, "MURPHY_NAME: required but not set")
	assert.ErrorContains(t, err, "MURPHY_PORT: not a valid integer")
	assert.ErrorContains(t, err, "MURPHY_TIMEOUT: not a valid duration")
}

func TestLoadNeverPutsValuesInErrors(t *testing.T) {
	t.Setenv("MURPHY_NAME", "murphy")
	t.Setenv("MURPHY_PORT", "s3cr3t")

	var s settings
	err := config.Load(&s)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t")
}

type listed struct {
	Origins []string `env:"CORS_ORIGINS"`
}

func TestLoadReadsCommaSeparatedLists(t *testing.T) {
	t.Setenv("MURPHY_CORS_ORIGINS", " http://localhost:8090, https://app.example.com ,")

	var s listed
	require.NoError(t, config.Load(&s))

	assert.Equal(t, []string{"http://localhost:8090", "https://app.example.com"}, s.Origins)
}

func TestLoadLeavesAnEmptyListEmpty(t *testing.T) {
	var s listed
	require.NoError(t, config.Load(&s))

	assert.Empty(t, s.Origins)
}
