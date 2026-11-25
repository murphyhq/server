package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
)

const (
	payments    = "team-payments"
	platform    = "team-platform"
	policy      = "policy-payments"
	service     = "service-checkout"
	integration = "integration-checkout"
	shared      = "schedule-shared"
	orphan      = "schedule-without-teams"
	override    = "override-on-shared"
	incident    = "incident-1"
	missing     = "missing"
	broken      = "someone-whose-roles-fail-to-load"
)

var notFound = &murphy.NotFoundError{Entity: "thing"}

type install struct {
	access      *access
	roles       map[string]map[string]murphy.TeamRole
	paged       []string
	roleLookups int
}

func newInstall(t *testing.T) *install {
	t.Helper()
	people, policies, services, schedules, incidents := NewMockTeamRoles(t), NewMockPolicyOwners(t), NewMockServiceOwners(t), NewMockScheduleOwners(t), NewMockIncidentResponders(t)
	i := &install{
		access: &access{people: people, policies: policies, services: services, schedules: schedules, incidents: incidents},
		roles:  map[string]map[string]murphy.TeamRole{},
	}
	people.EXPECT().TeamRolesOf(mock.Anything, broken).Return(nil, errors.New("the database is down")).Maybe()
	people.EXPECT().TeamRolesOf(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string) (map[string]murphy.TeamRole, error) {
		i.roleLookups++
		return i.roles[userID], nil
	}).Maybe()
	people.EXPECT().GetContactMethod(mock.Anything, "bo-webhook").Return(&murphy.ContactMethod{Id: "bo-webhook", UserId: "bo"}, nil).Maybe()
	people.EXPECT().GetContactMethod(mock.Anything, "cy-webhook").Return(&murphy.ContactMethod{Id: "cy-webhook", UserId: "cy"}, nil).Maybe()
	people.EXPECT().GetContactMethod(mock.Anything, missing).Return(nil, notFound).Maybe()
	policies.EXPECT().GetPolicyRecord(mock.Anything, policy).Return(&murphy.EscalationPolicy{Id: policy, TeamId: payments}, nil).Maybe()
	policies.EXPECT().GetPolicyRecord(mock.Anything, missing).Return(nil, notFound).Maybe()
	services.EXPECT().GetService(mock.Anything, service).Return(&murphy.Service{Id: service, EscalationPolicyId: policy}, nil).Maybe()
	services.EXPECT().GetService(mock.Anything, "service-of-a-missing-policy").Return(&murphy.Service{EscalationPolicyId: missing}, nil).Maybe()
	services.EXPECT().GetService(mock.Anything, missing).Return(nil, notFound).Maybe()
	services.EXPECT().GetIntegrationAndService(mock.Anything, integration).Return(&murphy.Integration{Id: integration, ServiceId: service}, &murphy.Service{Id: service, EscalationPolicyId: policy}, nil).Maybe()
	services.EXPECT().GetIntegrationAndService(mock.Anything, "integration-of-a-missing-policy").Return(&murphy.Integration{ServiceId: service}, &murphy.Service{EscalationPolicyId: missing}, nil).Maybe()
	services.EXPECT().GetIntegrationAndService(mock.Anything, missing).Return(nil, nil, notFound).Maybe()
	schedules.EXPECT().TeamsOfSchedule(mock.Anything, shared).Return([]string{payments, platform}, nil).Maybe()
	schedules.EXPECT().TeamsOfSchedule(mock.Anything, orphan).Return(nil, nil).Maybe()
	schedules.EXPECT().TeamsOfSchedule(mock.Anything, missing).Return(nil, notFound).Maybe()
	schedules.EXPECT().GetOverride(mock.Anything, override).Return(&murphy.Override{Id: override, ScheduleId: shared}, nil).Maybe()
	schedules.EXPECT().GetOverride(mock.Anything, "override-of-a-missing-schedule").Return(&murphy.Override{ScheduleId: missing}, nil).Maybe()
	schedules.EXPECT().GetOverride(mock.Anything, missing).Return(nil, notFound).Maybe()
	incidents.EXPECT().PolicyOfIncident(mock.Anything, incident, mock.Anything).RunAndReturn(func(_ context.Context, _, userID string) (string, bool, error) {
		return policy, slices.Contains(i.paged, userID), nil
	}).Maybe()
	incidents.EXPECT().PolicyOfIncident(mock.Anything, "incident-of-a-missing-policy", mock.Anything).Return(missing, false, nil).Maybe()
	incidents.EXPECT().PolicyOfIncident(mock.Anything, missing, mock.Anything).Return("", false, notFound).Maybe()
	return i
}

