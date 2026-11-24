package incidents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	murphy "github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Repository interface {
	FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (*murphy.Incident, error)
	Record(ctx context.Context, change *Change) (recorded bool, err error)
	GetIncident(ctx context.Context, id string) (*murphy.Incident, error)
	PolicyOfIncident(ctx context.Context, id, userID string) (policyID string, paged bool, err error)
	ListIncidents(ctx context.Context, filter Filter, before string, limit int) ([]*murphy.Incident, error)
	ListTimeline(ctx context.Context, incidentID string) ([]*murphy.TimelineEntry, error)
	LatestFiringAt(ctx context.Context, incidentID string) (time.Time, error)
	OpenInstances(ctx context.Context, incidentID string) ([]string, error)
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	ClaimDueMessages(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]*ScheduledMessage, error)
	DeleteMessages(ctx context.Context, ids []string) error
}

type Catalog interface {
	GetIntegrationAndService(ctx context.Context, integrationID string) (*murphy.Integration, *murphy.Service, error)
}

type Policies interface {
	CurrentVersion(ctx context.Context, id string) (int32, error)
	GetPolicyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error)
}

type Change struct {
	MessageID  string
	IncidentID string
	Alert      *murphy.Alert
	Decision   *Decision
}

type MessageKind string

const (
	MessageEvent MessageKind = "event"
	MessageTimer MessageKind = "timer"
)

type ScheduledMessage struct {
	ID         string
	IncidentID string
	Kind       MessageKind
	DueAt      time.Time
	Envelope   []byte
}

const (
	HandledRetention = 7 * 24 * time.Hour
	clockSkew        = time.Second
	resolveWait      = time.Hour
	changeAttempts   = 3
)

var errNothingToResolve = errors.New("no open incident to resolve yet; retrying until its firing alert is handled")

type Service struct {
	repo     Repository
	catalog  Catalog
	policies Policies
	versions sync.Map
}

func New(repo Repository, catalog Catalog, policies Policies) *Service {
	return &Service{repo: repo, catalog: catalog, policies: policies}
}

func (s *Service) AlertReceived(ctx context.Context, messageID string, alert *murphy.Alert) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("alert.id", alert.GetId()),
		telemetry.WithField("integration.id", alert.GetIntegrationId()),
		telemetry.WithField("service.id", alert.GetServiceId()),
	)
	defer span.End(&err)

	if err := alert.Validate(); err != nil {
		return err
	}
	service, err := s.accept(ctx, alert)
	if err != nil {
		return err
	}
	open, err := s.repo.FindOpenIncident(ctx, alert.GetServiceId(), alert.GetDedupKey())
	if err != nil && !errors.Is(err, murphy.ErrNotFound) {
		return fmt.Errorf("find open incident: %w", err)
	}

	firing := alert.GetStatus() == murphy.AlertStatus_ALERT_STATUS_FIRING
	var decision *Decision
	switch {
	case open == nil && firing:
		policy, err := s.currentPolicy(ctx, service.GetEscalationPolicyId())
		if err != nil {
			return err
		}
		decision = OpenIncident(alert, service, policy, murphy.Now())
	case open == nil && time.Since(alert.GetReceivedAt().AsTime()) < resolveWait:
		return errNothingToResolve
	case open != nil:
		stillFiring, err := s.stillFiring(ctx, open, alert)
		if err != nil {
			return err
		}
		decision = ReceiveAlert(open, alert, stillFiring, murphy.Now())
	}

	incidentID := decision.GetIncident().GetId()
	ctx = span.Set(ctx, telemetry.WithField("incident.id", incidentID))
	return s.record(ctx, &Change{MessageID: messageID, IncidentID: incidentID, Alert: alert, Decision: decision})
}

func (s *Service) TimerExpired(ctx context.Context, messageID string, timer *murphy.Timer) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", timer.GetIncidentId()),
		telemetry.WithField("escalation.level", timer.GetEscalationLevel()),
		telemetry.WithField("escalation.round", timer.GetEscalationRound()),
	)
	defer span.End(&err)

	incident, policy, err := s.escalatingIncident(ctx, timer.GetIncidentId())
	if errors.Is(err, murphy.ErrNotFound) {
		return s.record(ctx, &Change{MessageID: messageID})
	}
	if err != nil {
		return err
	}
	return s.record(ctx, &Change{MessageID: messageID, IncidentID: incident.GetId(), Decision: ExpireTimer(incident, timer, policy, murphy.Now())})
}

