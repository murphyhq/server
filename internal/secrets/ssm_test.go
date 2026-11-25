//go:build integration

package secrets_test

import (
	"context"
	"os"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/aws"
	"github.com/murphyhq/server/internal/secrets"
	"github.com/murphyhq/server/internal/secrets/secretstest"
)

func parameterStore(t *testing.T) *ssm.Client {
	t.Helper()
	for key, value := range map[string]string{
		"AWS_ENDPOINT_URL":      "http://localhost:4566",
		"AWS_REGION":            "us-east-1",
		"AWS_ACCESS_KEY_ID":     "test",
		"AWS_SECRET_ACCESS_KEY": "test",
	} {
		t.Setenv(key, murphy.Or(os.Getenv(key), value))
	}
	cfg, err := aws.Config(t.Context())
	require.NoError(t, err)
	return ssm.NewFromConfig(cfg)
}

func put(t *testing.T, store *ssm.Client, name, value string) string {
	t.Helper()
	_, err := store.PutParameter(t.Context(), &ssm.PutParameterInput{Name: sdkaws.String(name), Value: sdkaws.String(value), Type: types.ParameterTypeSecureString, Overwrite: sdkaws.Bool(true)})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := store.DeleteParameter(context.Background(), &ssm.DeleteParameterInput{Name: sdkaws.String(name)})
		require.NoError(t, err)
	})
	return name
}

func TestParameterStore(t *testing.T) {
	store := parameterStore(t)
	run := murphy.NewID()
	secretstest.Run(t, secretstest.Environment{
		Source: secrets.NewSSM(store, []string{"/murphy-test/" + run + "/"}),
		Put: func(t *testing.T, value string) string {
			return put(t, store, "/murphy-test/"+run+"/smtp", value)
		},
		Outside: func(t *testing.T, value string) string {
			return put(t, store, "/other-test/"+run+"/database-url", value)
		},
		Missing: "/murphy-test/" + run + "/never-put",
	})
}

func TestAParameterNameCannotLeaveItsPath(t *testing.T) {
	store := parameterStore(t)
	run := murphy.NewID()
	put(t, store, "/other-test/"+run+"/database-url", "postgres://murphy:hunter2@db/murphy")
	parameters := secrets.NewSSM(store, []string{"/murphy-test/" + run})

	for _, name := range []string{
		"/murphy-test/" + run + "/../../other-test/" + run + "/database-url",
		"/murphy-test/" + run + "//smtp",
		"/murphy-test/" + run,
		"/murphy-test/" + run + "-other/smtp",
		"murphy-test/" + run + "/smtp",
		"arn:aws:ssm:us-east-1:123456789012:parameter/murphy-test/" + run + "/smtp",
	} {
		_, err := parameters.Read(t.Context(), name)

		assert.ErrorIs(t, err, secrets.ErrNotAllowed, name)
	}
}
