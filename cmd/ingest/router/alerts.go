package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	maxDedupKey = 255
	maxBody     = 1 << 20
)

type translate func(ctx context.Context, s *Server, body []byte) ([]*murphy.Alert, error)

type source struct {
	integration murphy.IntegrationType
	translate   translate
}

var sources = map[ReceiveSourceAlertsParamsSource]source{
	Alertmanager: {murphy.IntegrationType_INTEGRATION_TYPE_ALERTMANAGER, fromAlertmanager},
	Grafana:      {murphy.IntegrationType_INTEGRATION_TYPE_GRAFANA, fromGrafana},
	Datadog:      {murphy.IntegrationType_INTEGRATION_TYPE_DATADOG, fromDatadog},
	Cloudwatch:   {murphy.IntegrationType_INTEGRATION_TYPE_CLOUDWATCH, fromCloudWatch},
	GoogleCloud:  {murphy.IntegrationType_INTEGRATION_TYPE_GOOGLE_CLOUD, fromGoogleCloud},
}

func (s *Server) ReceiveAlert(ctx context.Context, request ReceiveAlertRequestObject) (_ ReceiveAlertResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	alert := &murphy.Alert{
		DedupKey: request.Body.DedupKey,
		Status:   murphy.AlertStatusFromText(string(request.Body.Status)),
		Summary:  request.Body.Summary,
	}
	if request.Body.Labels != nil {
		alert.Labels = *request.Body.Labels
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_GENERIC, alert)
	if err != nil {
		return nil, err
	}
	return ReceiveAlert202JSONResponse{Id: ids[0]}, nil
}

func (s *Server) ReceiveSourceAlerts(ctx context.Context, request ReceiveSourceAlertsRequestObject) (_ ReceiveSourceAlertsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	src, ok := sources[request.Source]
	if !ok {
		return nil, &murphy.NotFoundError{Entity: "alert source"}
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read the request body: %w", err)
	}
	if len(body) > maxBody {
		return nil, invalid("body", "must be at most 1 MiB")
	}
	alerts, err := src.translate(ctx, s, body)
	if err != nil {
		return nil, err
	}
	ids, err := s.publish(ctx, src.integration, alerts...)
	if err != nil {
		return nil, err
	}
	return ReceiveSourceAlerts202JSONResponse{Ids: ids}, nil
}

func (s *Server) publish(ctx context.Context, source murphy.IntegrationType, alerts ...*murphy.Alert) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	key := keyFrom(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("integration.id", key.GetIntegrationId()),
		telemetry.WithField("service.id", key.GetServiceId()),
		telemetry.WithField("alert.source", source.Text()),
		telemetry.WithField("alert.count", len(alerts)),
	)

	now := murphy.Now()
	for _, alert := range alerts {
		alert.Id = murphy.NewID()
		alert.IntegrationId = key.GetIntegrationId()
		alert.ServiceId = key.GetServiceId()
		alert.KeyVersion = key.GetKeyVersion()
		alert.Source = source
		alert.ReceivedAt = now
		alert.DedupKey = dedupKey(alert.GetDedupKey())
		if err := alert.Validate(); err != nil {
			return nil, err
		}
	}

	ids := make([]string, len(alerts))
	for i, alert := range alerts {
		envelope := &murphy.Envelope{
			Id:         murphy.NewID(),
			OccurredAt: now,
			Event: &murphy.Envelope_Alert{Alert: &murphy.AlertEvent{
				Verb:    murphy.AlertVerb_ALERT_VERB_RECEIVED,
				Current: alert,
			}},
		}
		if err := s.publisher.Publish(ctx, envelope); err != nil {
			return nil, fmt.Errorf("publish alert %d of %d: %w", i+1, len(alerts), err)
		}
		ids[i] = envelope.GetId()
	}
	return ids, nil
}

type groupedAlertPayload struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	SilenceURL   string            `json:"silenceURL"`
	DashboardURL string            `json:"dashboardURL"`
	PanelURL     string            `json:"panelURL"`
}

