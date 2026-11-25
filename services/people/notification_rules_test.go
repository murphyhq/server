package people_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

const (
	dina  = "0195f3a0-0000-7000-8000-0000000000d1"
	phone = "0195f3a0-0000-7000-8000-0000000000e1"
	inbox = "0195f3a0-0000-7000-8000-0000000000e2"
	hook  = "0195f3a0-0000-7000-8000-0000000000e3"
)

func methodsOf(methods ...*murphy.ContactMethod) []*murphy.ContactMethod {
	return methods
}

func method(id string, kind murphy.ContactMethodType) *murphy.ContactMethod {
	return &murphy.ContactMethod{Id: id, UserId: dina, Type: kind}
}

func step(contactMethod string, delay time.Duration) *murphy.NotificationStep {
	return &murphy.NotificationStep{ContactMethodId: contactMethod, Delay: durationpb.New(delay)}
}

type delays map[string]time.Duration

func delaysOf(rule *murphy.NotificationRule) delays {
	out := delays{}
	for _, s := range rule.GetSteps() {
		out[s.GetContactMethodId()] = s.GetDelay().AsDuration()
	}
	return out
}

func rulesService(t *testing.T, methods []*murphy.ContactMethod) (*people.Service, *MockRepository) {
	repo := NewMockRepository(t)
	repo.EXPECT().GetUser(mock.Anything, dina).Return(&murphy.User{Id: dina}, nil).Maybe()
	repo.EXPECT().ListContactMethods(mock.Anything, dina).Return(methods, nil).Maybe()
	return people.New(repo), repo
}

func TestByDefaultAPagePushesAtOnceAndReachesEverythingElseTwoMinutesLater(t *testing.T) {
	methods := methodsOf(method(phone, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH), method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL), method(hook, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK))

	rule := people.DefaultNotificationRule(dina, murphy.Urgency_URGENCY_HIGH, methods)

	assert.Equal(t, delays{phone: 0, inbox: 2 * time.Minute, hook: 2 * time.Minute}, delaysOf(rule))
	assert.False(t, rule.GetCustom())
}

func TestWithoutAPhoneTheDefaultReachesEveryContactMethodAtOnce(t *testing.T) {
	methods := methodsOf(method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL), method(hook, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK))

	rule := people.DefaultNotificationRule(dina, murphy.Urgency_URGENCY_HIGH, methods)

	assert.Equal(t, delays{inbox: 0, hook: 0}, delaysOf(rule), "nobody waits two minutes for their first notification")
}

func TestADisabledPhoneDoesntDelayTheRestOfTheDefault(t *testing.T) {
	gone := method(phone, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH)
	gone.DisabledAt = murphy.Now()

	rule := people.DefaultNotificationRule(dina, murphy.Urgency_URGENCY_HIGH, methodsOf(gone, method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)))

	assert.Equal(t, delays{inbox: 0}, delaysOf(rule))
}

func TestStepsAreSavedInTheOrderTheyFire(t *testing.T) {
	service, repo := rulesService(t, methodsOf(method(phone, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH), method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)))
	var saved *murphy.NotificationRule
	repo.EXPECT().ReplaceNotificationRule(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, rule *murphy.NotificationRule, _ time.Time) error {
		saved = rule
		return nil
	}).Once()

	_, err := service.SetNotificationRule(t.Context(), &murphy.NotificationRule{
		UserId: dina, Urgency: murphy.Urgency_URGENCY_HIGH, Steps: []*murphy.NotificationStep{step(inbox, 5*time.Minute), step(phone, 0)},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{phone, inbox}, []string{saved.GetSteps()[0].GetContactMethodId(), saved.GetSteps()[1].GetContactMethodId()})
	assert.True(t, saved.GetCustom())
}

func TestRulesThatWouldLeaveAPageWaitingAreRefused(t *testing.T) {
	service, _ := rulesService(t, methodsOf(method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)))

	_, err := service.SetNotificationRule(t.Context(), &murphy.NotificationRule{
		UserId: dina, Urgency: murphy.Urgency_URGENCY_HIGH, Steps: []*murphy.NotificationStep{step(inbox, 2*time.Minute)},
	})

	var field *murphy.FieldError
	require.ErrorAs(t, err, &field)
	assert.Equal(t, "steps", field.Field)
}

func TestRulesUseOnlyYourOwnWorkingContactMethods(t *testing.T) {
	gone := method(phone, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH)
	gone.DisabledAt, gone.DisabledReason = murphy.Now(), "the phone is no longer registered for pushes"
	service, _ := rulesService(t, methodsOf(gone, method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)))

	_, err := service.SetNotificationRule(t.Context(), &murphy.NotificationRule{
		UserId: dina, Urgency: murphy.Urgency_URGENCY_HIGH, Steps: []*murphy.NotificationStep{step(inbox, 0), step(gone.GetId(), 0), step(murphy.NewID(), time.Minute)},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "steps[1].contact_method is disabled: the phone is no longer registered for pushes")
	assert.Contains(t, err.Error(), "steps[2].contact_method must be one of your contact methods")
}

func TestEveryonePagedIsReachedTheirOwnWayOrByTheDefault(t *testing.T) {
	repo := NewMockRepository(t)
	ana := "0195f3a0-0000-7000-8000-0000000000a1"
	anaInbox := &murphy.ContactMethod{Id: murphy.NewID(), UserId: ana, Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL}
	dinaPhone, dinaInbox := method(phone, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH), method(inbox, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL)
	repo.EXPECT().ListContactMethodsOfUsers(mock.Anything, []string{dina, ana}).Return([]*murphy.ContactMethod{dinaPhone, dinaInbox, anaInbox}, nil).Once()
	repo.EXPECT().ListNotificationRulesOfUsers(mock.Anything, []string{dina, ana}, murphy.Urgency_URGENCY_HIGH).Return([]*murphy.NotificationRule{{
		UserId: dina, Urgency: murphy.Urgency_URGENCY_HIGH, Custom: true,
		Steps: []*murphy.NotificationStep{step(inbox, 0), step(hook, time.Minute), step(phone, 5*time.Minute)},
	}}, nil).Once()

	rules, err := people.New(repo).NotificationRulesOfUsers(t.Context(), []string{dina, ana}, murphy.Urgency_URGENCY_HIGH)

	require.NoError(t, err)
	assert.Equal(t, delays{inbox: 0, phone: 5 * time.Minute}, delaysOf(rules[dina]), "a step whose contact method no longer works is skipped")
	assert.Equal(t, dinaInbox, rules[dina].GetSteps()[0].GetContactMethod(), "steps carry their contact method")
	assert.Equal(t, delays{anaInbox.GetId(): 0}, delaysOf(rules[ana]), "the default, for someone without rules")
}
