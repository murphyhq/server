//go:build load

package load

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

type Report struct {
	Scenario    string         `json:"scenario"`
	Profile     string         `json:"profile"`
	StartedAt   time.Time      `json:"started_at"`
	Duration    float64        `json:"duration_seconds"`
	Requests    uint64         `json:"requests"`
	Rate        float64        `json:"rate_per_second"`
	Throughput  float64        `json:"throughput_per_second"`
	Success     float64        `json:"success"`
	Latency     Latency        `json:"latency_ms"`
	StatusCodes map[string]int `json:"status_codes"`
	Thresholds  Limits         `json:"thresholds"`
	Passed      bool           `json:"passed"`
}

type Latency struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

type Limits struct {
	Success float64 `json:"success"`
	P99     float64 `json:"p99_ms"`
}

func newReport(scenario string, p Profile, m *vegeta.Metrics, thresholds Thresholds) Report {
	return Report{
		Scenario:    scenario,
		Profile:     p.Name,
		StartedAt:   m.Earliest.UTC(),
		Duration:    m.Duration.Seconds(),
		Requests:    m.Requests,
		Rate:        m.Rate,
		Throughput:  m.Throughput,
		Success:     m.Success,
		Latency:     Latency{Mean: ms(m.Latencies.Mean), P50: ms(m.Latencies.P50), P90: ms(m.Latencies.P90), P95: ms(m.Latencies.P95), P99: ms(m.Latencies.P99), Max: ms(m.Latencies.Max)},
		StatusCodes: m.StatusCodes,
		Thresholds:  Limits{Success: thresholds.Success, P99: ms(thresholds.P99)},
		Passed:      m.Success >= thresholds.Success && m.Latencies.P99 <= thresholds.P99,
	}
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func (r Report) markdown() string {
	verdict := "passed"
	if !r.Passed {
		verdict = "failed"
	}
	codes := make([]string, 0, len(r.StatusCodes))
	for code, count := range r.StatusCodes {
		codes = append(codes, fmt.Sprintf("%s × %d", code, count))
	}
	slices.Sort(codes)
	var b strings.Builder
	fmt.Fprintf(&b, "### %s (%s profile): %s\n\n", r.Scenario, r.Profile, verdict)
	b.WriteString("| Requests | Rate | Success | p50 | p90 | p99 | Max | Status codes |\n|---|---|---|---|---|---|---|---|\n")
	fmt.Fprintf(&b, "| %d | %.1f/s | %.2f%% | %.1f ms | %.1f ms | %.1f ms | %.1f ms | %s |\n\n",
		r.Requests, r.Rate, r.Success*100, r.Latency.P50, r.Latency.P90, r.Latency.P99, r.Latency.Max, strings.Join(codes, ", "))
	fmt.Fprintf(&b, "Thresholds: success at least %.2f%%, p99 at most %.0f ms.\n\n", r.Thresholds.Success*100, r.Thresholds.P99)
	return b.String()
}

func (r Report) write() error {
	dir := url("MURPHY_LOAD_REPORT_DIR", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := strings.ReplaceAll(strings.ToLower(r.Scenario), " ", "-")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(r.markdown()), 0o644); err != nil {
		return err
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if _, err := f.WriteString(r.markdown()); err != nil {
			return err
		}
	}
	return nil
}
