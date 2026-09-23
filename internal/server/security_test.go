package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

type capturedLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *capturedLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range records {
		e.records = append(e.records, records[i].Clone())
	}
	return nil
}
func (*capturedLogExporter) Shutdown(context.Context) error   { return nil }
func (*capturedLogExporter) ForceFlush(context.Context) error { return nil }

type exportingTelemetry struct{}

func (*exportingTelemetry) IsEnabled() bool { return true }
func (*exportingTelemetry) HTTPMiddleware(name string, mode loomotel.HTTPMetricMode) func(http.Handler) http.Handler {
	return loomotel.HTTPMiddleware(loomotel.HTTPConfig{ServiceName: name, MetricMode: mode})
}
func (*exportingTelemetry) WrapHTTPClient(client *http.Client, _ string, _ loomotel.HTTPMetricMode) *http.Client {
	return client
}
func (*exportingTelemetry) Emit(ctx context.Context, name, body string, attrs ...attribute.KeyValue) {
	record := otellog.Record{}
	record.SetTimestamp(time.Now())
	record.SetBody(attribute.StringValue(body))
	record.AddAttributes(attrs...)
	logglobal.Logger(name).Emit(ctx, record)
}
func (*exportingTelemetry) Shutdown(context.Context) error { return nil }

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func TestSecurityEnvironmentPresence(t *testing.T) {
	if _, err := ConfigFromLookup(lookup(nil)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ARGO_MCP_AUTH_TOKEN", "ARGO_MCP_ALLOW_UNAUTHENTICATED", "ARGO_MCP_ALLOWED_HOSTS", "ARGO_MCP_ALLOWED_ORIGINS"} {
		t.Run(key, func(t *testing.T) {
			if _, err := ConfigFromLookup(lookup(map[string]string{key: ""})); err == nil {
				t.Fatal("explicit empty value accepted")
			}
		})
	}
	cfg, err := ConfigFromLookup(lookup(map[string]string{
		"ARGO_MCP_TRANSPORT": "stdio", "ARGO_MCP_AUTH_TOKEN": " bad ",
		"ARGO_MCP_ALLOW_UNAUTHENTICATED": "not-bool", "ARGO_MCP_ALLOWED_HOSTS": "", "ARGO_MCP_ALLOWED_ORIGINS": "",
	}))
	if err != nil || cfg.Transport != TransportStdio {
		t.Fatalf("stdio parsed HTTP-only settings: %#v %v", cfg, err)
	}
}

