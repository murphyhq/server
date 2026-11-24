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
	UpdatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy, previousVersion int32) error
	CreateVersionRow(ctx context.Context, policy *murphy.EscalationPolicy, versionID string, at time.Time) error
	CreateLevels(ctx context.Context, versionID string, levels []*murphy.EscalationLevel, at time.Time) error
	CreateLevelTargets(ctx context.Context, levels []*murphy.EscalationLevel, at time.Time) error
}

type Engine[T Tx] interface {
	GetPolicyRow(ctx context.Context, id string) (_ *murphy.EscalationPolicy, versionID string, _ error)
	GetPolicyVersionRow(ctx context.Context, id string, version int32) (_ *murphy.EscalationPolicy, versionID string, _ error)
	ListPolicyRows(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.EscalationPolicy, versionIDs []string, _ error)
	ListLevels(ctx context.Context, versionIDs []string) (map[string][]*murphy.EscalationLevel, error)
	ListLevelTargets(ctx context.Context, versionIDs []string) (map[string][]*murphy.EscalationTarget, error)
	DeletePolicy(ctx context.Context, id string, at time.Time) error
	CurrentVersion(ctx context.Context, id string) (int32, error)
	PolicyNames(ctx context.Context, ids []string) (map[string]string, error)
	PoliciesOfTeams(ctx context.Context, teamIDs []string) (map[string]string, error)
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
		return writeVersion(ctx, tx, policy)
	})
}

func (r *Repository[T]) UpdatePolicy(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.UpdatePolicyRow(ctx, policy, policy.GetVersion()-1); err != nil {
			return err
		}
		return writeVersion(ctx, tx, policy)
	})
}

func writeVersion[T Tx](ctx context.Context, tx T, policy *murphy.EscalationPolicy) error {
	at := policy.GetUpdatedAt().AsTime()
	versionID := murphy.NewID()
	if err := tx.CreateVersionRow(ctx, policy, versionID, at); err != nil {
		return err
	}
	for _, level := range policy.GetLevels() {
		level.Id = murphy.NewID()
	}
	if err := tx.CreateLevels(ctx, versionID, policy.GetLevels(), at); err != nil {
		return err
	}
	return tx.CreateLevelTargets(ctx, policy.GetLevels(), at)
}

func (r *Repository[T]) GetPolicy(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policy, versionID, err := r.GetPolicyRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, []*murphy.EscalationPolicy{policy}, []string{versionID}); err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *Repository[T]) GetPolicyVersion(ctx context.Context, id string, version int32) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policy, versionID, err := r.GetPolicyVersionRow(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, []*murphy.EscalationPolicy{policy}, []string{versionID}); err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *Repository[T]) ListPolicies(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policies, versionIDs, err := r.ListPolicyRows(ctx, teamID, before, limit)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, policies, versionIDs); err != nil {
		return nil, err
	}
	return policies, nil
}

func (r *Repository[T]) DeletePolicy(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Engine.DeletePolicy(ctx, id, time.Now().UTC())
}

func (r *Repository[T]) complete(ctx context.Context, policies []*murphy.EscalationPolicy, versionIDs []string) error {
	if len(policies) == 0 {
		return nil
	}
	levels, err := r.ListLevels(ctx, versionIDs)
	if err != nil {
		return err
	}
	targets, err := r.ListLevelTargets(ctx, versionIDs)
	if err != nil {
		return err
	}
	for i, policy := range policies {
		policy.Levels = levels[versionIDs[i]]
		for _, level := range policy.GetLevels() {
			level.Targets = targets[level.GetId()]
		}
	}
	return nil
}

func (r *Repository[T]) GetPolicyRecord(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	policy, _, err := r.GetPolicyRow(ctx, id)
	return policy, err
}