type groupPayload struct {
	GroupKey          string                `json:"groupKey"`
	Title             string                `json:"title"`
	CommonAnnotations map[string]string     `json:"commonAnnotations"`
	Alerts            []groupedAlertPayload `json:"alerts"`
}

func fromAlertmanager(_ context.Context, _ *Server, body []byte) ([]*murphy.Alert, error) {
	return fromGroup(body)
}

func fromGrafana(_ context.Context, _ *Server, body []byte) ([]*murphy.Alert, error) {
	return fromGroup(body)
}

func fromGroup(body []byte) ([]*murphy.Alert, error) {
	var group groupPayload
	if err := json.Unmarshal(body, &group); err != nil {
		return nil, invalid("body", "must be a webhook payload")
	}
	if group.GroupKey == "" || len(group.Alerts) == 0 {
		return nil, invalid("body", "must have a groupKey and at least one alert")
	}
	alerts := make([]*murphy.Alert, len(group.Alerts))
	for i, a := range group.Alerts {
		annotations := maps.Clone(a.Annotations)
		if annotations == nil {
			annotations = map[string]string{}
		}
		for name, link := range map[string]string{"generator_url": a.GeneratorURL, "dashboard_url": a.DashboardURL, "panel_url": a.PanelURL, "silence_url": a.SilenceURL} {
			if link != "" {
				annotations[name] = link
			}
		}
		alerts[i] = &murphy.Alert{
			DedupKey:    group.GroupKey,
			Instance:    a.Fingerprint + "@" + a.StartsAt.UTC().Format(time.RFC3339Nano),
			Status:      murphy.AlertStatusFromText(a.Status),
			Summary:     firstOf(a.Annotations["summary"], a.Annotations["description"], group.CommonAnnotations["summary"], group.Title, a.Labels["alertname"]),
			Labels:      maps.Clone(a.Labels),
			Annotations: annotations,
		}
	}
	return alerts, nil
}

type datadogPayload struct {
	AlertID        string `json:"alert_id"`
	AggregationKey string `json:"aggregation_key"`
	CycleKey       string `json:"cycle_key"`
	Transition     string `json:"transition"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Priority       string `json:"priority"`
	Link           string `json:"link"`
	Tags           string `json:"tags"`
}

func fromDatadog(_ context.Context, _ *Server, body []byte) ([]*murphy.Alert, error) {
	var payload datadogPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Transition == "" {
		return nil, invalid("body", "must be Murphy's Datadog webhook template")
	}
	status := murphy.AlertStatus_ALERT_STATUS_FIRING
	if payload.Transition == "Recovered" {
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	}
	labels := map[string]string{}
	for tag := range strings.SplitSeq(payload.Tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			key, value, _ := strings.Cut(tag, ":")
			labels[key] = value
		}
	}
	annotations := map[string]string{"transition": payload.Transition}
	for name, value := range map[string]string{"link": payload.Link, "priority": payload.Priority, "message": payload.Message} {
		if value != "" {
			annotations[name] = value
		}
	}
	return []*murphy.Alert{{
		DedupKey:    firstOf(payload.AggregationKey, payload.AlertID),
		Instance:    payload.CycleKey,
		Status:      status,
		Summary:     payload.Title,
		Labels:      labels,
		Annotations: annotations,
	}}, nil
}

var snsHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$`)

type snsPayload struct {
	Type         string `json:"Type"`
	Message      string `json:"Message"`
	SubscribeURL string `json:"SubscribeURL"`
}

type cloudWatchAlarm struct {
	AlarmName                  string            `json:"AlarmName"`
	AlarmDescription           string            `json:"AlarmDescription"`
	AlarmArn                   string            `json:"AlarmArn"`
	AWSAccountID               string            `json:"AWSAccountId"`
	Region                     string            `json:"Region"`
	NewStateValue              string            `json:"NewStateValue"`
	NewStateReason             string            `json:"NewStateReason"`
	AlarmContributorID         string            `json:"AlarmContributorId"`
	AlarmContributorAttributes map[string]string `json:"AlarmContributorAttributes"`
}

