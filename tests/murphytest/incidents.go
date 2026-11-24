package murphytest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
)

func (s *Service) Send(payload Payload) int {
	s.m.t.Helper()
	return s.m.SendWithKey(payload, s.key)
}

func (m *Murphy) SendWithKey(payload Payload, key string) int {
	m.t.Helper()
	authorization := ""
	if key != "" {
		authorization = "Bearer " + key
	}
	status, _ := m.SendWithAuthorization(payload, authorization)
	return status
}

func (m *Murphy) SendWithAuthorization(payload Payload, authorization string) (int, map[string]any) {
	m.t.Helper()
	request, err := http.NewRequestWithContext(m.t.Context(), http.MethodPost, m.ingest+payload.Path, bytes.NewReader(payload.Body))
	require.NoError(m.t, err)
	request.Header.Set("Content-Type", payload.ContentType)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(m.t, err)
	defer func() { _ = response.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
	return response.StatusCode, body
}

func (s *Service) Fire(dedupKey string) *Incident {
	s.m.t.Helper()
	require.Equal(s.m.t, http.StatusAccepted, s.Send(Generic(dedupKey, "firing")), "send a firing alert")
	return s.IncidentFor(dedupKey)
}

const clockSkew = 1100 * time.Millisecond

func (s *Service) Recover(dedupKey string) {
	s.m.t.Helper()
	s.SendRecovery(Generic(dedupKey, "resolved"))
}

func (s *Service) SendRecovery(payload Payload) {
	s.m.t.Helper()
	time.Sleep(clockSkew)
	require.Equal(s.m.t, http.StatusAccepted, s.Send(payload), "send a recovery")
}

func (s *Service) StaysWithoutIncidents() {
	s.m.t.Helper()
	services := []string{s.ID}
	require.Never(s.m.t, func() bool {
		list, err := s.m.API.ListIncidentsWithResponse(s.m.t.Context(), &client.ListIncidentsParams{ServiceId: &services})
		return err != nil || list.StatusCode() != http.StatusOK || len(list.JSON200.Items) > 0
	}, 2*time.Second, tick, "no incident opens")
}

type ServiceExpectation func(c *assert.CollectT, incidents []client.IncidentWithTimeline)

func (s *Service) WaitFor(expectations ...ServiceExpectation) {
	s.m.t.Helper()
	services := []string{s.ID}
	require.EventuallyWithT(s.m.t, func(c *assert.CollectT) {
		list, err := s.m.API.ListIncidentsWithResponse(s.m.t.Context(), &client.ListIncidentsParams{ServiceId: &services})
		if !assert.NoError(c, err) || !assert.Equal(c, http.StatusOK, list.StatusCode()) {
			return
		}
		incidents := make([]client.IncidentWithTimeline, 0, len(list.JSON200.Items))
		for _, incident := range list.JSON200.Items {
			got, err := s.m.API.GetIncidentWithResponse(s.m.t.Context(), incident.Id)
			if !assert.NoError(c, err) || !assert.Equal(c, http.StatusOK, got.StatusCode()) {
				return
			}
			incidents = append(incidents, *got.JSON200)
		}
		for _, expect := range expectations {
			expect(c, incidents)
		}
	}, s.m.patience, tick)
}

func AlertsAcrossIncidents(count int) ServiceExpectation {
	return func(c *assert.CollectT, incidents []client.IncidentWithTimeline) {
		got := 0
		for _, incident := range incidents {
			got += alerts(&incident)
		}
		assert.Equal(c, count, got, "alerts across the service's incidents")
	}
}

func OpenIncidents(count int) ServiceExpectation {
	return func(c *assert.CollectT, incidents []client.IncidentWithTimeline) {
		open := 0
		for _, incident := range incidents {
			if incident.Status != client.IncidentStatusResolved {
				open++
			}
		}
		assert.Equal(c, count, open, "open incidents")
	}
}

func (s *Service) Incidents() []client.Incident {
	s.m.t.Helper()
	services := []string{s.ID}
	list, err := s.m.API.ListIncidentsWithResponse(s.m.t.Context(), &client.ListIncidentsParams{ServiceId: &services})
	s.m.ok(list, err, "list the service's incidents")
	return list.JSON200.Items
}

func (s *Service) IncidentFor(dedupKey string) *Incident {
	s.m.t.Helper()
	services := []string{s.ID}
	var id string
	require.EventuallyWithT(s.m.t, func(c *assert.CollectT) {
		list, err := s.m.API.ListIncidentsWithResponse(s.m.t.Context(), &client.ListIncidentsParams{ServiceId: &services})
		if !assert.NoError(c, err) || !assert.Equal(c, http.StatusOK, list.StatusCode()) {
			return
		}
		for _, incident := range list.JSON200.Items {
			if incident.DedupKey == dedupKey {
				id = incident.Id
			}
		}
		assert.NotEmpty(c, id, "an incident for %s", dedupKey)
	}, s.m.patience, tick)
	return &Incident{ID: id, m: s.m}
}

func (s *Service) WaitForIncident() *Incident {
	s.m.t.Helper()
	var id string
	services := []string{s.ID}
	require.EventuallyWithT(s.m.t, func(c *assert.CollectT) {
		list, err := s.m.API.ListIncidentsWithResponse(s.m.t.Context(), &client.ListIncidentsParams{ServiceId: &services})
		if assert.NoError(c, err) && assert.Equal(c, http.StatusOK, list.StatusCode()) && assert.Len(c, list.JSON200.Items, 1, "the service has one incident") {
			id = list.JSON200.Items[0].Id
		}
	}, s.m.patience, tick)
	return &Incident{ID: id, m: s.m}
}

type Incident struct {
	ID string
	m  *Murphy
}

func (i *Incident) Get() *client.IncidentWithTimeline {
	i.m.t.Helper()
	got, err := i.m.API.GetIncidentWithResponse(i.m.t.Context(), i.ID)
	i.m.ok(got, err, "get the incident")
	return got.JSON200
}

type Expectation func(c *assert.CollectT, incident *client.IncidentWithTimeline)

func (i *Incident) WaitFor(expectations ...Expectation) *client.IncidentWithTimeline {
	i.m.t.Helper()
	var last *client.IncidentWithTimeline
	require.EventuallyWithT(i.m.t, func(c *assert.CollectT) {
		got, err := i.m.API.GetIncidentWithResponse(i.m.t.Context(), i.ID)
		if !assert.NoError(c, err) || !assert.Equal(c, http.StatusOK, got.StatusCode()) {
			return
		}
		last = got.JSON200
		for _, expect := range expectations {
			expect(c, last)
		}
	}, i.m.patience, tick)
	return last
}

func (i *Incident) Acknowledge() *client.Incident {
	i.m.t.Helper()
	got, err := i.m.API.AcknowledgeIncidentWithResponse(i.m.t.Context(), i.ID, nil)
	i.m.ok(got, err, "acknowledge the incident")
	return got.JSON200
}

func (i *Incident) Resolve() *client.Incident {
	i.m.t.Helper()
	got, err := i.m.API.ResolveIncidentWithResponse(i.m.t.Context(), i.ID, nil)
	i.m.ok(got, err, "resolve the incident")
	return got.JSON200
}

func Status(status client.IncidentStatus) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		assert.Equal(c, status, incident.Status)
	}
}

