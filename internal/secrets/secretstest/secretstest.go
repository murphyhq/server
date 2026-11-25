package secretstest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/secrets"
)

type Environment struct {
	Source  secrets.Source
	Put     func(t *testing.T, value string) (name string)
	Outside func(t *testing.T, value string) (name string)
	Missing string
}

const secret = "s3cret-value"

func Run(t *testing.T, env Environment) {
	t.Run("reads a secret exactly", func(t *testing.T) {
		got, err := env.Source.Read(t.Context(), env.Put(t, "p@ss word=with;symbols"))

		require.NoError(t, err)
		assert.Equal(t, "p@ss word=with;symbols", got)
	})

	t.Run("says a missing secret is missing", func(t *testing.T) {
		_, err := env.Source.Read(t.Context(), env.Missing)

		assert.ErrorIs(t, err, secrets.ErrMissing)
	})

	t.Run("refuses a secret outside what it may read, without reading it", func(t *testing.T) {
		_, err := env.Source.Read(t.Context(), env.Outside(t, secret))

		require.ErrorIs(t, err, secrets.ErrNotAllowed)
		assert.NotContains(t, err.Error(), secret)
	})
}
