package murphy

import (
	"errors"
	"fmt"
	"time"
)

const minTurnLength = time.Minute

func (s *Schedule) Validate() error {
	var errs []error
	errs = append(errs, requireName("name", s.GetName()))
	if err := validTimeZone(s.GetTimeZone()); err != nil {
		errs = append(errs, err)
	}
	for i, team := range s.GetTeamIds() {
		errs = append(errs, requireID(fmt.Sprintf("team_ids[%d]", i), team))
	}
	if len(s.GetLayers()) == 0 {
		errs = append(errs, invalid("layers", "must have at least one layer"))
	}
	for i, layer := range s.GetLayers() {
		field := fmt.Sprintf("layers[%d]", i)
		if len(layer.GetParticipants()) == 0 {
			errs = append(errs, invalid(field+".participants", "must have at least one user"))
		}
		for j, participant := range layer.GetParticipants() {
			errs = append(errs, requireID(fmt.Sprintf("%s.participants[%d]", field, j), participant))
		}
		if layer.GetTurnLength().AsDuration() < minTurnLength {
			errs = append(errs, invalid(field+".turn_length", "must be at least a minute"))
		}
		if layer.GetStartsAt() == nil {
			errs = append(errs, invalid(field+".starts_at", "is required"))
		}
	}
	return errors.Join(errs...)
}

func (o *Override) Validate() error {
	errs := []error{requireID("schedule_id", o.GetScheduleId()), requireID("user_id", o.GetUserId())}
	if o.GetStartsAt() == nil || o.GetEndsAt() == nil || !o.GetEndsAt().AsTime().After(o.GetStartsAt().AsTime()) {
		errs = append(errs, invalid("ends_at", "must be after starts_at"))
	}
	return errors.Join(errs...)
}
