package notifications_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/notifications"
)

const (
	primary = "0195f3a0-0000-7000-8000-00000000000a"
	empty   = "0195f3a0-0000-7000-8000-00000000000b"
	ana     = "0195f3a0-0000-7000-8000-0000000000a1"
	bo      = "0195f3a0-0000-7000-8000-0000000000b1"
	cy      = "0195f3a0-0000-7000-8000-0000000000c1"
)

type domain struct {
	service    *notifications.Service
	repo       *MockRepository
	policy     *murphy.EscalationPolicy
	onCall     map[string]string
	active     map[string]bool
	methods    map[string][]*murphy.ContactMethod
	recipients []string
	recorded   *notifications.Change
}

func target(kind murphy.EscalationTargetType, id string) *murphy.EscalationTarget {
	return &murphy.EscalationTarget{Type: kind, Id: id}
}

func contactMethod(user string, kind murphy.ContactMethodType) *murphy.ContactMethod {
	return &murphy.ContactMethod{Id: murphy.NewID(), UserId: user, Type: kind, Address: "https://example.com/" + user}
}

func newDomain(t *testing.T) *domain {
	t.Helper()
	repo, policies, oncall, people := NewMockRepository(t), NewMockPolicies(t), NewMockOnCall(t), NewMockPeople(t)
	d := &domain{
		service: notifications.New(repo, policies, oncall, people),
		repo:    repo,
		policy: &murphy.EscalationPolicy{Id: murphy.NewID(), Version: 3, Levels: []*murphy.EscalationLevel{{
			Targets: []*murphy.EscalationTarget{
				target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, primary),
				target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, bo),
			},
		}}},
		onCall: map[string]string{primary: ana},
		active: map[string]bool{ana: true, bo: true},
		methods: map[string][]*murphy.ContactMethod{
			ana: {contactMethod(ana, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK), contactMethod(ana, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)},
			bo:  {contactMethod(bo, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK)},
		},
	}
	policies.EXPECT().GetPolicyVersion(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string, int32) (*murphy.EscalationPolicy, error) {
		return d.policy, nil
	}).Maybe()
	oncall.EXPECT().WhoIsOnCall(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string, _ time.Time) (map[string]string, error) {
		found := map[string]string{}
		for _, id := range ids {
			if user, ok := d.onCall[id]; ok {
				found[id] = user
			}
		}
		return found, nil
	}).Maybe()
	people.EXPECT().ActiveUsers(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string) (map[string]bool, error) {
		found := map[string]bool{}
		for _, id := range ids {
			found[id] = d.active[id]
		}
		return found, nil
	}).Maybe()
	people.EXPECT().ContactMethodsOfUsers(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string) (map[string][]*murphy.ContactMethod, error) {
		found := map[string][]*murphy.ContactMethod{}
		for _, id := range ids {
			if methods, ok := d.methods[id]; ok {
				found[id] = methods
			}
		}
		return found, nil
	}).Maybe()
	repo.EXPECT().RecipientsOf(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) ([]string, error) {
		return d.recipients, nil
	}).Maybe()
	repo.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, change *notifications.Change) (bool, error) {
		d.recorded = change
		return true, nil
	}).Maybe()
	return d
}

func (d *domain) escalated(level int32) *murphy.Incident {
	return &murphy.Incident{
		Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED,
		EscalationPolicyId: d.policy.GetId(), EscalationPolicyVersion: d.policy.GetVersion(), EscalationLevel: level, EscalationRound: 1,
	}
}

func (d *domain) envelopes(t *testing.T) []*murphy.Envelope {
	t.Helper()
	out := make([]*murphy.Envelope, len(d.recorded.Messages))
	for i, message := range d.recorded.Messages {
		out[i] = &murphy.Envelope{}
		require.NoError(t, proto.Unmarshal(message.Envelope, out[i]))
	}
	return out
}

