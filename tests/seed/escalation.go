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
	user := func(handle string) client.EscalationTarget {
		return client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: users[handle+"@murphy.test"]}
	}
	level := func(delay int64, targets ...client.EscalationTarget) client.EscalationLevel {
		return client.EscalationLevel{DelaySeconds: delay, Targets: targets}
	}
	policy := func(name, team string, repeat int32, levels ...client.EscalationLevel) client.EscalationPolicyInput {
		return client.EscalationPolicyInput{Name: name, Team: client.Reference{Id: teams[team]}, RepeatCount: &repeat, Levels: levels}
	}
	policies := []client.EscalationPolicyInput{
		{
			Name: "platform", Team: client.Reference{Id: teams["platform"]}, RepeatCount: &repeat,
			Levels: []client.EscalationLevel{
				{DelaySeconds: 5 * minute, Targets: []client.EscalationTarget{schedule("platform primary")}},
				{DelaySeconds: 15 * minute, Targets: []client.EscalationTarget{schedule("platform secondary")}},
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
	policies = append(policies,
		policy("mobile", "mobile", 2,
			level(5*minute, schedule("mobile primary")),
			level(10*minute, schedule("mobile secondary"), user("eli")),
			level(15*minute, schedule("engineering leads"))),
		policy("web", "web", 1,
			level(3*minute, schedule("web primary")),
			level(5*minute, schedule("web secondary"), schedule("sre primary")),
			level(10*minute, user("lena"), user("yara")),
			level(15*minute, schedule("engineering leads"))),
		policy("data", "data", 0,
			level(15*minute, schedule("data on-call")),
			level(30*minute, user("sara"), user("will"))),
		policy("sre", "sre", 3,
			level(2*minute, schedule("sre primary")),
			level(5*minute, schedule("sre secondary"), schedule("platform weekend")),
			level(10*minute, user("yara"), user("zoe"), user("ana"))),
		policy("security", "security", 2,
			level(minute, schedule("security on-call"), user("gia")),
			level(5*minute, schedule("sre primary"), user("leo")),
			level(10*minute, schedule("engineering leads"))),
		policy("payments critical", "payments", 2,
			level(minute, schedule("payments primary"), schedule("sre primary")),
			level(5*minute, schedule("incident commanders"), user("dee")),
			level(10*minute, schedule("engineering leads"), user("ana"))),
	)

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
