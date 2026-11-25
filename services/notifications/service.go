package notifications

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Repository interface {
	Record(ctx context.Context, change *Change) (recorded bool, err error)
	RecipientsOf(ctx context.Context, incidentID string) ([]string, error)
	ClaimDueMessages(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]*ScheduledMessage, error)
	DeleteMessages(ctx context.Context, ids []string) error
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	GetEmailProvider(ctx context.Context) (*murphy.EmailProvider, error)
	SetEmailProvider(ctx context.Context, provider *murphy.EmailProvider) error
	DeleteEmailProvider(ctx context.Context) error
}

type Policies interface {
	GetPolicyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error)
}

type OnCall interface {
	WhoIsOnCall(ctx context.Context, scheduleIDs []string, at time.Time) (map[string]string, error)
}

type People interface {
	ActiveUsers(ctx context.Context, ids []string) (map[string]bool, error)
	ContactMethodsOfUsers(ctx context.Context, userIDs []string) (map[string][]*murphy.ContactMethod, error)
	DisableContactMethod(ctx context.Context, method *murphy.ContactMethod, reason string, at time.Time) error
}

type Change struct {
	MessageID     string
	Page          *murphy.Page
	Notifications []*murphy.Notification
	Outcome       *murphy.Notification
	Messages      []*ScheduledMessage
}

type ScheduledMessage struct {
	ID       string
	DueAt    time.Time
	Envelope []byte
}

const (
	HandledRetention = 7 * 24 * time.Hour
	FollowUpDeadline = 5 * time.Minute
)

type Service struct {
	repo     Repository
	policies Policies
	oncall   OnCall
	people   People
	versions sync.Map
}

func New(repo Repository, policies Policies, oncall OnCall, people People) *Service {
	return &Service{repo: repo, policies: policies, oncall: oncall, people: people}
}

func (s *Service) IncidentEscalated(ctx context.Context, messageID string, incident *murphy.Incident) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", incident.GetId()),
		telemetry.WithField("escalation.level", incident.GetEscalationLevel()),
		telemetry.WithField("escalation.round", incident.GetEscalationRound()),
	)
	defer span.End(&err)

	policy, err := s.policyVersion(ctx, incident.GetEscalationPolicyId(), incident.GetEscalationPolicyVersion())
	if err != nil {
		return fmt.Errorf("get the incident's escalation policy version: %w", err)
	}
	level := incident.GetEscalationLevel()
	if level < 1 || int(level) > len(policy.GetLevels()) {
		return messaging.Reject(fmt.Errorf("escalation level %d is not in policy version %d", level, policy.GetVersion()))
	}
	now := murphy.Now()
	page := &murphy.Page{
		Id: murphy.NewID(), Incident: incident, EscalationLevel: level, EscalationRound: incident.GetEscalationRound(), CreatedAt: now,
	}
	nextLevelDue := timestamppb.New(now.AsTime().Add(policy.GetLevels()[level-1].GetDelay().AsDuration()))
	if err := s.reach(ctx, page, policy.GetLevels()[level-1], now.AsTime()); err != nil {
		return err
	}
	users := make([]string, len(page.GetReached()))
	for i, recipient := range page.GetReached() {
		users[i] = recipient.GetUserId()
	}
	methods, err := s.people.ContactMethodsOfUsers(ctx, users)
	if err != nil {
		return fmt.Errorf("find contact methods: %w", err)
	}
	var created []*murphy.Notification
	for _, recipient := range page.GetReached() {
		recipient.ContactMethods = int32(len(methods[recipient.GetUserId()]))
		for _, method := range methods[recipient.GetUserId()] {
			notification := newNotification(incident, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, method, now, nextLevelDue)
			notification.PageId, notification.Via = page.GetId(), recipient.GetVia()
			created = append(created, notification)
		}
	}

	pageCreated, err := message(ctx, now.AsTime(), &murphy.Envelope{
		Event: &murphy.Envelope_Page{Page: &murphy.PageEvent{Verb: murphy.PageVerb_PAGE_VERB_CREATED, Current: page}},
	})
	if err != nil {
		return err
	}
	messages, err := notificationsCreated(ctx, created)
	if err != nil {
		return err
	}
	return s.record(ctx, &Change{MessageID: messageID, Page: page, Notifications: created, Messages: append([]*ScheduledMessage{pageCreated}, messages...)})
}

