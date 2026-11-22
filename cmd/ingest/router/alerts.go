package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

const maxDedupKey = 255

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

func (s *Server) ReceiveAlertmanagerAlerts(ctx context.Context, request ReceiveAlertmanagerAlertsRequestObject) (_ ReceiveAlertmanagerAlertsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	body := request.Body
	alerts := make([]*murphy.Alert, len(body.Alerts))
	for i, a := range body.Alerts {
		alerts[i] = groupedAlert(body.GroupKey, string(a.Status), a.Fingerprint, a.StartsAt, a.Labels, a.Annotations, value(body.CommonAnnotations))
		addLink(alerts[i], "generator_url", a.GeneratorURL)
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_ALERTMANAGER, alerts...)
	if err != nil {
		return nil, err
	}
	return ReceiveAlertmanagerAlerts202JSONResponse{Ids: ids}, nil
}

func groupedAlert(groupKey, status, fingerprint string, startsAt time.Time, labels, annotations, common map[string]string) *murphy.Alert {
	alert := &murphy.Alert{
		DedupKey:    groupKey,
		Instance:    fingerprint + "@" + startsAt.UTC().Format(time.RFC3339Nano),
		Status:      murphy.AlertStatusFromText(status),
		Summary:     firstOf(annotations["summary"], annotations["description"], common["summary"], labels["alertname"]),
		Labels:      maps.Clone(labels),
		Annotations: maps.Clone(annotations),
	}
	if alert.Annotations == nil {
		alert.Annotations = map[string]string{}
	}
	return alert
}

func addLink(alert *murphy.Alert, name string, url *string) {
	if url != nil && *url != "" {
		alert.Annotations[name] = *url
	}
}

func value[T any](pointer *T) T {
	var zero T
	if pointer == nil {
		return zero
	}
	return *pointer
}

func (s *Server) ReceiveGrafanaAlerts(ctx context.Context, request ReceiveGrafanaAlertsRequestObject) (_ ReceiveGrafanaAlertsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	body := request.Body
	common := value(body.CommonAnnotations)
	if title := value(body.Title); title != "" && common["summary"] == "" {
		common = map[string]string{"summary": title}
	}
	alerts := make([]*murphy.Alert, len(body.Alerts))
	for i, a := range body.Alerts {
		alerts[i] = groupedAlert(body.GroupKey, string(a.Status), a.Fingerprint, a.StartsAt, a.Labels, a.Annotations, common)
		addLink(alerts[i], "generator_url", a.GeneratorURL)
		addLink(alerts[i], "dashboard_url", a.DashboardURL)
		addLink(alerts[i], "panel_url", a.PanelURL)
		addLink(alerts[i], "silence_url", a.SilenceURL)
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_GRAFANA, alerts...)
	if err != nil {
		return nil, err
	}
	return ReceiveGrafanaAlerts202JSONResponse{Ids: ids}, nil
}

const datadogRecovered = "Recovered"

func (s *Server) ReceiveDatadogAlert(ctx context.Context, request ReceiveDatadogAlertRequestObject) (_ ReceiveDatadogAlertResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	body := request.Body
	status := murphy.AlertStatus_ALERT_STATUS_FIRING
	if body.Transition == datadogRecovered {
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	}
	alert := &murphy.Alert{
		DedupKey:    firstOf(value(body.AggregationKey), body.AlertId),
		Instance:    value(body.CycleKey),
		Status:      status,
		Summary:     body.Title,
		Labels:      datadogTags(value(body.Tags)),
		Annotations: map[string]string{"transition": body.Transition},
	}
	for name, field := range map[string]*string{"link": body.Link, "priority": body.Priority, "message": body.Message} {
		addLink(alert, name, field)
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_DATADOG, alert)
	if err != nil {
		return nil, err
	}
	return ReceiveDatadogAlert202JSONResponse{Ids: ids}, nil
}

func datadogTags(tags string) map[string]string {
	labels := map[string]string{}
	for tag := range strings.SplitSeq(tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key, value, _ := strings.Cut(tag, ":")
		labels[key] = value
	}
	return labels
}

var snsHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$`)

type snsMessage struct {
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

func (s *Server) ReceiveCloudWatchAlarm(ctx context.Context, request ReceiveCloudWatchAlarmRequestObject) (_ ReceiveCloudWatchAlarmResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	var message snsMessage
	if err := json.Unmarshal([]byte(*request.Body), &message); err != nil {
		return nil, &murphy.FieldError{Field: "body", Reason: "must be an SNS message"}
	}
	switch message.Type {
	case "SubscriptionConfirmation":
		if err := s.confirmSubscription(ctx, message.SubscribeURL); err != nil {
			return nil, err
		}
		return ReceiveCloudWatchAlarm202JSONResponse{Ids: []string{}}, nil
	case "Notification":
	default:
		return ReceiveCloudWatchAlarm202JSONResponse{Ids: []string{}}, nil
	}

	var alarm cloudWatchAlarm
	if err := json.Unmarshal([]byte(message.Message), &alarm); err != nil || alarm.AlarmName == "" {
		return nil, &murphy.FieldError{Field: "Message", Reason: "must be a CloudWatch alarm notification"}
	}
	var status murphy.AlertStatus
	switch alarm.NewStateValue {
	case "ALARM":
		status = murphy.AlertStatus_ALERT_STATUS_FIRING
	case "OK":
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	default:
		return ReceiveCloudWatchAlarm202JSONResponse{Ids: []string{}}, nil
	}

	dedup := firstOf(alarm.AlarmArn, alarm.AlarmName)
	if alarm.AlarmContributorID != "" {
		dedup += "/" + alarm.AlarmContributorID
	}
	labels := map[string]string{"alarm_name": alarm.AlarmName, "account_id": alarm.AWSAccountID, "region": alarm.Region}
	for name, value := range alarm.AlarmContributorAttributes {
		labels[name] = value
	}
	alert := &murphy.Alert{
		DedupKey:    dedup,
		Status:      status,
		Summary:     firstOf(alarm.AlarmDescription, alarm.AlarmName),
		Labels:      labels,
		Annotations: map[string]string{"reason": alarm.NewStateReason, "alarm_arn": alarm.AlarmArn},
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_CLOUDWATCH, alert)
	if err != nil {
		return nil, err
	}
	return ReceiveCloudWatchAlarm202JSONResponse{Ids: ids}, nil
}

func (s *Server) confirmSubscription(ctx context.Context, subscribeURL string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	u, err := url.Parse(subscribeURL)
	if err != nil || u.Scheme != "https" || !snsHost.MatchString(u.Hostname()) {
		return &murphy.FieldError{Field: "SubscribeURL", Reason: "must be an https URL on an SNS endpoint"}
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

func (s *Server) ReceiveGoogleCloudIncident(ctx context.Context, request ReceiveGoogleCloudIncidentRequestObject) (_ ReceiveGoogleCloudIncidentResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	incident := request.Body.Incident
	status := murphy.AlertStatus_ALERT_STATUS_FIRING
	if incident.State == Closed {
		status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	}
	labels := map[string]string{}
	if incident.Resource != nil {
		maps.Copy(labels, value(incident.Resource.Labels))
	}
	if incident.Metric != nil {
		maps.Copy(labels, value(incident.Metric.Labels))
	}
	maps.Copy(labels, value(incident.PolicyUserLabels))

	alert := &murphy.Alert{
		DedupKey:    incident.IncidentId,
		Instance:    incident.IncidentId,
		Status:      status,
		Summary:     firstOf(incident.Summary, value(incident.PolicyName)),
		Labels:      labels,
		Annotations: map[string]string{},
	}
	for name, field := range map[string]*string{"url": incident.Url, "policy_name": incident.PolicyName, "condition_name": incident.ConditionName, "severity": incident.Severity} {
		addLink(alert, name, field)
	}
	ids, err := s.publish(ctx, murphy.IntegrationType_INTEGRATION_TYPE_GOOGLE_CLOUD, alert)
	if err != nil {
		return nil, err
	}
	return ReceiveGoogleCloudIncident202JSONResponse{Ids: ids}, nil
}
