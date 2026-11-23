//go:build load

package load

import (
	"slices"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

type Profile struct {
	Name     string
	Pacer    vegeta.Pacer
	Duration time.Duration
	Settle   time.Duration
}

var profiles = map[string]Profile{
	"smoke": {Pacer: vegeta.Rate{Freq: 10, Per: time.Second}, Duration: 10 * time.Second, Settle: 30 * time.Second},
	"load":  {Pacer: vegeta.Rate{Freq: 50, Per: time.Second}, Duration: 2 * time.Minute, Settle: time.Minute},
	"stress": {
		Pacer:    vegeta.LinearPacer{StartAt: vegeta.Rate{Freq: 10, Per: time.Second}, Slope: 1},
		Duration: 5 * time.Minute,
		Settle:   2 * time.Minute,
	},
}

func profile(t *testing.T) Profile {
	t.Helper()
	name := profileName()
	p, ok := profiles[name]
	if !ok {
		names := make([]string, 0, len(profiles))
		for known := range profiles {
			names = append(names, known)
		}
		slices.Sort(names)
		t.Fatalf("MURPHY_LOAD_PROFILE must be one of %v", names)
	}
	p.Name = name
	return p
}

func profileName() string {
	return url("MURPHY_LOAD_PROFILE", "smoke")
}
