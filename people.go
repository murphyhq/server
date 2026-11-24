package murphy

import (
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"time"
)

var phoneNumber = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

func (u *User) Validate() error {
	var errs []error
	errs = append(errs, requireName("name", u.GetName()))
	if _, err := mail.ParseAddress(u.GetEmail()); err != nil {
		errs = append(errs, invalid("email", "must be an email address"))
	}
	if err := validTimeZone(u.GetTimeZone()); err != nil {
		errs = append(errs, err)
	}
	if !u.GetStatus().valid() {
		errs = append(errs, invalid("status", "must be active or deactivated"))
	}
	return errors.Join(errs...)
}

func (c *ContactMethod) Validate() error {
	errs := []error{requireID("user_id", c.GetUserId())}
	address := c.GetAddress()
	switch c.GetType() {
	case ContactMethodType_CONTACT_METHOD_TYPE_EMAIL:
		if _, err := mail.ParseAddress(address); err != nil {
			errs = append(errs, invalid("address", "must be an email address"))
		}
	case ContactMethodType_CONTACT_METHOD_TYPE_SMS, ContactMethodType_CONTACT_METHOD_TYPE_VOICE:
		if !phoneNumber.MatchString(address) {
			errs = append(errs, invalid("address", "must be a phone number in E.164 format, as in +14155550100"))
		}
	case ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK:
		if u, err := url.Parse(address); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, invalid("address", "must be an http or https URL"))
		}
	case ContactMethodType_CONTACT_METHOD_TYPE_PUSH:
		if address == "" {
			errs = append(errs, invalid("address", "must be the device's relay handle"))
		}
	default:
		errs = append(errs, invalid("type", "must be email, sms, voice, webhook or push"))
	}
	return errors.Join(errs...)
}

func validTimeZone(zone string) error {
	if zone == "" {
		return invalid("time_zone", "is required")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return invalid("time_zone", "must be an IANA time zone, as in Europe/Malta")
	}
	return nil
}

func (s UserStatus) valid() bool {
	return s == UserStatus_USER_STATUS_ACTIVE || s == UserStatus_USER_STATUS_DEACTIVATED
}

func (s UserStatus) Text() string {
	return enumText(s, "USER_STATUS_")
}

func (r UserRole) Text() string {
	return enumText(r, "USER_ROLE_")
}

func UserRoleFromText(text string) UserRole {
	return enumFromText[UserRole](UserRole_value, "USER_ROLE_", text)
}

func UserStatusFromText(text string) UserStatus {
	return enumFromText[UserStatus](UserStatus_value, "USER_STATUS_", text)
}

func (t ContactMethodType) Text() string {
	return enumText(t, "CONTACT_METHOD_TYPE_")
}

func ContactMethodTypeFromText(text string) ContactMethodType {
	return enumFromText[ContactMethodType](ContactMethodType_value, "CONTACT_METHOD_TYPE_", text)
}

func (t *Team) Validate() error {
	return requireName("name", t.GetName())
}

func (m *TeamMembership) Validate() error {
	errs := []error{requireID("team_id", m.GetTeamId()), requireID("user_id", m.GetUserId())}
	if !m.GetRole().valid() {
		errs = append(errs, invalid("role", "must be manager, responder or observer"))
	}
	return errors.Join(errs...)
}

func (r TeamRole) valid() bool {
	return r == TeamRole_TEAM_ROLE_MANAGER || r == TeamRole_TEAM_ROLE_RESPONDER || r == TeamRole_TEAM_ROLE_OBSERVER
}

func (r TeamRole) Text() string {
	return enumText(r, "TEAM_ROLE_")
}

func TeamRoleFromText(text string) TeamRole {
	return enumFromText[TeamRole](TeamRole_value, "TEAM_ROLE_", text)
}

func (p OneTimeCodePurpose) Text() string {
	return enumText(p, "ONE_TIME_CODE_PURPOSE_")
}

func OneTimeCodePurposeFromText(text string) OneTimeCodePurpose {
	return enumFromText[OneTimeCodePurpose](OneTimeCodePurpose_value, "ONE_TIME_CODE_PURPOSE_", text)
}

func (c SessionClient) Text() string {
	return enumText(c, "SESSION_CLIENT_")
}

func SessionClientFromText(text string) SessionClient {
	return enumFromText[SessionClient](SessionClient_value, "SESSION_CLIENT_", text)
}