func (s *Service) PageCreated(ctx context.Context, messageID string, page *murphy.Page) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", page.GetIncident().GetId()),
		telemetry.WithField("escalation.level", page.GetEscalationLevel()),
		telemetry.WithField("escalation.round", page.GetEscalationRound()),
	)
	defer span.End(&err)

	incident, policy, err := s.escalatingIncident(ctx, page.GetIncident().GetId())
	if err != nil {
		return err
	}
	return s.record(ctx, &Change{MessageID: messageID, IncidentID: incident.GetId(), Decision: RecordPage(incident, page, policy, murphy.Now())})
}

func (s *Service) NotificationSent(ctx context.Context, messageID string, notification *murphy.Notification) error {
	return s.notificationOutcome(ctx, messageID, notification, false)
}

func (s *Service) NotificationFailed(ctx context.Context, messageID string, notification *murphy.Notification) error {
	return s.notificationOutcome(ctx, messageID, notification, true)
}

func (s *Service) notificationOutcome(ctx context.Context, messageID string, notification *murphy.Notification, failed bool) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", notification.GetIncident().GetId()),
		telemetry.WithField("notification.id", notification.GetId()),
	)
	defer span.End(&err)

	return s.record(ctx, &Change{MessageID: messageID, IncidentID: notification.GetIncident().GetId(), Decision: NotificationOutcome(notification, failed)})
}

func (s *Service) Acknowledge(ctx context.Context, id, actorID string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	return s.change(ctx, id, func(incident *murphy.Incident, now *timestamppb.Timestamp) *Decision {
		return AcknowledgeIncident(incident, actorID, now)
	})
}

func (s *Service) Resolve(ctx context.Context, id, actorID string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	return s.change(ctx, id, func(incident *murphy.Incident, now *timestamppb.Timestamp) *Decision {
		return ResolveIncident(incident, actorID, now)
	})
}

func (s *Service) change(ctx context.Context, id string, decide func(*murphy.Incident, *timestamppb.Timestamp) *Decision) (*murphy.Incident, error) {
	if err := murphy.LookupID("incident", id); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		incident, err := s.repo.GetIncident(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get incident: %w", err)
		}
		decision := decide(incident, murphy.Now())
		if decision == nil {
			return incident, nil
		}
		err = s.record(ctx, &Change{IncidentID: id, Decision: decision})
		if errors.Is(err, murphy.ErrConflict) && attempt < changeAttempts {
			continue
		}
		return incident, err
	}
}

func (s *Service) record(ctx context.Context, change *Change) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	recorded, err := s.repo.Record(ctx, change)
	if errors.Is(err, murphy.ErrConflict) {
		return fmt.Errorf("the incident changed meanwhile; retrying: %w", err)
	}
	if err != nil {
		return fmt.Errorf("record the change: %w", err)
	}
	span.Set(ctx, telemetry.WithField("message.duplicate", !recorded))
	return nil
}

func (s *Service) escalatingIncident(ctx context.Context, id string) (*murphy.Incident, *murphy.EscalationPolicy, error) {
	incident, err := s.repo.GetIncident(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get incident: %w", err)
	}
	if !Escalating(incident) {
		return incident, nil, nil
	}
	policy, err := s.policyVersion(ctx, incident.GetEscalationPolicyId(), incident.GetEscalationPolicyVersion())
	if errors.Is(err, murphy.ErrNotFound) {
		return incident, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get the incident's escalation policy version: %w", err)
	}
	return incident, policy, nil
}

func (s *Service) currentPolicy(ctx context.Context, id string) (*murphy.EscalationPolicy, error) {
	version, err := s.policies.CurrentVersion(ctx, id)
	if errors.Is(err, murphy.ErrNotFound) || errors.Is(err, murphy.ErrInvalid) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get the service's escalation policy version: %w", err)
	}
	return s.policyVersion(ctx, id, version)
}

func (s *Service) stillFiring(ctx context.Context, incident *murphy.Incident, resolve *murphy.Alert) (bool, error) {
	if resolve.GetStatus() == murphy.AlertStatus_ALERT_STATUS_FIRING {
		return true, nil
	}
	if resolve.GetInstance() != "" {
		instances, err := s.repo.OpenInstances(ctx, incident.GetId())
		if err != nil {
			return false, fmt.Errorf("find open instances: %w", err)
		}
		for _, instance := range instances {
			if instance != resolve.GetInstance() {
				return true, nil
			}
		}
		return false, nil
	}
	latest, err := s.repo.LatestFiringAt(ctx, incident.GetId())
	if err != nil {
		return false, fmt.Errorf("find latest firing alert: %w", err)
	}
	return resolve.GetReceivedAt().AsTime().Sub(latest) <= clockSkew, nil
}

