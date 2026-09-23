package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPMiddlewareExportsNoRawQueryCredential(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	runtime := &Runtime{Enabled: true, tracesEnabled: true}
	h := runtime.HTTPMiddleware("test", loomotel.HTTPMetricModeOTelOnly)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/unknown?credential=actual-export-secret", nil))
	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("HTTP middleware exported no spans")
	}
	for _, span := range spans {
		if strings.Contains(span.Name(), "actual-export-secret") {
			t.Fatalf("span name leaked credential: %q", span.Name())
		}
		for _, attr := range span.Attributes() {
			if strings.Contains(attr.Value.String(), "actual-export-secret") {
				t.Fatalf("span attribute %s leaked credential", attr.Key)
			}
		}
	}
}
