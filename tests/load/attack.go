//go:build load

package load

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/client"
)

var (
	apiURL    = url("MURPHY_TEST_API_URL", "http://localhost:8080")
	ingestURL = url("MURPHY_TEST_INGEST_URL", "http://localhost:8081")
)

func url(variable, fallback string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return fallback
}

type Thresholds struct {
	Success float64
	P99     time.Duration
}

func api(t *testing.T) *client.ClientWithResponses {
	t.Helper()
	c, err := client.NewClientWithResponses(apiURL)
	require.NoError(t, err)
	return c
}

func attack(t *testing.T, p Profile, name string, targeter vegeta.Targeter, thresholds Thresholds, observe func(*vegeta.Result)) {
	t.Helper()
	attacker := vegeta.NewAttacker(vegeta.Timeout(10 * time.Second))
	var metrics vegeta.Metrics
	for result := range attacker.Attack(targeter, p.Pacer, p.Duration, name) {
		metrics.Add(result)
		if observe != nil {
			observe(result)
		}
	}
	metrics.Close()

	var text bytes.Buffer
	require.NoError(t, vegeta.NewTextReporter(&metrics).Report(&text))
	t.Logf("%s\n%s", name, text.String())
	require.NoError(t, newReport(name, p, &metrics, thresholds).write(), "write the %s report", name)

	assert.GreaterOrEqual(t, metrics.Success, thresholds.Success, "share of successful requests, by status code: %v, errors: %v", metrics.StatusCodes, metrics.Errors)
	assert.LessOrEqual(t, metrics.Latencies.P99, thresholds.P99, "p99 latency")
}
