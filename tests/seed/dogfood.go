package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/murphyhq/server/client"
)

type dogfoodSettings struct {
	GrafanaURL      string `env:"SEED_GRAFANA_URL" default:"http://localhost:3000"`
	GrafanaUser     string `env:"SEED_GRAFANA_USER" default:"admin"`
	GrafanaPassword string `env:"SEED_GRAFANA_PASSWORD" default:"admin"`
	IngestURL       string `env:"SEED_DOGFOOD_INGEST_URL" default:"http://ingest:8080"`
	KeyFile         string `env:"SEED_DOGFOOD_KEY_FILE" default:".murphy/dogfood.json"`
}

type dogfoodKey struct {
	ServiceID     string `json:"service_id"`
	IntegrationID string `json:"integration_id"`
	Key           string `json:"key"`
}

const (
	dogfoodService = "murphy"
	grafanaFolder  = "murphy"
	contactPoint   = "murphy"
)

func seedDogfood(ctx context.Context, api *client.ClientWithResponses, policyID string, s dogfoodSettings) error {
	key, err := dogfoodIntegration(ctx, api, policyID, s.KeyFile)
	if err != nil {
		return err
	}
	grafana := grafanaClient{url: s.GrafanaURL, user: s.GrafanaUser, password: s.GrafanaPassword}
	return grafana.configure(ctx, s.IngestURL+"/v1/alerts/grafana", key.Key)
}

func dogfoodIntegration(ctx context.Context, api *client.ClientWithResponses, policyID, file string) (*dogfoodKey, error) {
	if saved, err := readKey(file); err == nil {
		got, err := api.GetIntegrationWithResponse(ctx, saved.IntegrationID)
		if err != nil {
			return nil, fmt.Errorf("look up the dogfood integration: %w", err)
		}
		if got.StatusCode() == http.StatusOK {
			return saved, nil
		}
	}

	existing, err := serviceIDs(ctx, api)
	if err != nil {
		return nil, err
	}
	serviceID, ok := existing[dogfoodService]
	if !ok {
		created, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{
			Name:             dogfoodService,
			Description:      new("Murphy itself, paged by the Grafana that watches it."),
			EscalationPolicy: client.Reference{Id: policyID},
		})
		if err != nil || created.StatusCode() != http.StatusCreated {
			return nil, fmt.Errorf("create the %s service: %v %s", dogfoodService, err, statusOf(created))
		}
		serviceID = created.JSON201.Id
	}
	integration, err := api.CreateIntegrationWithResponse(ctx, serviceID, nil, client.IntegrationCreate{Name: "Grafana (dogfood)", Type: client.Grafana})
	if err != nil || integration.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("create the dogfood integration: %v %s", err, statusOf(integration))
	}
	key := &dogfoodKey{ServiceID: serviceID, IntegrationID: integration.JSON201.Id, Key: integration.JSON201.Key}
	return key, writeKey(file, key)
}

func statusOf(response interface{ StatusCode() int }) string {
	if response == nil {
		return ""
	}
	return http.StatusText(response.StatusCode())
}

func readKey(file string) (*dogfoodKey, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var key dogfoodKey
	if err := json.Unmarshal(data, &key); err != nil || key.Key == "" {
		return nil, errors.New("unreadable dogfood key file")
	}
	return &key, nil
}

func writeKey(file string, key *dogfoodKey) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(file), err)
	}
	data, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o600)
}

type grafanaClient struct {
	url, user, password string
}

func (g grafanaClient) configure(ctx context.Context, webhookURL, key string) error {
	if err := g.call(ctx, http.MethodPost, "/api/folders", map[string]string{"uid": grafanaFolder, "title": "Murphy"}, http.StatusConflict, http.StatusPreconditionFailed); err != nil {
		return fmt.Errorf("create the Grafana folder: %w", err)
	}

	point := map[string]any{
		"uid":  contactPoint,
		"name": "Murphy",
		"type": "webhook",
		"settings": map[string]any{
			"url":                       webhookURL,
			"authorization_scheme":      "Bearer",
			"authorization_credentials": key,
		},
	}
	if err := g.call(ctx, http.MethodPut, "/api/v1/provisioning/contact-points/"+contactPoint, point, http.StatusNotFound); err != nil {
		return fmt.Errorf("update the Grafana contact point: %w", err)
	}
	if err := g.call(ctx, http.MethodPost, "/api/v1/provisioning/contact-points", point, http.StatusBadRequest, http.StatusConflict); err != nil {
		return fmt.Errorf("create the Grafana contact point: %w", err)
	}

	if err := g.call(ctx, http.MethodPut, "/api/v1/provisioning/policies", map[string]any{"receiver": "Murphy", "group_by": []string{"alertname"}, "group_wait": "5s", "group_interval": "5s"}); err != nil {
		return fmt.Errorf("route Grafana alerts to Murphy: %w", err)
	}

	group := map[string]any{
		"title":     "murphy",
		"folderUid": grafanaFolder,
		"interval":  10,
		"rules": []map[string]any{
			rule("murphy-rejected-messages", "Murphy rejected messages",
				`sum(increase(murphy_messages_processed_total{murphy_outcome="rejected"}[5m]))`,
				"A component rejected messages it couldn't handle; check its logs for the reason.", "1m", false),
			rule("murphy-server-errors", "Murphy server errors",
				`sum(increase(http_server_request_duration_seconds_count{service_namespace="murphy",http_response_status_code=~"5.."}[5m]))`,
				"api or ingest answered with 5xx errors.", "1m", false),
			rule("murphy-test-alert", "Murphy test alert", `vector(1)`,
				"Fires as soon as it's resumed, to check that Grafana pages Murphy.", "0s", true),
		},
	}
	if err := g.call(ctx, http.MethodPut, "/api/v1/provisioning/folder/"+grafanaFolder+"/rule-groups/murphy", group); err != nil {
		return fmt.Errorf("create the Grafana alert rules: %w", err)
	}
	return nil
}

func rule(uid, title, expr, summary, pending string, paused bool) map[string]any {
	return map[string]any{
		"uid":          uid,
		"title":        title,
		"folderUID":    grafanaFolder,
		"ruleGroup":    "murphy",
		"condition":    "C",
		"for":          pending,
		"noDataState":  "OK",
		"execErrState": "Error",
		"isPaused":     paused,
		"labels":       map[string]string{"source": "murphy"},
		"annotations":  map[string]string{"summary": summary},
		"data": []map[string]any{
			{
				"refId":             "A",
				"relativeTimeRange": map[string]int{"from": 300, "to": 0},
				"datasourceUid":     "prometheus",
				"model":             map[string]any{"refId": "A", "expr": expr, "instant": true},
			},
			{
				"refId":         "C",
				"datasourceUid": "__expr__",
				"model": map[string]any{
					"refId":      "C",
					"type":       "threshold",
					"expression": "A",
					"conditions": []map[string]any{{"evaluator": map[string]any{"type": "gt", "params": []int{0}}}},
				},
			},
		},
	}
}

func (g grafanaClient) call(ctx context.Context, method, path string, body any, tolerated ...int) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, g.url+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.SetBasicAuth(g.user, g.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Disable-Provenance", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 300 {
		return nil
	}
	for _, code := range tolerated {
		if resp.StatusCode == code {
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, detail)
}
