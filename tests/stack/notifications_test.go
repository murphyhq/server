//go:build stack

package stack

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
	"github.com/murphyhq/server/tests/murphytest"
)

func TestAPageReachesEveryContactMethodOfEveryonePaged(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook(), murphytest.WithPhone(), murphytest.ReachedAtOnce())
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(
		murphytest.Paged(ana),
		murphytest.Notified(ana, client.Webhook),
		murphytest.Notified(ana, client.Push),
	)
	page := ana.Webhook().WaitForWebhooks(1)[0]
	assert.True(t, page.Verified, "signed with a key the API publishes")
	assert.Equal(t, webhook.IncidentPaged, page.Body.Type)
	assert.Equal(t, incident.ID, page.Body.Data.Incident.Id)
	assert.Equal(t, ana.ID, page.Body.Data.User.Id)
	assert.Equal(t, ana.Webhook().ContactMethodID, page.Body.Data.ContactMethod.Id)
	require.NotNil(t, page.Body.Data.Escalation)
	assert.Equal(t, int32(1), page.Body.Data.Escalation.Level)
}

func TestAPageReachesAPhoneThroughTheRelay(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithPhone())
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.Paged(ana), murphytest.Notified(ana, client.Push))
	push := ana.Phone().WaitForPushes(1)[0]
	assert.Equal(t, "ios", push.Platform)
	assert.Equal(t, "paged", push.Event)
	assert.Equal(t, "incident", push.Subject.Type)
	assert.Equal(t, incident.ID, push.Subject.ID)
	assert.Equal(t, m.APIURL(), push.Server)
}

func TestAPhoneWhoseTokenIsGoneIsDisabledAndNotTriedAgain(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithGonePhone(), murphytest.WithoutEmail())
	service := m.Service(murphytest.Pages(ana.Target()))
	reason := "the phone is no longer registered for pushes, so it's disabled until the app registers again"

	service.Fire("disk-full").WaitFor(murphytest.NotificationFailedBecause(ana, client.Push, reason))
	phone := ana.WaitForDisabledPhone()
	next := service.Fire("cpu-high")

	assert.Equal(t, &reason, phone.DisabledReason)
	next.WaitFor(murphytest.Unreachable(ana))
}

func TestANewTokenEnablesADisabledPhone(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithGonePhone())
	m.Service(murphytest.Pages(ana.Target())).Fire("disk-full")
	phone := ana.WaitForDisabledPhone()
	ios := client.Ios

	updated, err := m.API.UpdateContactMethodWithResponse(t.Context(), phone.Id, client.ContactMethodInput{Type: client.Push, Address: m.Name("device"), Platform: &ios})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, updated.StatusCode())
	assert.Nil(t, updated.JSON200.DisabledAt)
	assert.Nil(t, updated.JSON200.DisabledReason)
}

func TestTheSameDeadTokenKeepsAPhoneDisabled(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithGonePhone())
	m.Service(murphytest.Pages(ana.Target())).Fire("disk-full")
	phone := ana.WaitForDisabledPhone()
	ios := client.Ios

	updated, err := m.API.UpdateContactMethodWithResponse(t.Context(), phone.Id, client.ContactMethodInput{Type: client.Push, Address: phone.Address, Platform: &ios})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, updated.StatusCode())
	assert.Equal(t, phone.DisabledAt, updated.JSON200.DisabledAt)
}

func TestSomeoneOnCallIsPagedThroughTheirSchedule(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	ana := m.User(murphytest.WithWebhook())
	primary := m.Schedule(team, ana)
	service := m.Service(murphytest.OwnedBy(team), murphytest.Pages(primary.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.PagedVia(ana, primary.Target()), murphytest.Notified(ana, client.Webhook))
}

func TestAcknowledgingAndResolvingTellEveryonePaged(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Pages(ana.Target()))
	incident := service.Fire("disk-full")
	ana.Webhook().WaitForWebhooks(1)

	incident.Acknowledge()
	incident.Resolve()

	var types []webhook.WebhookType
	for _, received := range ana.Webhook().WaitForWebhooks(3) {
		assert.True(t, received.Verified)
		types = append(types, received.Body.Type)
	}
	assert.ElementsMatch(t, []webhook.WebhookType{webhook.IncidentPaged, webhook.IncidentAcknowledged, webhook.IncidentResolved}, types)
}

