package aws

import (
	"context"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go/metrics"
	"github.com/aws/smithy-go/metrics/smithyotelmetrics"
	"github.com/aws/smithy-go/tracing"
	"github.com/aws/smithy-go/tracing/smithyoteltracing"
	"go.opentelemetry.io/otel"
)

func Config(ctx context.Context) (sdkaws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	return cfg, nil
}

func TracerProvider() tracing.TracerProvider {
	return smithyoteltracing.Adapt(otel.GetTracerProvider())
}

func MeterProvider() metrics.MeterProvider {
	return smithyotelmetrics.Adapt(otel.GetMeterProvider())
}