func Entries(types ...client.TimelineEntryType) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		var got []client.TimelineEntryType
		for _, entry := range incident.Timeline {
			if slices.Contains(types, entry.Type) {
				got = append(got, entry.Type)
			}
		}
		assert.Equal(c, types, got, "timeline entries of these types, in order")
	}
}

func Alerts(count int) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		assert.Equal(c, count, alerts(incident), "alerts on the timeline")
	}
}

func alerts(incident *client.IncidentWithTimeline) int {
	count := 0
	for _, entry := range incident.Timeline {
		if entry.Alert != nil {
			count++
		}
	}
	return count
}

func Paged(users ...*User) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		want := make([]string, len(users))
		for i, user := range users {
			want[i] = user.ID
		}
		assert.ElementsMatch(c, want, PagedUsers(incident))
	}
}

func PagedVia(user *User, via client.EscalationTarget) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		assert.Contains(c, pages(incident), client.Page{User: client.Reference{Id: user.ID}, Via: via})
	}
}

func pages(incident *client.IncidentWithTimeline) []client.Page {
	var all []client.Page
	for _, entry := range incident.Timeline {
		if entry.Type == client.TimelineEntryTypeEscalated {
			all = append(all, entry.Escalation.Paged...)
		}
	}
	return all
}

func PagedUsers(incident *client.IncidentWithTimeline) []string {
	var paged []string
	for _, entry := range incident.Timeline {
		if entry.Type == client.TimelineEntryTypeEscalated {
			for _, page := range entry.Escalation.Paged {
				paged = append(paged, page.User.Id)
			}
		}
	}
	return paged
}

func Notified(user *User, channel client.ContactMethodType) Expectation {
	return notification(client.TimelineEntryTypeNotified, user, channel)
}

func NotificationFailed(user *User, channel client.ContactMethodType) Expectation {
	return notification(client.TimelineEntryTypeNotificationFailed, user, channel)
}

func NotReachable(user *User) Expectation {
	return notification(client.TimelineEntryTypeNotificationFailed, user, "")
}

func NotificationFailedBecause(user *User, channel client.ContactMethodType, failure string) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		var failures []string
		for _, entry := range incident.Timeline {
			if entry.Type == client.TimelineEntryTypeNotificationFailed && entry.Notification.User.Id == user.ID && entry.Notification.Channel != nil && *entry.Notification.Channel == channel {
				failures = append(failures, *entry.Notification.Failure)
			}
		}
		assert.Equal(c, []string{failure}, failures, "why the %s notification for %s failed", channel, user.Email)
	}
}

func notification(kind client.TimelineEntryType, user *User, channel client.ContactMethodType) Expectation {
	return func(c *assert.CollectT, incident *client.IncidentWithTimeline) {
		for _, entry := range incident.Timeline {
			if entry.Type != kind || entry.Notification == nil || entry.Notification.User.Id != user.ID {
				continue
			}
			if channel == "" && entry.Notification.Channel == nil || entry.Notification.Channel != nil && *entry.Notification.Channel == channel {
				return
			}
		}
		assert.Fail(c, "no such notification on the timeline", "%s for %s by %q", kind, user.Email, channel)
	}
}
