package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func person(name string) Reference {
	return Reference{Id: name + "-id", Name: &name}
}

func notified(kind TimelineEntryType, reason TimelineNotificationReason, channel ContactMethodType, failure string) TimelineEntry {
	notification := TimelineNotification{Reason: reason, User: person("Ana"), Channel: &channel, Failure: &failure}
	return TimelineEntry{Type: kind, Notification: &notification, Summary: "stored"}
}

func TestAPageSaysWhoWasReachedAndHow(t *testing.T) {
	assert.Equal(t, "Paged Ana by webhook", summarize(notified(TimelineEntryTypeNotified, TimelineNotificationReasonPaged, Webhook, "")))
	assert.Equal(t, "Told Ana by push that it was acknowledged", summarize(notified(TimelineEntryTypeNotified, TimelineNotificationReasonAcknowledged, Push, "")))
	assert.Equal(t, "Told Ana by push that it was resolved", summarize(notified(TimelineEntryTypeNotified, TimelineNotificationReasonResolved, Push, "")))
}

func TestAFailedNotificationSaysWhoWasntReachedAndWhy(t *testing.T) {
	failed := notified(TimelineEntryTypeNotificationFailed, TimelineNotificationReasonPaged, Push, "the phone is no longer registered for pushes")
	unreachable := TimelineEntry{Type: TimelineEntryTypeNotificationFailed, Notification: &TimelineNotification{
		Reason: TimelineNotificationReasonPaged, User: person("Ana"), Failure: new("the user has no working contact methods"),
	}}

	assert.Equal(t, "Couldn't page Ana by push: the phone is no longer registered for pushes", summarize(failed))
	assert.Equal(t, "Couldn't page Ana: the user has no working contact methods", summarize(unreachable))
}

func TestALevelNamesEveryoneItPagedOnce(t *testing.T) {
	escalation := TimelineEscalation{Level: 2, Round: 1, Paged: []Page{{User: person("Ana")}, {User: person("Bo")}, {User: person("Ana")}, {User: person("Cy")}}}
	again := TimelineEscalation{Level: 1, Round: 2, Paged: []Page{{User: person("Ana")}}}

	assert.Equal(t, "Level 2 paged Ana, Bo and Cy", summarize(TimelineEntry{Type: TimelineEntryTypeEscalated, Escalation: &escalation}))
	assert.Equal(t, "Level 1 paged Ana, round 2", summarize(TimelineEntry{Type: TimelineEntryTypeEscalated, Escalation: &again}))
}

func TestWhoActedIsNamed(t *testing.T) {
	bo := person("Bo")

	assert.Equal(t, "Acknowledged by Bo", summarize(TimelineEntry{Type: TimelineEntryTypeAcknowledged, Actor: &bo}))
	assert.Equal(t, "Resolved by Bo", summarize(TimelineEntry{Type: TimelineEntryTypeResolved, Actor: &bo}))
}

func TestSomeoneDeletedIsStillMentioned(t *testing.T) {
	gone := TimelineEntry{Type: TimelineEntryTypeNotified, Notification: &TimelineNotification{Reason: TimelineNotificationReasonPaged, User: Reference{Id: "gone", Name: new("")}, Channel: new(Webhook)}}

	assert.Equal(t, "Paged someone since deleted by webhook", summarize(gone))
}

func TestEntriesWithoutPeopleKeepTheirStoredSummary(t *testing.T) {
	alert := TimelineEntry{Type: TimelineEntryTypeTriggered, Summary: "Disk almost full"}
	exhausted := TimelineEntry{Type: TimelineEntryTypeEscalationExhausted, Summary: "Escalation is exhausted: every level and repeat was paged"}
	resolvedByMonitoring := TimelineEntry{Type: TimelineEntryTypeResolved, Summary: "Resolved: every alert recovered"}

	assert.Equal(t, "Disk almost full", summarize(alert))
	assert.Equal(t, "Escalation is exhausted: every level and repeat was paged", summarize(exhausted))
	assert.Equal(t, "Resolved: every alert recovered", summarize(resolvedByMonitoring))
}
