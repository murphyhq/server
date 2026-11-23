package main

import (
	"context"
	"net/http"

	"github.com/murphyhq/server/client"
)

func seedPolicies(ctx context.Context, api *client.ClientWithResponses, teams, users, schedules map[string]string) (map[string]string, error) {
	const minute = int64(60)
	repeat := int32(2)
	schedule := func(name string) client.EscalationTarget {
		return client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: schedules[name]}
	}
	user := func(email string) client.EscalationTarget {
		return client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: users[email]}
	}
	policies := []client.EscalationPolicyInput{
		{
			Name: "platform", Team: client.Reference{Id: teams["platform"]}, RepeatCount: &repeat,
			Levels: []client.EscalationLevel{
				{DelaySeconds: 5 * minute, Targets: []client.EscalationTarget{schedule("platform primary")}},
				{DelaySeconds: 15 * minute, Targets: []client.EscalationTarget{user("ana@example.com")}},
			},
		},
		{
			Name: "payments", Team: client.Reference{Id: teams["payments"]}, RepeatCount: &repeat,
			Levels: []client.EscalationLevel{
				{DelaySeconds: 5 * minute, Targets: []client.EscalationTarget{schedule("payments primary")}},
				{DelaySeconds: 15 * minute, Targets: []client.EscalationTarget{schedule("incident commanders")}},
			},
		},
	}

	existing, err := byName(func(cursor *string) ([]client.EscalationPolicy, string, error) {
		page, err := api.ListEscalationPoliciesWithResponse(ctx, &client.ListEscalationPoliciesParams{Cursor: cursor})
		if err := expect(page, err, http.StatusOK, "list escalation policies"); err != nil {
			return nil, "", err
		}
		return page.JSON200.Items, page.JSON200.NextCursor, nil
	}, func(policy client.EscalationPolicy) (string, string) { return policy.Name, policy.Id })
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		if _, ok := existing[policy.Name]; ok {
			continue
		}
		created, err := api.CreateEscalationPolicyWithResponse(ctx, nil, policy)
		if err := expect(created, err, http.StatusCreated, "create escalation policy "+policy.Name); err != nil {
			return nil, err
		}
		existing[policy.Name] = created.JSON201.Id
	}
	return existing, nil
}
