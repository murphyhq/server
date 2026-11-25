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
	{"Ana Costa", "ana@murphy.test", "Europe/Malta", map[string]client.TeamRole{"platform": client.Manager, "payments": client.Observer}},
	{"Bo Lindqvist", "bo@murphy.test", "Europe/Stockholm", map[string]client.TeamRole{"platform": client.Responder}},
	{"Cy Okafor", "cy@murphy.test", "Africa/Lagos", map[string]client.TeamRole{"payments": client.Responder}},
	{"Dee Park", "dee@murphy.test", "America/New_York", map[string]client.TeamRole{"payments": client.Manager}},
	{"Eli Navarro", "eli@murphy.test", "Europe/Lisbon", map[string]client.TeamRole{"mobile": client.Manager}},
	{"Fay Wong", "fay@murphy.test", "Asia/Singapore", map[string]client.TeamRole{"mobile": client.Responder}},
	{"Gus Müller", "gus@murphy.test", "Europe/Berlin", map[string]client.TeamRole{"mobile": client.Responder}},
	{"Hana Sato", "hana@murphy.test", "Asia/Tokyo", map[string]client.TeamRole{"mobile": client.Responder}},
	{"Ivo Petrov", "ivo@murphy.test", "Europe/Sofia", map[string]client.TeamRole{"mobile": client.Responder}},
	{"Jade Okoro", "jade@murphy.test", "Africa/Lagos", map[string]client.TeamRole{"mobile": client.Responder, "web": client.Observer}},
	{"Kai Lindgren", "kai@murphy.test", "Europe/Stockholm", map[string]client.TeamRole{"mobile": client.Observer}},
	{"Lena Fischer", "lena@murphy.test", "Europe/Berlin", map[string]client.TeamRole{"web": client.Manager}},
	{"Marco Rossi", "marco@murphy.test", "Europe/Rome", map[string]client.TeamRole{"web": client.Responder}},
	{"Nia Brooks", "nia@murphy.test", "America/New_York", map[string]client.TeamRole{"web": client.Responder}},
	{"Omar Haddad", "omar@murphy.test", "Asia/Dubai", map[string]client.TeamRole{"web": client.Responder}},
	{"Priya Raman", "priya@murphy.test", "Asia/Kolkata", map[string]client.TeamRole{"web": client.Responder}},
	{"Quinn Avery", "quinn@murphy.test", "America/Los_Angeles", map[string]client.TeamRole{"web": client.Responder}},
	{"Rafa Souza", "rafa@murphy.test", "America/Sao_Paulo", map[string]client.TeamRole{"web": client.Responder}},
	{"Sara Kim", "sara@murphy.test", "Asia/Seoul", map[string]client.TeamRole{"data": client.Manager}},
	{"Tom Becker", "tom@murphy.test", "Europe/Berlin", map[string]client.TeamRole{"data": client.Responder}},
	{"Uma Patel", "uma@murphy.test", "Asia/Kolkata", map[string]client.TeamRole{"data": client.Responder}},
	{"Vic Moreau", "vic@murphy.test", "Europe/Paris", map[string]client.TeamRole{"data": client.Responder}},
	{"Wen Li", "wen@murphy.test", "Asia/Shanghai", map[string]client.TeamRole{"data": client.Responder}},
	{"Xavi Ortega", "xavi@murphy.test", "Europe/Madrid", map[string]client.TeamRole{"data": client.Observer, "payments": client.Observer}},
	{"Yara Haddad", "yara@murphy.test", "Asia/Beirut", map[string]client.TeamRole{"sre": client.Manager}},
	{"Zoe Clarke", "zoe@murphy.test", "Europe/London", map[string]client.TeamRole{"sre": client.Responder, "platform": client.Responder}},
	{"Abe Turner", "abe@murphy.test", "America/Chicago", map[string]client.TeamRole{"sre": client.Responder}},
	{"Bea Silva", "bea@murphy.test", "America/Sao_Paulo", map[string]client.TeamRole{"sre": client.Responder}},
	{"Cal Nguyen", "cal@murphy.test", "Asia/Ho_Chi_Minh", map[string]client.TeamRole{"sre": client.Responder}},
	{"Dina Levi", "dina@murphy.test", "Asia/Jerusalem", map[string]client.TeamRole{"sre": client.Responder}},
	{"Emil Novak", "emil@murphy.test", "Europe/Prague", map[string]client.TeamRole{"sre": client.Responder}},
	{"Finn Walsh", "finn@murphy.test", "Europe/Dublin", map[string]client.TeamRole{"sre": client.Responder, "platform": client.Observer}},
	{"Gia Russo", "gia@murphy.test", "Europe/Rome", map[string]client.TeamRole{"security": client.Manager}},
	{"Hugo Martin", "hugo@murphy.test", "Europe/Paris", map[string]client.TeamRole{"security": client.Responder}},
	{"Iris Bauer", "iris@murphy.test", "Europe/Vienna", map[string]client.TeamRole{"security": client.Responder}},
	{"Jon Mendes", "jon@murphy.test", "Europe/Lisbon", map[string]client.TeamRole{"security": client.Responder}},
	{"Kira Ivanova", "kira@murphy.test", "Europe/Warsaw", map[string]client.TeamRole{"security": client.Responder}},
	{"Leo Ward", "leo@murphy.test", "Australia/Sydney", map[string]client.TeamRole{"security": client.Observer, "sre": client.Observer}},
	{"Mia Costa", "mia@murphy.test", "Europe/Lisbon", map[string]client.TeamRole{"platform": client.Responder}},
	{"Noah Fox", "noah@murphy.test", "America/Toronto", map[string]client.TeamRole{"platform": client.Responder}},
	{"Olga Smirnova", "olga@murphy.test", "Europe/Helsinki", map[string]client.TeamRole{"platform": client.Responder}},
	{"Pete Holm", "pete@murphy.test", "Europe/Oslo", map[string]client.TeamRole{"platform": client.Responder}},
	{"Rosa Diaz", "rosa@murphy.test", "America/Mexico_City", map[string]client.TeamRole{"payments": client.Responder}},
	{"Sam Reid", "sam@murphy.test", "Europe/London", map[string]client.TeamRole{"payments": client.Responder}},
	{"Tara Singh", "tara@murphy.test", "Asia/Kolkata", map[string]client.TeamRole{"payments": client.Responder}},
	{"Ugo Bianchi", "ugo@murphy.test", "Europe/Rome", map[string]client.TeamRole{"payments": client.Responder}},
	{"Vera Kral", "vera@murphy.test", "Europe/Prague", map[string]client.TeamRole{"payments": client.Responder, "web": client.Responder}},
	{"Will Grant", "will@murphy.test", "America/Denver", map[string]client.TeamRole{"data": client.Responder}},
	{"Xena Park", "xena@murphy.test", "Asia/Seoul", map[string]client.TeamRole{"mobile": client.Responder}},
	{"Yuri Volkov", "yuri@murphy.test", "Europe/Riga", map[string]client.TeamRole{"sre": client.Responder}},
}

var failingWebhooks = map[string]string{"dina": "/refuses", "yara": "/refuses", "noah": "/unavailable"}

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
	for _, name := range teamNames() {
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
		input := client.UserInput{Name: user.name, Email: user.email, TimeZone: &user.timeZone}
		if id, ok := users[user.email]; ok {
			updated, err := api.UpdateUserWithResponse(ctx, id, input)
			if err := expect(updated, err, http.StatusOK, "update user "+user.email); err != nil {
				return nil, nil, err
			}
		} else {
			created, err := api.CreateUserWithResponse(ctx, nil, input)
			if err := expect(created, err, http.StatusCreated, "create user "+user.email); err != nil {
				return nil, nil, err
			}
			users[user.email] = created.JSON201.Id
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

func teamNames() []string {
	var names []string
	for _, user := range seededUsers {
		for team := range user.teams {
			names = append(names, team)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
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
		created, err := api.CreateContactMethodWithResponse(ctx, id, nil, client.ContactMethodInput{Type: client.Webhook, Address: receiver + failingWebhooks[local] + "/" + local})
		if err := expect(created, err, http.StatusCreated, "create webhook for "+user.email); err != nil {
			return err
		}
	}
	return nil
}
