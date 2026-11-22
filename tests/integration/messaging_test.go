//go:build integration

package integration

import (
	"testing"

	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/tests/testenv"
)

func TestNATSBroker(t *testing.T) {
	brokertest.Run(t, testenv.NATS())
}

func TestSQSBroker(t *testing.T) {
	brokertest.Run(t, testenv.SQS(t))
}
