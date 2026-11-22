//go:build integration

package testenv

import (
	"encoding/json"
	"time"
)

type Webhook struct {
	Path        string
	ContentType string
	Body        []byte
}

type GroupedAlert struct {
	Fingerprint string
	Status      string
	StartsAt    time.Time
	Summary     string
}

func jsonWebhook(path string, body any) Webhook {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return Webhook{Path: path, ContentType: "application/json", Body: data}
}

func groupStatus(alerts []GroupedAlert) string {
	for _, alert := range alerts {
		if alert.Status == "firing" {
			return "firing"
		}
	}
	return "resolved"
}

func Alertmanager(groupKey string, alerts ...GroupedAlert) Webhook {
	items := make([]map[string]any, len(alerts))
	for i, alert := range alerts {
		items[i] = map[string]any{
			"status":       alert.Status,
			"labels":       map[string]string{"alertname": "DiskFull", "instance": alert.Fingerprint},
			"annotations":  map[string]string{"summary": alert.Summary},
			"startsAt":     alert.StartsAt.Format(time.RFC3339Nano),
			"endsAt":       "0001-01-01T00:00:00Z",
			"generatorURL": "http://prometheus/graph",
			"fingerprint":  alert.Fingerprint,
		}
	}
	return jsonWebhook("/v1/alerts/alertmanager", map[string]any{
		"version": "4", "groupKey": groupKey, "truncatedAlerts": 0, "status": groupStatus(alerts), "receiver": "murphy",
		"groupLabels": map[string]string{"alertname": "DiskFull"}, "commonLabels": map[string]string{}, "commonAnnotations": map[string]string{},
		"externalURL": "http://alertmanager", "alerts": items,
	})
}

func Grafana(groupKey string, alerts ...GroupedAlert) Webhook {
	items := make([]map[string]any, len(alerts))
	for i, alert := range alerts {
		items[i] = map[string]any{
			"status":       alert.Status,
			"labels":       map[string]string{"alertname": "DiskFull", "grafana_folder": "murphy"},
			"annotations":  map[string]string{"summary": alert.Summary},
			"startsAt":     alert.StartsAt.Format(time.RFC3339Nano),
			"endsAt":       "0001-01-01T00:00:00Z",
			"generatorURL": "http://grafana/alerting/grafana/x/view",
			"fingerprint":  alert.Fingerprint,
			"silenceURL":   "http://grafana/alerting/silence/new",
			"dashboardURL": "http://grafana/d/murphy",
			"panelURL":     "http://grafana/d/murphy?viewPanel=1",
			"values":       map[string]float64{"A": 1},
		}
	}
	return jsonWebhook("/v1/alerts/grafana", map[string]any{
		"receiver": "murphy", "status": groupStatus(alerts), "orgId": 1, "groupKey": groupKey, "groupLabels": map[string]string{},
		"commonLabels": map[string]string{}, "commonAnnotations": map[string]string{}, "externalURL": "http://grafana",
		"version": "1", "truncatedAlerts": 0, "state": "alerting", "title": "[FIRING:1] DiskFull", "alerts": items,
	})
}

func Datadog(aggregationKey, cycleKey, transition string) Webhook {
	return jsonWebhook("/v1/alerts/datadog", map[string]string{
		"alert_id": "1234", "aggregation_key": aggregationKey, "cycle_key": cycleKey, "transition": transition,
		"title": "[Triggered] Disk almost full", "priority": "P1", "link": "https://app.datadoghq.com/event/1", "tags": "env:prod,role:db",
	})
}

func CloudWatch(alarmArn, state string) Webhook {
	alarm, err := json.Marshal(map[string]any{
		"AlarmName": "disk-full", "AlarmDescription": "Disk almost full", "AWSAccountId": "123456789012",
		"NewStateValue": state, "NewStateReason": "Threshold Crossed", "StateChangeTime": time.Now().UTC().Format("2006-01-02T15:04:05.000-0700"),
		"Region": "EU (Ireland)", "AlarmArn": alarmArn, "OldStateValue": "OK",
	})
	if err != nil {
		panic(err)
	}
	return sns(map[string]string{"Type": "Notification", "MessageId": "1", "TopicArn": "arn:aws:sns:eu-west-1:123456789012:alarms", "Message": string(alarm)})
}

func SNSSubscriptionConfirmation(subscribeURL string) Webhook {
	return sns(map[string]string{"Type": "SubscriptionConfirmation", "MessageId": "1", "Token": "token", "SubscribeURL": subscribeURL})
}

func sns(message map[string]string) Webhook {
	data, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	return Webhook{Path: "/v1/alerts/cloudwatch", ContentType: "text/plain; charset=UTF-8", Body: data}
}

func GoogleCloud(incidentID, state string) Webhook {
	return jsonWebhook("/v1/alerts/google-cloud", map[string]any{
		"version": "1.2",
		"incident": map[string]any{
			"incident_id": incidentID, "state": state, "started_at": time.Now().Unix(), "summary": "Disk almost full on db-1",
			"url": "https://console.cloud.google.com/monitoring/alerting/incidents/1", "policy_name": "Disk usage", "condition_name": "Disk above 90%",
			"severity": "Critical", "resource": map[string]any{"type": "gce_instance", "labels": map[string]string{"instance_id": "db-1"}},
			"metric": map[string]any{"type": "compute.googleapis.com/disk/usage", "labels": map[string]string{}},
		},
	})
}
