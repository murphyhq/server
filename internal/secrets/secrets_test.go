package secrets_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/secrets"
	"github.com/murphyhq/server/internal/secrets/secretstest"
)

func TestEnvironmentVariables(t *testing.T) {
	secretstest.Run(t, secretstest.Environment{
		Source: secrets.NewEnv("MURPHY_SECRET_"),
		Put: func(t *testing.T, value string) string {
			t.Setenv("MURPHY_SECRET_SMTP", value)
			return "MURPHY_SECRET_SMTP"
		},
		Outside: func(t *testing.T, value string) string {
			t.Setenv("MURPHY_NOTIFICATIONS_DATABASE_URL", value)
			return "MURPHY_NOTIFICATIONS_DATABASE_URL"
		},
		Missing: "MURPHY_SECRET_NEVER_SET",
	})
}

func TestFiles(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	secretstest.Run(t, secretstest.Environment{
		Source: secrets.NewFile(directory),
		Put: func(t *testing.T, value string) string {
			return write(t, directory, "smtp", value)
		},
		Outside: func(t *testing.T, value string) string {
			return write(t, outside, "database-url", value)
		},
		Missing: filepath.Join(directory, "never-written"),
	})
}

func write(t *testing.T, directory, name, value string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, []byte(value), 0o600))
	return path
}

func TestAFileDropsOneTrailingNewline(t *testing.T) {
	directory := t.TempDir()
	files := secrets.NewFile(directory)

	unix, err := files.Read(t.Context(), write(t, directory, "unix", "password\n"))
	require.NoError(t, err)
	windows, err := files.Read(t.Context(), write(t, directory, "windows", "password\r\n"))
	require.NoError(t, err)
	kept, err := files.Read(t.Context(), write(t, directory, "kept", "password\n\n"))
	require.NoError(t, err)

	assert.Equal(t, "password", unix)
	assert.Equal(t, "password", windows)
	assert.Equal(t, "password\n", kept)
}

func TestAFileCannotLeaveItsDirectory(t *testing.T) {
	directory, outside := t.TempDir(), t.TempDir()
	files := secrets.NewFile(directory)
	target := write(t, outside, "database-url", "postgres://murphy:hunter2@db/murphy")
	require.NoError(t, os.Symlink(target, filepath.Join(directory, "innocent")))
	require.NoError(t, os.Mkdir(filepath.Join(directory, "nested"), 0o700))
	require.NoError(t, syscall.Mkfifo(filepath.Join(directory, "pipe"), 0o600))

	for _, name := range []string{
		filepath.Join(directory, "innocent"),
		filepath.Join(directory, "..", filepath.Base(outside), "database-url"),
		filepath.Join(directory, "nested"),
		directory,
		filepath.Join(directory, "pipe"),
		"relative/path",
		"/etc/passwd",
	} {
		_, err := files.Read(t.Context(), name)

		assert.ErrorIs(t, err, secrets.ErrNotAllowed, name)
	}
}

func TestAFileTooLargeIsRefused(t *testing.T) {
	directory := t.TempDir()

	_, err := secrets.NewFile(directory).Read(t.Context(), write(t, directory, "huge", strings.Repeat("x", 65<<10)))

	assert.ErrorIs(t, err, secrets.ErrNotAllowed)
}

func TestAVariableNameMustBeExact(t *testing.T) {
	env := secrets.NewEnv("MURPHY_SECRET_")

	for _, name := range []string{"MURPHY_SECRET_A B", "MURPHY_SECRET_$(id)", "MURPHY_SECRET_${HOME}", "murphy_secret_lower"} {
		_, err := env.Read(t.Context(), name)

		assert.ErrorIs(t, err, secrets.ErrNotAllowed, name)
	}
}

func TestTheReaderSendsEachSchemeToItsSource(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("MURPHY_SECRET_SMTP", "from-env")
	path := write(t, directory, "smtp", "from-file")
	reader := secrets.NewReader(secrets.NewEnv("MURPHY_SECRET_"), secrets.NewFile(directory), nil)

	fromEnv, err := reader.Read(t.Context(), "env://MURPHY_SECRET_SMTP")
	require.NoError(t, err)
	fromFile, err := reader.Read(t.Context(), "file://"+path)
	require.NoError(t, err)
	_, unknown := reader.Read(t.Context(), "exec:///bin/sh")
	_, withoutSSM := reader.Read(t.Context(), "aws-ssm:///murphy/smtp")

	assert.Equal(t, "from-env", fromEnv)
	assert.Equal(t, "from-file", fromFile)
	assert.ErrorIs(t, unknown, secrets.ErrNotAllowed)
	assert.ErrorIs(t, withoutSSM, secrets.ErrNotAllowed)
}
