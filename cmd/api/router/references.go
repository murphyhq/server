package router

import (
	"context"
	"maps"
	"slices"

	"github.com/murphyhq/server"
)

func ref(id string) Reference {
	return Reference{Id: id}
}

func ids(references []Reference) []string {
	out := make([]string, len(references))
	for i, reference := range references {
		out[i] = reference.Id
	}
	return out
}

type names struct {
	users, teams, schedules, services, policies map[string]*string
}

func newNames() *names {
	return &names{users: map[string]*string{}, teams: map[string]*string{}, schedules: map[string]*string{}, services: map[string]*string{}, policies: map[string]*string{}}
}

func (n *names) user(id string) Reference     { return named(n.users, id) }
func (n *names) team(id string) Reference     { return named(n.teams, id) }
func (n *names) schedule(id string) Reference { return named(n.schedules, id) }
func (n *names) service(id string) Reference  { return named(n.services, id) }
func (n *names) policy(id string) Reference   { return named(n.policies, id) }

func (n *names) target(target *murphy.EscalationTarget) EscalationTarget {
	reference := n.user(target.GetId())
	if target.GetType() == murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE {
		reference = n.schedule(target.GetId())
	}
	return EscalationTarget{Type: EscalationTargetType(target.GetType().Text()), Id: reference.Id, Name: reference.Name}
}

func each(ids []string, reference func(id string) Reference) []Reference {
	out := make([]Reference, len(ids))
	for i, id := range ids {
		out[i] = reference(id)
	}
	return out
}

func named(slots map[string]*string, id string) Reference {
	if _, ok := slots[id]; !ok {
		slots[id] = new(string)
	}
	return Reference{Id: id, Name: slots[id]}
}

func (s *Server) name(ctx context.Context, n *names) error {
	lookups := []struct {
		slots map[string]*string
		find  func(ctx context.Context, ids []string) (map[string]string, error)
	}{
		{n.users, s.people.UserNames},
		{n.teams, s.people.TeamNames},
		{n.schedules, s.oncall.ScheduleNames},
		{n.services, s.catalog.ServiceNames},
		{n.policies, s.escalation.PolicyNames},
	}
	for _, lookup := range lookups {
		found, err := lookup.find(ctx, slices.Collect(maps.Keys(lookup.slots)))
		if err != nil {
			return err
		}
		for id, name := range lookup.slots {
			*name = found[id]
		}
	}
	return nil
}