func (i *install) member(userID, team string, role murphy.TeamRole) string {
	i.roles[userID] = map[string]murphy.TeamRole{team: role}
	return userID
}

type call struct {
	role    murphy.UserRole
	userID  string
	method  string
	pattern string
	target  string
	body    string
	signed  bool
}

func (i *install) send(t *testing.T, c call) int {
	t.Helper()
	spec, err := GetSwagger()
	require.NoError(t, err)
	rules, err := newAccess(spec, i.access.people, i.access.policies, i.access.services, i.access.schedules, i.access.incidents)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(c.method+" "+c.pattern, requireAccess(rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
	if c.signed {
		session := &currentSession{session: &murphy.Session{UserId: c.userID}, role: c.role}
		request = request.WithContext(context.WithValue(request.Context(), sessionContext{}, session))
	}
	answer := httptest.NewRecorder()
	mux.ServeHTTP(answer, request)
	return answer.Code
}

func asMember(userID string) call {
	return call{role: murphy.UserRole_USER_ROLE_MEMBER, userID: userID, signed: true}
}

func (c call) to(method, pattern, target string) call {
	c.method, c.pattern, c.target = method, pattern, target
	return c
}

func (c call) with(body string) call {
	c.body = body
	return c
}

func specWith(t *testing.T, change func(spec *openapi3.T)) *openapi3.T {
	t.Helper()
	spec, err := GetSwagger()
	require.NoError(t, err)
	change(spec)
	return spec
}

func TestEveryOperationInTheSpecDeclaresItsAccess(t *testing.T) {
	spec := specWith(t, func(*openapi3.T) {})

	_, err := newAccess(spec, nil, nil, nil, nil, nil)

	assert.NoError(t, err)
}

func TestAChangeWithoutAScopeStopsTheServerFromStarting(t *testing.T) {
	spec := specWith(t, func(spec *openapi3.T) {
		spec.Paths.Find("/v1/services/{id}").Delete.Security = &openapi3.SecurityRequirements{{"session": []string{}}}
	})

	_, err := newAccess(spec, nil, nil, nil, nil, nil)

	assert.ErrorContains(t, err, "DELETE /v1/services/{id}: a change must declare a scope")
}

func TestAnUnknownScopeStopsTheServerFromStarting(t *testing.T) {
	spec := specWith(t, func(spec *openapi3.T) {
		spec.Paths.Find("/v1/services/{id}").Delete.Security = &openapi3.SecurityRequirements{{"session": []string{"services:delete"}}}
	})

	_, err := newAccess(spec, nil, nil, nil, nil, nil)

	assert.ErrorContains(t, err, `unknown scope "services:delete"`)
}

func TestAScopeWithNothingToCheckStopsTheServerFromStarting(t *testing.T) {
	spec := specWith(t, func(spec *openapi3.T) {
		spec.Paths.Find("/v1/services").Post.Security = &openapi3.SecurityRequirements{{"session": []string{"services:manage"}}}
		spec.Paths.Find("/v1/services").Post.Extensions = nil
	})

	_, err := newAccess(spec, nil, nil, nil, nil, nil)

	assert.ErrorContains(t, err, `POST /v1/services: scope "services:manage": needs a resource`)
}

func TestAdminsPassWithoutLookingAnythingUp(t *testing.T) {
	i := newInstall(t)
	admin := call{role: murphy.UserRole_USER_ROLE_ADMIN, userID: "ana", signed: true}

	got := i.send(t, admin.to(http.MethodDelete, "/v1/services/{id}", "/v1/services/a-service-nobody-looks-up"))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestAMemberAllowedReachesTheHandler(t *testing.T) {
	i := newInstall(t)
	bo := i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(bo).to(http.MethodDelete, "/v1/services/{id}", "/v1/services/"+service))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestAMemberRefusedNeverReachesTheHandler(t *testing.T) {
	i := newInstall(t)
	cy := i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(cy).to(http.MethodDelete, "/v1/services/{id}", "/v1/services/"+service))

	assert.Equal(t, http.StatusForbidden, got)
}

func TestAMissingResourceIsNotFound(t *testing.T) {
	i := newInstall(t)
	bo := i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(bo).to(http.MethodDelete, "/v1/services/{id}", "/v1/services/"+missing))

	assert.Equal(t, http.StatusNotFound, got)
}

func TestReadingNeedsNoScope(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, asMember("bo").to(http.MethodGet, "/v1/services", "/v1/services"))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestARequestWithoutASessionIsLeftToTheSessionCheck(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, call{}.to(http.MethodDelete, "/v1/services/{id}", "/v1/services/"+service))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestABodyThatIsNotJSONIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, asMember("bo").to(http.MethodPost, "/v1/escalation-policies", "/v1/escalation-policies").with("{not json"))

	assert.Equal(t, http.StatusBadRequest, got)
}

