package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/client"
)

type seededService struct {
	input        client.ServiceInput
	integrations []client.IntegrationCreate
}

func seedCatalog(ctx context.Context, api *client.ClientWithResponses, policies map[string]string) (keys map[string]string, err error) {
	low := client.Low
	services := []seededService{
		{
			input: client.ServiceInput{Name: "checkout-api", Description: new("Takes orders and payments from the storefront."), EscalationPolicy: client.Reference{Id: policies["payments"]}},
			integrations: []client.IntegrationCreate{
				{Name: "Alertmanager", Type: client.Alertmanager},
				{Name: "Grafana", Type: client.Grafana},
			},
		},
		{
			input:        client.ServiceInput{Name: "payments-api", Description: new("Charges cards through the payment provider."), EscalationPolicy: client.Reference{Id: policies["payments"]}},
			integrations: []client.IntegrationCreate{{Name: "Datadog", Type: client.Datadog}},
		},
		{
			input:        client.ServiceInput{Name: "internal-wiki", Description: new("Company documentation."), EscalationPolicy: client.Reference{Id: policies["platform"]}, Urgency: &low},
			integrations: []client.IntegrationCreate{{Name: "Uptime checks", Type: client.Generic}},
		},
	}
	monitored := func(name, policy string, urgency client.Urgency, description string) seededService {
		return seededService{
			input:        client.ServiceInput{Name: name, Description: &description, EscalationPolicy: client.Reference{Id: policies[policy]}, Urgency: &urgency},
			integrations: []client.IntegrationCreate{{Name: "Monitoring", Type: client.Generic}},
		}
	}
	services = append(services,
		monitored("ios-app", "mobile", client.High, "The iPhone app's backend for feeds and sign-in."),
		monitored("android-app", "mobile", client.High, "The Android app's backend."),
		monitored("storefront", "web", client.High, "The public shop."),
		monitored("admin-console", "web", client.Low, "Back-office tools for support staff."),
		monitored("cdn", "web", client.High, "Static assets and images."),
		monitored("warehouse-etl", "data", client.Low, "Nightly loads into the warehouse."),
		monitored("analytics-api", "data", client.Low, "Dashboards and reports."),
		monitored("kubernetes", "sre", client.High, "The production clusters."),
		monitored("postgres-cluster", "sre", client.High, "The main databases."),
		monitored("dns", "sre", client.High, "Public and internal DNS."),
		monitored("auth-gateway", "security", client.High, "Single sign-on for every app."),
		monitored("waf", "security", client.High, "The web application firewall."),
		monitored("fraud-checks", "payments critical", client.High, "Scores every payment before it's charged."),
	)

	existing, err := serviceIDs(ctx, api)
	if err != nil {
		return nil, err
	}
	keys = map[string]string{}
	for _, service := range services {
		id, ok := existing[service.input.Name]
		if !ok {
			created, err := api.CreateServiceWithResponse(ctx, nil, service.input)
			if err != nil {
				return nil, fmt.Errorf("create service %s: %w", service.input.Name, err)
			}
			if created.StatusCode() != http.StatusCreated {
				return nil, fmt.Errorf("create service %s: %s", service.input.Name, created.Status())
			}
			id = created.JSON201.Id
		}
		created, err := seedIntegrations(ctx, api, id, service.integrations)
		if err != nil {
			return nil, err
		}
		if key, ok := created["Monitoring"]; ok {
			keys[service.input.Name] = key
		}
	}
	return keys, nil
}

func seedIntegrations(ctx context.Context, api *client.ClientWithResponses, serviceID string, integrations []client.IntegrationCreate) (keys map[string]string, err error) {
	existing := map[string]bool{}
	params := &client.ListIntegrationsParams{}
	for {
		page, err := api.ListIntegrationsWithResponse(ctx, serviceID, params)
		if err != nil {
			return nil, fmt.Errorf("list integrations: %w", err)
		}
		if page.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("list integrations: %s", page.Status())
		}
		for _, integration := range page.JSON200.Items {
			existing[integration.Name] = true
		}
		if page.JSON200.NextCursor == "" {
			break
		}
		params.Cursor = &page.JSON200.NextCursor
	}

	keys = map[string]string{}
	for _, integration := range integrations {
		if existing[integration.Name] {
			continue
		}
		created, err := api.CreateIntegrationWithResponse(ctx, serviceID, nil, integration)
		if err != nil {
			return nil, fmt.Errorf("create integration %s: %w", integration.Name, err)
		}
		if created.StatusCode() != http.StatusCreated {
			return nil, fmt.Errorf("create integration %s: %s", integration.Name, created.Status())
		}
		keys[integration.Name] = created.JSON201.Key
	}
	return keys, nil
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
