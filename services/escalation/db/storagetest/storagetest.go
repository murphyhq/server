package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/escalation"
)

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

func Run(t *testing.T, open func(t *testing.T) escalation.Repository) {
	t.Run("stores and reads back a policy with its levels in order", func(t *testing.T) {
		repo := open(t)
		schedule, ana, bo := murphy.NewID(), murphy.NewID(), murphy.NewID()
		policy := newPolicy("payments", murphy.NewID(),
			level(5*time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, schedule)),
			level(15*time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, bo), target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, ana)),
		)

		require.NoError(t, repo.CreatePolicy(t.Context(), policy))
		got, err := repo.GetPolicy(t.Context(), policy.GetId())

		require.NoError(t, err)
		assert.Equal(t, policy.GetTeamId(), got.GetTeamId())
		assert.Equal(t, int32(2), got.GetRepeatCount())
		require.Len(t, got.GetLevels(), 2)
		assert.Equal(t, 5*time.Minute, got.GetLevels()[0].GetDelay().AsDuration())
		assert.Equal(t, []string{schedule}, targetIDs(got.GetLevels()[0]))
		assert.Equal(t, murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, got.GetLevels()[0].GetTargets()[0].GetType())
		assert.Equal(t, 15*time.Minute, got.GetLevels()[1].GetDelay().AsDuration())
		assert.Equal(t, []string{bo, ana}, targetIDs(got.GetLevels()[1]))
	})

	t.Run("updating a policy makes a new version and keeps the previous one", func(t *testing.T) {
		repo := open(t)
		first, second := murphy.NewID(), murphy.NewID()
		policy := newPolicy("payments", murphy.NewID(),
			level(5*time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, first)),
			level(10*time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, second)),
		)
		require.NoError(t, repo.CreatePolicy(t.Context(), policy))

		user := murphy.NewID()
		policy.TeamId = murphy.NewID()
		policy.Version = 2
		policy.RepeatCount = 0
		policy.Levels = []*murphy.EscalationLevel{level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, user))}
		require.NoError(t, repo.UpdatePolicy(t.Context(), policy))

		got, err := repo.GetPolicy(t.Context(), policy.GetId())
		require.NoError(t, err)
		assert.Equal(t, int32(2), got.GetVersion())
		assert.Equal(t, policy.GetTeamId(), got.GetTeamId())
		assert.Equal(t, int32(0), got.GetRepeatCount())
		require.Len(t, got.GetLevels(), 1)
		assert.Equal(t, []string{user}, targetIDs(got.GetLevels()[0]))

		previous, err := repo.GetPolicyVersion(t.Context(), policy.GetId(), 1)
		require.NoError(t, err)
		assert.Equal(t, int32(1), previous.GetVersion())
		assert.Equal(t, int32(2), previous.GetRepeatCount())
		require.Len(t, previous.GetLevels(), 2)
		assert.Equal(t, []string{first}, targetIDs(previous.GetLevels()[0]))
		assert.Equal(t, []string{second}, targetIDs(previous.GetLevels()[1]))
	})

	t.Run("refuses an update made from an outdated version", func(t *testing.T) {
		repo := open(t)
		policy := newPolicy("payments", murphy.NewID(), level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, murphy.NewID())))
		require.NoError(t, repo.CreatePolicy(t.Context(), policy))
		policy.Version = 2
		require.NoError(t, repo.UpdatePolicy(t.Context(), policy))

		assert.ErrorIs(t, repo.UpdatePolicy(t.Context(), policy), murphy.ErrConflict)
	})

	t.Run("deleting a policy hides it and keeps its versions", func(t *testing.T) {
		repo := open(t)
		team := murphy.NewID()
		policy := newPolicy("payments", team, level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, murphy.NewID())))
		require.NoError(t, repo.CreatePolicy(t.Context(), policy))

		require.NoError(t, repo.DeletePolicy(t.Context(), policy.GetId()))

		_, err := repo.GetPolicy(t.Context(), policy.GetId())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		listed, err := repo.ListPolicies(t.Context(), team, maxID, 10)
		require.NoError(t, err)
		assert.Empty(t, listed)
		version, err := repo.GetPolicyVersion(t.Context(), policy.GetId(), 1)
		require.NoError(t, err)
		assert.Len(t, version.GetLevels(), 1)
		assert.NoError(t, repo.CreatePolicy(t.Context(), newPolicy("payments", team)), "the name is free again")
	})

	t.Run("lists policies newest first, optionally of one team", func(t *testing.T) {
		repo := open(t)
		platform, payments := murphy.NewID(), murphy.NewID()
		first := newPolicy("first", payments, level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, murphy.NewID())))
		second := newPolicy("second", platform, level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, murphy.NewID())))
		third := newPolicy("third", payments, level(time.Minute, target(murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, murphy.NewID())))
		for _, policy := range []*murphy.EscalationPolicy{first, second, third} {
			require.NoError(t, repo.CreatePolicy(t.Context(), policy))
		}

		all, err := repo.ListPolicies(t.Context(), "", maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{third.GetId(), second.GetId(), first.GetId()}, policyIDs(all))

		ofPayments, err := repo.ListPolicies(t.Context(), payments, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{third.GetId(), first.GetId()}, policyIDs(ofPayments))
		assert.Len(t, ofPayments[1].GetLevels(), 1)

		page, err := repo.ListPolicies(t.Context(), payments, third.GetId(), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.GetId()}, policyIDs(page))
	})

	t.Run("refuses a second policy with the same name", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreatePolicy(t.Context(), newPolicy("payments", murphy.NewID())))

		assert.ErrorIs(t, repo.CreatePolicy(t.Context(), newPolicy("payments", murphy.NewID())), murphy.ErrConflict)
	})

	t.Run("reports missing policies", func(t *testing.T) {
		repo := open(t)
		_, err := repo.GetPolicy(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		_, err = repo.GetPolicyVersion(t.Context(), murphy.NewID(), 1)
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeletePolicy(t.Context(), murphy.NewID()), murphy.ErrNotFound)
	})
}

func newPolicy(name, team string, levels ...*murphy.EscalationLevel) *murphy.EscalationPolicy {
	now := murphy.Now()
	return &murphy.EscalationPolicy{
		Id: murphy.NewID(), Name: name, TeamId: team, Version: 1, RepeatCount: 2, Levels: levels, CreatedAt: now, UpdatedAt: now,
	}
}

func level(delay time.Duration, targets ...*murphy.EscalationTarget) *murphy.EscalationLevel {
	return &murphy.EscalationLevel{Delay: durationpb.New(delay), Targets: targets}
}

func target(kind murphy.EscalationTargetType, id string) *murphy.EscalationTarget {
	return &murphy.EscalationTarget{Type: kind, Id: id}
}

func targetIDs(level *murphy.EscalationLevel) []string {
	ids := make([]string, len(level.GetTargets()))
	for i, target := range level.GetTargets() {
		ids[i] = target.GetId()
	}
	return ids
}

func policyIDs(policies []*murphy.EscalationPolicy) []string {
	ids := make([]string, len(policies))
	for i, policy := range policies {
		ids[i] = policy.GetId()
	}
	return ids
}
