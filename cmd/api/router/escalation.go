package router

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ListEscalationPolicies(ctx context.Context, request ListEscalationPoliciesRequestObject) (_ ListEscalationPoliciesResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	policies, next, err := s.escalation.ListPolicies(ctx, text(request.Params.TeamId), page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]EscalationPolicy, len(policies))
	for i, policy := range policies {
		items[i] = toPolicy(policy, n)
	}
	return ListEscalationPolicies200JSONResponse{Items: items, NextCursor: next}, s.name(ctx, n)
}

func (s *Server) CreateEscalationPolicy(ctx context.Context, request CreateEscalationPolicyRequestObject) (_ CreateEscalationPolicyResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	policy, err := s.escalation.CreatePolicy(ctx, fromPolicyInput(request.Body))
	if err != nil {
		return nil, err
	}
	return CreateEscalationPolicy201JSONResponse(toPolicy(policy, n)), s.name(ctx, n)
}

func (s *Server) GetEscalationPolicy(ctx context.Context, request GetEscalationPolicyRequestObject) (_ GetEscalationPolicyResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	policy, err := s.escalation.GetPolicy(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetEscalationPolicy200JSONResponse(toPolicy(policy, n)), s.name(ctx, n)
}

func (s *Server) UpdateEscalationPolicy(ctx context.Context, request UpdateEscalationPolicyRequestObject) (_ UpdateEscalationPolicyResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	in := fromPolicyInput(request.Body)
	in.Id = request.Id
	policy, err := s.escalation.UpdatePolicy(ctx, in)
	if err != nil {
		return nil, err
	}
	return UpdateEscalationPolicy200JSONResponse(toPolicy(policy, n)), s.name(ctx, n)
}

func (s *Server) DeleteEscalationPolicy(ctx context.Context, request DeleteEscalationPolicyRequestObject) (_ DeleteEscalationPolicyResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.escalation.DeletePolicy(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteEscalationPolicy204Response{}, nil
}

func (s *Server) GetEscalationPolicyVersion(ctx context.Context, request GetEscalationPolicyVersionRequestObject) (_ GetEscalationPolicyVersionResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	policy, err := s.escalation.GetPolicyVersion(ctx, request.Id, request.Version)
	if err != nil {
		return nil, err
	}
	return GetEscalationPolicyVersion200JSONResponse(toPolicy(policy, n)), s.name(ctx, n)
}

func fromPolicyInput(in *EscalationPolicyInput) *murphy.EscalationPolicy {
	policy := &murphy.EscalationPolicy{Name: in.Name, TeamId: in.Team.Id}
	if in.RepeatCount != nil {
		policy.RepeatCount = *in.RepeatCount
	}
	for _, level := range in.Levels {
		l := &murphy.EscalationLevel{Delay: durationpb.New(time.Duration(level.DelaySeconds) * time.Second)}
		for _, target := range level.Targets {
			l.Targets = append(l.Targets, &murphy.EscalationTarget{
				Type: murphy.EscalationTargetTypeFromText(string(target.Type)),
				Id:   target.Id,
			})
		}
		policy.Levels = append(policy.Levels, l)
	}
	return policy
}

func toPolicy(policy *murphy.EscalationPolicy, n *names) EscalationPolicy {
	out := EscalationPolicy{
		Id:          policy.GetId(),
		Name:        policy.GetName(),
		Team:        n.team(policy.GetTeamId()),
		Version:     policy.GetVersion(),
		RepeatCount: policy.GetRepeatCount(),
		Levels:      []EscalationLevel{},
		CreatedAt:   policy.GetCreatedAt().AsTime(),
		UpdatedAt:   policy.GetUpdatedAt().AsTime(),
	}
	for _, level := range policy.GetLevels() {
		l := EscalationLevel{DelaySeconds: int64(level.GetDelay().AsDuration() / time.Second), Targets: []EscalationTarget{}}
		for _, target := range level.GetTargets() {
			l.Targets = append(l.Targets, n.target(target))
		}
		out.Levels = append(out.Levels, l)
	}
	return out
}
