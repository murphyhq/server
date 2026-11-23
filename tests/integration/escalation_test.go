//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/testenv"
)

func TestEscalationPoliciesBelongToATeam(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()
		platform, payments := createTeam(t, api, "platform"), createTeam(t, api, "payments")
		ana := createUser(t, api, "ana@example.com")
		schedule := createSchedule(t, api, "primary", []string{payments}, ana)

		created, err := api.CreateEscalationPolicyWithResponse(ctx, nil, policyInput("payments", payments,
			client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: schedule},
			client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: ana},
		))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode())
		assert.Equal(t, payments, created.JSON201.TeamId)

		ofPlatform, err := api.ListEscalationPoliciesWithResponse(ctx, &client.ListEscalationPoliciesParams{TeamId: &platform})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, ofPlatform.StatusCode())
		assert.Empty(t, ofPlatform.JSON200.Items)
		ofPayments, err := api.ListEscalationPoliciesWithResponse(ctx, &client.ListEscalationPoliciesParams{TeamId: &payments})
		require.NoError(t, err)
		require.Len(t, ofPayments.JSON200.Items, 1)
		assert.Len(t, ofPayments.JSON200.Items[0].Levels[0].Targets, 2)
	})
}

func TestEscalationPoliciesRefuseUnknownTeamsAndTargets(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		unknown := "0195f3a0-0000-7000-8000-000000000000"
		team := createTeam(t, api, "platform")
		ana := createUser(t, api, "ana@example.com")

		for name, input := range map[string]client.EscalationPolicyInput{
			"team":     policyInput("platform", unknown, client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: ana}),
			"user":     policyInput("platform", team, client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: unknown}),
			"schedule": policyInput("platform", team, client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: unknown}),
		} {
			created, err := api.CreateEscalationPolicyWithResponse(t.Context(), nil, input)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, created.StatusCode(), name)
		}
	})
}

func policyInput(name, team string, targets ...client.EscalationTarget) client.EscalationPolicyInput {
	return client.EscalationPolicyInput{
		Name:   name,
		TeamId: team,
		Levels: []client.EscalationLevel{{DelaySeconds: 5 * 60, Targets: targets}},
	}
}
