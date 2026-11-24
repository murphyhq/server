package people_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

type contactMethods struct {
	service *people.Service
	stored  *murphy.ContactMethod
	updated *murphy.ContactMethod
}

func withDisabledPhone(t *testing.T) *contactMethods {
	t.Helper()
	repo := NewMockRepository(t)
	c := &contactMethods{service: people.New(repo), stored: &murphy.ContactMethod{
		Id: murphy.NewID(), UserId: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH, Address: "dead-token",
		PushPlatform: murphy.PushPlatform_PUSH_PLATFORM_IOS, CreatedAt: murphy.Now(), UpdatedAt: murphy.Now(),
		DisabledAt: timestamppb.New(time.Now().Add(-time.Hour)), DisabledReason: "the phone is no longer registered for pushes",
	}}
	repo.EXPECT().GetContactMethod(mock.Anything, c.stored.GetId()).Return(c.stored, nil).Maybe()
	repo.EXPECT().UpdateContactMethod(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, method *murphy.ContactMethod) error {
		c.updated = method
		return nil
	}).Maybe()
	return c
}

func TestANewAddressEnablesADisabledContactMethod(t *testing.T) {
	c := withDisabledPhone(t)

	_, err := c.service.UpdateContactMethod(t.Context(), &murphy.ContactMethod{Id: c.stored.GetId(), Address: "new-token"})

	require.NoError(t, err)
	assert.Nil(t, c.updated.GetDisabledAt())
	assert.Empty(t, c.updated.GetDisabledReason())
}

func TestTheSameAddressKeepsAContactMethodDisabled(t *testing.T) {
	c := withDisabledPhone(t)

	_, err := c.service.UpdateContactMethod(t.Context(), &murphy.ContactMethod{Id: c.stored.GetId(), Address: "dead-token"})

	require.NoError(t, err)
	assert.Equal(t, c.stored.GetDisabledAt(), c.updated.GetDisabledAt(), "the token is still dead")
	assert.Equal(t, c.stored.GetDisabledReason(), c.updated.GetDisabledReason())
}