func TestCreatingForATeamYouDontManageIsRefused(t *testing.T) {
	i := newInstall(t)
	cy := i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(cy).to(http.MethodPost, "/v1/escalation-policies", "/v1/escalation-policies").with(`{"team": {"id": "team-payments"}}`))

	assert.Equal(t, http.StatusForbidden, got)
}

func TestCreatingForATeamYouManageIsAllowed(t *testing.T) {
	i := newInstall(t)
	bo := i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(bo).to(http.MethodPost, "/v1/escalation-policies", "/v1/escalation-policies").with(`{"team": {"id": "team-payments"}}`))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestTeamRolesAreReadOncePerRequest(t *testing.T) {
	i := newInstall(t)
	bo := i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)

	got := i.send(t, asMember(bo).to(http.MethodPut, "/v1/escalation-policies/{id}", "/v1/escalation-policies/"+policy).with(`{"team": {"id": "team-payments"}}`))

	assert.Equal(t, http.StatusNoContent, got)
	assert.Equal(t, 1, i.roleLookups, "the policy's team and the body's team are checked against the same roles")
}

func TestChangingYourselfIsAllowed(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, asMember("bo").to(http.MethodPut, "/v1/users/{id}", "/v1/users/bo").with(`{"name": "Bo"}`))

	assert.Equal(t, http.StatusNoContent, got)
}

func TestChangingYourOwnStatusNeedsAnAdmin(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, asMember("bo").to(http.MethodPut, "/v1/users/{id}", "/v1/users/bo").with(`{"name": "Bo", "status": "deactivated"}`))

	assert.Equal(t, http.StatusForbidden, got)
}

func TestChangingSomeoneElseIsRefused(t *testing.T) {
	i := newInstall(t)

	got := i.send(t, asMember("bo").to(http.MethodPut, "/v1/users/{id}", "/v1/users/cy").with(`{"name": "Cy"}`))

	assert.Equal(t, http.StatusForbidden, got)
}

func TestYourOwnSessionsAreAlwaysYours(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "sessions:own", nil)

	assert.NoError(t, err)
}

func TestMembersDontSetUpTheInstall(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "install:manage", nil)

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestYourOwnContactMethodsAreYours(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "contact-methods:own", []string{"bo-webhook"})

	assert.NoError(t, err)
}

func TestNobodyChangesSomeoneElsesContactMethods(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "contact-methods:own", []string{"cy-webhook"})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAMissingContactMethodIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "contact-methods:own", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestATeamsManagerManagesTheTeam(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "teams:manage", []string{payments})

	assert.NoError(t, err)
}

func TestATeamsResponderDoesntManageTheTeam(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER)), "teams:manage", []string{payments})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestATeamsManagerManagesItsPolicies(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "policies:manage", []string{policy})

	assert.NoError(t, err)
}

func TestAnotherTeamsManagerDoesntManageThePolicy(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)), "policies:manage", []string{policy})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAMissingPolicyIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "policies:manage", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestATeamsManagerManagesItsServices(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "services:manage", []string{service})

	assert.NoError(t, err)
}

func TestATeamsResponderDoesntManageItsServices(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER)), "services:manage", []string{service})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAServiceWhosePolicyIsMissingIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "services:manage", []string{"service-of-a-missing-policy"})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestATeamsManagerManagesItsIntegrations(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "integrations:manage", []string{integration})

	assert.NoError(t, err)
}

