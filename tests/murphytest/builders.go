package murphytest

import (
	"time"
	"uuid"

	"github.com/murphyhq/server/client"
)

type Team struct {
	ID   string
	Name string
	m    *Murphy
}

func (m *Murphy) Team() *Team {
	m.t.Helper()
	name := m.Name("team")
	created, err := m.API.CreateTeamWithResponse(m.t.Context(), nil, client.TeamInput{Name: name})
	m.created(created, err, "create a team")
	return &Team{ID: created.JSON201.Id, Name: name, m: m}
}

func (t *Team) Reference() client.Reference {
	return client.Reference{Id: t.ID, Name: &t.Name}
}

func (t *Team) Add(member *Member, role client.TeamRole) {
	t.m.t.Helper()
	added, err := t.m.API.SetTeamMemberWithResponse(t.m.t.Context(), t.ID, member.ID, client.TeamMemberInput{Role: role})
	t.m.ok(added, err, "add someone to the team")
}

type Member struct {
	*User
	API *client.ClientWithResponses
}

func (m *Murphy) Member(email string) *Member {
	m.t.Helper()
	api := m.ClientWith(m.SessionAs(email))
	session, err := api.GetSessionWithResponse(m.t.Context())
	m.ok(session, err, "read who signed in")
	return &Member{User: &User{ID: session.JSON200.User.Id, Name: session.JSON200.User.Name, Email: email, m: m}, API: api}
}

type User struct {
	ID       string
	Name     string
	Email    string
	webhooks []*Webhook
	phones   []*Phone
	m        *Murphy
}

type UserOption func(*userSpec)

type userSpec struct {
	input    client.UserInput
	methods  []client.ContactMethodInput
	webhooks [][]WebhookOption
	phones   []string
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

func WithPhone() UserOption {
	return func(s *userSpec) { s.phones = append(s.phones, "device-"+uuid.NewV4().String()) }
}

func WithGonePhone() UserOption {
	return func(s *userSpec) { s.phones = append(s.phones, "gone-"+uuid.NewV4().String()) }
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
	user := &User{ID: created.JSON201.Id, Name: spec.input.Name, Email: spec.input.Email, m: m}
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
	for _, token := range spec.phones {
		ios := client.Ios
		added, err := m.API.CreateContactMethodWithResponse(m.t.Context(), user.ID, nil, client.ContactMethodInput{Type: client.Push, Address: token, Platform: &ios})
		m.created(added, err, "add a phone")
		user.phones = append(user.phones, &Phone{Token: added.JSON201.Address, receiver: m.Receiver()})
	}
	return user
}

func (u *User) Phone() *Phone {
	u.m.t.Helper()
	if len(u.phones) == 0 {
		u.m.t.Errorf("%s has no phone", u.Email)
		u.m.t.FailNow()
	}
	return u.phones[0]
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
	return client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: u.ID, Name: &u.Name}
}

func (u *User) Reference() client.Reference {
	return client.Reference{Id: u.ID, Name: &u.Name}
}

type Schedule struct {
	ID   string
	Name string
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
	name := m.Name("schedule")
	created, err := m.API.CreateScheduleWithResponse(m.t.Context(), nil, client.ScheduleInput{
		Name:   name,
		Teams:  &teams,
		Layers: []client.Layer{{TurnLengthSeconds: 7 * 24 * 60 * 60, StartsAt: starts, Participants: people}},
	})
	m.created(created, err, "create a schedule")
	return &Schedule{ID: created.JSON201.Id, Name: name}
}

func (m *Murphy) Override(schedule *Schedule, user *User, from, to time.Time) {
	m.t.Helper()
	created, err := m.API.CreateOverrideWithResponse(m.t.Context(), schedule.ID, nil, client.OverrideInput{User: client.Reference{Id: user.ID}, StartsAt: from, EndsAt: to})
	m.created(created, err, "create an override")
}

func (s *Schedule) Target() client.EscalationTarget {
	return client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: s.ID, Name: &s.Name}
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
	urgency     *client.Urgency
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

func Urgency(urgency client.Urgency) ServiceOption {
	return func(s *serviceSpec) { s.urgency = &urgency }
}

func Receives(kind client.IntegrationType) ServiceOption {
	return func(s *serviceSpec) { s.integration = kind }
}

type Service struct {
	ID            string
	PolicyID      string
	PolicyName    string
	PolicyVersion int32
	IntegrationID string
	Name          string
	key           string
	m             *Murphy
}

func (s *Service) Disable() {
	s.m.t.Helper()
	disabled := client.ServiceStatusDisabled
	updated, err := s.m.API.UpdateServiceWithResponse(s.m.t.Context(), s.ID, client.ServiceInput{Name: s.Name, EscalationPolicy: client.Reference{Id: s.PolicyID}, Status: &disabled})
	s.m.ok(updated, err, "disable the service")
}

func (s *Service) ChangePolicy(levels ...Level) {
	s.m.t.Helper()
	got, err := s.m.API.GetEscalationPolicyWithResponse(s.m.t.Context(), s.PolicyID)
	s.m.ok(got, err, "get the service's policy")
	updated, err := s.m.API.UpdateEscalationPolicyWithResponse(s.m.t.Context(), s.PolicyID, client.EscalationPolicyInput{
		Name: got.JSON200.Name, Team: got.JSON200.Team, Levels: escalationLevels(levels), RepeatCount: &got.JSON200.RepeatCount,
	})
	s.m.ok(updated, err, "change the service's policy")
}

func (m *Murphy) Service(options ...ServiceOption) *Service {
	m.t.Helper()
	spec := serviceSpec{integration: client.Generic}
	for _, option := range options {
		option(&spec)
	}
	policy := m.policy(spec)
	name := m.Name("service")
	service, err := m.API.CreateServiceWithResponse(m.t.Context(), nil, client.ServiceInput{Name: name, EscalationPolicy: client.Reference{Id: policy.ID}, Urgency: spec.urgency})
	m.created(service, err, "create a service")
	integration, err := m.API.CreateIntegrationWithResponse(m.t.Context(), service.JSON201.Id, nil, client.IntegrationCreate{Name: string(spec.integration), Type: spec.integration})
	m.created(integration, err, "create an integration")
	return &Service{
		ID: service.JSON201.Id, PolicyID: policy.ID, PolicyName: policy.Name, PolicyVersion: policy.Version,
		IntegrationID: integration.JSON201.Id, key: integration.JSON201.Key, Name: name, m: m,
	}
}

type Policy struct {
	ID      string
	Name    string
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
	name := m.Name("policy")
	policy, err := m.API.CreateEscalationPolicyWithResponse(m.t.Context(), nil, client.EscalationPolicyInput{
		Name: name, Team: client.Reference{Id: spec.team.ID}, Levels: escalationLevels(spec.levels), RepeatCount: &spec.repeats,
	})
	m.created(policy, err, "create an escalation policy")
	return &Policy{ID: policy.JSON201.Id, Name: name, Version: policy.JSON201.Version}
}

func (s *Service) Key() string {
	return s.key
}

func escalationLevels(levels []Level) []client.EscalationLevel {
	out := make([]client.EscalationLevel, len(levels))
	for i, level := range levels {
		out[i] = client.EscalationLevel{DelaySeconds: int64(level.Delay / time.Second), Targets: level.Targets}
	}
	return out
}

func Within(delay time.Duration, targets ...client.EscalationTarget) Level {
	return Level{Delay: delay, Targets: targets}
}
