package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
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
	access *access
	roles  map[string]map[string]murphy.TeamRole
	paged  []string
}

func newInstall(t *testing.T) *install {
	t.Helper()
	people, policies, services, schedules, incidents := NewMockTeamRoles(t), NewMockPolicyOwners(t), NewMockServiceOwners(t), NewMockScheduleOwners(t), NewMockIncidentResponders(t)
	i := &install{
		access: &access{people: people, policies: policies, services: services, schedules: schedules, incidents: incidents},
		roles:  map[string]map[string]murphy.TeamRole{},
	}
	people.EXPECT().TeamRolesOf(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string) (map[string]murphy.TeamRole, error) {
		return i.roles[userID], nil
	}).Maybe()
	people.EXPECT().GetContactMethod(mock.Anything, "cy-webhook").Return(&murphy.ContactMethod{Id: "cy-webhook", UserId: "cy"}, nil).Maybe()
	policies.EXPECT().GetPolicy(mock.Anything, policy).Return(&murphy.EscalationPolicy{Id: policy, TeamId: payments}, nil).Maybe()
	services.EXPECT().GetService(mock.Anything, service).Return(&murphy.Service{Id: service, EscalationPolicyId: policy}, nil).Maybe()
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

func (i *install) member(userID, team string, role murphy.TeamRole) string {
	i.roles[userID] = map[string]murphy.TeamRole{team: role}
	return userID
}

func (i *install) request(t *testing.T, role murphy.UserRole, method, pattern, target string) int {
	t.Helper()
	spec, err := GetSwagger()
	require.NoError(t, err)
	rules, err := newAccess(spec, i.access.people, i.access.policies, i.access.services, i.access.schedules, i.access.incidents)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(method+" "+pattern, requireAccess(rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(method, target, nil)
	session := &currentSession{session: &murphy.Session{UserId: "ana"}, role: role}
	request = request.WithContext(context.WithValue(request.Context(), sessionContext{}, session))
	answer := httptest.NewRecorder()
	mux.ServeHTTP(answer, request)
	return answer.Code
}

func TestEveryOperationInTheSpecDeclaresItsAccess(t *testing.T) {
	spec, err := GetSwagger()
	require.NoError(t, err)

	_, err = newAccess(spec, nil, nil, nil, nil, nil)

	assert.NoError(t, err)
}

func TestAChangeWithoutAScopeStopsTheServerFromStarting(t *testing.T) {
	spec, err := GetSwagger()
	require.NoError(t, err)
	spec.Paths.Find("/v1/services/{id}").Delete.Security = &openapi3.SecurityRequirements{{"session": []string{}}}

	_, err = newAccess(spec, nil, nil, nil, nil, nil)

	assert.ErrorContains(t, err, "DELETE /v1/services/{id}")
}

func TestAdminsPassWithoutLookingAnythingUp(t *testing.T) {
	i := newInstall(t)

	got := i.request(t, murphy.UserRole_USER_ROLE_ADMIN, http.MethodDelete, "/v1/services/{id}", "/v1/services/a-service-nobody-looks-up")

	assert.Equal(t, http.StatusNoContent, got)
}

func TestAMemberIsCheckedOnTheWayIn(t *testing.T) {
	i := newInstall(t)

	got := i.request(t, murphy.UserRole_USER_ROLE_MEMBER, http.MethodDelete, "/v1/services/{id}", "/v1/services/"+service)

	assert.Equal(t, http.StatusForbidden, got)
}

func TestMembersDontSetUpTheInstall(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), "install:manage", nil)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestATeamsManagerManagesItsServices(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), "services:manage", []string{service})

	assert.NoError(t, err)
}

func TestATeamsResponderDoesntManageIt(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER), "services:manage", []string{service})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAnotherTeamsManagerDoesntManageIt(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER), "services:manage", []string{service})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestASharedScheduleIsManagedFromAnyOfItsTeams(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER), "schedules:manage", []string{shared})

	assert.NoError(t, err)
}

func TestAScheduleWithoutTeamsIsManagedByAdminsOnly(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), "schedules:manage", []string{orphan})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestTheOwningTeamsRespondersRespond(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER), "incidents:respond", []string{incident})

	assert.NoError(t, err)
}

func TestObserversOnlyWatch(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_OBSERVER), "incidents:respond", []string{incident})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestSomeonePagedDirectlyRespondsFromOutsideTheTeam(t *testing.T) {
	i := newInstall(t)
	i.paged = []string{"cy"}

	err := i.access.allows(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_OBSERVER), "incidents:respond", []string{incident})

	assert.NoError(t, err)
}

func TestNobodyChangesSomeoneElsesContactMethods(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER), "contact-methods:own", []string{"cy-webhook"})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestTheResourceComesFromThePathOrTheBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/v1/schedules/schedule-1", nil)
	request.SetPathValue("id", "schedule-1")
	body := map[string]any{"team": map[string]any{"id": payments}, "teams": []any{map[string]any{"id": payments}, map[string]any{"id": platform}}}

	fromPath, _ := resource{inPath: "id"}.in(request, body)
	fromBody, _ := resource{inBody: []string{"team", "id"}}.in(request, body)
	fromList, _ := resource{inBody: []string{"teams", "id"}}.in(request, body)

	assert.Equal(t, []string{"schedule-1"}, fromPath)
	assert.Equal(t, []string{payments}, fromBody)
	assert.Equal(t, []string{payments, platform}, fromList)
}

func TestAnOptionalResourceThatIsAbsentDoesntApply(t *testing.T) {
	_, applies := resource{inBody: []string{"status"}, optional: true}.in(httptest.NewRequest(http.MethodPatch, "/v1/users/bo", nil), map[string]any{"name": "Bo"})

	assert.False(t, applies)
}
