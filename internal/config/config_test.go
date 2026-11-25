package config_test

import (
	"net/netip"
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

type origins struct {
	Allowed []string `env:"ALLOWED_ORIGINS" validate:"base-url"`
}

func TestLoadAcceptsBaseURLsAndRejectsEverythingElseWithoutEchoingIt(t *testing.T) {
	t.Setenv("MURPHY_ALLOWED_ORIGINS", "http://localhost:8090,https://app.example.com:8443")
	var ok origins
	require.NoError(t, config.Load(&ok))

	t.Setenv("MURPHY_ALLOWED_ORIGINS", "https://app.example.com,s3cret-host/path")
	var bad origins
	err := config.Load(&bad)
	require.Error(t, err)
	assert.ErrorContains(t, err, "MURPHY_ALLOWED_ORIGINS: entry 2")
	assert.NotContains(t, err.Error(), "s3cret-host")
}

type networks struct {
	Allowed []netip.Prefix `env:"ALLOWED_NETWORKS"`
}

func TestLoadAcceptsNetworksAndRejectsEverythingElseWithoutEchoingIt(t *testing.T) {
	t.Setenv("MURPHY_ALLOWED_NETWORKS", "10.0.0.0/8,fd00::/8")
	var ok networks
	require.NoError(t, config.Load(&ok))
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}, ok.Allowed)

	t.Setenv("MURPHY_ALLOWED_NETWORKS", "10.0.0.0/8,s3cret-host")
	var bad networks
	err := config.Load(&bad)
	require.Error(t, err)
	assert.ErrorContains(t, err, "MURPHY_ALLOWED_NETWORKS: entry 2")
	assert.NotContains(t, err.Error(), "s3cret-host")
}
