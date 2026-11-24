package murphytest

import (
	"time"

	"github.com/murphyhq/server/client"
)

type Team struct {
	ID string
}

func (m *Murphy) Team() *Team {
	m.t.Helper()
	created, err := m.API.CreateTeamWithResponse(m.t.Context(), nil, client.TeamInput{Name: m.Name("team")})
	m.created(created, err, "create a team")
	return &Team{ID: created.JSON201.Id}
}

type User struct {
	ID       string
	Email    string
	webhooks []*Webhook
	m        *Murphy
}

type UserOption func(*userSpec)

type userSpec struct {
	input    client.UserInput
	methods  []client.ContactMethodInput
	webhooks [][]WebhookOption
}

func Deactivated() UserOption {
	return func(s *userSpec) {
		status := client.UserStatusDeactivated
		s.input.Status = &status
	}
}

func Email() UserOption {
	return func(s *userSpec) {
		s.methods = append(s.methods, client.ContactMethodInput{Type: client.Email, Address: s.input.Email})
	}
}

func WithWebhook(options ...WebhookOption) UserOption {
	return func(s *userSpec) { s.webhooks = append(s.webhooks, options) }
}

func (m *Murphy) User(options ...UserOption) *User {
	m.t.Helper()
	name := m.Name("user")
	spec := userSpec{input: client.UserInput{Name: name, Email: name + "@example.com"}}
	for _, option := range options {
		option(&spec)
	}
	created, err := m.API.CreateUserWithResponse(m.t.Context(), nil, spec.input)
	m.created(created, err, "create a user")
	user := &User{ID: created.JSON201.Id, Email: spec.input.Email, m: m}
	for _, options := range spec.webhooks {
		hook := m.Receiver().Webhook(options...)
		spec.methods = append(spec.methods, client.ContactMethodInput{Type: client.Webhook, Address: hook.Address})
		user.webhooks = append(user.webhooks, hook)
	}
	for _, method := range spec.methods {
		added, err := m.API.CreateContactMethodWithResponse(m.t.Context(), user.ID, nil, method)
		m.created(added, err, "add a contact method")
		for _, hook := range user.webhooks {
			if hook.Address == method.Address {
				hook.ContactMethodID = added.JSON201.Id
			}
		}
	}
	return user
}

func (u *User) Webhook() *Webhook {
	u.m.t.Helper()
	if len(u.webhooks) == 0 {
		u.m.t.Errorf("%s has no webhook", u.Email)
		u.m.t.FailNow()
	}
	return u.webhooks[0]
}

func (u *User) Target() client.EscalationTarget {
	return client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: u.ID}
}

type Schedule struct {
	ID string
}

func (m *Murphy) Schedule(team *Team, participants ...*User) *Schedule {
	m.t.Helper()
	starts := time.Now().Add(-time.Hour)
	if len(participants) == 0 {
		participants, starts = []*User{m.User()}, time.Now().Add(30*24*time.Hour)
	}
	people := make([]client.Reference, len(participants))
	for i, user := range participants {
		people[i] = client.Reference{Id: user.ID}
	}
	teams := []client.Reference{{Id: team.ID}}
	created, err := m.API.CreateScheduleWithResponse(m.t.Context(), nil, client.ScheduleInput{
		Name:   m.Name("schedule"),
		Teams:  &teams,
		Layers: []client.Layer{{TurnLengthSeconds: 7 * 24 * 60 * 60, StartsAt: starts, Participants: people}},
	})
	m.created(created, err, "create a schedule")
	return &Schedule{ID: created.JSON201.Id}
}

func (s *Schedule) Target() client.EscalationTarget {
	return client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: s.ID}
}

type Level struct {
	Delay   time.Duration
	Targets []client.EscalationTarget
}

type ServiceOption func(*serviceSpec)

type serviceSpec struct {
	team        *Team
	levels      []Level
	repeats     int32
	integration client.IntegrationType
}

func Pages(targets ...client.EscalationTarget) ServiceOption {
	return Levels(Level{Delay: time.Hour, Targets: targets})
}

func Levels(levels ...Level) ServiceOption {
	return func(s *serviceSpec) { s.levels = levels }
}

func Repeats(times int32) ServiceOption {
	return func(s *serviceSpec) { s.repeats = times }
}

func OwnedBy(team *Team) ServiceOption {
	return func(s *serviceSpec) { s.team = team }
}

func Receives(kind client.IntegrationType) ServiceOption {
	return func(s *serviceSpec) { s.integration = kind }
}

type Service struct {
	ID            string
	PolicyID      string
	PolicyVersion int32
	IntegrationID string
	key           string
	m             *Murphy
}

func (m *Murphy) Service(options ...ServiceOption) *Service {
	m.t.Helper()
	spec := serviceSpec{integration: client.Generic}
	for _, option := range options {
		option(&spec)
	}
	policy := m.policy(spec)
	service, err := m.API.CreateServiceWithResponse(m.t.Context(), nil, client.ServiceInput{Name: m.Name("service"), EscalationPolicy: client.Reference{Id: policy.ID}})
	m.created(service, err, "create a service")
	integration, err := m.API.CreateIntegrationWithResponse(m.t.Context(), service.JSON201.Id, nil, client.IntegrationCreate{Name: string(spec.integration), Type: spec.integration})
	m.created(integration, err, "create an integration")
	return &Service{
		ID: service.JSON201.Id, PolicyID: policy.ID, PolicyVersion: policy.Version,
		IntegrationID: integration.JSON201.Id, key: integration.JSON201.Key, m: m,
	}
}

type Policy struct {
	ID      string
	Version int32
}

func (m *Murphy) Policy(options ...ServiceOption) *Policy {
	m.t.Helper()
	spec := serviceSpec{}
	for _, option := range options {
		option(&spec)
	}
	return m.policy(spec)
}

func (m *Murphy) policy(spec serviceSpec) *Policy {
	m.t.Helper()
	if spec.team == nil {
		spec.team = m.Team()
	}
	if len(spec.levels) == 0 {
		spec.levels = []Level{{Delay: time.Hour, Targets: []client.EscalationTarget{m.User().Target()}}}
	}
	levels := make([]client.EscalationLevel, len(spec.levels))
	for i, level := range spec.levels {
		levels[i] = client.EscalationLevel{DelaySeconds: int64(level.Delay / time.Second), Targets: level.Targets}
	}
	policy, err := m.API.CreateEscalationPolicyWithResponse(m.t.Context(), nil, client.EscalationPolicyInput{
		Name: m.Name("policy"), Team: client.Reference{Id: spec.team.ID}, Levels: levels, RepeatCount: &spec.repeats,
	})
	m.created(policy, err, "create an escalation policy")
	return &Policy{ID: policy.JSON201.Id, Version: policy.JSON201.Version}
}

func (s *Service) Key() string {
	return s.key
}
