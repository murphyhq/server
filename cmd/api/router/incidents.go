package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
)

func (s *Server) ListIncidents(ctx context.Context, request ListIncidentsRequestObject) (_ ListIncidentsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	filter := incidents.Filter{
		ServiceIDs:    all(request.Params.ServiceId),
		PagedUserIDs:  all(request.Params.PagedUserId),
		CreatedAfter:  request.Params.CreatedAfter,
		CreatedBefore: request.Params.CreatedBefore,
	}
	for _, status := range all(request.Params.Status) {
		filter.Statuses = append(filter.Statuses, murphy.IncidentStatusFromText(string(status)))
	}
	for _, urgency := range all(request.Params.Urgency) {
		filter.Urgencies = append(filter.Urgencies, murphy.UrgencyFromText(string(urgency)))
	}
	if teams := all(request.Params.TeamId); len(teams) > 0 {
		policies, err := s.escalation.PoliciesOfTeams(ctx, teams)
		if err != nil {
			return nil, err
		}
		if len(policies) == 0 {
			return ListIncidents200JSONResponse{Items: []Incident{}}, nil
		}
		for id, name := range policies {
			filter.PolicyIDs = append(filter.PolicyIDs, id)
			n.knowPolicy(id, name)
		}
	}
	found, next, err := s.incidents.ListIncidents(ctx, filter, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Incident, len(found))
	for i, incident := range found {
		items[i] = toIncident(incident, n)
	}
	return ListIncidents200JSONResponse{Items: items, NextCursor: next}, s.name(ctx, n)
}

func (s *Server) GetIncident(ctx context.Context, request GetIncidentRequestObject) (_ GetIncidentResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	incident, timeline, err := s.incidents.GetIncident(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	out := toIncident(incident, n)
	entries := make([]TimelineEntry, len(timeline))
	for i, entry := range timeline {
		entries[i] = TimelineEntry{
			Id:      entry.GetId(),
			Type:    TimelineEntryType(entry.GetType().Text()),
			Summary: entry.GetSummary(),
			At:      entry.GetAt().AsTime(),
		}
		if id := entry.GetActorUserId(); id != "" {
			actor := n.user(id)
			entries[i].Actor = &actor
		}
		if id := entry.GetAlertId(); id != "" {
			alert := ref(id)
			entries[i].Alert = &alert
		}
		if escalation := entry.GetEscalation(); escalation != nil {
			out := &TimelineEscalation{Level: escalation.GetLevel(), Round: escalation.GetRound(), Paged: []Page{}, ReachedNobody: []EscalationTarget{}}
			for _, page := range escalation.GetPaged() {
				out.Paged = append(out.Paged, Page{User: n.user(page.GetUserId()), Via: n.target(page.GetVia())})
			}
			for _, target := range escalation.GetReachedNobody() {
				out.ReachedNobody = append(out.ReachedNobody, n.target(target))
			}
			entries[i].Escalation = out
		}
		if notification := entry.GetNotification(); notification != nil {
			out := &TimelineNotification{Reason: TimelineNotificationReason(notification.GetReason().Text()), User: n.user(notification.GetUserId())}
			if id := notification.GetContactMethodId(); id != "" {
				method := ref(id)
				out.ContactMethod = &method
				channel := ContactMethodType(notification.GetChannel().Text())
				out.Channel = &channel
			}
			if failure := notification.GetFailure(); failure != "" {
				out.Failure = &failure
			}
			entries[i].Notification = out
		}
	}
	return GetIncident200JSONResponse{
		Id:         out.Id,
		Number:     out.Number,
		Service:    out.Service,
		DedupKey:   out.DedupKey,
		Title:      out.Title,
		Status:     out.Status,
		Urgency:    out.Urgency,
		CreatedAt:  out.CreatedAt,
		UpdatedAt:  out.UpdatedAt,
		ResolvedAt: out.ResolvedAt,
		Timeline:   entries,

		AcknowledgedAt: out.AcknowledgedAt,
		Escalation:     out.Escalation,
	}, s.name(ctx, n)
}

func toIncident(incident *murphy.Incident, n *names) Incident {
	out := Incident{
		Id:        incident.GetId(),
		Number:    incident.GetNumber(),
		Service:   n.service(incident.GetServiceId()),
		DedupKey:  incident.GetDedupKey(),
		Title:     incident.GetTitle(),
		Status:    IncidentStatus(incident.GetStatus().Text()),
		Urgency:   Urgency(incident.GetUrgency().Text()),
		CreatedAt: incident.GetCreatedAt().AsTime(),
		UpdatedAt: incident.GetUpdatedAt().AsTime(),
	}
	if at := incident.GetResolvedAt(); at != nil {
		t := at.AsTime()
		out.ResolvedAt = &t
	}
	if at := incident.GetAcknowledgedAt(); at != nil {
		t := at.AsTime()
		out.AcknowledgedAt = &t
	}
	if incident.GetEscalationPolicyId() != "" {
		out.Escalation = &IncidentEscalation{
			Policy:    PolicyVersionReference{Id: incident.GetEscalationPolicyId(), Name: n.policy(incident.GetEscalationPolicyId()).Name, Version: incident.GetEscalationPolicyVersion()},
			Level:     incident.GetEscalationLevel(),
			Round:     incident.GetEscalationRound(),
			Exhausted: incident.GetEscalationExhausted(),
		}
	}
	return out
}

func (s *Server) AcknowledgeIncident(ctx context.Context, request AcknowledgeIncidentRequestObject) (_ AcknowledgeIncidentResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	incident, err := s.incidents.Acknowledge(ctx, request.Id, sessionFrom(ctx).session.GetUserId())
	if err != nil {
		return nil, err
	}
	return AcknowledgeIncident200JSONResponse(toIncident(incident, nil)), s.name(ctx, nil)
}

func (s *Server) ResolveIncident(ctx context.Context, request ResolveIncidentRequestObject) (_ ResolveIncidentResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	incident, err := s.incidents.Resolve(ctx, request.Id, sessionFrom(ctx).session.GetUserId())
	if err != nil {
		return nil, err
	}
	return ResolveIncident200JSONResponse(toIncident(incident, nil)), s.name(ctx, nil)
}

func all[T any](values *[]T) []T {
	if values == nil {
		return nil
	}
	return *values
}
