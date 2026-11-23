package router

import (
	"context"

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

	methods, err := s.people.ListContactMethods(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	items := make([]ContactMethod, len(methods))
	for i, method := range methods {
		items[i] = toContactMethod(method)
	}
	return ListContactMethods200JSONResponse{Items: items}, nil
}

func (s *Server) CreateContactMethod(ctx context.Context, request CreateContactMethodRequestObject) (_ CreateContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	method, err := s.people.CreateContactMethod(ctx, &murphy.ContactMethod{
		UserId:  request.Id,
		Type:    murphy.ContactMethodTypeFromText(string(request.Body.Type)),
		Address: request.Body.Address,
	})
	if err != nil {
		return nil, err
	}
	return CreateContactMethod201JSONResponse(toContactMethod(method)), nil
}

func (s *Server) GetContactMethod(ctx context.Context, request GetContactMethodRequestObject) (_ GetContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	method, err := s.people.GetContactMethod(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetContactMethod200JSONResponse(toContactMethod(method)), nil
}

func (s *Server) UpdateContactMethod(ctx context.Context, request UpdateContactMethodRequestObject) (_ UpdateContactMethodResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	method, err := s.people.UpdateContactMethod(ctx, &murphy.ContactMethod{
		Id:      request.Id,
		Type:    murphy.ContactMethodTypeFromText(string(request.Body.Type)),
		Address: request.Body.Address,
	})
	if err != nil {
		return nil, err
	}
	return UpdateContactMethod200JSONResponse(toContactMethod(method)), nil
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
		CreatedAt: user.GetCreatedAt().AsTime(),
		UpdatedAt: user.GetUpdatedAt().AsTime(),
	}
}

func toContactMethod(method *murphy.ContactMethod) ContactMethod {
	return ContactMethod{
		Id:        method.GetId(),
		User:      ref(method.GetUserId()),
		Type:      ContactMethodType(method.GetType().Text()),
		Address:   method.GetAddress(),
		CreatedAt: method.GetCreatedAt().AsTime(),
		UpdatedAt: method.GetUpdatedAt().AsTime(),
	}
}
