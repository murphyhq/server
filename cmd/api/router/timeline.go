package router

import (
	"fmt"
	"slices"
	"strings"
)

func summarize(entry TimelineEntry) string {
	switch {
	case entry.Notification != nil:
		return notificationSummary(entry.Type, *entry.Notification)
	case entry.Type == TimelineEntryTypeEscalated && entry.Escalation != nil && len(entry.Escalation.Paged) > 0:
		return escalationSummary(*entry.Escalation)
	case entry.Type == TimelineEntryTypeAcknowledged && entry.Actor != nil:
		return "Acknowledged by " + nameOf(*entry.Actor)
	case entry.Type == TimelineEntryTypeResolved && entry.Actor != nil:
		return "Resolved by " + nameOf(*entry.Actor)
	default:
		return entry.Summary
	}
}

func escalationSummary(escalation TimelineEscalation) string {
	var people []string
	for _, page := range escalation.Paged {
		if name := nameOf(page.User); !slices.Contains(people, name) {
			people = append(people, name)
		}
	}
	summary := fmt.Sprintf("Level %d paged %s", escalation.Level, listOf(people))
	if escalation.Round > 1 {
		summary += fmt.Sprintf(", round %d", escalation.Round)
	}
	return summary
}

func notificationSummary(kind TimelineEntryType, notification TimelineNotification) string {
	person := nameOf(notification.User)
	what := map[TimelineNotificationReason][2]string{
		TimelineNotificationReasonPaged:        {"Paged " + person, "Couldn't page " + person},
		TimelineNotificationReasonAcknowledged: {"Told " + person, "Couldn't tell " + person},
		TimelineNotificationReasonResolved:     {"Told " + person, "Couldn't tell " + person},
	}[notification.Reason]
	about := map[TimelineNotificationReason]string{
		TimelineNotificationReasonAcknowledged: " that it was acknowledged",
		TimelineNotificationReasonResolved:     " that it was resolved",
	}[notification.Reason]
	channel := ""
	if notification.Channel != nil {
		channel = " by " + string(*notification.Channel)
	}
	if kind == TimelineEntryTypeNotified {
		return what[0] + channel + about
	}
	failure := ""
	if notification.Failure != nil {
		failure = ": " + *notification.Failure
	}
	return what[1] + channel + about + failure
}

func nameOf(person Reference) string {
	if person.Name == nil || *person.Name == "" {
		return "someone since deleted"
	}
	return *person.Name
}

func listOf(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
