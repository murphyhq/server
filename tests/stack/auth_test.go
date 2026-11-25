//go:build stack

package stack

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

func TestRequestsWithoutASessionAreRefused(t *testing.T) {
	m := murphytest.New(t)

	got, err := m.Anonymous().ListServicesWithResponse(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, got.StatusCode())
}

func TestASessionMurphyNeverIssuedIsRefused(t *testing.T) {
	m := murphytest.New(t)

	got, err := m.ClientWith("mss_never-issued").ListServicesWithResponse(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, got.StatusCode())
}

func TestAnInvalidIdentityTokenSignsNobodyIn(t *testing.T) {
	m := murphytest.New(t)

	notAToken := "not-a-token"

	got, err := m.Anonymous().CreateSessionWithResponse(t.Context(), client.CreateSessionJSONRequestBody{IdToken: &notAToken})

	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, got.StatusCode())
}

func TestTheSigningKeysArePublic(t *testing.T) {
	m := murphytest.New(t)

	got, err := m.Anonymous().GetSigningKeysWithResponse(t.Context())

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, got.StatusCode())
}

func TestSigningInThroughTheBrowserSetsASessionCookie(t *testing.T) {
	m := murphytest.New(t)

	browser, signedIn := m.SignInThroughBrowser(murphytest.Bo)
	session, err := browser.GetSessionWithResponse(t.Context())

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, signedIn.StatusCode)
	require.Equal(t, http.StatusOK, session.StatusCode())
	assert.Equal(t, murphytest.Bo, session.JSON200.User.Email)
}

func TestSigningInReturnsTheBrowserToWhereItCameFrom(t *testing.T) {
	m := murphytest.New(t)

	browser, signedIn := m.SignInThroughBrowserReturningTo(murphytest.Bo, m.DocsURL())
	session, err := browser.GetSessionWithResponse(t.Context())

	require.NoError(t, err)
	assert.Equal(t, m.DocsURL(), signedIn.Request.URL.String())
	require.Equal(t, http.StatusOK, session.StatusCode())
	assert.Equal(t, murphytest.Bo, session.JSON200.User.Email)
}

func TestSigningInNeverReturnsToAnotherOrigin(t *testing.T) {
	m := murphytest.New(t)
	elsewhere := "https://evil.example/"

	got, err := m.Anonymous().SignInWithResponse(t.Context(), &client.SignInParams{ReturnTo: &elsewhere})

	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, got.StatusCode())
}

func TestASignInOnlyFinishesInTheBrowserThatStartedIt(t *testing.T) {
	m := murphytest.New(t)
	callback := m.CallbackFor(murphytest.Bo)

	got := m.OpenInAnotherBrowser(callback)

	assert.Equal(t, http.StatusUnauthorized, got.StatusCode)
	assert.Empty(t, got.Cookies(), "no session is handed to the other browser")
}

func TestTheInitialAdminIsAnAdminAndEveryoneElseAMember(t *testing.T) {
	m := murphytest.New(t)

	ana, err := m.ClientWith(m.SessionAs(murphytest.Ana)).GetSessionWithResponse(t.Context())
	require.NoError(t, err)
	bo, err := m.ClientWith(m.SessionAs(murphytest.Bo)).GetSessionWithResponse(t.Context())
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, ana.StatusCode())
	require.Equal(t, http.StatusOK, bo.StatusCode())
	assert.Equal(t, client.Admin, ana.JSON200.User.Role)
	assert.Equal(t, client.Member, bo.JSON200.User.Role)
}

func TestSigningOutEndsTheSessionAtOnce(t *testing.T) {
	m := murphytest.New(t)
	api := m.ClientWith(m.SessionAs(murphytest.Bo))

	signedOut, err := api.SignOutWithResponse(t.Context())
	require.NoError(t, err)
	after, err := api.GetSessionWithResponse(t.Context())
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, signedOut.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, after.StatusCode())
}