func fromCloudWatch(ctx context.Context, s *Server, body []byte) ([]*murphy.Alert, error) {
	var message snsPayload
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, invalid("body", "must be an SNS message")
	}
	switch message.Type {
	case "SubscriptionConfirmation":
		return nil, s.confirmSubscription(ctx, message.SubscribeURL)
	case "Notification":
	default:
		return nil, nil
	}

	var alarm cloudWatchAlarm
	if err := json.Unmarshal([]byte(message.Message), &alarm); err != nil || alarm.AlarmName == "" {
		return nil, invalid("Message", "must be a CloudWatch alarm notification")
	}
	var status murphy.AlertStatus
	switch alarm.NewStateValue {
	case "ALARM":
		status = murphy.AlertStatus_ALERT_STATUS_FIRING
	case "OK":
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	default:
		return nil, nil
	}
	dedup := firstOf(alarm.AlarmArn, alarm.AlarmName)
	if alarm.AlarmContributorID != "" {
		dedup += "/" + alarm.AlarmContributorID
	}
	labels := map[string]string{"alarm_name": alarm.AlarmName, "account_id": alarm.AWSAccountID, "region": alarm.Region}
	maps.Copy(labels, alarm.AlarmContributorAttributes)
	return []*murphy.Alert{{
		DedupKey:    dedup,
		Status:      status,
		Summary:     firstOf(alarm.AlarmDescription, alarm.AlarmName),
		Labels:      labels,
		Annotations: map[string]string{"reason": alarm.NewStateReason, "alarm_arn": alarm.AlarmArn},
	}}, nil
}

func (s *Server) confirmSubscription(ctx context.Context, subscribeURL string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	u, err := url.Parse(subscribeURL)
	if err != nil || u.Scheme != "https" || !snsHost.MatchString(u.Hostname()) {
		return invalid("SubscribeURL", "must be an https URL on an SNS endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("build subscription confirmation: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("confirm SNS subscription: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("confirm SNS subscription: " + resp.Status)
	}
	return nil
}

type googleCloudPayload struct {
	Incident struct {
		IncidentID    string `json:"incident_id"`
		State         string `json:"state"`
		Summary       string `json:"summary"`
		URL           string `json:"url"`
		PolicyName    string `json:"policy_name"`
		ConditionName string `json:"condition_name"`
		Severity      string `json:"severity"`
		Resource      struct {
			Labels map[string]string `json:"labels"`
		} `json:"resource"`
		Metric struct {
			Labels map[string]string `json:"labels"`
		} `json:"metric"`
		PolicyUserLabels map[string]string `json:"policy_user_labels"`
	} `json:"incident"`
}

func fromGoogleCloud(_ context.Context, _ *Server, body []byte) ([]*murphy.Alert, error) {
	var payload googleCloudPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Incident.IncidentID == "" {
		return nil, invalid("body", "must be a Cloud Monitoring incident")
	}
	incident := payload.Incident
	status := murphy.AlertStatus_ALERT_STATUS_FIRING
	if incident.State == "closed" {
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	}
	labels := map[string]string{}
	maps.Copy(labels, incident.Resource.Labels)
	maps.Copy(labels, incident.Metric.Labels)
	maps.Copy(labels, incident.PolicyUserLabels)
	annotations := map[string]string{}
	for name, value := range map[string]string{"url": incident.URL, "policy_name": incident.PolicyName, "condition_name": incident.ConditionName, "severity": incident.Severity} {
		if value != "" {
			annotations[name] = value
		}
	}
	return []*murphy.Alert{{
		DedupKey:    incident.IncidentID,
		Instance:    incident.IncidentID,
		Status:      status,
		Summary:     firstOf(incident.Summary, incident.PolicyName),
		Labels:      labels,
		Annotations: annotations,
	}}, nil
}

func invalid(field, reason string) error {
	return &murphy.FieldError{Field: field, Reason: reason}
}

func dedupKey(key string) string {
	if len(key) <= maxDedupKey {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
