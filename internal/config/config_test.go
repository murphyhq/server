package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/murphyhq/server/internal/config"
)

type nested struct {
	Addr string `env:"ADDR"`
}

type settings struct {
	Name     string        `env:"NAME" required:"true"`
	Port     int           `env:"PORT" default:"8080"`
	Debug    bool          `env:"DEBUG"`
	Timeout  time.Duration `env:"TIMEOUT" default:"5s"`
	Password string        `env:"PASSWORD"`
	Nested   nested
}

func TestLoadAppliesValuesAndDefaults(t *testing.T) {
	t.Setenv("MURPHY_NAME", "murphy")
	t.Setenv("MURPHY_DEBUG", "true")
	t.Setenv("MURPHY_ADDR", ":9000")

	var s settings
	if err := config.Load(&s); err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.Name != "murphy" || !s.Debug || s.Nested.Addr != ":9000" {
		t.Errorf("values not loaded: %+v", s)
	}
	if s.Port != 8080 || s.Timeout != 5*time.Second {
		t.Errorf("defaults not applied: %+v", s)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	t.Setenv("MURPHY_PORT", "eighty")
	t.Setenv("MURPHY_TIMEOUT", "soon")

	var s settings
	err := config.Load(&s)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"MURPHY_NAME: required but not set", "MURPHY_PORT: not a valid integer", "MURPHY_TIMEOUT: not a valid duration"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestLoadNeverPutsValuesInErrors(t *testing.T) {
	t.Setenv("MURPHY_NAME", "murphy")
	t.Setenv("MURPHY_PORT", "s3cr3t")

	var s settings
	err := config.Load(&s)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("error %q contains the value", err)
	}
}

func TestLoadRequiresAPointerToAStruct(t *testing.T) {
	var s settings
	if err := config.Load(s); err == nil {
		t.Error("expected an error for a non-pointer destination")
	}
}