const appVerifier = "a-verifier-only-the-app-knows-and-at-least-43-characters-long"

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestScanningAConnectCodeSignsTheAppInAsWhoeverShowedIt(t *testing.T) {
	m := murphytest.New(t)
	connect, err := m.API.CreateConnectCodeWithResponse(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, connect.StatusCode())

	session, err := m.SessionFromCode(connect.JSON201.Code, "")

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, session.StatusCode())
	assert.Equal(t, murphytest.Ana, session.JSON201.Session.User.Email)
	assert.Contains(t, connect.JSON201.Link, "server="+url.QueryEscape(m.APIURL()))
}

func TestTheAppSignsInThroughTheBrowserAndTradesItsCode(t *testing.T) {
	m := murphytest.New(t)
	code := m.SignInToTheApp(murphytest.Bo, challengeOf(appVerifier))

	session, err := m.SessionFromCode(code, appVerifier)

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, session.StatusCode())
	assert.Equal(t, murphytest.Bo, session.JSON201.Session.User.Email)
}

func TestAnAppCodeIsUselessWithoutTheAppsVerifier(t *testing.T) {
	m := murphytest.New(t)
	code := m.SignInToTheApp(murphytest.Bo, challengeOf(appVerifier))

	session, err := m.SessionFromCode(code, "a-verifier-from-another-app")

	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, session.StatusCode())
}

func TestYourSessionsAreListedWithThisOneMarked(t *testing.T) {
	m := murphytest.New(t)
	m.SignedOutEverywhere(murphytest.Cy)
	m.SessionAs(murphytest.Cy)
	api := m.ClientWith(m.SessionAs(murphytest.Cy))

	got, err := api.ListSessionsWithResponse(t.Context(), nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	require.Len(t, got.JSON200.Items, 2)
	assert.True(t, got.JSON200.Items[0].Current, "newest first, and the newest is this one")
	assert.False(t, got.JSON200.Items[1].Current)
	assert.Equal(t, client.Token, got.JSON200.Items[0].Client)
}

func TestEndingOneOfYourSessionsEndsOnlyThatOne(t *testing.T) {
	m := murphytest.New(t)
	m.SignedOutEverywhere(murphytest.Cy)
	lostPhone := m.ClientWith(m.SessionAs(murphytest.Cy))
	api := m.ClientWith(m.SessionAs(murphytest.Cy))
	sessions, err := api.ListSessionsWithResponse(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, sessions.JSON200.Items, 2)

	ended, err := api.EndSessionWithResponse(t.Context(), sessions.JSON200.Items[1].Id)
	require.NoError(t, err)
	lost, err := lostPhone.GetSessionWithResponse(t.Context())
	require.NoError(t, err)
	kept, err := api.GetSessionWithResponse(t.Context())
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, ended.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, lost.StatusCode())
	assert.Equal(t, http.StatusOK, kept.StatusCode())
}

func TestSigningOutEverywhereEndsEverySessionAndConnectCode(t *testing.T) {
	m := murphytest.New(t)
	m.SignedOutEverywhere(murphytest.Cy)
	other := m.ClientWith(m.SessionAs(murphytest.Cy))
	api := m.ClientWith(m.SessionAs(murphytest.Cy))
	connect, err := api.CreateConnectCodeWithResponse(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, connect.StatusCode())

	signedOut, err := api.SignOutEverywhereWithResponse(t.Context())
	require.NoError(t, err)
	here, err := api.GetSessionWithResponse(t.Context())
	require.NoError(t, err)
	elsewhere, err := other.GetSessionWithResponse(t.Context())
	require.NoError(t, err)
	scanned, err := m.SessionFromCode(connect.JSON201.Code, "")
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, signedOut.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, here.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, elsewhere.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, scanned.StatusCode(), "a connect code made before can't outlive signing out")
}

func TestAnAdminCanEndSomeonesSessions(t *testing.T) {
	m := murphytest.New(t)
	cy := m.SignedOutEverywhere(murphytest.Cy)
	cysPhone := m.ClientWith(m.SessionAs(murphytest.Cy))

	ended, err := m.API.EndUserSessionsWithResponse(t.Context(), cy)
	require.NoError(t, err)
	after, err := cysPhone.GetSessionWithResponse(t.Context())
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, ended.StatusCode())
	assert.Equal(t, http.StatusUnauthorized, after.StatusCode())
}

