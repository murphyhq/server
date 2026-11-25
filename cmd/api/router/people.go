package router

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ListUsers(ctx context.Context, request ListUsersRequestObject) (_ ListUsersResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	users, next, err := s.people.ListUsers(ctx, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]User, len(users))
	for i, user := range users {
		items[i] = toUser(user)
	}
	return ListUsers200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) CreateUser(ctx context.Context, request CreateUserRequestObject) (_ CreateUserResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	user, err := s.people.CreateUser(ctx, fromUserInput(request.Body))
	if err != nil {
		return nil, err
	}
	return CreateUser201JSONResponse(toUser(user)), nil
}

func (s *Server) GetUser(ctx context.Context, request GetUserRequestObject) (_ GetUserResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	user, err := s.people.GetUser(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetUser200JSONResponse(toUser(user)), nil
}

func (s *Server) UpdateUser(ctx context.Context, request UpdateUserRequestObject) (_ UpdateUserResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	in := fromUserInput(request.Body)
	in.Id = request.Id
	user, err := s.people.UpdateUser(ctx, in)
	if err != nil {
		return nil, err
	}
	return UpdateUser200JSONResponse(toUser(user)), nil
}

func (s *Server) DeleteUser(ctx context.Context, request DeleteUserRequestObject) (_ DeleteUserResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.people.DeleteUser(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteUser204Response{}, nil
}

func (s *Server) ListContactMethods(ctx context.Context, request ListContactMethodsRequestObject) (_ ListContactMethodsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	user, methods, err := s.people.ListContactMethods(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	n.knowUser(user)
	items := make([]ContactMethod, len(methods))
	for i, method := range methods {
		items[i] = toContactMethod(method, n)
	}
	return ListContactMethods200JSONResponse{Items: items}, s.name(ctx, n)
}

func (s *Server) CreateContactMethod(ctx context.Context, request CreateContactMethodRequestObject) (_ CreateContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	method, err := s.people.CreateContactMethod(ctx, &murphy.ContactMethod{
		UserId:       request.Id,
		Type:         murphy.ContactMethodTypeFromText(string(request.Body.Type)),
		Address:      request.Body.Address,
		PushPlatform: murphy.PushPlatformFromText(string(*murphy.Or(request.Body.Platform, new(PushPlatform)))),
	})
	if err != nil {
		return nil, err
	}
	return CreateContactMethod201JSONResponse(toContactMethod(method, nil)), nil
}

func (s *Server) GetContactMethod(ctx context.Context, request GetContactMethodRequestObject) (_ GetContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	method, err := s.people.GetContactMethod(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetContactMethod200JSONResponse(toContactMethod(method, n)), s.name(ctx, n)
}

func (s *Server) UpdateContactMethod(ctx context.Context, request UpdateContactMethodRequestObject) (_ UpdateContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	method, err := s.people.UpdateContactMethod(ctx, &murphy.ContactMethod{
		Id:           request.Id,
		Type:         murphy.ContactMethodTypeFromText(string(request.Body.Type)),
		Address:      request.Body.Address,
		PushPlatform: murphy.PushPlatformFromText(string(*murphy.Or(request.Body.Platform, new(PushPlatform)))),
	})
	if err != nil {
		return nil, err
	}
	return UpdateContactMethod200JSONResponse(toContactMethod(method, nil)), nil
}

func (s *Server) DeleteContactMethod(ctx context.Context, request DeleteContactMethodRequestObject) (_ DeleteContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.people.DeleteContactMethod(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteContactMethod204Response{}, nil
}

func fromUserInput(in *UserInput) *murphy.User {
	return &murphy.User{
		Name:     in.Name,
		Email:    in.Email,
		TimeZone: text(in.TimeZone),
		Status:   murphy.UserStatusFromText(text(in.Status)),
	}
}

func toUser(user *murphy.User) User {
	return User{
		Id:        user.GetId(),
		Name:      user.GetName(),
		Email:     user.GetEmail(),
		TimeZone:  user.GetTimeZone(),
		Status:    UserStatus(user.GetStatus().Text()),
		Role:      UserRole(user.GetRole().Text()),
		CreatedAt: user.GetCreatedAt().AsTime(),
		UpdatedAt: user.GetUpdatedAt().AsTime(),
	}
}

func toContactMethod(method *murphy.ContactMethod, n *names) ContactMethod {
	out := ContactMethod{
		Id:        method.GetId(),
		User:      n.user(method.GetUserId()),
		Type:      ContactMethodType(method.GetType().Text()),
		Address:   method.GetAddress(),
		Platform:  pushPlatform(method.GetPushPlatform()),
		CreatedAt: method.GetCreatedAt().AsTime(),
		UpdatedAt: method.GetUpdatedAt().AsTime(),
	}
	if at := method.GetDisabledAt(); at != nil {
		disabled, reason := at.AsTime(), method.GetDisabledReason()
		out.DisabledAt, out.DisabledReason = &disabled, &reason
	}
	return out
}

func pushPlatform(platform murphy.PushPlatform) *PushPlatform {
	if platform == murphy.PushPlatform_PUSH_PLATFORM_UNSPECIFIED {
		return nil
	}
	text := PushPlatform(platform.Text())
	return &text
}

func (s *Server) ListNotificationRules(ctx context.Context, request ListNotificationRulesRequestObject) (_ ListNotificationRulesResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rules, err := s.people.NotificationRules(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	items := make([]NotificationRule, len(rules))
	for i, rule := range rules {
		items[i] = toNotificationRule(rule)
	}
	return ListNotificationRules200JSONResponse{Items: items}, nil
}

func (s *Server) SetNotificationRule(ctx context.Context, request SetNotificationRuleRequestObject) (_ SetNotificationRuleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	in := &murphy.NotificationRule{UserId: request.Id, Urgency: murphy.UrgencyFromText(string(request.Urgency))}
	for _, step := range request.Body.Steps {
		in.Steps = append(in.Steps, &murphy.NotificationStep{ContactMethodId: step.ContactMethod.Id, Delay: durationpb.New(time.Duration(step.DelaySeconds) * time.Second)})
	}
	rule, err := s.people.SetNotificationRule(ctx, in)
	if err != nil {
		return nil, err
	}
	return SetNotificationRule200JSONResponse(toNotificationRule(rule)), nil
}

func (s *Server) ResetNotificationRule(ctx context.Context, request ResetNotificationRuleRequestObject) (_ ResetNotificationRuleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.people.ResetNotificationRule(ctx, request.Id, murphy.UrgencyFromText(string(request.Urgency))); err != nil {
		return nil, err
	}
	return ResetNotificationRule204Response{}, nil
}

func toNotificationRule(rule *murphy.NotificationRule) NotificationRule {
	out := NotificationRule{Urgency: Urgency(rule.GetUrgency().Text()), Custom: rule.GetCustom(), Steps: make([]NotificationStep, len(rule.GetSteps()))}
	for i, step := range rule.GetSteps() {
		out.Steps[i] = NotificationStep{ContactMethod: ref(step.GetContactMethodId()), DelaySeconds: int32(step.GetDelay().AsDuration() / time.Second)}
	}
	return out
}