func TestAPageReachesEveryoneTheLevelResolvesTo(t *testing.T) {
	d := newDomain(t)
	incident := d.escalated(1)

	require.NoError(t, d.service.IncidentEscalated(t.Context(), "message-1", incident))

	assert.Equal(t, "message-1", d.recorded.MessageID)
	page := d.recorded.Page
	require.NotNil(t, page)
	assert.Equal(t, int32(1), page.GetEscalationLevel())
	require.Len(t, page.GetReached(), 2)
	assert.Equal(t, ana, page.GetReached()[0].GetUserId())
	assert.Equal(t, primary, page.GetReached()[0].GetVia().GetId(), "reached through the schedule she is on call for")
	assert.Equal(t, int32(2), page.GetReached()[0].GetContactMethods())
	assert.Equal(t, bo, page.GetReached()[1].GetUserId())
	assert.Empty(t, page.GetUnreached())
}

func TestEachContactMethodOfEveryoneReachedGetsANotification(t *testing.T) {
	d := newDomain(t)

	require.NoError(t, d.service.IncidentEscalated(t.Context(), "message-1", d.escalated(1)))

	require.Len(t, d.recorded.Notifications, 3)
	for _, notification := range d.recorded.Notifications {
		assert.Equal(t, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, notification.GetReason())
		assert.Equal(t, d.recorded.Page.GetId(), notification.GetPageId())
	}
	envelopes := d.envelopes(t)
	require.Len(t, envelopes, 4)
	assert.Equal(t, murphy.PageVerb_PAGE_VERB_CREATED, envelopes[0].GetPage().GetVerb(), "the incident learns whom the level reached")
	for _, envelope := range envelopes[1:] {
		assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, envelope.GetNotification().GetVerb())
	}
}

func TestTargetsThatReachNobodyAreRecordedOnThePage(t *testing.T) {
	d := newDomain(t)
	d.policy.Levels[0].Targets = append(d.policy.Levels[0].Targets,
		target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, empty),
		target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, cy),
		target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, ana),
	)

	require.NoError(t, d.service.IncidentEscalated(t.Context(), "message-1", d.escalated(1)))

	page := d.recorded.Page
	assert.Len(t, page.GetReached(), 2, "Ana, reached twice, is paged once")
	require.Len(t, page.GetUnreached(), 2)
	assert.Equal(t, empty, page.GetUnreached()[0].GetId(), "nobody is on call")
	assert.Equal(t, cy, page.GetUnreached()[1].GetId(), "deactivated")
}

func TestALevelThatReachesNobodyStillAnnouncesItsPage(t *testing.T) {
	d := newDomain(t)
	delete(d.onCall, primary)
	d.active[bo] = false

	require.NoError(t, d.service.IncidentEscalated(t.Context(), "message-1", d.escalated(1)))

	assert.Empty(t, d.recorded.Page.GetReached())
	assert.Empty(t, d.recorded.Notifications)
	envelopes := d.envelopes(t)
	require.Len(t, envelopes, 1)
	assert.Equal(t, murphy.PageVerb_PAGE_VERB_CREATED, envelopes[0].GetPage().GetVerb())
}

func TestAcknowledgingTellsEveryoneAlreadyPagedOnEveryContactMethod(t *testing.T) {
	d := newDomain(t)
	d.recipients = []string{ana, bo}
	incident := d.escalated(2)
	incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED

	require.NoError(t, d.service.IncidentAcknowledged(t.Context(), "message-1", incident))

	assert.Nil(t, d.recorded.Page)
	require.Len(t, d.recorded.Notifications, 3)
	for _, notification := range d.recorded.Notifications {
		assert.Equal(t, murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED, notification.GetReason())
		assert.Empty(t, notification.GetPageId())
	}
	assert.Len(t, d.recorded.Messages, 3)
}

func TestResolvingAnIncidentNobodyWasPagedForSendsNothing(t *testing.T) {
	d := newDomain(t)

	require.NoError(t, d.service.IncidentResolved(t.Context(), "message-1", d.escalated(1)))

	assert.Equal(t, "message-1", d.recorded.MessageID)
	assert.Empty(t, d.recorded.Notifications)
	assert.Empty(t, d.recorded.Messages)
}

func TestAnOutcomeIsRecordedOnItsNotification(t *testing.T) {
	d := newDomain(t)
	sent := &murphy.Notification{Id: murphy.NewID(), SentAt: murphy.Now()}

	require.NoError(t, d.service.NotificationSent(t.Context(), "message-1", sent))

	assert.Equal(t, "message-1", d.recorded.MessageID)
	assert.Equal(t, sent, d.recorded.Outcome)
}