func TestBearerTokenValidation(t *testing.T) {
	for _, valid := range []string{"a", "abc.DEF-_~+/=="} {
		if err := validateBearerToken(valid); err != nil {
			t.Errorf("valid %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", " token", "token ", "a=b", "a\nb", "é"} {
		if err := validateBearerToken(invalid); err == nil {
			t.Errorf("invalid token %q accepted", invalid)
		}
	}
}

func TestHTTPPolicyConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"loopback defaults", Config{Addr: "127.0.0.1:8080"}, false},
		{"localhost", Config{Addr: "localhost:8080"}, false},
		{"mapped loopback", Config{Addr: "[::ffff:127.0.0.1]:8080"}, false},
		{"external without acknowledgement", Config{Addr: "192.0.2.1:8080"}, true},
		{"external auth", Config{Addr: "192.0.2.1:8080", AuthToken: "secret"}, false},
		{"external proxy", Config{Addr: "192.0.2.1:8080", AllowUnauthenticated: true}, false},
		{"wildcard no hosts", Config{Addr: ":8080", AuthToken: "secret"}, true},
		{"empty wildcard host explicit", Config{Addr: "0.0.0.0:8080", AuthToken: "secret", AllowedHosts: []string{""}}, true},
		{"wildcard allowed", Config{Addr: ":8080", AuthToken: "secret", AllowedHosts: []string{"proxy.example:443"}}, false},
		{"zero port", Config{Addr: "127.0.0.1:0"}, true},
		{"large port", Config{Addr: "127.0.0.1:65536"}, true},
		{"zone", Config{Addr: "[fe80::1%eth0]:8080", AuthToken: "secret"}, true},
		{"trailing dot", Config{Addr: "localhost.:8080"}, true},
		{"host scheme", Config{Addr: "127.0.0.1:8080", AllowedHosts: []string{"https://localhost:8080"}}, true},
		{"origin slash", Config{Addr: "127.0.0.1:8080", AllowedOrigins: []string{"https://proxy.example/"}}, true},
		{"origin empty query", Config{Addr: "127.0.0.1:8080", AllowedOrigins: []string{"https://proxy.example?"}}, true},
		{"origin empty fragment", Config{Addr: "127.0.0.1:8080", AllowedOrigins: []string{"https://proxy.example#"}}, true},
		{"origin userinfo", Config{Addr: "127.0.0.1:8080", AllowedOrigins: []string{"https://u:p@proxy.example"}}, true},
		{"origin canonical", Config{Addr: "127.0.0.1:8080", AllowedOrigins: []string{"HTTPS://Proxy.Example:443"}}, false},
		{"leading zero port canonical", Config{Addr: "127.0.0.1:08080", AllowedHosts: []string{"localhost:8080"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildHTTPSecurityPolicy(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestBracketedIPv6RequestMayUseDefaultPort(t *testing.T) {
	p, err := buildHTTPSecurityPolicy(Config{Addr: "[::1]:80"})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	h := p.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://[::1]/rpc", nil)
	req.Host = "[::1]"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatal("bracketed IPv6 authority without port did not use HTTP default")
	}
}

func TestLeadingZeroRequestPortMatchesCanonicalAuthority(t *testing.T) {
	p, err := buildHTTPSecurityPolicy(Config{Addr: "127.0.0.1:8080", AllowedHosts: []string{"proxy.example:08080"}})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	h := p.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "http://proxy.example:8080/rpc", nil)
	req.Host = "proxy.example:08080"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatal("numeric request port was not canonicalized")
	}
}

func TestOriginIsCanonicalBeforeSDKConstruction(t *testing.T) {
	deps, _, _ := lifecycleDeps(t)
	var got *mcpargo.SDKServerOptions
	deps.newSDKServer = func(_ genargo.Service, options *mcpargo.SDKServerOptions) (*mcpargo.SDKServer, error) {
		got = options
		return &mcpargo.SDKServer{Server: mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil), Handler: http.NotFoundHandler()}, nil
	}
	app, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", AllowedOrigins: []string{"HTTPS://Proxy.Example:443"}}, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	if got == nil || got.OriginProtection == nil || len(got.OriginProtection.TrustedOrigins) != 1 || got.OriginProtection.TrustedOrigins[0] != "https://proxy.example" {
		t.Fatalf("SDK origins = %#v", got)
	}
	if got.StreamableHTTP == nil || !got.StreamableHTTP.DisableLocalhostProtection {
		t.Fatal("exact Host middleware did not replace SDK localhost protection")
	}
}

