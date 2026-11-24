//go:build stack

package stack

import (
	"net/http"
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
	ana := m.User(murphytest.WithWebhook(), murphytest.WithPhone())
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
	assert.Equal(t, incident.ID, push.IncidentID)
	assert.Equal(t, m.APIURL(), push.Server)
}

func TestAPhoneWhoseTokenIsGoneIsDisabledAndNotTriedAgain(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithGonePhone())
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
	ana := m.User()
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
	ana := m.User(murphytest.WithWebhook())
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

	created, err := m.API.CreateContactMethodWithResponse(t.Context(), ana.ID, nil, client.ContactMethodInput{Type: client.Email, Address: ana.Email})

	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, created.StatusCode())
}
