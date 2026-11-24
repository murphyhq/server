package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/access"
)

const (
	payments = "team-payments"
	platform = "team-platform"
	policy   = "policy-payments"
	service  = "service-checkout"
	shared   = "schedule-shared"
	orphan   = "schedule-without-teams"
	incident = "incident-1"
)

type install struct {
	access *access.Access
	roles  map[string]map[string]murphy.TeamRole
	paged  []string
}

func newInstall(t *testing.T) *install {
	t.Helper()
	people, policies, catalog, schedules, incidents := NewMockPeople(t), NewMockPolicies(t), NewMockCatalog(t), NewMockSchedules(t), NewMockIncidents(t)
	i := &install{
		access: access.New(people, policies, catalog, schedules, incidents),
		roles:  map[string]map[string]murphy.TeamRole{},
	}
	people.EXPECT().TeamRolesOf(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string) (map[string]murphy.TeamRole, error) {
		return i.roles[userID], nil
	}).Maybe()
	policies.EXPECT().GetPolicy(mock.Anything, policy).Return(&murphy.EscalationPolicy{Id: policy, TeamId: payments}, nil).Maybe()
	catalog.EXPECT().GetService(mock.Anything, service).Return(&murphy.Service{Id: service, EscalationPolicyId: policy}, nil).Maybe()
	schedules.EXPECT().GetSchedule(mock.Anything, shared).Return(&murphy.Schedule{Id: shared, TeamIds: []string{payments, platform}}, nil).Maybe()
	schedules.EXPECT().GetSchedule(mock.Anything, orphan).Return(&murphy.Schedule{Id: orphan}, nil).Maybe()
	incidents.EXPECT().GetIncident(mock.Anything, incident).RunAndReturn(func(context.Context, string) (*murphy.Incident, []*murphy.TimelineEntry, error) {
		pages := make([]*murphy.TimelinePage, len(i.paged))
		for n, user := range i.paged {
			pages[n] = &murphy.TimelinePage{UserId: user}
		}
		timeline := []*murphy.TimelineEntry{{Escalation: &murphy.TimelineEscalation{Level: 1, Paged: pages}}}
		return &murphy.Incident{Id: incident, EscalationPolicyId: policy}, timeline, nil
	}).Maybe()
	return i
}

func (i *install) member(userID, team string, role murphy.TeamRole) access.Caller {
	i.roles[userID] = map[string]murphy.TeamRole{team: role}
	return access.Caller{UserID: userID, Role: murphy.UserRole_USER_ROLE_MEMBER}
}

func admin() access.Caller {
	return access.Caller{UserID: "ana", Role: murphy.UserRole_USER_ROLE_ADMIN}
}

func TestAdminsManageEverythingWithoutLookingAnythingUp(t *testing.T) {
	i := newInstall(t)

	assert.NoError(t, i.access.Admin(admin()))
	assert.NoError(t, i.access.ManagesService(t.Context(), admin(), "a-service-nobody-looks-up"))
	assert.NoError(t, i.access.ManagesSchedule(t.Context(), admin(), "a-schedule-nobody-looks-up"))
	assert.NoError(t, i.access.Responds(t.Context(), admin(), "an-incident-nobody-looks-up"))
}

func TestMembersDontSetUpTheInstall(t *testing.T) {
	i := newInstall(t)

	err := i.access.Admin(i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER))

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestATeamsManagerManagesItsServices(t *testing.T) {
	i := newInstall(t)

	err := i.access.ManagesService(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), service)

	assert.NoError(t, err)
}

func TestATeamsResponderDoesntManageIt(t *testing.T) {
	i := newInstall(t)

	err := i.access.ManagesService(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER), service)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAnotherTeamsManagerDoesntManageIt(t *testing.T) {
	i := newInstall(t)

	err := i.access.ManagesService(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER), service)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestASharedScheduleIsManagedFromAnyOfItsTeams(t *testing.T) {
	i := newInstall(t)

	err := i.access.ManagesSchedule(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER), shared)

	assert.NoError(t, err)
}

func TestAScheduleWithoutTeamsIsManagedByAdminsOnly(t *testing.T) {
	i := newInstall(t)

	err := i.access.ManagesSchedule(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), orphan)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestTheOwningTeamsRespondersRespond(t *testing.T) {
	i := newInstall(t)

	err := i.access.Responds(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER), incident)

	assert.NoError(t, err)
}

func TestObserversOnlyWatch(t *testing.T) {
	i := newInstall(t)

	err := i.access.Responds(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_OBSERVER), incident)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestSomeonePagedDirectlyRespondsFromOutsideTheTeam(t *testing.T) {
	i := newInstall(t)
	i.paged = []string{"cy"}

	err := i.access.Responds(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_OBSERVER), incident)

	assert.NoError(t, err)
}

func TestEveryoneChangesOnlyThemselves(t *testing.T) {
	i := newInstall(t)
	bo := i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)

	assert.NoError(t, i.access.SelfOrAdmin(bo, "bo"))
	assert.ErrorIs(t, i.access.SelfOrAdmin(bo, "cy"), murphy.ErrForbidden)
}
