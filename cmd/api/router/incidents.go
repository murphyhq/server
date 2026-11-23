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

	var filter incidents.Filter
	if request.Params.Status != nil {
		for _, status := range *request.Params.Status {
			filter.Statuses = append(filter.Statuses, murphy.IncidentStatusFromText(string(status)))
		}
	}
	if request.Params.ServiceId != nil {
		filter.ServiceIDs = *request.Params.ServiceId
	}
	found, next, err := s.incidents.ListIncidents(ctx, filter, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Incident, len(found))
	for i, incident := range found {
		items[i] = toIncident(incident)
	}
	return ListIncidents200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) GetIncident(ctx context.Context, request GetIncidentRequestObject) (_ GetIncidentResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	incident, timeline, err := s.incidents.GetIncident(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	out := toIncident(incident)
	entries := make([]TimelineEntry, len(timeline))
	for i, entry := range timeline {
		entries[i] = TimelineEntry{
			Id:      entry.GetId(),
			Type:    TimelineEntryType(entry.GetType().Text()),
			Summary: entry.GetSummary(),
			At:      entry.GetAt().AsTime(),
		}
		if id := entry.GetAlertId(); id != "" {
			entries[i].AlertId = &id
		}
	}
	return GetIncident200JSONResponse{
		Id:         out.Id,
		Number:     out.Number,
		ServiceId:  out.ServiceId,
		DedupKey:   out.DedupKey,
		Title:      out.Title,
		Status:     out.Status,
		Urgency:    out.Urgency,
		CreatedAt:  out.CreatedAt,
		UpdatedAt:  out.UpdatedAt,
		ResolvedAt: out.ResolvedAt,
		Timeline:   entries,
	}, nil
}

func toIncident(incident *murphy.Incident) Incident {
	out := Incident{
		Id:        incident.GetId(),
		Number:    incident.GetNumber(),
		ServiceId: incident.GetServiceId(),
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
	return out
}
