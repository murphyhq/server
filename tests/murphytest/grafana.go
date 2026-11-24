package murphytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/stretchr/testify/require"
)

var (
	grafanaURL       = setting("MURPHY_TEST_GRAFANA_URL", "http://localhost:3000")
	ingestForGrafana = setting("MURPHY_TEST_GRAFANA_INGEST_URL", "http://ingest:8081")
)

type GrafanaRule struct {
	m       *Murphy
	name    string
	service *Service
}

func (m *Murphy) GrafanaRuleFor(service *Service) *GrafanaRule {
	m.t.Helper()
	rule := &GrafanaRule{m: m, name: m.Name("rule"), service: service}
	contact := "murphy-" + rule.name
	_, _ = m.grafana(http.MethodPost, "/api/folders", map[string]string{"uid": "stack", "title": "Stack tests"})
	m.mustGrafana(http.MethodPost, "/api/v1/provisioning/contact-points", map[string]any{
		"uid": contact, "name": contact, "type": "webhook",
		"settings": map[string]any{"url": ingestForGrafana + "/v1/alerts/grafana", "authorization_scheme": "Bearer", "authorization_credentials": service.Key()},
	})
	policies := m.mustGrafana(http.MethodGet, "/api/v1/provisioning/policies", nil)
	original, err := json.Marshal(policies)
	require.NoError(m.t, err)
	routes, _ := policies["routes"].([]any)
	policies["routes"] = append([]any{map[string]any{
		"receiver": contact, "object_matchers": [][]string{{"test_rule", "=", rule.name}},
		"group_by": []string{"alertname"}, "group_wait": "1s", "group_interval": "1s", "repeat_interval": "1h",
	}}, routes...)
	m.mustGrafana(http.MethodPut, "/api/v1/provisioning/policies", policies)
	m.t.Cleanup(func() {
		var restore map[string]any
		_ = json.Unmarshal(original, &restore)
		_, _ = m.grafana(http.MethodPut, "/api/v1/provisioning/policies", restore)
		_, _ = m.grafana(http.MethodDelete, "/api/v1/provisioning/folder/stack/rule-groups/"+rule.name, nil)
		_, _ = m.grafana(http.MethodDelete, "/api/v1/provisioning/contact-points/"+contact, nil)
	})
	return rule
}

func (r *GrafanaRule) Fire() {
	r.m.t.Helper()
	r.evaluate("vector(1)")
}

func (r *GrafanaRule) Recover() {
	r.m.t.Helper()
	r.evaluate("vector(0)")
}

func (r *GrafanaRule) evaluate(expr string) {
	r.m.t.Helper()
	r.m.mustGrafana(http.MethodPut, "/api/v1/provisioning/folder/stack/rule-groups/"+r.name, map[string]any{
		"title": r.name, "folderUid": "stack", "interval": 10, "rules": []map[string]any{{
			"uid": r.name, "title": r.name, "folderUID": "stack", "ruleGroup": r.name, "condition": "C", "for": "0s",
			"noDataState": "OK", "execErrState": "Error", "labels": map[string]string{"test_rule": r.name},
			"annotations": map[string]string{"summary": "Stack test " + r.name},
			"data": []map[string]any{
				{"refId": "A", "relativeTimeRange": map[string]int{"from": 60, "to": 0}, "datasourceUid": "prometheus",
					"model": map[string]any{"refId": "A", "expr": expr, "instant": true}},
				{"refId": "C", "datasourceUid": "__expr__",
					"model": map[string]any{"refId": "C", "type": "threshold", "expression": "A",
						"conditions": []map[string]any{{"evaluator": map[string]any{"type": "gt", "params": []int{0}}}}}},
			},
		}},
	})
}

func (m *Murphy) mustGrafana(method, path string, body any) map[string]any {
	m.t.Helper()
	out, err := m.grafana(method, path, body)
	require.NoError(m.t, err)
	return out
}

func (m *Murphy) grafana(method, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(m.t.Context(), method, grafanaURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth("admin", "admin")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Disable-Provenance", "true")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	answer, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s %s", method, path, response.Status, answer)
	}
	out := map[string]any{}
	_ = json.Unmarshal(answer, &out)
	return out, nil
}