func TestAFailingWebhookIsRetriedUntilItIsSent(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook(murphytest.FailsWith(http.StatusServiceUnavailable)))
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.Notified(ana, client.Webhook))
	attempts := ana.Webhook().WaitForWebhooks(2)
	assert.Equal(t, attempts[0].ID, attempts[1].ID, "a retry carries the same webhook-id")
}

func TestARefusedWebhookFailsAtOnce(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook(murphytest.Refuses(http.StatusGone)))
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.NotificationFailedBecause(ana, client.Webhook, "the receiver answered 410"))
	assert.Len(t, ana.Webhook().Received(), 1, "a refused webhook is not sent again")
}

func TestSomeonePagedWithoutContactMethodsIsOnTheTimeline(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithoutEmail())
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.Paged(ana), murphytest.NotReachable(ana))
}

func TestALevelThatReachesNobodyMovesOnAtOnce(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	nobody := m.Schedule(team)
	bo := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.OwnedBy(team), murphytest.Levels(
		murphytest.Level{Delay: time.Hour, Targets: []client.EscalationTarget{nobody.Target()}},
		murphytest.Level{Delay: time.Hour, Targets: []client.EscalationTarget{bo.Target()}},
	))

	incident := service.Fire("disk-full")

	incident.WaitFor(
		murphytest.Entries(client.TimelineEntryTypeEscalationSkipped, client.TimelineEntryTypeEscalated),
		murphytest.Paged(bo),
		murphytest.Notified(bo, client.Webhook),
	)
}

func TestResolvingThroughTheAlertTellsEveryonePaged(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Pages(ana.Target()))
	service.Fire("disk-full").WaitFor(murphytest.Notified(ana, client.Webhook))

	service.Recover("disk-full")

	received := ana.Webhook().WaitForWebhooks(2)
	assert.Equal(t, []webhook.WebhookType{webhook.IncidentPaged, webhook.IncidentResolved}, []webhook.WebhookType{received[0].Body.Type, received[1].Body.Type})
}

func TestAPageGivesUpWhenTheNextLevelIsDue(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook(murphytest.Refuses(http.StatusServiceUnavailable)))
	service := m.Service(murphytest.Levels(murphytest.Within(2*time.Second, ana.Target())))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.NotificationFailedBecause(ana, client.Webhook, "still failing after 2s: the receiver answered 503"))
}

func TestWebhooksCarryASignatureFromEveryKeyDuringARotation(t *testing.T) {
	m := murphytest.New(t)
	keys := m.SigningKeys()
	require.Len(t, keys, 2, "the local stack is always mid-rotation")
	ana := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Pages(ana.Target()))

	service.Fire("disk-full")

	page := ana.Webhook().WaitForWebhooks(1)[0]
	assert.Equal(t, 2, page.Signatures())
	assert.True(t, page.VerifiesWith(keys[0]), "a receiver still on the old key accepts it")
	assert.True(t, page.VerifiesWith(keys[1]), "a receiver already on the new key accepts it")
}

func TestTheTimelineSaysWhoWasPagedAndHow(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook(), murphytest.WithoutEmail())
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(
		murphytest.Summaries(client.TimelineEntryTypeEscalated, "Level 1 paged "+ana.Name),
		murphytest.Summaries(client.TimelineEntryTypeNotified, "Paged "+ana.Name+" by webhook"),
	)
}

func TestAContactMethodMurphyCantSendToYetIsRefused(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User()

	created, err := m.API.CreateContactMethodWithResponse(t.Context(), ana.ID, nil, client.ContactMethodInput{Type: client.Sms, Address: "+14155550100"})

	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, created.StatusCode())
}

func TestSomeoneNewCanBeReachedAtTheirEmail(t *testing.T) {
	m := murphytest.New(t)

	ana := m.User()

	methods := ana.ContactMethods()
	require.Len(t, methods, 1)
	assert.Equal(t, client.Email, methods[0].Type)
	assert.Equal(t, ana.Email, methods[0].Address)
}

