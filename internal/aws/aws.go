package aws

import (
	"context"
	"fmt"
	"net/http"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/murphyhq/server/internal/httpclient"
)

func Config(ctx context.Context, options ...func(*config.LoadOptions) error) (sdkaws.Config, error) {
	client := &http.Client{Transport: httpclient.Instrument(awshttp.NewBuildableClient().GetTransport())}
	cfg, err := config.LoadDefaultConfig(ctx, append([]func(*config.LoadOptions) error{config.WithHTTPClient(client)}, options...)...)
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	return cfg, nil
}
