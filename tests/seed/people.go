package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/murphyhq/server/client"
)

type seededUser struct {
	name, email, timeZone string
	teams                 map[string]client.TeamRole
}

var seededUsers = []seededUser{
	{"Ana Costa", "ana@example.com", "Europe/Malta", map[string]client.TeamRole{"platform": client.Manager, "payments": client.Observer}},
	{"Bo Lindqvist", "bo@example.com", "Europe/Stockholm", map[string]client.TeamRole{"platform": client.Responder}},
	{"Cy Okafor", "cy@example.com", "Africa/Lagos", map[string]client.TeamRole{"payments": client.Responder}},
	{"Dee Park", "dee@example.com", "America/New_York", map[string]client.TeamRole{"payments": client.Manager}},
}

func seedPeople(ctx context.Context, api *client.ClientWithResponses) (teams, users map[string]string, err error) {
	teams, err = byName(func(cursor *string) ([]client.Team, string, error) {
		page, err := api.ListTeamsWithResponse(ctx, &client.ListTeamsParams{Cursor: cursor})
		if err := expect(page, err, http.StatusOK, "list teams"); err != nil {
			return nil, "", err
		}
		return page.JSON200.Items, page.JSON200.NextCursor, nil
	}, func(team client.Team) (string, string) { return team.Name, team.Id })
	if err != nil {
		return nil, nil, err
	}
	for _, name := range []string{"platform", "payments"} {
		if _, ok := teams[name]; ok {
			continue
		}
		created, err := api.CreateTeamWithResponse(ctx, nil, client.TeamInput{Name: name})
		if err := expect(created, err, http.StatusCreated, "create team "+name); err != nil {
			return nil, nil, err
		}
		teams[name] = created.JSON201.Id
	}

	users, err = byName(func(cursor *string) ([]client.User, string, error) {
		page, err := api.ListUsersWithResponse(ctx, &client.ListUsersParams{Cursor: cursor})
		if err := expect(page, err, http.StatusOK, "list users"); err != nil {
			return nil, "", err
		}
		return page.JSON200.Items, page.JSON200.NextCursor, nil
	}, func(user client.User) (string, string) { return user.Email, user.Id })
	if err != nil {
		return nil, nil, err
	}
	for _, user := range seededUsers {
		if _, ok := users[user.email]; !ok {
			created, err := api.CreateUserWithResponse(ctx, nil, client.UserInput{Name: user.name, Email: user.email, TimeZone: &user.timeZone})
			if err := expect(created, err, http.StatusCreated, "create user "+user.email); err != nil {
				return nil, nil, err
			}
			users[user.email] = created.JSON201.Id
			method, err := api.CreateContactMethodWithResponse(ctx, created.JSON201.Id, nil, client.ContactMethodInput{Type: client.Email, Address: user.email})
			if err := expect(method, err, http.StatusCreated, "create contact method for "+user.email); err != nil {
				return nil, nil, err
			}
		}
		for team, role := range user.teams {
			member, err := api.SetTeamMemberWithResponse(ctx, teams[team], users[user.email], client.TeamMemberInput{Role: role})
			if err := expect(member, err, http.StatusOK, fmt.Sprintf("add %s to %s", user.email, team)); err != nil {
				return nil, nil, err
			}
		}
	}
	return teams, users, nil
}

func seedWebhooks(ctx context.Context, api *client.ClientWithResponses, users map[string]string, receiver string) error {
	for _, user := range seededUsers {
		id := users[user.email]
		methods, err := api.ListContactMethodsWithResponse(ctx, id)
		if err := expect(methods, err, http.StatusOK, "list contact methods of "+user.email); err != nil {
			return err
		}
		if slices.ContainsFunc(methods.JSON200.Items, func(method client.ContactMethod) bool { return method.Type == client.Webhook }) {
			continue
		}
		local, _, _ := strings.Cut(user.email, "@")
		created, err := api.CreateContactMethodWithResponse(ctx, id, nil, client.ContactMethodInput{Type: client.Webhook, Address: receiver + "/" + local})
		if err := expect(created, err, http.StatusCreated, "create webhook for "+user.email); err != nil {
			return err
		}
	}
	return nil
}
