package murphy

import (
	"errors"
	"fmt"
)

const maxRepeatCount = 9

func (p *EscalationPolicy) Validate() error {
	var errs []error
	errs = append(errs, requireName("name", p.GetName()), requireID("team_id", p.GetTeamId()))
	if p.GetRepeatCount() < 0 || p.GetRepeatCount() > maxRepeatCount {
		errs = append(errs, invalid("repeat_count", "must be between 0 and 9"))
	}
	if len(p.GetLevels()) == 0 {
		errs = append(errs, invalid("levels", "must have at least one level"))
	}
	for i, level := range p.GetLevels() {
		field := fmt.Sprintf("levels[%d]", i)
		if len(level.GetTargets()) == 0 {
			errs = append(errs, invalid(field+".targets", "must have at least one target"))
		}
		for j, target := range level.GetTargets() {
			targetField := fmt.Sprintf("%s.targets[%d]", field, j)
			if !target.GetType().valid() {
				errs = append(errs, invalid(targetField+".type", "must be user or schedule"))
			}
			errs = append(errs, requireID(targetField+".id", target.GetId()))
		}
	}
	return errors.Join(errs...)
}

func (t EscalationTargetType) valid() bool {
	return t == EscalationTargetType_ESCALATION_TARGET_TYPE_USER || t == EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE
}

func (t EscalationTargetType) Text() string {
	return enumText(t, "ESCALATION_TARGET_TYPE_")
}

func EscalationTargetTypeFromText(text string) EscalationTargetType {
	return enumFromText[EscalationTargetType](EscalationTargetType_value, "ESCALATION_TARGET_TYPE_", text)
}