func (s *Service) reach(ctx context.Context, page *murphy.Page, level *murphy.EscalationLevel, at time.Time) error {
	var schedules []string
	for _, target := range level.GetTargets() {
		if target.GetType() == murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE {
			schedules = append(schedules, target.GetId())
		}
	}
	onCall, err := s.oncall.WhoIsOnCall(ctx, schedules, at)
	if err != nil {
		return fmt.Errorf("find who is on call: %w", err)
	}
	candidates := make([]string, len(level.GetTargets()))
	for i, target := range level.GetTargets() {
		candidates[i] = target.GetId()
		if target.GetType() == murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE {
			candidates[i] = onCall[target.GetId()]
		}
	}
	active, err := s.people.ActiveUsers(ctx, slices.DeleteFunc(slices.Clone(candidates), func(user string) bool { return user == "" }))
	if err != nil {
		return fmt.Errorf("find active users: %w", err)
	}
	seen := map[string]bool{}
	for i, target := range level.GetTargets() {
		user := candidates[i]
		switch {
		case user == "" || !active[user]:
			page.Unreached = append(page.Unreached, target)
		case !seen[user]:
			seen[user] = true
			page.Reached = append(page.Reached, &murphy.PageRecipient{UserId: user, Via: target})
		}
	}
	return nil
}

func (s *Service) IncidentAcknowledged(ctx context.Context, messageID string, incident *murphy.Incident) error {
	return s.followUp(ctx, messageID, incident, murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED)
}

func (s *Service) IncidentResolved(ctx context.Context, messageID string, incident *murphy.Incident) error {
	return s.followUp(ctx, messageID, incident, murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED)
}

func (s *Service) followUp(ctx context.Context, messageID string, incident *murphy.Incident, reason murphy.NotificationReason) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", incident.GetId()))
	defer span.End(&err)

	users, err := s.repo.RecipientsOf(ctx, incident.GetId())
	if err != nil {
		return fmt.Errorf("find who was paged: %w", err)
	}
	methods, err := s.people.ContactMethodsOfUsers(ctx, users)
	if err != nil {
		return fmt.Errorf("find contact methods: %w", err)
	}
	now := murphy.Now()
	deadline := timestamppb.New(now.AsTime().Add(FollowUpDeadline))
	var created []*murphy.Notification
	for _, user := range users {
		for _, method := range methods[user] {
			created = append(created, newNotification(incident, reason, method, now, deadline))
		}
	}
	messages, err := notificationsCreated(ctx, created)
	if err != nil {
		return err
	}
	return s.record(ctx, &Change{MessageID: messageID, Notifications: created, Messages: messages})
}

func (s *Service) NotificationSent(ctx context.Context, messageID string, notification *murphy.Notification) error {
	return s.outcome(ctx, messageID, notification)
}

func (s *Service) NotificationFailed(ctx context.Context, messageID string, notification *murphy.Notification) error {
	return s.outcome(ctx, messageID, notification)
}

func (s *Service) outcome(ctx context.Context, messageID string, notification *murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("notification.id", notification.GetId()))
	defer span.End(&err)

	if notification.GetContactMethodGone() {
		if err := s.people.DisableContactMethod(ctx, notification.GetContactMethod(), notification.GetFailure(), time.Now()); err != nil {
			return fmt.Errorf("disable the contact method that is gone: %w", err)
		}
	}
	return s.record(ctx, &Change{MessageID: messageID, Outcome: notification})
}

