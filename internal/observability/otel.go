package observability

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	otelglobal "go.opentelemetry.io/otel/log/global"
)

type Config struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	Insecure       bool
	Headers        map[string]string
	MetricMode     loomotel.HTTPMetricMode
}

type Runtime struct {
	Enabled       bool
	shutdown      func(context.Context) error
	tracesEnabled bool
	logsEnabled   bool
}

func Start(ctx context.Context, cfg Config) (*Runtime, error) {
	if !cfg.Enabled {
		return &Runtime{}, nil
	}
	rt, err := loomotel.New(ctx, loomotel.Config{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.ServiceVersion,
		Environment:    cfg.Environment,
		Traces: loomotel.TraceConfig{
			Enabled:     true,
			Endpoint:    cfg.OTLPEndpoint,
			Insecure:    cfg.Insecure,
			Headers:     cfg.Headers,
			SampleRatio: 1,
		},
		Logs: loomotel.LogConfig{
			Enabled:  true,
			Endpoint: cfg.OTLPEndpoint,
			Insecure: cfg.Insecure,
			Headers:  cfg.Headers,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("start otel runtime: %w", err)
	}
	return &Runtime{
		Enabled:       true,
		shutdown:      rt.Shutdown,
		tracesEnabled: rt.TracerProvider != nil,
		logsEnabled:   rt.LoggerProvider != nil,
	}, nil
}

func (r *Runtime) HTTPMiddleware(serviceName string, mode loomotel.HTTPMetricMode) func(http.Handler) http.Handler {
	if r == nil || !r.Enabled {
		return identityMiddleware
	}
	return loomotel.HTTPMiddleware(loomotel.HTTPConfig{
		ServiceName: serviceName,
		MetricMode:  mode,
	})
}

func (r *Runtime) WrapHTTPClient(client *http.Client, serviceName string, mode loomotel.HTTPMetricMode) *http.Client {
	if r == nil || !r.Enabled {
		return client
	}
	return loomotel.WrapHTTPClient(client, loomotel.HTTPClientConfig{
		ServiceName: serviceName,
		MetricMode:  mode,
	})
}

func (r *Runtime) Emit(ctx context.Context, loggerName, body string, attrs ...attribute.KeyValue) {
	if r == nil || !r.Enabled || !r.logsEnabled {
		return
	}
	record := otellog.Record{}
	record.SetTimestamp(time.Now())
	record.SetSeverity(otellog.SeverityInfo)
	record.SetBody(otellog.StringValue(body))
	if len(attrs) > 0 {
		logAttrs := make([]otellog.KeyValue, 0, len(attrs))
		for _, attr := range attrs {
			logAttrs = append(logAttrs, otellog.KeyValueFromAttribute(attr))
		}
		record.AddAttributes(logAttrs...)
	}
	otelglobal.Logger(loggerName).Emit(ctx, record)
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.shutdown == nil {
		return nil
	}
	return r.shutdown(ctx)
}

func HeadersFromEnv(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	headers := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		headers[key] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func ConfigFromEnv(serviceName, serviceVersion string) Config {
	enabled := envBool("OTEL_ENABLED", false)
	endpoint := envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if endpoint == "" {
		endpoint = envOrDefault("OTEL_EXPORTER_OTLP_HTTP_ENDPOINT", "")
	}
	return Config{
		Enabled:        enabled,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    envOrDefault("OTEL_ENVIRONMENT", envOrDefault("ENVIRONMENT", "")),
		OTLPEndpoint:   endpoint,
		Insecure:       envBool("OTEL_EXPORTER_OTLP_INSECURE", false),
		Headers:        HeadersFromEnv(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		MetricMode:     httpMetricModeFromEnv("OTEL_HTTP_METRIC_MODE", loomotel.HTTPMetricModeNone),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	case "0", "f", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func httpMetricModeFromEnv(key string, fallback loomotel.HTTPMetricMode) loomotel.HTTPMetricMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return fallback
	case string(loomotel.HTTPMetricModeOTelOnly):
		return loomotel.HTTPMetricModeOTelOnly
	case string(loomotel.HTTPMetricModeCustomOnly):
		return loomotel.HTTPMetricModeCustomOnly
	case string(loomotel.HTTPMetricModeBoth):
		return loomotel.HTTPMetricModeBoth
	case string(loomotel.HTTPMetricModeNone):
		return loomotel.HTTPMetricModeNone
	default:
		return fallback
	}
}

func identityMiddleware(next http.Handler) http.Handler { return next }