func TestAMemberCannotEndSomeoneElsesSessions(t *testing.T) {
	m := murphytest.New(t)
	cy := m.SignedOutEverywhere(murphytest.Cy)
	bo := m.ClientWith(m.SessionAs(murphytest.Bo))

	got, err := bo.EndUserSessionsWithResponse(t.Context(), cy)

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestAMemberReadsEverything(t *testing.T) {
	m := murphytest.New(t)
	m.Service()
	bo := m.Member(murphytest.Bo)

	got, err := bo.API.ListServicesWithResponse(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, got.StatusCode())
}

func TestOnlyAdminsAddPeople(t *testing.T) {
	m := murphytest.New(t)
	bo := m.Member(murphytest.Bo)

	got, err := bo.API.CreateUserWithResponse(t.Context(), nil, client.UserInput{Name: "Eve", Email: m.Name("eve") + "@example.com"})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestATeamsManagerManagesItsServices(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.Member(murphytest.Bo)
	team.Add(bo, client.Manager)
	service := m.Service(murphytest.OwnedBy(team))

	got, err := bo.API.DeleteServiceWithResponse(t.Context(), service.ID)

	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, got.StatusCode())
}

func TestSomeoneOutsideTheTeamCannotChangeItsServices(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	team.Add(m.Member(murphytest.Bo), client.Manager)
	service := m.Service(murphytest.OwnedBy(team))
	cy := m.Member(murphytest.Cy)

	got, err := cy.API.DeleteServiceWithResponse(t.Context(), service.ID)

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestTheOwningTeamsResponderAcknowledges(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.Member(murphytest.Bo)
	team.Add(bo, client.Responder)
	incident := m.Service(murphytest.OwnedBy(team), murphytest.Pages(m.User().Target())).Fire("disk-full")

	got, err := bo.API.AcknowledgeIncidentWithResponse(t.Context(), incident.ID, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, got.StatusCode())
}

func TestAnObserverCannotAcknowledge(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.Member(murphytest.Bo)
	team.Add(bo, client.Observer)
	incident := m.Service(murphytest.OwnedBy(team), murphytest.Pages(m.User().Target())).Fire("disk-full")

	got, err := bo.API.AcknowledgeIncidentWithResponse(t.Context(), incident.ID, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestSomeonePagedDirectlyAcknowledgesFromOutsideTheTeam(t *testing.T) {
	m := murphytest.New(t)
	cy := m.Member(murphytest.Cy)
	incident := m.Service(murphytest.OwnedBy(m.Team()), murphytest.Pages(cy.Target())).Fire("disk-full")
	incident.WaitFor(murphytest.Paged(cy.User))

	got, err := cy.API.AcknowledgeIncidentWithResponse(t.Context(), incident.ID, nil)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, got.StatusCode())
}

func TestNobodyChangesSomeoneElsesContactMethods(t *testing.T) {
	m := murphytest.New(t)
	bo := m.Member(murphytest.Bo)
	cy := m.Member(murphytest.Cy)

	got, err := bo.API.CreateContactMethodWithResponse(t.Context(), cy.ID, nil, client.ContactMethodInput{Type: client.Webhook, Address: "https://example.com/hook"})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestWritesSignedInWithTheCookieMustSendJSON(t *testing.T) {
	m := murphytest.New(t)

	got := m.PostThroughBrowser(murphytest.Bo, "/v1/auth/connect-codes", "text/plain", "{}")

	assert.Equal(t, http.StatusUnsupportedMediaType, got.StatusCode)
}

func TestAMemberCannotChangeTheirOwnStatus(t *testing.T) {
	m := murphytest.New(t)
	bo := m.Member(murphytest.Bo)
	deactivated := client.UserStatusDeactivated

	got, err := bo.API.UpdateUserWithResponse(t.Context(), bo.ID, client.UserInput{Name: "Bo", Email: murphytest.Bo, Status: &deactivated})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestAPolicyCannotBeCreatedForATeamYouDontManage(t *testing.T) {
	m := murphytest.New(t)
	theirs := m.Team()
	bo := m.Member(murphytest.Bo)
	m.Team().Add(bo, client.Manager)

	got, err := bo.API.CreateEscalationPolicyWithResponse(t.Context(), nil, client.EscalationPolicyInput{
		Name: m.Name("policy"), Team: client.Reference{Id: theirs.ID},
		Levels: []client.EscalationLevel{{DelaySeconds: 300, Targets: []client.EscalationTarget{bo.Target()}}},
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}

func TestAPersonsTeamsAreListedWithTheirRoles(t *testing.T) {
	m := murphytest.New(t)
	bo := m.Member(murphytest.Bo)
	cy := m.User()
	payments := m.Team()
	payments.Add(&murphytest.Member{User: cy}, client.Responder)

	got, err := bo.API.ListUserMembershipsWithResponse(t.Context(), cy.ID)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	require.Len(t, got.JSON200.Items, 1)
	assert.Equal(t, payments.Reference(), got.JSON200.Items[0].Team)
	assert.Equal(t, cy.Reference(), got.JSON200.Items[0].User)
	assert.Equal(t, client.Responder, got.JSON200.Items[0].Role)
}

func TestALinkFromAnAdminLetsSomeoneSetAPasswordOnceAndSignInWithIt(t *testing.T) {
	m := murphytest.New(t)
	cy := m.User()
	link := m.PasswordLinkFor(cy)

	set := m.SetPasswordThrough(link, "a long and good password")
	again := m.SetPasswordThrough(link, "another long password")
	signedIn := m.SignInWithPassword(cy.Email, "a long and good password")

	assert.Equal(t, http.StatusNoContent, set)
	assert.Equal(t, http.StatusUnauthorized, again, "a link works once")
	assert.Equal(t, http.StatusCreated, signedIn)
}

func TestRepeatedWrongPasswordsSlowDown(t *testing.T) {
	m := murphytest.New(t)
	cy := m.User()
	m.SetPasswordThrough(m.PasswordLinkFor(cy), "a long and good password")
	wrong := make([]int, 5)
	for i := range wrong {
		wrong[i] = m.SignInWithPassword(cy.Email, "a wrong password, again")
	}

	refused := m.SignInWithPassword(cy.Email, "a long and good password")

	assert.Equal(t, []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized}, wrong)
	assert.Equal(t, http.StatusTooManyRequests, refused, "even the right password waits")
}

func TestAResponderCoversForATeammateWithAnOverride(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.Member(murphytest.Bo)
	team.Add(bo, client.Responder)
	schedule := m.Schedule(team)
	from := time.Now().Add(24 * time.Hour)

	created, err := bo.API.CreateOverrideWithResponse(t.Context(), schedule.ID, nil, client.OverrideInput{User: client.Reference{Id: bo.ID}, StartsAt: from, EndsAt: from.Add(8 * time.Hour)})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	removed, err := bo.API.DeleteOverrideWithResponse(t.Context(), created.JSON201.Id)
	require.NoError(t, err)
	edited, err := bo.API.DeleteScheduleWithResponse(t.Context(), schedule.ID)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, removed.StatusCode())
	assert.Equal(t, http.StatusForbidden, edited.StatusCode(), "changing the schedule itself stays with managers")
}

func TestAnObserverCannotOverrideTheirTeamsSchedule(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.Member(murphytest.Bo)
	team.Add(bo, client.Observer)
	schedule := m.Schedule(team)
	from := time.Now().Add(24 * time.Hour)

	got, err := bo.API.CreateOverrideWithResponse(t.Context(), schedule.ID, nil, client.OverrideInput{User: client.Reference{Id: bo.ID}, StartsAt: from, EndsAt: from.Add(8 * time.Hour)})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, got.StatusCode())
}
