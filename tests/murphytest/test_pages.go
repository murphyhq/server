package murphytest

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
)

func (m *Member) WaitForTestPage(id string) client.TestPage {
	m.m.t.Helper()
	var last client.TestPage
	require.EventuallyWithT(m.m.t, func(c *assert.CollectT) {
		got, err := m.API.GetTestPageWithResponse(m.m.t.Context(), m.ID, id)
		if !assert.NoError(c, err) || !assert.NotNil(c, got.JSON200) {
			return
		}
		last = *got.JSON200
		for _, delivery := range last.Deliveries {
			assert.NotEqual(c, client.TestDeliveryStatusPending, delivery.Status, "every delivery has an outcome")
		}
	}, Settle, tick)
	return last
}

func DeliveriesByChannel(page client.TestPage) map[client.ContactMethodType]client.TestDeliveryStatus {
	statuses := map[client.ContactMethodType]client.TestDeliveryStatus{}
	for _, delivery := range page.Deliveries {
		statuses[delivery.Channel] = delivery.Status
	}
	return statuses
}

func Failures(page client.TestPage) []string {
	var failures []string
	for _, delivery := range page.Deliveries {
		if delivery.Status == client.TestDeliveryStatusFailed {
			failures = append(failures, string(delivery.Channel)+": "+*delivery.Failure)
		}
	}
	return failures
}
