package mysql

import (
	"context"
	"embed"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphymysql.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphymysql.OpenDatabase(ctx, murphymysql.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "escalation",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{tx: tx, queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

type Tx struct {
	tx      murphymysql.DBTX
	queries *Queries
}

func (t *Tx) CreatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("escalation policy", t.queries.CreatePolicy(ctx, CreatePolicyParams{
		ID: policy.GetId(), Name: policy.GetName(), TeamID: policy.GetTeamId(), Version: policy.GetVersion(),
		CreatedAt: policy.GetCreatedAt().AsTime(), UpdatedAt: policy.GetUpdatedAt().AsTime(),
	}))
}

func (t *Tx) UpdatePolicyRow(ctx context.Context, policy *murphy.EscalationPolicy, previousVersion int32) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.UpdatePolicy(ctx, UpdatePolicyParams{
		ID: policy.GetId(), Name: policy.GetName(), TeamID: policy.GetTeamId(), Version: policy.GetVersion(),
		PreviousVersion: previousVersion, UpdatedAt: policy.GetUpdatedAt().AsTime(),
	})
	if err != nil {
		return murphymysql.Error("escalation policy", err)
	}
	if rows == 0 {
		return murphy.ErrConflict
	}
	return nil
}

func (t *Tx) CreateVersionRow(ctx context.Context, policy *murphy.EscalationPolicy, versionID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("escalation policy version", t.queries.CreateVersion(ctx, CreateVersionParams{
		ID: versionID, PolicyID: policy.GetId(), Version: policy.GetVersion(), RepeatCount: policy.GetRepeatCount(), CreatedAt: at,
	}))
}

func (t *Tx) CreateLevels(ctx context.Context, versionID string, levels []*murphy.EscalationLevel, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateLevels", Table: "escalation_levels", Columns: []string{"id", "version_id", "position", "delay_seconds", "created_at"}}
	for i, level := range levels {
		rows.Values = append(rows.Values, []any{level.GetId(), versionID, i, int64(level.GetDelay().AsDuration() / time.Second), at})
	}
	return murphymysql.Error("escalation level", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) CreateLevelTargets(ctx context.Context, levels []*murphy.EscalationLevel, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{
		Name:    "CreateLevelTargets",
		Table:   "level_targets",
		Columns: []string{"id", "level_id", "position", "target_type", "target_id", "created_at"},
	}
	for _, level := range levels {
		for i, target := range level.GetTargets() {
			rows.Values = append(rows.Values, []any{murphy.NewID(), level.GetId(), i, target.GetType().Text(), target.GetId(), at})
		}
	}
	return murphymysql.Error("escalation target", murphymysql.Insert(ctx, t.tx, rows))
}

func (r *Repository) GetPolicyRow(ctx context.Context, id string) (_ *murphy.EscalationPolicy, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetPolicy(ctx, id)
	if err != nil {
		return nil, "", murphymysql.Error("escalation policy", err)
	}
	return toPolicy(GetPolicyVersionRow(row)), row.VersionID, nil
}

func (r *Repository) GetPolicyVersionRow(ctx context.Context, id string, version int32) (_ *murphy.EscalationPolicy, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetPolicyVersion(ctx, GetPolicyVersionParams{ID: id, Version: version})
	if err != nil {
		return nil, "", murphymysql.Error("escalation policy version", err)
	}
	return toPolicy(row), row.VersionID, nil
}

func (r *Repository) ListPolicyRows(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.EscalationPolicy, _ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListPolicies(ctx, ListPoliciesParams{Before: before, TeamID: database.Text(teamID), Limit: int32(limit)})
	if err != nil {
		return nil, nil, murphymysql.Error("escalation policy", err)
	}
	policies := make([]*murphy.EscalationPolicy, len(rows))
	versionIDs := make([]string, len(rows))
	for i, row := range rows {
		policies[i], versionIDs[i] = toPolicy(GetPolicyVersionRow(row)), row.VersionID
	}
	return policies, versionIDs, nil
}

func (r *Repository) ListLevels(ctx context.Context, versionIDs []string) (_ map[string][]*murphy.EscalationLevel, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLevels(ctx, versionIDs)
	if err != nil {
		return nil, murphymysql.Error("escalation level", err)
	}
	levels := map[string][]*murphy.EscalationLevel{}
	for _, row := range rows {
		levels[row.VersionID] = append(levels[row.VersionID], &murphy.EscalationLevel{
			Id: row.ID, Delay: durationpb.New(time.Duration(row.DelaySeconds) * time.Second),
		})
	}
	return levels, nil
}

func (r *Repository) ListLevelTargets(ctx context.Context, versionIDs []string) (_ map[string][]*murphy.EscalationTarget, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLevelTargets(ctx, versionIDs)
	if err != nil {
		return nil, murphymysql.Error("escalation target", err)
	}
	targets := map[string][]*murphy.EscalationTarget{}
	for _, row := range rows {
		targets[row.LevelID] = append(targets[row.LevelID], &murphy.EscalationTarget{
			Type: murphy.EscalationTargetTypeFromText(row.TargetType), Id: row.TargetID,
		})
	}
	return targets, nil
}

func (r *Repository) DeletePolicy(ctx context.Context, id string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeletePolicy(ctx, DeletePolicyParams{ID: id, DeletedAt: &at})
	return murphymysql.Changed("escalation policy", rows, err)
}

func toPolicy(row GetPolicyVersionRow) *murphy.EscalationPolicy {
	return &murphy.EscalationPolicy{
		Id: row.ID, Name: row.Name, TeamId: row.TeamID, Version: row.Version, RepeatCount: row.RepeatCount,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func (r *Repository) CurrentVersion(ctx context.Context, id string) (_ int32, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	version, err := r.queries.CurrentVersion(ctx, id)
	if err != nil {
		return 0, murphymysql.Error("escalation policy", err)
	}
	return version, nil
}

func (r *Repository) PolicyNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.PolicyNames(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("escalation policy", err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
