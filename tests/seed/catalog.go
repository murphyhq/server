package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/client"
)

const (
	platformTeam   = "0195f3a0-0000-7000-8000-000000000001"
	platformPolicy = "0195f3a0-0000-7000-8000-000000000002"
	paymentsTeam   = "0195f3a0-0000-7000-8000-000000000003"
	paymentsPolicy = "0195f3a0-0000-7000-8000-000000000004"
)

type seededService struct {
	input        client.ServiceInput
	integrations []client.IntegrationCreate
}

func seedCatalog(ctx context.Context, api *client.ClientWithResponses) error {
	low := client.Low
	services := []seededService{
		{
			input: client.ServiceInput{Name: "checkout-api", Description: new("Takes orders and payments from the storefront."), TeamId: paymentsTeam, EscalationPolicyId: paymentsPolicy},
			integrations: []client.IntegrationCreate{
				{Name: "Alertmanager", Type: client.Alertmanager},
				{Name: "Grafana", Type: client.Grafana},
			},
		},
		{
			input:        client.ServiceInput{Name: "payments-api", Description: new("Charges cards through the payment provider."), TeamId: paymentsTeam, EscalationPolicyId: paymentsPolicy},
			integrations: []client.IntegrationCreate{{Name: "Datadog", Type: client.Datadog}},
		},
		{
			input:        client.ServiceInput{Name: "internal-wiki", Description: new("Company documentation."), TeamId: platformTeam, EscalationPolicyId: platformPolicy, Urgency: &low},
			integrations: []client.IntegrationCreate{{Name: "Uptime checks", Type: client.Generic}},
		},
	}

	existing, err := serviceIDs(ctx, api)
	if err != nil {
		return err
	}
	for _, service := range services {
		id, ok := existing[service.input.Name]
		if !ok {
			created, err := api.CreateServiceWithResponse(ctx, nil, service.input)
			if err != nil {
				return fmt.Errorf("create service %s: %w", service.input.Name, err)
			}
			if created.StatusCode() != http.StatusCreated {
				return fmt.Errorf("create service %s: %s", service.input.Name, created.Status())
			}
			id = created.JSON201.Id
		}
		if err := seedIntegrations(ctx, api, id, service.integrations); err != nil {
			return err
		}
	}
	return nil
}

func seedIntegrations(ctx context.Context, api *client.ClientWithResponses, serviceID string, integrations []client.IntegrationCreate) error {
	existing := map[string]bool{}
	params := &client.ListIntegrationsParams{}
	for {
		page, err := api.ListIntegrationsWithResponse(ctx, serviceID, params)
		if err != nil {
			return fmt.Errorf("list integrations: %w", err)
		}
		if page.StatusCode() != http.StatusOK {
			return fmt.Errorf("list integrations: %s", page.Status())
		}
		for _, integration := range page.JSON200.Items {
			existing[integration.Name] = true
		}
		if page.JSON200.NextCursor == "" {
			break
		}
		params.Cursor = &page.JSON200.NextCursor
	}

	for _, integration := range integrations {
		if existing[integration.Name] {
			continue
		}
		created, err := api.CreateIntegrationWithResponse(ctx, serviceID, nil, integration)
		if err != nil {
			return fmt.Errorf("create integration %s: %w", integration.Name, err)
		}
		if created.StatusCode() != http.StatusCreated {
			return fmt.Errorf("create integration %s: %s", integration.Name, created.Status())
		}
	}
	return nil
}

func serviceIDs(ctx context.Context, api *client.ClientWithResponses) (map[string]string, error) {
	ids := map[string]string{}
	params := &client.ListServicesParams{}
	for {
		page, err := api.ListServicesWithResponse(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		if page.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("list services: %s", page.Status())
		}
		for _, service := range page.JSON200.Items {
			ids[service.Name] = service.Id
		}
		if page.JSON200.NextCursor == "" {
			return ids, nil
		}
		params.Cursor = &page.JSON200.NextCursor
	}
}
