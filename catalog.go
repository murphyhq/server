package murphy

import "errors"

func (s *Service) Validate() error {
	var errs []error
	errs = append(errs, requireName("name", s.GetName()), requireID("team_id", s.GetTeamId()), requireID("escalation_policy_id", s.GetEscalationPolicyId()))
	if !s.GetUrgency().valid() {
		errs = append(errs, invalid("urgency", "must be high or low"))
	}
	if !s.GetStatus().valid() {
		errs = append(errs, invalid("status", "must be active or disabled"))
	}
	return errors.Join(errs...)
}

func (i *Integration) Validate() error {
	var errs []error
	errs = append(errs, requireID("service_id", i.GetServiceId()), requireName("name", i.GetName()))
	if !i.GetType().valid() {
		errs = append(errs, invalid("type", "must be generic, alertmanager, grafana or datadog"))
	}
	if !i.GetStatus().valid() {
		errs = append(errs, invalid("status", "must be active or disabled"))
	}
	return errors.Join(errs...)
}

func (u Urgency) valid() bool {
	return u == Urgency_URGENCY_HIGH || u == Urgency_URGENCY_LOW
}

func (s ServiceStatus) valid() bool {
	return s == ServiceStatus_SERVICE_STATUS_ACTIVE || s == ServiceStatus_SERVICE_STATUS_DISABLED
}

func (t IntegrationType) valid() bool {
	_, ok := IntegrationType_name[int32(t)]
	return ok && t != IntegrationType_INTEGRATION_TYPE_UNSPECIFIED
}

func (s IntegrationStatus) valid() bool {
	return s == IntegrationStatus_INTEGRATION_STATUS_ACTIVE || s == IntegrationStatus_INTEGRATION_STATUS_DISABLED
}

func (s ServiceStatus) Text() string {
	return enumText(s, "SERVICE_STATUS_")
}

func ServiceStatusFromText(text string) ServiceStatus {
	return enumFromText[ServiceStatus](ServiceStatus_value, "SERVICE_STATUS_", text)
}

func (t IntegrationType) Text() string {
	return enumText(t, "INTEGRATION_TYPE_")
}

func IntegrationTypeFromText(text string) IntegrationType {
	return enumFromText[IntegrationType](IntegrationType_value, "INTEGRATION_TYPE_", text)
}

func (s IntegrationStatus) Text() string {
	return enumText(s, "INTEGRATION_STATUS_")
}

func IntegrationStatusFromText(text string) IntegrationStatus {
	return enumFromText[IntegrationStatus](IntegrationStatus_value, "INTEGRATION_STATUS_", text)
}
