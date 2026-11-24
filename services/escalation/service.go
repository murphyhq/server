package escalation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Repository interface {
	CreatePolicy(ctx context.Context, policy *murphy.EscalationPolicy) error
	GetPolicy(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
	GetPolicyRecord(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
	ListPolicies(ctx context.Context, teamID string, before string, limit int) ([]*murphy.EscalationPolicy, error)
	UpdatePolicy(ctx context.Context, policy *murphy.EscalationPolicy) error
	DeletePolicy(ctx context.Context, id string) error
	GetPolicyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error)
	CurrentVersion(ctx context.Context, id string) (int32, error)
	PolicyNames(ctx context.Context, ids []string) (map[string]string, error)
	PoliciesOfTeams(ctx context.Context, teamIDs []string) (map[string]string, error)
}

type People interface {
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	TeamNames(ctx context.Context, ids []string) (map[string]string, error)
}

type Schedules interface {
	ScheduleNames(ctx context.Context, ids []string) (map[string]string, error)
}

type Catalog interface {
	PolicyInUse(ctx context.Context, policyID string) (bool, error)
}

type Service struct {
	repo         Repository
	people       People
	schedules    Schedules
	catalog      Catalog
	minimumDelay time.Duration
}

func New(repo Repository, people People, schedules Schedules, catalog Catalog, minimumDelay time.Duration) *Service {
	return &Service{repo: repo, people: people, schedules: schedules, catalog: catalog, minimumDelay: minimumDelay}
}

func (s *Service) CreatePolicy(ctx context.Context, in *murphy.EscalationPolicy) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := murphy.Now()
	policy := &murphy.EscalationPolicy{
		Id:          murphy.NewID(),
		Name:        in.GetName(),
		TeamId:      in.GetTeamId(),
		RepeatCount: in.GetRepeatCount(),
		Levels:      in.GetLevels(),
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", policy.GetId()))
	if err := s.check(ctx, policy); err != nil {
		return nil, err
	}
	if err := s.repo.CreatePolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("create escalation policy: %w", err)
	}
	return policy, nil
}

func (s *Service) GetPolicy(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("escalation policy", id); err != nil {
		return nil, err
	}
	policy, err := s.repo.GetPolicy(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get escalation policy: %w", err)
	}
	return policy, nil
}

func (s *Service) ListPolicies(ctx context.Context, teamID string, page pagination.Page) (_ []*murphy.EscalationPolicy, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	if teamID != "" {
		if err := murphy.LookupID("team", teamID); err != nil {
			return nil, "", err
		}
	}
	policies, err := s.repo.ListPolicies(ctx, teamID, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list escalation policies: %w", err)
	}
	items, next := pagination.Trim(policies, window)
	return items, next, nil
}

func (s *Service) UpdatePolicy(ctx context.Context, in *murphy.EscalationPolicy) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetPolicyRecord(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	policy := &murphy.EscalationPolicy{
		Id:          current.GetId(),
		Name:        in.GetName(),
		TeamId:      in.GetTeamId(),
		RepeatCount: in.GetRepeatCount(),
		Levels:      in.GetLevels(),
		Version:     current.GetVersion() + 1,
		CreatedAt:   current.GetCreatedAt(),
		UpdatedAt:   murphy.Now(),
	}
	if err := s.check(ctx, policy); err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("update escalation policy: %w", err)
	}
	return policy, nil
}

func (s *Service) DeletePolicy(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("escalation policy", id); err != nil {
		return err
	}
	inUse, err := s.catalog.PolicyInUse(ctx, id)
	if err != nil {
		return fmt.Errorf("check services using the escalation policy: %w", err)
	}
	if inUse {
		return murphy.ErrConflict
	}
	if err := s.repo.DeletePolicy(ctx, id); err != nil {
		return fmt.Errorf("delete escalation policy: %w", err)
	}
	return nil
}

func (s *Service) check(ctx context.Context, policy *murphy.EscalationPolicy) error {
	invalid := []error{policy.Validate()}
	for i, level := range policy.GetLevels() {
		if level.GetDelay().AsDuration() < s.minimumDelay {
			invalid = append(invalid, &murphy.FieldError{Field: fmt.Sprintf("levels[%d].delay", i), Reason: "must be at least " + s.minimumDelay.String()})
		}
	}
	if err := errors.Join(invalid...); err != nil {
		return err
	}
	var userIDs, scheduleIDs []string
	for _, level := range policy.GetLevels() {
		for _, target := range level.GetTargets() {
			switch target.GetType() {
			case murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER:
				userIDs = append(userIDs, target.GetId())
			case murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE:
				scheduleIDs = append(scheduleIDs, target.GetId())
			}
		}
	}
	teams, err := s.people.TeamNames(ctx, []string{policy.GetTeamId()})
	if err != nil {
		return fmt.Errorf("check team: %w", err)
	}
	users, err := s.people.UserNames(ctx, userIDs)
	if err != nil {
		return fmt.Errorf("check user targets: %w", err)
	}
	schedules, err := s.schedules.ScheduleNames(ctx, scheduleIDs)
	if err != nil {
		return fmt.Errorf("check schedule targets: %w", err)
	}

	var errs []error
	if _, ok := teams[policy.GetTeamId()]; !ok {
		errs = append(errs, &murphy.FieldError{Field: "team_id", Reason: "must be an existing team"})
	}
	for i, level := range policy.GetLevels() {
		for j, target := range level.GetTargets() {
			found := users
			if target.GetType() == murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE {
				found = schedules
			}
			if _, exists := found[target.GetId()]; !exists {
				errs = append(errs, &murphy.FieldError{
					Field:  fmt.Sprintf("levels[%d].targets[%d].id", i, j),
					Reason: "must be an existing " + target.GetType().Text(),
				})
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) GetPolicyVersion(ctx context.Context, id string, version int32) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", id), telemetry.WithField("escalation_policy.version", version))
	defer span.End(&err)

	if err := murphy.LookupID("escalation policy", id); err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, &murphy.NotFoundError{Entity: "escalation policy version"}
	}
	policy, err := s.repo.GetPolicyVersion(ctx, id, version)
	if err != nil {
		return nil, fmt.Errorf("get escalation policy version: %w", err)
	}
	return policy, nil
}

func (s *Service) CurrentVersion(ctx context.Context, id string) (_ int32, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("escalation policy", id); err != nil {
		return 0, err
	}
	version, err := s.repo.CurrentVersion(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("get the escalation policy's current version: %w", err)
	}
	return version, nil
}

func (s *Service) PolicyNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.repo.PolicyNames(ctx, ids)
}

func (s *Service) PoliciesOfTeams(ctx context.Context, teamIDs []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	for _, id := range teamIDs {
		if err := murphy.LookupID("team", id); err != nil {
			return nil, err
		}
	}
	return s.repo.PoliciesOfTeams(ctx, teamIDs)
}

func (s *Service) GetPolicyRecord(ctx context.Context, id string) (_ *murphy.EscalationPolicy, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("escalation_policy.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("escalation policy", id); err != nil {
		return nil, err
	}
	return s.repo.GetPolicyRecord(ctx, id)
}
