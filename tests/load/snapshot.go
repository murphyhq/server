//go:build load

package load

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const dashboardDir = "../provision/local/grafana/dashboards"

var (
	grafanaURL      = url("MURPHY_TEST_GRAFANA_URL", "http://localhost:3000")
	grafanaUser     = url("MURPHY_TEST_GRAFANA_USER", "admin")
	grafanaPassword = url("MURPHY_TEST_GRAFANA_PASSWORD", "admin")
)

type Snapshot struct {
	Dashboard string          `json:"dashboard"`
	Component string          `json:"component,omitempty"`
	Profile   string          `json:"profile"`
	From      time.Time       `json:"from"`
	To        time.Time       `json:"to"`
	Step      string          `json:"step"`
	Panels    []SnapshotPanel `json:"panels"`
}

type SnapshotPanel struct {
	Title  string           `json:"title"`
	Unit   string           `json:"unit"`
	Series []SnapshotSeries `json:"series"`
}

type SnapshotSeries struct {
	Query  string            `json:"query"`
	Legend string            `json:"legend"`
	Labels map[string]string `json:"labels"`
	Points [][2]float64      `json:"points"`
}

type datasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

var prometheus = datasource{Type: "prometheus", UID: "prometheus"}

func (d datasource) queryRangePath() string {
	if d.Type == "loki" {
		return "/api/datasources/proxy/uid/" + d.UID + "/loki/api/v1/query_range"
	}
	return "/api/datasources/proxy/uid/" + d.UID + "/api/v1/query_range"
}

type dashboard struct {
	Title  string `json:"title"`
	Panels []struct {
		Type        string      `json:"type"`
		Title       string      `json:"title"`
		Datasource  *datasource `json:"datasource"`
		FieldConfig struct {
			Defaults struct {
				Unit string `json:"unit"`
			} `json:"defaults"`
		} `json:"fieldConfig"`
		Targets []struct {
			Expr         string `json:"expr"`
			LegendFormat string `json:"legendFormat"`
		} `json:"targets"`
	} `json:"panels"`
}

func snapshot(profile string, from, to time.Time) error {
	files, err := filepath.Glob(filepath.Join(dashboardDir, "*.json"))
	if err != nil {
		return err
	}
	components, err := murphyComponents()
	if err != nil {
		return fmt.Errorf("list components: %w", err)
	}
	dir := filepath.Join(reportDir(), "dashboards")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		var board dashboard
		if err := json.Unmarshal(data, &board); err != nil {
			return fmt.Errorf("decode %s: %w", file, err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		if !strings.Contains(string(data), "$component") {
			if err := snapshotBoard(board, profile, from, to, "", filepath.Join(dir, name+".json")); err != nil {
				return err
			}
			continue
		}
		for _, component := range components {
			if err := snapshotBoard(board, profile, from, to, component, filepath.Join(dir, name+"-"+component+".json")); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotBoard(board dashboard, profile string, from, to time.Time, component, file string) error {
	step := 15 * time.Second
	window := to.Sub(from).Round(time.Second)
	replacer := strings.NewReplacer("$__rate_interval", "1m", "$__range", window.String(), "$component", component)
	snap := Snapshot{Dashboard: board.Title, Component: component, Profile: profile, From: from.UTC(), To: to.UTC(), Step: step.String()}
	for _, panel := range board.Panels {
		if panel.Type == "row" {
			continue
		}
		out := SnapshotPanel{Title: panel.Title, Unit: panel.FieldConfig.Defaults.Unit, Series: []SnapshotSeries{}}
		for _, target := range panel.Targets {
			source := prometheus
			if panel.Datasource != nil && panel.Datasource.UID != "" {
				source = *panel.Datasource
			}
			series, err := queryRange(source, replacer.Replace(target.Expr), from, to, step)
			if err != nil {
				return fmt.Errorf("%s, panel %q: %w", board.Title, panel.Title, err)
			}
			for i := range series {
				series[i].Legend = target.LegendFormat
			}
			out.Series = append(out.Series, series...)
		}
		snap.Panels = append(snap.Panels, out)
	}
	encoded, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, encoded, 0o644)
}

func murphyComponents() ([]string, error) {
	series, err := queryRange(prometheus, `group by (service_name) (target_info{service_namespace="murphy"})`, time.Now().Add(-time.Minute), time.Now(), 15*time.Second)
	if err != nil {
		return nil, err
	}
	components := make([]string, 0, len(series))
	for _, s := range series {
		components = append(components, s.Labels["service_name"])
	}
	slices.Sort(components)
	return components, nil
}

func queryRange(source datasource, query string, from, to time.Time, step time.Duration) ([]SnapshotSeries, error) {
	params := neturl.Values{
		"query": {query},
		"start": {strconv.FormatInt(from.Unix(), 10)},
		"end":   {strconv.FormatInt(to.Unix(), 10)},
		"step":  {strconv.Itoa(int(step.Seconds()))},
	}
	req, err := http.NewRequest(http.MethodGet, grafanaURL+source.queryRangePath()+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(grafanaUser, grafanaPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query answered %s", resp.Status)
	}
	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	series := make([]SnapshotSeries, 0, len(result.Data.Result))
	for _, r := range result.Data.Result {
		points := make([][2]float64, 0, len(r.Values))
		for _, v := range r.Values {
			at, _ := v[0].(float64)
			text, _ := v[1].(string)
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			points = append(points, [2]float64{at, value})
		}
		series = append(series, SnapshotSeries{Query: query, Labels: r.Metric, Points: points})
	}
	return series, nil
}