func TestInstrumentedRequestPreservesHandlerQuery(t *testing.T) {
	var query string
	runtime := &countingTelemetry{}
	h := instrumentedHTTPHandler(runtime, "", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { query = r.URL.RawQuery }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/rpc?cursor=next", nil))
	if query != "cursor=next" {
		t.Fatalf("handler query = %q, want original query", query)
	}
}

func TestSecurityMiddleware(t *testing.T) {
	p, err := buildHTTPSecurityPolicy(Config{
		Addr: "127.0.0.1:8080", AuthToken: "correct-token",
		AllowedOrigins: []string{"https://proxy.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	h := p.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	tests := []struct {
		name, host, origin string
		auth               []string
		want               int
	}{
		{"accepted", "LOCALHOST:8080", "", []string{"bEaReR correct-token"}, 204},
		{"proxy origin", "127.0.0.1:8080", "https://PROXY.example:443", []string{"Bearer correct-token"}, 204},
		{"missing", "127.0.0.1:8080", "", nil, 401},
		{"wrong length", "127.0.0.1:8080", "", []string{"Bearer x"}, 401},
		{"tab separator", "127.0.0.1:8080", "", []string{"Bearer\tcorrect-token"}, 401},
		{"ascii space separator", "127.0.0.1:8080", "", []string{"bEaReR   correct-token"}, 204},
		{"duplicate auth", "127.0.0.1:8080", "", []string{"Bearer correct-token", "Bearer correct-token"}, 401},
		{"host", "evil.example:8080", "", []string{"Bearer correct-token"}, 403},
		{"origin", "127.0.0.1:8080", "https://evil.example", []string{"Bearer correct-token"}, 403},
		{"origin empty query", "127.0.0.1:8080", "http://127.0.0.1:8080?", []string{"Bearer correct-token"}, 403},
		{"origin empty fragment", "127.0.0.1:8080", "http://127.0.0.1:8080#", []string{"Bearer correct-token"}, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tt.host+"/rpc?credential=query-secret", strings.NewReader("{}"))
			for _, value := range tt.auth {
				req.Header.Add("Authorization", value)
			}
			if tt.origin != "" {
				req.Header.Add("Origin", tt.origin)
			}
			req.Header.Set("Forwarded", "host=trusted.example")
			rr := httptest.NewRecorder()
			before := calls.Load()
			h.ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Fatalf("status=%d want=%d", rr.Code, tt.want)
			}
			if tt.want != 204 && calls.Load() != before {
				t.Fatal("rejection reached handler")
			}
			if tt.want == 401 && !strings.HasPrefix(rr.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Fatal("missing Bearer challenge")
			}
		})
	}
}

func TestHTTPValidationPrecedesLifecycleAndStdioBypassesIt(t *testing.T) {
	deps, _, _ := lifecycleDeps(t)
	var listens atomic.Int32
	deps.listen = func(string, string) (net.Listener, error) { listens.Add(1); return nil, errors.New("called") }
	err := runWithDependencies(context.Background(), Config{Transport: TransportHTTP, Addr: ":8080"}, deps)
	if err == nil || listens.Load() != 0 {
		t.Fatalf("err=%v listens=%d", err, listens.Load())
	}
	deps.runStdio = func(context.Context, *mcp.Server) error { return nil }
	if err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, Addr: "bad", AuthToken: " bad "}, deps); err != nil {
		t.Fatal(err)
	}
}

