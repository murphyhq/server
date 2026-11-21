//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/murphyhq/server/tests/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Main(m))
}