func (s *Service) accept(ctx context.Context, alert *murphy.Alert) (*murphy.Service, error) {
	integration, service, err := s.catalog.GetIntegrationAndService(ctx, alert.GetIntegrationId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, invalidKey("its integration no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if integration.GetServiceId() != alert.GetServiceId() {
		return nil, invalidKey("it belongs to another service")
	}
	if source := murphy.Or(alert.GetSource(), murphy.IntegrationType_INTEGRATION_TYPE_GENERIC); source != integration.GetType() {
		return nil, invalidKey("it was sent in the " + source.Text() + " format, and the integration expects " + integration.GetType().Text())
	}
	if !integration.Accepts(alert.GetKeyVersion(), alert.GetReceivedAt().AsTime()) {
		return nil, invalidKey("the integration is disabled, or the key was rotated")
	}
	if service.GetStatus() != murphy.ServiceStatus_SERVICE_STATUS_ACTIVE {
		return nil, &murphy.FieldError{Field: "service_id", Reason: "the service is disabled"}
	}
	return service, nil
}

func invalidKey(reason string) error {
	return &murphy.FieldError{Field: "key", Reason: "refused because " + reason}
}

func (s *Service) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, _ []*murphy.TimelineEntry, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("incident", id); err != nil {
		return nil, nil, err
	}
	incident, err := s.repo.GetIncident(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get incident: %w", err)
	}
	timeline, err := s.repo.ListTimeline(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list timeline: %w", err)
	}
	return incident, timeline, nil
}

type Filter struct {
	Statuses      []murphy.IncidentStatus
	ServiceIDs    []string
	PolicyIDs     []string
	Urgencies     []murphy.Urgency
	PagedUserIDs  []string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

func (s *Service) ListIncidents(ctx context.Context, filter Filter, page pagination.Page) (_ []*murphy.Incident, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	for _, status := range filter.Statuses {
		if status == murphy.IncidentStatus_INCIDENT_STATUS_UNSPECIFIED || murphy.IncidentStatus_name[int32(status)] == "" {
			return nil, "", &murphy.FieldError{Field: "status", Reason: "must be triggered, acknowledged or resolved"}
		}
	}
	for _, id := range filter.ServiceIDs {
		if err := murphy.LookupID("service", id); err != nil {
			return nil, "", err
		}
	}
	for _, id := range filter.PagedUserIDs {
		if err := murphy.LookupID("user", id); err != nil {
			return nil, "", err
		}
	}
	for _, urgency := range filter.Urgencies {
		if urgency == murphy.Urgency_URGENCY_UNSPECIFIED || murphy.Urgency_name[int32(urgency)] == "" {
			return nil, "", &murphy.FieldError{Field: "urgency", Reason: "must be high or low"}
		}
	}
	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	incidents, err := s.repo.ListIncidents(ctx, filter, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list incidents: %w", err)
	}
	items, next := pagination.Trim(incidents, window)
	return items, next, nil
}

func (s *Service) PurgeHandledMessages(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := s.repo.PurgeHandledMessages(ctx, time.Now().Add(-HandledRetention))
	if err != nil {
		return fmt.Errorf("purge handled messages: %w", err)
	}
	span.Set(ctx, telemetry.WithField("messages.purged", purged))
	return nil
}

func (s *Service) policyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error) {
	key := fmt.Sprintf("%s/%d", id, version)
	if cached, ok := s.versions.Load(key); ok {
		return cached.(*murphy.EscalationPolicy), nil
	}
	policy, err := s.policies.GetPolicyVersion(ctx, id, version)
	if err != nil {
		return nil, err
	}
	s.versions.Store(key, policy)
	return policy, nil
}

func (s *Service) ClaimDueMessages(ctx context.Context, limit int, lease time.Duration) (_ []messaging.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	claimed, err := s.repo.ClaimDueMessages(ctx, time.Now().UTC(), limit, lease)
	if err != nil {
		return nil, fmt.Errorf("claim due messages: %w", err)
	}
	due := make([]messaging.ScheduledMessage, len(claimed))
	for i, message := range claimed {
		due[i] = messaging.ScheduledMessage{ID: message.ID, Kind: string(message.Kind), DueAt: message.DueAt, Envelope: message.Envelope}
	}
	return due, nil
}

func (s *Service) MessagesSent(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.repo.DeleteMessages(ctx, ids); err != nil {
		return fmt.Errorf("delete sent messages: %w", err)
	}
	return nil
}

func (s *Service) PolicyOfIncident(ctx context.Context, id, userID string) (policyID string, paged bool, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("incident", id); err != nil {
		return "", false, err
	}
	return s.repo.PolicyOfIncident(ctx, id, userID)
}
