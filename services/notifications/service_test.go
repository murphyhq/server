package notifications_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

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
	delays     map[string]time.Duration
	recipients []string
	recorded   *notifications.Change
	people     *MockPeople
	steps      []string
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
		people:  people,
		policy: &murphy.EscalationPolicy{Id: murphy.NewID(), Version: 3, Levels: []*murphy.EscalationLevel{{
			Delay: durationpb.New(5 * time.Minute),
			Targets: []*murphy.EscalationTarget{
				target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, primary),
				target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, bo),
			},
		}}},
		onCall: map[string]string{primary: ana},
		active: map[string]bool{ana: true, bo: true},
		delays: map[string]time.Duration{},
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
		assert.NotContains(t, ids, "", "only real user IDs are looked up")
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
	people.EXPECT().NotificationRulesOfUsers(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string, urgency murphy.Urgency) (map[string]*murphy.NotificationRule, error) {
		found := map[string]*murphy.NotificationRule{}
		for _, id := range ids {
			rule := &murphy.NotificationRule{UserId: id, Urgency: urgency}
			for _, method := range d.methods[id] {
				rule.Steps = append(rule.Steps, &murphy.NotificationStep{ContactMethodId: method.GetId(), ContactMethod: method, Delay: durationpb.New(d.delays[method.GetId()])})
			}
			found[id] = rule
		}
		return found, nil
	}).Maybe()
	repo.EXPECT().RecipientsOf(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) ([]string, error) {
		return d.recipients, nil
	}).Maybe()
	repo.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, change *notifications.Change) (bool, error) {
		d.recorded = change
		d.steps = append(d.steps, "record")
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
		assert.WithinDuration(t, time.Now().Add(5*time.Minute), notification.GetDeadline().AsTime(), time.Second, "retried until the next level is due")
	}
	envelopes := d.envelopes(t)
	require.Len(t, envelopes, 4)
	assert.Equal(t, murphy.PageVerb_PAGE_VERB_CREATED, envelopes[0].GetPage().GetVerb(), "the incident learns whom the level reached")
	for _, envelope := range envelopes[1:] {
		assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, envelope.GetNotification().GetVerb())
	}
}

func TestALaterStepWaitsForItsDelayAndCanStillBeRetriedAfterTheNextLevel(t *testing.T) {
	d := newDomain(t)
	email := d.methods[ana][1]
	d.delays[email.GetId()] = 2 * time.Minute

	require.NoError(t, d.service.IncidentEscalated(t.Context(), "message-1", d.escalated(1)))

	require.Len(t, d.recorded.Messages, 4)
	webhook, later := d.recorded.Messages[1], d.recorded.Messages[2]
	assert.WithinDuration(t, time.Now(), webhook.DueAt, time.Second, "the first step goes at once")
	assert.Nil(t, webhook.Waiting, "a step going at once is never cancelled")
	assert.WithinDuration(t, time.Now().Add(2*time.Minute), later.DueAt, time.Second)
	require.NotNil(t, later.Waiting, "a step that waits can be cancelled")
	assert.Equal(t, email.GetId(), later.Waiting.GetContactMethod().GetId())
	assert.WithinDuration(t, time.Now().Add(7*time.Minute), later.Waiting.GetDeadline().AsTime(), time.Second, "retried for 5 minutes from when it's due, past the next level")
}

