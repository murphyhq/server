package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/escalation"
)

type Tx interface {
	CreatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy) error
	UpdatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy) error
	DeleteLevels(ctx context.Context, policyID string) error
	CreateLevels(ctx context.Context, policyID string, levels []*murphy.EscalationLevel, at time.Time) error
	CreateLevelTargets(ctx context.Context, levels []*murphy.EscalationLevel, at time.Time) error
}

type Engine[T Tx] interface {
	GetPolicyRow(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
	ListPolicyRows(ctx context.Context, teamID string, before string, limit int) ([]*murphy.EscalationPolicy, error)
	ListLevels(ctx context.Context, policyIDs []string) (map[string][]*murphy.EscalationLevel, error)
	ListLevelTargets(ctx context.Context, policyIDs []string) (map[string][]*murphy.EscalationTarget, error)
	DeletePolicy(ctx context.Context, id string) error
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ escalation.Repository = (*Repository[Tx])(nil)

func (r *Repository[T]) CreatePolicy(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.CreatePolicyRow(ctx, policy); err != nil {
			return err
		}
		return writeLevels(ctx, tx, policy)
	})
}

func (r *Repository[T]) UpdatePolicy(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.UpdatePolicyRow(ctx, policy); err != nil {
			return err
		}
		if err := tx.DeleteLevels(ctx, policy.GetId()); err != nil {
			return err
		}
		return writeLevels(ctx, tx, policy)
	})
}

func writeLevels[T Tx](ctx context.Context, tx T, policy *murphy.EscalationPolicy) error {
	at := policy.GetUpdatedAt().AsTime()
	for _, level := range policy.GetLevels() {
		level.Id = murphy.NewID()
	}
	if err := tx.CreateLevels(ctx, policy.GetId(), policy.GetLevels(), at); err != nil {
		return err
	}
	return tx.CreateLevelTargets(ctx, policy.GetLevels(), at)
}

func (r *Repository[T]) GetPolicy(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policy, err := r.GetPolicyRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, []*murphy.EscalationPolicy{policy}); err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *Repository[T]) ListPolicies(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policies, err := r.ListPolicyRows(ctx, teamID, before, limit)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, policies); err != nil {
		return nil, err
	}
	return policies, nil
}

func (r *Repository[T]) complete(ctx context.Context, policies []*murphy.EscalationPolicy) error {
	if len(policies) == 0 {
		return nil
	}
	ids := make([]string, len(policies))
	for i, policy := range policies {
		ids[i] = policy.GetId()
	}
	levels, err := r.ListLevels(ctx, ids)
	if err != nil {
		return err
	}
	targets, err := r.ListLevelTargets(ctx, ids)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		policy.Levels = levels[policy.GetId()]
		for _, level := range policy.GetLevels() {
			level.Targets = targets[level.GetId()]
		}
	}
	return nil
}
