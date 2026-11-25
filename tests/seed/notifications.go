package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/client"
)

func seedEmail(ctx context.Context, api *client.ClientWithResponses, server string) error {
	current, err := api.GetEmailProviderWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("get the email provider: %w", err)
	}
	if current.StatusCode() == http.StatusOK {
		return nil
	}
	set, err := api.SetEmailProviderWithResponse(ctx, client.EmailProviderInput{
		Type: client.Smtp, From: "Murphy <murphy@murphy.test>", Host: server, Port: 1025,
		Username: new("murphy"), Secret: new("env://MURPHY_SECRET_SMTP_PASSWORD"),
	})
	return expect(set, err, http.StatusOK, "set the email provider")
}
