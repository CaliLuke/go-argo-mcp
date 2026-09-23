package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

type countingTelemetry struct{ shutdowns atomic.Int32 }

func (*countingTelemetry) IsEnabled() bool { return false }
func (*countingTelemetry) HTTPMiddleware(string, loomotel.HTTPMetricMode) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
func (*countingTelemetry) WrapHTTPClient(client *http.Client, _ string, _ loomotel.HTTPMetricMode) *http.Client {
	return client
}
func (*countingTelemetry) Emit(context.Context, string, string, ...attribute.KeyValue) {}
func (t *countingTelemetry) Shutdown(context.Context) error {
	t.shutdowns.Add(1)
	return nil
}

type countingAudit struct{ closes atomic.Int32 }

func (*countingAudit) Interceptor() mcpargo.ToolCallInterceptor { return nil }
func (a *countingAudit) Close() error {
	a.closes.Add(1)
	return nil
}

func lifecycleDeps(t *testing.T) (dependencies, *countingTelemetry, *countingAudit) {
	t.Helper()
	telemetry := &countingTelemetry{}
	audit := &countingAudit{}
	deps := defaultDependencies()
	deps.startTelemetry = func(context.Context, Config) (telemetryRuntime, error) { return telemetry, nil }
	deps.openAudit = func(string) (auditLogger, error) { return audit, nil }
	deps.newSDKServer = func(genargo.Service, *mcpargo.SDKServerOptions) (*mcpargo.SDKServer, error) {
		return &mcpargo.SDKServer{Server: mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil), Handler: http.NotFoundHandler()}, nil
	}
	return deps, telemetry, audit
}

func TestCleanupAfterAdapterConstructionFailure(t *testing.T) {
	deps, telemetry, audit := lifecycleDeps(t)
	deps.newSDKServer = func(genargo.Service, *mcpargo.SDKServerOptions) (*mcpargo.SDKServer, error) {
		return nil, errors.New("adapter failed")
	}
	err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, AuditEnabled: true}, deps)
	if err == nil {
		t.Fatal("expected adapter error")
	}
	assertCleanupOnce(t, telemetry, audit)
}

func TestTelemetryCleanupAfterAuditOpenFailure(t *testing.T) {
	deps, telemetry, _ := lifecycleDeps(t)
	deps.openAudit = func(string) (auditLogger, error) { return nil, errors.New("audit failed") }
	err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, AuditEnabled: true}, deps)
	if err == nil {
		t.Fatal("expected audit error")
	}
	if got := telemetry.shutdowns.Load(); got != 1 {
		t.Fatalf("telemetry shutdowns = %d, want 1", got)
	}
}

func TestCleanupAfterListenFailure(t *testing.T) {
	deps, telemetry, audit := lifecycleDeps(t)
	deps.listen = func(string, string) (net.Listener, error) { return nil, errors.New("listen failed") }
	err := runWithDependencies(context.Background(), Config{Transport: TransportHTTP, AuditEnabled: true}, deps)
	if err == nil {
		t.Fatal("expected listen error")
	}
	assertCleanupOnce(t, telemetry, audit)
}

func TestCleanupAfterTransportFailure(t *testing.T) {
	deps, telemetry, audit := lifecycleDeps(t)
	deps.runStdio = func(context.Context, *mcp.Server) error { return errors.New("transport failed") }
	err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, AuditEnabled: true}, deps)
	if err == nil {
		t.Fatal("expected transport error")
	}
	assertCleanupOnce(t, telemetry, audit)
}

func TestCleanupAfterNormalStdioExit(t *testing.T) {
	deps, telemetry, audit := lifecycleDeps(t)
	deps.runStdio = func(context.Context, *mcp.Server) error { return nil }
	if err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, AuditEnabled: true}, deps); err != nil {
		t.Fatal(err)
	}
	assertCleanupOnce(t, telemetry, audit)
}

func TestCanceledStdioRunIsCleanExit(t *testing.T) {
	deps, _, _ := lifecycleDeps(t)
	deps.runStdio = func(context.Context, *mcp.Server) error { return context.Canceled }
	if err := runWithDependencies(context.Background(), Config{Transport: TransportStdio, AuditEnabled: true}, deps); err != nil {
		t.Fatalf("context cancellation returned error: %v", err)
	}
}

func TestHTTPShutdownTimeoutStillCleansUp(t *testing.T) {
	deps, telemetry, audit := lifecycleDeps(t)
	started := make(chan struct{})
	release := make(chan struct{})
	deps.newSDKServer = func(genargo.Service, *mcpargo.SDKServerOptions) (*mcpargo.SDKServer, error) {
		return &mcpargo.SDKServer{
			Server: mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.WriteHeader(http.StatusOK)
			}),
		}, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deps.listen = func(string, string) (net.Listener, error) { return listener, nil }
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- runWithDependencies(ctx, Config{
			Transport:       TransportHTTP,
			AuditEnabled:    true,
			ShutdownTimeout: 50 * time.Millisecond,
		}, deps)
	}()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, requestErr := http.Get("http://" + listener.Addr().String() + "/rpc")
		if requestErr == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("signal shutdown returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not enforce shutdown timeout")
	}
	assertCleanupOnce(t, telemetry, audit)
	close(release)
	<-requestDone
}

func assertCleanupOnce(t *testing.T, telemetry *countingTelemetry, audit *countingAudit) {
	t.Helper()
	if got := telemetry.shutdowns.Load(); got != 1 {
		t.Fatalf("telemetry shutdowns = %d, want 1", got)
	}
	if got := audit.closes.Load(); got != 1 {
		t.Fatalf("audit closes = %d, want 1", got)
	}
}
