package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphypostgres.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphypostgres.OpenDatabase(ctx, murphypostgres.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "escalation",
		Migrations: migrations,
		Tx:         func(tx pgx.Tx) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.Pool())}, nil
}

type Tx struct {
	queries *Queries
}

func (t *Tx) CreatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("escalation policy", t.queries.CreatePolicy(ctx, CreatePolicyParams{
		ID: policy.GetId(), Name: policy.GetName(), TeamID: policy.GetTeamId(), RepeatCount: policy.GetRepeatCount(),
		CreatedAt: policy.GetCreatedAt().AsTime(), UpdatedAt: policy.GetUpdatedAt().AsTime(),
	}))
}

func (t *Tx) UpdatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.UpdatePolicy(ctx, UpdatePolicyParams{
		ID: policy.GetId(), Name: policy.GetName(), TeamID: policy.GetTeamId(), RepeatCount: policy.GetRepeatCount(), UpdatedAt: policy.GetUpdatedAt().AsTime(),
	})
	return murphypostgres.Changed("escalation policy", rows, err)
}

func (t *Tx) DeleteLevels(ctx context.Context, policyID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("escalation level", t.queries.DeleteLevels(ctx, policyID))
}

func (t *Tx) CreateLevels(ctx context.Context, policyID string, levels []*murphy.EscalationLevel, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(levels) == 0 {
		return nil
	}
	params := CreateLevelsParams{PolicyID: policyID, CreatedAt: at}
	for i, level := range levels {
		params.Ids = append(params.Ids, level.GetId())
		params.Positions = append(params.Positions, int32(i))
		params.DelaySeconds = append(params.DelaySeconds, int64(level.GetDelay().AsDuration()/time.Second))
	}
	return murphypostgres.Error("escalation level", t.queries.CreateLevels(ctx, params))
}

func (t *Tx) CreateLevelTargets(ctx context.Context, levels []*murphy.EscalationLevel, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := CreateLevelTargetsParams{CreatedAt: at}
	for _, level := range levels {
		for i, target := range level.GetTargets() {
			params.Ids = append(params.Ids, murphy.NewID())
			params.LevelIds = append(params.LevelIds, level.GetId())
			params.Positions = append(params.Positions, int32(i))
			params.TargetTypes = append(params.TargetTypes, target.GetType().Text())
			params.TargetIds = append(params.TargetIds, target.GetId())
		}
	}
	if len(params.Ids) == 0 {
		return nil
	}
	return murphypostgres.Error("escalation target", t.queries.CreateLevelTargets(ctx, params))
}

func (r *Repository) GetPolicyRow(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetPolicy(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("escalation policy", err)
	}
	return toPolicy(row), nil
}

func (r *Repository) ListPolicyRows(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListPolicies(ctx, ListPoliciesParams{Before: before, TeamID: database.Text(teamID), MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("escalation policy", err)
	}
	policies := make([]*murphy.EscalationPolicy, len(rows))
	for i, row := range rows {
		policies[i] = toPolicy(row)
	}
	return policies, nil
}

func (r *Repository) ListLevels(ctx context.Context, policyIDs []string) (_ map[string][]*murphy.EscalationLevel, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLevels(ctx, policyIDs)
	if err != nil {
		return nil, murphypostgres.Error("escalation level", err)
	}
	levels := map[string][]*murphy.EscalationLevel{}
	for _, row := range rows {
		levels[row.PolicyID] = append(levels[row.PolicyID], &murphy.EscalationLevel{
			Id: row.ID, Delay: durationpb.New(time.Duration(row.DelaySeconds) * time.Second),
		})
	}
	return levels, nil
}

func (r *Repository) ListLevelTargets(ctx context.Context, policyIDs []string) (_ map[string][]*murphy.EscalationTarget, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLevelTargets(ctx, policyIDs)
	if err != nil {
		return nil, murphypostgres.Error("escalation target", err)
	}
	targets := map[string][]*murphy.EscalationTarget{}
	for _, row := range rows {
		targets[row.LevelID] = append(targets[row.LevelID], &murphy.EscalationTarget{
			Type: murphy.EscalationTargetTypeFromText(row.TargetType), Id: row.TargetID,
		})
	}
	return targets, nil
}

func toPolicy(row EscalationPolicy) *murphy.EscalationPolicy {
	return &murphy.EscalationPolicy{
		Id: row.ID, Name: row.Name, TeamId: row.TeamID, RepeatCount: row.RepeatCount,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func (r *Repository) DeletePolicy(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeletePolicy(ctx, id)
	return murphypostgres.Changed("escalation policy", rows, err)
}