func TestAPageReachesSomeoneByEmail(t *testing.T) {
	m := murphytest.New(t)
	m.EmailThroughMailpit()
	ana := m.User()
	service := m.Service(murphytest.Pages(ana.Target()))

	incident := service.Fire("disk-full")

	mail := ana.Mailbox().WaitForMail(1)
	assert.Contains(t, mail[0].Subject, "disk-full is firing")
	incident.WaitFor(murphytest.Summaries(client.TimelineEntryTypeNotified, "Paged "+ana.Name+" by email"))
}

func TestAPasswordOutsideWhatTheInstallAllowsFailsTheEmailProvider(t *testing.T) {
	m := murphytest.New(t)

	m.SetEmailProvider(murphytest.Mailpit("env://SMTP_PASSWORD"))

	failing := m.WaitForEmailProvider(client.EmailProviderStatusFailing)
	assert.Contains(t, failing.Problem, "only variables starting with MURPHY_SECRET_")
}

func TestAnAdminSetsAndRemovesTheEmailProvider(t *testing.T) {
	m := murphytest.New(t)
	input := client.EmailProviderInput{Type: client.Smtp, From: "Murphy <murphy@example.com>", Host: "smtp.example.com", Port: 587, Username: new("murphy"), Secret: new("env://SMTP_PASSWORD")}

	set, err := m.API.SetEmailProviderWithResponse(t.Context(), input)
	require.NoError(t, err)
	read, err := m.API.GetEmailProviderWithResponse(t.Context())
	require.NoError(t, err)
	removed, err := m.API.RemoveEmailProviderWithResponse(t.Context())
	require.NoError(t, err)
	gone, err := m.API.GetEmailProviderWithResponse(t.Context())
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, set.StatusCode())
	require.Equal(t, http.StatusOK, read.StatusCode())
	assert.Equal(t, set.JSON200.UpdatedAt, read.JSON200.UpdatedAt)
	assert.Equal(t, "smtp.example.com", read.JSON200.Host)
	assert.Equal(t, "env://SMTP_PASSWORD", read.JSON200.Secret)
	assert.Equal(t, client.EmailProviderStatusPending, set.JSON200.Status, "notifier hasn't checked it yet")
	assert.Equal(t, http.StatusNoContent, removed.StatusCode())
	assert.Equal(t, http.StatusNotFound, gone.StatusCode())
}

func TestOnlyAdminsSeeOrChangeTheEmailProvider(t *testing.T) {
	m := murphytest.New(t)
	bo := m.ClientWith(m.SessionAs(murphytest.Bo))

	read, err := bo.GetEmailProviderWithResponse(t.Context())
	require.NoError(t, err)
	set, err := bo.SetEmailProviderWithResponse(t.Context(), client.EmailProviderInput{Type: client.Smtp, From: "murphy@example.com", Host: "smtp.example.com", Port: 587})
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, read.StatusCode())
	assert.Equal(t, http.StatusForbidden, set.StatusCode())
}

func TestSomeoneChoosesHowTheyArePagedAndCanGoBackToTheDefault(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook())
	email, hook := ana.ContactMethodOf(client.Email), ana.ContactMethodOf(client.Webhook)
	steps := client.NotificationRuleInput{Steps: []client.NotificationStep{
		{ContactMethod: client.Reference{Id: email.Id}, DelaySeconds: 120},
		{ContactMethod: client.Reference{Id: hook.Id}, DelaySeconds: 0},
	}}

	before, err := m.API.ListNotificationRulesWithResponse(t.Context(), ana.ID)
	require.NoError(t, err)
	set, err := m.API.SetNotificationRuleWithResponse(t.Context(), ana.ID, client.High, steps)
	require.NoError(t, err)
	reset, err := m.API.ResetNotificationRuleWithResponse(t.Context(), ana.ID, client.High)
	require.NoError(t, err)
	after, err := m.API.ListNotificationRulesWithResponse(t.Context(), ana.ID)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, before.StatusCode())
	assert.Equal(t, []client.NotificationRule{
		{Urgency: client.High, Steps: []client.NotificationStep{{ContactMethod: client.Reference{Id: email.Id}}, {ContactMethod: client.Reference{Id: hook.Id}}}},
		{Urgency: client.Low, Steps: []client.NotificationStep{{ContactMethod: client.Reference{Id: email.Id}}, {ContactMethod: client.Reference{Id: hook.Id}}}},
	}, before.JSON200.Items, "without a phone, the default reaches everything at once")
	require.Equal(t, http.StatusOK, set.StatusCode())
	assert.Equal(t, client.NotificationRule{Urgency: client.High, Custom: true, Steps: []client.NotificationStep{
		{ContactMethod: client.Reference{Id: hook.Id}, DelaySeconds: 0},
		{ContactMethod: client.Reference{Id: email.Id}, DelaySeconds: 120},
	}}, *set.JSON200, "steps come back in the order they fire")
	assert.Equal(t, http.StatusNoContent, reset.StatusCode())
	assert.Equal(t, before.JSON200.Items, after.JSON200.Items)
}

