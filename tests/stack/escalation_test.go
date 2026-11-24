//go:build stack

package stack

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

const unknown = "0195f3a0-0000-7000-8000-000000000000"

func policyInput(m *murphytest.Murphy, team string, targets ...client.EscalationTarget) client.EscalationPolicyInput {
	return client.EscalationPolicyInput{
		Name:   m.Name("policy"),
		Team:   client.Reference{Id: team},
		Levels: []client.EscalationLevel{{DelaySeconds: 5 * 60, Targets: targets}},
	}
}

func TestEscalationPoliciesBelongToATeam(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	platform, payments := m.Team(), m.Team()
	ana := m.User()
	primary := m.Schedule(payments, ana)

	policy := m.Policy(murphytest.OwnedBy(payments), murphytest.Pages(primary.Target(), ana.Target()))

	ofPlatform, err := m.API.ListEscalationPoliciesWithResponse(ctx, &client.ListEscalationPoliciesParams{TeamId: &platform.ID})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, ofPlatform.StatusCode())
	assert.Empty(t, ofPlatform.JSON200.Items)
	ofPayments, err := m.API.ListEscalationPoliciesWithResponse(ctx, &client.ListEscalationPoliciesParams{TeamId: &payments.ID})
	require.NoError(t, err)
	require.Len(t, ofPayments.JSON200.Items, 1)
	assert.Equal(t, policy.ID, ofPayments.JSON200.Items[0].Id)
	assert.Equal(t, payments.ID, ofPayments.JSON200.Items[0].Team.Id)
	assert.Len(t, ofPayments.JSON200.Items[0].Levels[0].Targets, 2)
}

func TestEscalationPoliciesRefuseUnknownTeamsAndTargets(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	ana := m.User()

	for name, input := range map[string]client.EscalationPolicyInput{
		"team":     policyInput(m, unknown, ana.Target()),
		"user":     policyInput(m, team.ID, client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: unknown}),
		"schedule": policyInput(m, team.ID, client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: unknown}),
	} {
		created, err := m.API.CreateEscalationPolicyWithResponse(t.Context(), nil, input)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, created.StatusCode(), name)
	}
}

func TestEscalationPoliciesKeepEveryVersion(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	team := m.Team()
	ana, bo := m.User(), m.User()
	policy := m.Policy(murphytest.OwnedBy(team), murphytest.Pages(ana.Target()))
	assert.Equal(t, int32(1), policy.Version)

	updated, err := m.API.UpdateEscalationPolicyWithResponse(ctx, policy.ID, policyInput(m, team.ID, bo.Target()))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, updated.StatusCode())
	assert.Equal(t, int32(2), updated.JSON200.Version)
	first, err := m.API.GetEscalationPolicyVersionWithResponse(ctx, policy.ID, 1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, first.StatusCode())
	assert.Equal(t, ana.ID, first.JSON200.Levels[0].Targets[0].Id)

	name := m.Name("service")
	service, err := m.API.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: name, EscalationPolicy: client.Reference{Id: policy.ID}})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, service.StatusCode())
	inUse, err := m.API.DeleteEscalationPolicyWithResponse(ctx, policy.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, inUse.StatusCode(), "a policy services page with can't be deleted")

	other := m.Policy(murphytest.OwnedBy(team))
	moved, err := m.API.UpdateServiceWithResponse(ctx, service.JSON201.Id, client.ServiceInput{Name: name, EscalationPolicy: client.Reference{Id: other.ID}})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, moved.StatusCode())
	deleted, err := m.API.DeleteEscalationPolicyWithResponse(ctx, policy.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, deleted.StatusCode())

	gone, err := m.API.GetEscalationPolicyWithResponse(ctx, policy.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, gone.StatusCode())
	kept, err := m.API.GetEscalationPolicyVersionWithResponse(ctx, policy.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, kept.StatusCode(), "versions stay for the incidents that followed them")
}

func TestTeamsAndTheirMembers(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	team, ana := m.Team(), m.User()

	added, err := m.API.SetTeamMemberWithResponse(ctx, team.ID, ana.ID, client.TeamMemberInput{Role: client.Responder})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, added.StatusCode())
	promoted, err := m.API.SetTeamMemberWithResponse(ctx, team.ID, ana.ID, client.TeamMemberInput{Role: client.Manager})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, promoted.StatusCode())

	members, err := m.API.ListTeamMembersWithResponse(ctx, team.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, members.StatusCode())
	require.Len(t, members.JSON200.Items, 1)
	assert.Equal(t, client.Manager, members.JSON200.Items[0].Role)

	stranger, err := m.API.SetTeamMemberWithResponse(ctx, team.ID, unknown, client.TeamMemberInput{Role: client.Observer})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, stranger.StatusCode())

	removed, err := m.API.RemoveTeamMemberWithResponse(ctx, team.ID, ana.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, removed.StatusCode())
	members, err = m.API.ListTeamMembersWithResponse(ctx, team.ID)
	require.NoError(t, err)
	assert.Empty(t, members.JSON200.Items)
}
