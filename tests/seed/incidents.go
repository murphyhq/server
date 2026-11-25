package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type seededAlert struct {
	service, dedupKey, summary string
	resolved                   bool
}

var seededAlerts = []seededAlert{
	{"storefront", "checkout-5xx", "Checkout is returning 5xx for 12% of requests", false},
	{"kubernetes", "node-pressure", "3 nodes in eu-west-1b under memory pressure", false},
	{"postgres-cluster", "replica-lag", "Replica lag above 30s on pg-main-2", true},
	{"auth-gateway", "login-latency", "SSO logins taking over 8s at p95", false},
	{"fraud-checks", "scoring-timeout", "Fraud scoring timing out for 4% of payments", false},
	{"ios-app", "push-failures", "Push delivery failures above 5% on iOS", false},
	{"warehouse-etl", "nightly-load", "Nightly load into the warehouse failed", false},
	{"dns", "resolver-errors", "SERVFAIL rate rising on internal resolvers", false},
	{"cdn", "image-origin", "Image origin returning 503", true},
}

func seedIncidents(ctx context.Context, ingest string, keys map[string]string) error {
	var fired []seededAlert
	for _, alert := range seededAlerts {
		key, ok := keys[alert.service]
		if !ok {
			continue
		}
		if err := sendAlert(ctx, ingest, key, alert, "firing"); err != nil {
			return err
		}
		fired = append(fired, alert)
	}
	if len(fired) == 0 {
		return nil
	}
	time.Sleep(3 * time.Second)
	for _, alert := range fired {
		if !alert.resolved {
			continue
		}
		if err := sendAlert(ctx, ingest, keys[alert.service], alert, "resolved"); err != nil {
			return err
		}
	}
	return nil
}

func sendAlert(ctx context.Context, ingest, key string, alert seededAlert, status string) error {
	body, err := json.Marshal(map[string]string{"dedup_key": alert.dedupKey, "status": status, "summary": alert.summary})
	if err != nil {
		return fmt.Errorf("encode alert %s: %w", alert.dedupKey, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, ingest+"/v1/alerts", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build alert %s: %w", alert.dedupKey, err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("send alert %s: %w", alert.dedupKey, err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("send alert %s: %s", alert.dedupKey, response.Status)
	}
	return nil
}
