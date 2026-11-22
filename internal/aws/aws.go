package aws

import (
	"context"
	"fmt"
	"net/http"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func Config(ctx context.Context) (sdkaws.Config, error) {
	client := &http.Client{Transport: otelhttp.NewTransport(awshttp.NewBuildableClient().GetTransport())}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithHTTPClient(client))
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	return cfg, nil
}