func TestAnotherTeamsManagerDoesntManageTheIntegration(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)), "integrations:manage", []string{integration})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAMissingIntegrationIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "integrations:manage", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestAnIntegrationWhosePolicyIsMissingIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "integrations:manage", []string{"integration-of-a-missing-policy"})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestASharedScheduleIsManagedFromAnyOfItsTeams(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)), "schedules:manage", []string{shared})

	assert.NoError(t, err)
}

func TestAScheduleWithoutTeamsIsManagedByAdminsOnly(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "schedules:manage", []string{orphan})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestAMissingScheduleIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "schedules:manage", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestAScheduleManagerManagesItsOverrides(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_MANAGER)), "overrides:manage", []string{override})

	assert.NoError(t, err)
}

func TestAResponderCoversForOthersWithOverrides(t *testing.T) {
	i := newInstall(t)
	cy := i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_RESPONDER))

	created := i.access.allows(t.Context(), cy, "overrides:create", []string{shared})
	removed := i.access.allows(t.Context(), cy, "overrides:manage", []string{override})
	edited := i.access.allows(t.Context(), cy, "schedules:manage", []string{shared})

	assert.NoError(t, created)
	assert.NoError(t, removed)
	assert.ErrorIs(t, edited, murphy.ErrForbidden, "the rotation itself stays with managers")
}

func TestAnObserverDoesntTouchOverrides(t *testing.T) {
	i := newInstall(t)
	cy := i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_OBSERVER))

	created := i.access.allows(t.Context(), cy, "overrides:create", []string{shared})
	removed := i.access.allows(t.Context(), cy, "overrides:manage", []string{override})

	assert.ErrorIs(t, created, murphy.ErrForbidden)
	assert.ErrorIs(t, removed, murphy.ErrForbidden)
}

func TestAMissingOverrideIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "overrides:manage", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestAnOverrideWhoseScheduleIsMissingIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "overrides:manage", []string{"override-of-a-missing-schedule"})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestTheOwningTeamsRespondersRespond(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_RESPONDER)), "incidents:respond", []string{incident})

	assert.NoError(t, err)
}

func TestTheOwningTeamsManagersRespond(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_MANAGER)), "incidents:respond", []string{incident})

	assert.NoError(t, err)
}

func TestObserversOnlyWatch(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("bo", payments, murphy.TeamRole_TEAM_ROLE_OBSERVER)), "incidents:respond", []string{incident})

	assert.ErrorIs(t, err, murphy.ErrForbidden)
}

func TestSomeonePagedDirectlyRespondsFromOutsideTheTeam(t *testing.T) {
	i := newInstall(t)
	i.paged = []string{"cy"}

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), i.member("cy", platform, murphy.TeamRole_TEAM_ROLE_OBSERVER)), "incidents:respond", []string{incident})

	assert.NoError(t, err)
}

func TestAMissingIncidentIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "incidents:respond", []string{missing})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestAnIncidentWhosePolicyIsMissingIsNotFound(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), "bo"), "incidents:respond", []string{"incident-of-a-missing-policy"})

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestRolesThatFailToLoadAreAnErrorNotARefusal(t *testing.T) {
	i := newInstall(t)

	err := i.access.allows(t.Context(), i.access.caller(t.Context(), broken), "teams:manage", []string{payments})

	require.Error(t, err)
	assert.NotErrorIs(t, err, murphy.ErrForbidden)
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

func TestOnlyTextIsAnID(t *testing.T) {
	body := map[string]any{"team": map[string]any{"id": 42}, "name": "not an object"}

	notText, _ := resource{inBody: []string{"team", "id"}}.in(httptest.NewRequest(http.MethodPost, "/", nil), body)
	underText, _ := resource{inBody: []string{"name", "id"}}.in(httptest.NewRequest(http.MethodPost, "/", nil), body)

	assert.Empty(t, notText)
	assert.Empty(t, underText)
}

func TestAnOptionalResourceThatIsAbsentDoesntApply(t *testing.T) {
	_, applies := resource{inBody: []string{"status"}, optional: true}.in(httptest.NewRequest(http.MethodPut, "/v1/users/bo", nil), map[string]any{"name": "Bo"})

	assert.False(t, applies)
}

func TestAScopeWithoutAResourceAlwaysApplies(t *testing.T) {
	ids, applies := resource{}.in(httptest.NewRequest(http.MethodPost, "/v1/users", nil), nil)

	assert.Nil(t, ids)
	assert.True(t, applies)
}