func TestAcknowledgingStopsTheStepsStillWaiting(t *testing.T) {
	d := newDomain(t)
	d.recipients = []string{ana}
	incident := d.escalated(1)
	incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED

	require.NoError(t, d.service.IncidentAcknowledged(t.Context(), "message-1", incident))

	assert.Equal(t, incident.GetId(), d.recorded.StopWaiting)
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

func TestAPhoneThatIsGoneIsDisabledBeforeTheOutcomeIsRecorded(t *testing.T) {
	d := newDomain(t)
	phone := &murphy.ContactMethod{Id: murphy.NewID(), UserId: ana, Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH, Address: "dead-token"}
	failed := &murphy.Notification{Id: murphy.NewID(), ContactMethod: phone, Failure: "the phone is no longer registered", ContactMethodGone: true}
	d.people.EXPECT().DisableContactMethod(mock.Anything, phone, "the phone is no longer registered", mock.Anything).RunAndReturn(func(context.Context, *murphy.ContactMethod, string, time.Time) error {
		d.steps = append(d.steps, "disable")
		return nil
	})

	require.NoError(t, d.service.NotificationFailed(t.Context(), "message-1", failed))

	assert.Equal(t, []string{"disable", "record"}, d.steps, "a redelivery after a crash disables it again")
	assert.Equal(t, failed, d.recorded.Outcome)
}

func TestAnOutcomeWaitsUntilTheGonePhoneIsDisabled(t *testing.T) {
	d := newDomain(t)
	failed := &murphy.Notification{Id: murphy.NewID(), ContactMethod: &murphy.ContactMethod{Id: murphy.NewID()}, ContactMethodGone: true}
	d.people.EXPECT().DisableContactMethod(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("the database is down"))

	err := d.service.NotificationFailed(t.Context(), "message-1", failed)

	assert.Error(t, err, "so the broker redelivers it")
	assert.Nil(t, d.recorded)
}

func smtpProvider() *murphy.EmailProvider {
	return &murphy.EmailProvider{
		Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "Murphy <murphy@example.com>", Host: "smtp.example.com", Port: 587,
		Username: "murphy", Secret: "env://SMTP_PASSWORD",
	}
}

func TestAnEmailProviderReadsItsSecretFromAReference(t *testing.T) {
	d := newDomain(t)
	var kept []string
	d.repo.EXPECT().SetEmailProvider(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, provider *murphy.EmailProvider) error {
		kept = append(kept, provider.GetSecret())
		return nil
	})

	for _, secret := range []string{"env://SMTP_PASSWORD", "file:///run/secrets/smtp", "aws-ssm:///murphy/smtp-password"} {
		provider := smtpProvider()
		provider.Secret = secret
		_, err := d.service.SetEmailProvider(t.Context(), provider)
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"env://SMTP_PASSWORD", "file:///run/secrets/smtp", "aws-ssm:///murphy/smtp-password"}, kept)
}

func TestAnEmailProviderWithoutASecretNeedsNoUsername(t *testing.T) {
	d := newDomain(t)
	d.repo.EXPECT().SetEmailProvider(mock.Anything, mock.Anything).Return(nil)
	provider := smtpProvider()
	provider.Username, provider.Secret = "", ""

	got, err := d.service.SetEmailProvider(t.Context(), provider)

	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), got.GetUpdatedAt().AsTime(), time.Minute)
}

func TestAnInvalidEmailProviderIsRefusedAndNotKept(t *testing.T) {
	cases := map[string]func(*murphy.EmailProvider){
		"no type":                       func(p *murphy.EmailProvider) { p.Type = murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_UNSPECIFIED },
		"a sender that isn't an email":  func(p *murphy.EmailProvider) { p.From = "Murphy" },
		"no host":                       func(p *murphy.EmailProvider) { p.Host = "" },
		"no port":                       func(p *murphy.EmailProvider) { p.Port = 0 },
		"a port too high":               func(p *murphy.EmailProvider) { p.Port = 65536 },
		"the password itself":           func(p *murphy.EmailProvider) { p.Secret = "hunter2" },
		"an unknown secret store":       func(p *murphy.EmailProvider) { p.Secret = "vault://murphy/smtp" },
		"an env reference with a path":  func(p *murphy.EmailProvider) { p.Secret = "env://SMTP/PASSWORD" },
		"a file reference without path": func(p *murphy.EmailProvider) { p.Secret = "file://" },
		"a secret without a username":   func(p *murphy.EmailProvider) { p.Username = "" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDomain(t)
			provider := smtpProvider()
			change(provider)

			_, err := d.service.SetEmailProvider(t.Context(), provider)

			require.ErrorIs(t, err, murphy.ErrInvalid)
			d.repo.AssertNotCalled(t, "SetEmailProvider", mock.Anything, mock.Anything)
		})
	}
}