func TestRulesThatWouldDelayEveryNotificationAreRefused(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User()
	email := ana.ContactMethodOf(client.Email)

	set, err := m.API.SetNotificationRuleWithResponse(t.Context(), ana.ID, client.High, client.NotificationRuleInput{Steps: []client.NotificationStep{
		{ContactMethod: client.Reference{Id: email.Id}, DelaySeconds: 300},
	}})

	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, set.StatusCode())
}

func TestNobodyChangesHowSomeoneElseIsPaged(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User()
	bo := m.Member(murphytest.Bo)

	read, err := bo.API.ListNotificationRulesWithResponse(t.Context(), ana.ID)
	require.NoError(t, err)
	reset, err := bo.API.ResetNotificationRuleWithResponse(t.Context(), ana.ID, client.High)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, read.StatusCode())
	assert.Equal(t, http.StatusForbidden, reset.StatusCode())
}

func TestAcknowledgingStopsTheStepsStillWaiting(t *testing.T) {
	m := murphytest.New(t)
	m.EmailThroughMailpit()
	ana := m.User(murphytest.WithWebhook())
	set, err := m.API.SetNotificationRuleWithResponse(t.Context(), ana.ID, client.High, client.NotificationRuleInput{Steps: []client.NotificationStep{
		{ContactMethod: client.Reference{Id: ana.Webhook().ContactMethodID}, DelaySeconds: 0},
		{ContactMethod: client.Reference{Id: ana.ContactMethodOf(client.Email).Id}, DelaySeconds: 10},
	}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, set.StatusCode())
	incident := m.Service(murphytest.Pages(ana.Target())).Fire("disk-full")
	ana.Webhook().WaitForWebhooks(1)

	incident.Acknowledge()

	told := ana.Mailbox().WaitForMail(1)
	assert.True(t, strings.HasPrefix(told[0].Subject, "[Murphy] Acknowledged:"), "only the acknowledgement reaches her by email: %s", told[0].Subject)
	ana.Mailbox().GetsNoMoreFor(1, 15*time.Second)
}

func TestATestPageShowsWhetherEachContactMethodWasReached(t *testing.T) {
	m := murphytest.New(t)
	m.EmailThroughMailpit()
	bo := m.Member(murphytest.Bo)

	sent, err := bo.API.SendTestPageWithResponse(t.Context(), nil, client.TestPageInput{})
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, sent.StatusCode())
	page := bo.WaitForTestPage(sent.JSON202.Id)
	again, err := bo.API.SendTestPageWithResponse(t.Context(), nil, client.TestPageInput{})
	require.NoError(t, err)
	someoneElses, err := m.API.GetTestPageWithResponse(t.Context(), sent.JSON202.Id)
	require.NoError(t, err)

	assert.Equal(t, client.TestDeliveryStatusSent, murphytest.DeliveriesByChannel(page)[client.Email], "everyone has their email")
	assert.Empty(t, murphytest.Failures(page))
	assert.Equal(t, http.StatusTooManyRequests, again.StatusCode(), "one test page a minute")
	assert.Equal(t, http.StatusNotFound, someoneElses.StatusCode(), "a test page is only its sender's, admins included")
}
