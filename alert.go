package murphy

import "errors"

func (a *Alert) Validate() error {
	errs := []error{requireName("dedup_key", a.GetDedupKey())}
	if a.GetSummary() == "" {
		errs = append(errs, invalid("summary", "is required"))
	}
	if !a.GetStatus().valid() {
		errs = append(errs, invalid("status", "must be firing or resolved"))
	}
	return errors.Join(errs...)
}

func (s AlertStatus) valid() bool {
	return s == AlertStatus_ALERT_STATUS_FIRING || s == AlertStatus_ALERT_STATUS_RESOLVED
}

func (s AlertStatus) Text() string {
	return enumText(s, "ALERT_STATUS_")
}

func AlertStatusFromText(text string) AlertStatus {
	return enumFromText[AlertStatus](AlertStatus_value, "ALERT_STATUS_", text)
}
