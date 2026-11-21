package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const instrumentation = "github.com/murphyhq/server"

func Setup(ctx context.Context, component, level string, stdout io.Writer) (func(context.Context) error, error) {
	minimum, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.namespace", "murphy"),
			attribute.String("service.name", component),
			attribute.String("service.version", version()),
			attribute.String("service.instance.id", uuid.NewV4().String()),
		),
		resource.WithHost(),
		resource.WithContainer(),
		resource.WithOS(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("describe telemetry resource: %w", err)
	}

	client := exportClient()

	tracerOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if exporting("TRACES") {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithHTTPClient(client))
		if err != nil {
			return nil, fmt.Errorf("create trace exporter: %w", err)
		}
		tracerOptions = append(tracerOptions, sdktrace.WithBatcher(exporter))
	}

	meterOptions := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if exporting("METRICS") {
		exporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithHTTPClient(client))
		if err != nil {
			return nil, fmt.Errorf("create metric exporter: %w", err)
		}
		meterOptions = append(meterOptions, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	}

	loggerOptions := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	if exporting("LOGS") {
		exporter, err := otlploghttp.New(ctx, otlploghttp.WithHTTPClient(client))
		if err != nil {
			return nil, fmt.Errorf("create log exporter: %w", err)
		}
		loggerOptions = append(loggerOptions, sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
	}

	tracers := sdktrace.NewTracerProvider(tracerOptions...)
	meters := sdkmetric.NewMeterProvider(meterOptions...)
	loggers := sdklog.NewLoggerProvider(loggerOptions...)

	if err := otelruntime.Start(otelruntime.WithMeterProvider(meters)); err != nil {
		return nil, fmt.Errorf("record runtime metrics: %w", err)
	}

	otel.SetTracerProvider(tracers)
	otel.SetMeterProvider(meters)
	otel.SetLoggerProvider(loggers)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	stdoutHandler := newStdoutHandler(stdout).WithAttrs([]slog.Attr{
		slog.String("service.name", serviceName(res, component)),
		slog.String("service.version", version()),
	})
	failures := slog.New(levelHandler{Handler: stdoutHandler, minimum: minimum})
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		failures.Warn("telemetry export failed", "murphy.error", err.Error())
	}))

	logs := &logState{stdout: stdoutHandler, provider: loggers, minimum: minimum}
	current.Store(logs)
	slog.SetDefault(logs.logger(instrumentation))

	return func(ctx context.Context) error {
		return errors.Join(tracers.Shutdown(ctx), meters.Shutdown(ctx), loggers.Shutdown(ctx))
	}, nil
}

func exportClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}

func exporting(signal string) bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT") != ""
}

type logState struct {
	stdout   slog.Handler
	provider *sdklog.LoggerProvider
	minimum  slog.Level
	loggers  sync.Map
}

func (s *logState) logger(scope string) *slog.Logger {
	if l, ok := s.loggers.Load(scope); ok {
		return l.(*slog.Logger)
	}
	l := slog.New(levelHandler{
		Handler: slog.NewMultiHandler(s.stdout, otelslog.NewHandler(scope, otelslog.WithLoggerProvider(s.provider))),
		minimum: s.minimum,
	})
	actual, _ := s.loggers.LoadOrStore(scope, l)
	return actual.(*slog.Logger)
}

var current atomic.Pointer[logState]

func Logger(scope string) *slog.Logger {
	if s := current.Load(); s != nil {
		return s.logger(scope)
	}
	return slog.Default()
}

type levelHandler struct {
	slog.Handler
	minimum slog.Level
}

func (h levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.minimum && h.Handler.Enabled(ctx, level)
}

func (h levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelHandler{Handler: h.Handler.WithAttrs(attrs), minimum: h.minimum}
}

func (h levelHandler) WithGroup(name string) slog.Handler {
	return levelHandler{Handler: h.Handler.WithGroup(name), minimum: h.minimum}
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("log level must be debug, info, warn or error")
	}
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

func serviceName(res *resource.Resource, fallback string) string {
	if v, ok := res.Set().Value("service.name"); ok {
		return v.AsString()
	}
	return fallback
}
