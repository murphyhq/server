package incidents

import (
	"context"
	"errors"
	"fmt"
	"time"

	murphy "github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Repository interface {
	FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (*murphy.Incident, error)
	RecordAlert(ctx context.Context, outcome *AlertOutcome) (recorded bool, err error)
	GetIncident(ctx context.Context, id string) (*murphy.Incident, error)
	ListIncidents(ctx context.Context, statuses []murphy.IncidentStatus, before string, limit int) ([]*murphy.Incident, error)
	ListTimeline(ctx context.Context, incidentID string) ([]*murphy.TimelineEntry, error)
	LatestFiringAt(ctx context.Context, incidentID string) (time.Time, error)
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
}

type Catalog interface {
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
	GetService(ctx context.Context, id string) (*murphy.Service, error)
}

type AlertOutcome struct {
	MessageID  string
	Alert      *murphy.Alert
	IncidentID string
	Opened     *murphy.Incident
	Joined     *murphy.Incident
	Resolved   *murphy.Incident
	Timeline   []*murphy.TimelineEntry
}

const (
	HandledRetention = 7 * 24 * time.Hour
	clockSkew        = time.Second
)

type Service struct {
	repo    Repository
	catalog Catalog
}

func New(repo Repository, catalog Catalog) *Service {
	return &Service{repo: repo, catalog: catalog}
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

	now := murphy.Now()
	outcome := &AlertOutcome{MessageID: messageID, Alert: alert}
	firing := alert.GetStatus() == murphy.AlertStatus_ALERT_STATUS_FIRING
	switch {
	case open == nil && firing:
		incident := &murphy.Incident{
			Id:        murphy.NewID(),
			ServiceId: alert.GetServiceId(),
			DedupKey:  alert.GetDedupKey(),
			Title:     alert.GetSummary(),
			Status:    murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED,
			Urgency:   service.GetUrgency(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		outcome.IncidentID, outcome.Opened = incident.GetId(), incident
		outcome.Timeline = append(outcome.Timeline, entry(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, alert, now))
	case open != nil && firing:
		open.UpdatedAt = now
		outcome.IncidentID, outcome.Joined = open.GetId(), open
		outcome.Timeline = append(outcome.Timeline, entry(open, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, alert, now))
	case open != nil:
		outcome.IncidentID = open.GetId()
		latest, err := s.repo.LatestFiringAt(ctx, open.GetId())
		if err != nil {
			return fmt.Errorf("find latest firing alert: %w", err)
		}
		if alert.GetReceivedAt().AsTime().Sub(latest) <= clockSkew {
			open.UpdatedAt = now
			outcome.Joined = open
			outcome.Timeline = append(outcome.Timeline, entry(open, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, alert, now))
			break
		}
		open.Resolve(now)
		outcome.Resolved = open
		outcome.Timeline = append(outcome.Timeline, entry(open, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, alert, now))
	}
	if outcome.IncidentID != "" {
		ctx = span.Set(ctx, telemetry.WithField("incident.id", outcome.IncidentID))
	}

	recorded, err := s.repo.RecordAlert(ctx, outcome)
	if err != nil {
		return fmt.Errorf("record alert: %w", err)
	}
	span.Set(ctx, telemetry.WithField("message.duplicate", !recorded))
	return nil
}

func (s *Service) accept(ctx context.Context, alert *murphy.Alert) (*murphy.Service, error) {
	integration, err := s.catalog.GetIntegration(ctx, alert.GetIntegrationId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, invalidKey("its integration no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if integration.GetServiceId() != alert.GetServiceId() {
		return nil, invalidKey("it belongs to another service")
	}
	if !integration.Accepts(alert.GetKeyVersion(), alert.GetReceivedAt().AsTime()) {
		return nil, invalidKey("the integration is disabled, or the key was rotated")
	}
	service, err := s.catalog.GetService(ctx, alert.GetServiceId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, invalidKey("its service no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if service.GetStatus() != murphy.ServiceStatus_SERVICE_STATUS_ACTIVE {
		return nil, &murphy.FieldError{Field: "service_id", Reason: "the service is disabled"}
	}
	return service, nil
}

func invalidKey(reason string) error {
	return &murphy.FieldError{Field: "key", Reason: "refused because " + reason}
}

func entry(incident *murphy.Incident, kind murphy.TimelineEntryType, alert *murphy.Alert, at *timestamppb.Timestamp) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{
		Id:         murphy.NewID(),
		IncidentId: incident.GetId(),
		Type:       kind,
		AlertId:    alert.GetId(),
		Summary:    alert.GetSummary(),
		At:         at,
	}
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

func (s *Service) ListIncidents(ctx context.Context, statuses []murphy.IncidentStatus, page pagination.Page) (_ []*murphy.Incident, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	for _, status := range statuses {
		if status == murphy.IncidentStatus_INCIDENT_STATUS_UNSPECIFIED || murphy.IncidentStatus_name[int32(status)] == "" {
			return nil, "", &murphy.FieldError{Field: "status", Reason: "must be triggered, acknowledged or resolved"}
		}
	}
	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	incidents, err := s.repo.ListIncidents(ctx, statuses, window.Before, window.Rows())
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