func (s *Service) record(ctx context.Context, change *Change) error {
	if _, err := s.repo.Record(ctx, change); err != nil {
		return fmt.Errorf("record notifications: %w", err)
	}
	return nil
}

func (s *Service) policyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error) {
	key := fmt.Sprintf("%s/%d", id, version)
	if cached, ok := s.versions.Load(key); ok {
		return cached.(*murphy.EscalationPolicy), nil
	}
	policy, err := s.policies.GetPolicyVersion(ctx, id, version)
	if err != nil {
		return nil, err
	}
	s.versions.Store(key, policy)
	return policy, nil
}

func newNotification(incident *murphy.Incident, reason murphy.NotificationReason, method *murphy.ContactMethod, now, deadline *timestamppb.Timestamp) *murphy.Notification {
	return &murphy.Notification{
		Id:              murphy.NewID(),
		Reason:          reason,
		Incident:        incident,
		UserId:          method.GetUserId(),
		ContactMethod:   method,
		EscalationLevel: incident.GetEscalationLevel(),
		EscalationRound: incident.GetEscalationRound(),
		CreatedAt:       now,
		Deadline:        deadline,
	}
}

func notificationsCreated(ctx context.Context, notifications []*murphy.Notification) ([]*ScheduledMessage, error) {
	messages := make([]*ScheduledMessage, len(notifications))
	for i, notification := range notifications {
		created, err := message(ctx, notification.GetCreatedAt().AsTime(), &murphy.Envelope{
			Event: &murphy.Envelope_Notification{Notification: &murphy.NotificationEvent{Verb: murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, Current: notification}},
		})
		if err != nil {
			return nil, err
		}
		messages[i] = created
	}
	return messages, nil
}

func message(ctx context.Context, due time.Time, envelope *murphy.Envelope) (*ScheduledMessage, error) {
	envelope.Id, envelope.OccurredAt, envelope.TraceContext = murphy.NewID(), murphy.Now(), tracer.Inject(ctx)
	data, err := proto.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	return &ScheduledMessage{ID: envelope.GetId(), DueAt: due, Envelope: data}, nil
}

func (s *Service) ClaimDueMessages(ctx context.Context, limit int, lease time.Duration) (_ []messaging.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	claimed, err := s.repo.ClaimDueMessages(ctx, time.Now().UTC(), limit, lease)
	if err != nil {
		return nil, fmt.Errorf("claim due messages: %w", err)
	}
	due := make([]messaging.ScheduledMessage, len(claimed))
	for i, message := range claimed {
		due[i] = messaging.ScheduledMessage{ID: message.ID, Kind: "event", DueAt: message.DueAt, Envelope: message.Envelope}
	}
	return due, nil
}

func (s *Service) MessagesSent(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.repo.DeleteMessages(ctx, ids); err != nil {
		return fmt.Errorf("delete sent messages: %w", err)
	}
	return nil
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

func (s *Service) EmailProvider(ctx context.Context) (_ *murphy.EmailProvider, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	provider, err := s.repo.GetEmailProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("get email provider: %w", err)
	}
	return provider, nil
}

func (s *Service) SetEmailProvider(ctx context.Context, in *murphy.EmailProvider) (_ *murphy.EmailProvider, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	provider := &murphy.EmailProvider{
		Type: in.GetType(), From: in.GetFrom(), Host: in.GetHost(), Port: in.GetPort(),
		Username: in.GetUsername(), Secret: in.GetSecret(), UpdatedAt: murphy.Now(),
	}
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.SetEmailProvider(ctx, provider); err != nil {
		return nil, fmt.Errorf("set email provider: %w", err)
	}
	return provider, nil
}

func (s *Service) RemoveEmailProvider(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.repo.DeleteEmailProvider(ctx); err != nil {
		return fmt.Errorf("remove email provider: %w", err)
	}
	return nil
}