func TestHealthIsStaticAndUnauthenticated(t *testing.T) {
	deps, _, _ := lifecycleDeps(t)
	app, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", AuthToken: "secret"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	rr := httptest.NewRecorder()
	app.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/healthz?credential=query-secret", nil))
	if rr.Code != 200 || rr.Body.String() != "ok" {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestRejectedArgoBaseURLDoesNotEchoCredentials(t *testing.T) {
	const sentinel = "upstream-config-secret"
	deps, _, _ := lifecycleDeps(t)
	var telemetryStarts atomic.Int32
	deps.startTelemetry = func(context.Context, Config) (telemetryRuntime, error) {
		telemetryStarts.Add(1)
		return &exportingTelemetry{}, nil
	}
	for _, raw := range []string{"https://user:" + sentinel + "@argo.example", "https://argo.example?token=" + sentinel, "https://argo.example#" + sentinel} {
		_, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", ArgoBaseURL: raw}, deps)
		if err == nil {
			t.Fatal("credential-bearing base URL accepted")
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("error leaked credential: %v", err)
		}
	}
	for _, raw := range []string{"https://argo.example?", "https://argo.example#"} {
		if _, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", ArgoBaseURL: raw}, deps); err == nil {
			t.Fatalf("explicit empty query/fragment accepted: %q", raw)
		}
	}
	app, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", ArgoBaseURL: "https://argo.example/api/%3Fversion%3D1", AuditEnabled: false}, deps)
	if err != nil {
		t.Fatalf("escaped path rejected: %v", err)
	}
	_ = app.Close(context.Background())
	if telemetryStarts.Load() != 1 {
		t.Fatalf("telemetry starts=%d, want only the valid escaped-path application", telemetryStarts.Load())
	}
}

func TestProductionHTTPPathsExportNoCredentialValues(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	previousTrace := otel.GetTracerProvider()
	otel.SetTracerProvider(traceProvider)
	logExporter := &capturedLogExporter{}
	logProvider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExporter)))
	previousLogs := logglobal.GetLoggerProvider()
	logglobal.SetLoggerProvider(logProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTrace)
		logglobal.SetLoggerProvider(previousLogs)
		_ = traceProvider.Shutdown(context.Background())
		_ = logProvider.Shutdown(context.Background())
	})

	deps, _, _ := lifecycleDeps(t)
	deps.startTelemetry = func(context.Context, Config) (telemetryRuntime, error) { return &exportingTelemetry{}, nil }
	app, err := newApplication(context.Background(), Config{Transport: TransportHTTP, Addr: "127.0.0.1:8080", AuthToken: "correct", AuditEnabled: false}, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	requests := []struct {
		method, path, token, host string
		wantStatus                int
	}{
		{http.MethodPost, "/rpc?credential=accepted-rpc-secret", "correct", "127.0.0.1:8080", http.StatusNotFound},
		{http.MethodPost, "/rpc?credential=rejected-rpc-secret", "wrong", "127.0.0.1:8080", http.StatusUnauthorized},
		{http.MethodPost, "/rpc?credential=rejected-host-query-secret", "correct", "rejected-host-credential-secret:8080", http.StatusForbidden},
		{http.MethodGet, "/healthz?credential=health-secret", "", "127.0.0.1:8080", http.StatusOK},
		{"method-credential-secret", "/missing/%70ath-raw-secret?credential=unknown-secret", "", "127.0.0.1:8080", http.StatusNotFound},
	}
	for _, item := range requests {
		req := httptest.NewRequest(item.method, "http://127.0.0.1:8080"+item.path, strings.NewReader("{}"))
		req.Host = item.host
		req.RemoteAddr = "remote-credential-secret:1234"
		req.Header.Set("User-Agent", "agent-credential-secret")
		req.Header.Set("Forwarded", "host=forwarded-credential-secret")
		if strings.Contains(item.path, "missing") {
			req.Header.Set("Origin", "https://origin-credential-secret.example")
		}
		if item.token != "" {
			req.Header.Set("Authorization", "Bearer "+item.token)
		}
		rr := httptest.NewRecorder()
		app.Handler().ServeHTTP(rr, req)
		if rr.Code != item.wantStatus {
			t.Fatalf("%s %s status=%d want=%d", item.method, item.path, rr.Code, item.wantStatus)
		}
	}
	spans := spanRecorder.Ended()
	if len(spans) < len(requests) {
		t.Fatalf("exported spans=%d want at least %d", len(spans), len(requests))
	}
	logExporter.mu.Lock()
	records := append([]sdklog.Record(nil), logExporter.records...)
	logExporter.mu.Unlock()
	if len(records) < len(requests) {
		t.Fatalf("exported logs=%d want at least %d", len(records), len(requests))
	}
	sentinels := []string{"accepted-rpc-secret", "rejected-rpc-secret", "rejected-host-query-secret", "rejected-host-credential-secret", "health-secret", "raw-secret", "unknown-secret", "method-credential-secret", "remote-credential-secret", "agent-credential-secret", "forwarded-credential-secret", "origin-credential-secret", "correct", "wrong"}
	assertSafe := func(value string) {
		for _, sentinel := range sentinels {
			if strings.Contains(value, sentinel) {
				t.Fatalf("exported telemetry leaked %q in %q", sentinel, value)
			}
		}
	}
	for _, span := range spans {
		assertSafe(span.Name())
		for _, attr := range span.Attributes() {
			assertSafe(attr.Value.String())
		}
		for _, event := range span.Events() {
			assertSafe(event.Name)
			for _, attr := range event.Attributes {
				assertSafe(attr.Value.String())
			}
		}
	}
	for i := range records {
		assertSafe(records[i].Body().String())
		records[i].WalkAttributes(func(attr attribute.KeyValue) bool { assertSafe(attr.Value.String()); return true })
	}
}
