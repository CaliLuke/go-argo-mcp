package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/CaliLuke/loom-mcp/v2/runtime/mcp/sdkbridge"
	loomotel "github.com/CaliLuke/loom/observability/otel"
	loom "github.com/CaliLuke/loom/pkg"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/mcpaudit"
	"github.com/CaliLuke/go-argo-mcp/internal/mcpvalidation"
	"github.com/CaliLuke/go-argo-mcp/internal/observability"
	"github.com/CaliLuke/go-argo-mcp/internal/service"
)

type telemetryRuntime interface {
	IsEnabled() bool
	HTTPMiddleware(string, loomotel.HTTPMetricMode) func(http.Handler) http.Handler
	WrapHTTPClient(*http.Client, string, loomotel.HTTPMetricMode) *http.Client
	Emit(context.Context, string, string, ...attribute.KeyValue)
	Shutdown(context.Context) error
}

type auditLogger interface {
	Interceptor() mcpargo.ToolCallInterceptor
	Close() error
}

type dependencies struct {
	startTelemetry func(context.Context, Config) (telemetryRuntime, error)
	openAudit      func(string) (auditLogger, error)
	newSDKServer   func(genargo.Service, *mcpargo.SDKServerOptions) (*mcpargo.SDKServer, error)
	listen         func(string, string) (net.Listener, error)
	runStdio       func(context.Context, *mcp.Server) error
}

func defaultDependencies() dependencies {
	return dependencies{
		startTelemetry: func(ctx context.Context, cfg Config) (telemetryRuntime, error) {
			return observability.Start(ctx, observability.ConfigFromEnv(genargo.ServiceName, cfg.Version))
		},
		openAudit:    func(path string) (auditLogger, error) { return mcpaudit.Open(path) },
		newSDKServer: mcpargo.NewSDKServer,
		listen:       net.Listen,
		runStdio: func(ctx context.Context, server *mcp.Server) error {
			return server.Run(ctx, &mcp.StdioTransport{})
		},
	}
}

type Application struct {
	cfg       Config
	telemetry telemetryRuntime
	audit     auditLogger
	sdk       *mcpargo.SDKServer
	handler   http.Handler
	closeOnce sync.Once
	closeErr  error
}

func New(ctx context.Context, cfg Config) (*Application, error) {
	return newApplication(ctx, cfg, defaultDependencies())
}

func newApplication(ctx context.Context, cfg Config, deps dependencies) (*Application, error) {
	if cfg.Transport != TransportHTTP && cfg.Transport != TransportHTTPStateless && cfg.Transport != TransportStdio {
		return nil, fmt.Errorf("invalid transport %q", cfg.Transport)
	}
	if cfg.Transport == TransportStdio && cfg.AuditEnabled && isStdoutPath(cfg.AuditFile) {
		return nil, fmt.Errorf("MCP audit destination %q resolves to stdout", cfg.AuditFile)
	}
	runtime, err := deps.startTelemetry(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("start otel runtime: %w", err)
	}
	app := &Application{cfg: cfg, telemetry: runtime}
	cleanupOnError := func(cause error) (*Application, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout(cfg))
		defer cancel()
		if closeErr := app.Close(cleanupCtx); closeErr != nil {
			return nil, errors.Join(cause, closeErr)
		}
		return nil, cause
	}

	baseClient := newArgoBaseHTTPClient(cfg)
	argoHTTPClient := runtime.WrapHTTPClient(baseClient, "argo-api", loomotel.HTTPMetricModeNone)
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{
			BaseURL:            cfg.ArgoBaseURL,
			Token:              cfg.ArgoToken,
			Username:           cfg.ArgoUsername,
			Password:           cfg.ArgoPassword,
			InsecureSkipVerify: cfg.ArgoInsecureSkipVerify,
			TLSServerName:      cfg.ArgoTLSServerName,
			RequestTimeout:     cfg.ArgoRequestTimeout,
			HTTPClient:         argoHTTPClient,
		}),
		DefaultNamespace: cfg.DefaultNamespace,
		Policy: service.Policy{
			AllowMutations:      cfg.AllowMutations,
			AllowDestructive:    cfg.AllowDestructive,
			RequireConfirmation: cfg.RequireConfirmation,
			AllowedNamespaces:   cfg.AllowedNamespaces,
			DeniedNamespaces:    cfg.DeniedNamespaces,
		},
	})
	adapterOptions := &mcpargo.MCPAdapterOptions{
		StructuredStreamJSON: true,
		ToolCallInterceptors: []mcpargo.ToolCallInterceptor{mcpvalidation.PaginationLimits()},
		ErrorMapper: func(err error) error {
			var named loom.LoomErrorNamer
			if errors.As(err, &named) {
				return err
			}
			return genargo.MakeArgoAPIError(err)
		},
	}
	if cfg.AuditEnabled {
		audit, auditErr := deps.openAudit(cfg.AuditFile)
		if auditErr != nil {
			return cleanupOnError(fmt.Errorf("open MCP audit log: %w", auditErr))
		}
		app.audit = audit
		adapterOptions.ToolCallInterceptors = append(adapterOptions.ToolCallInterceptors, audit.Interceptor())
	}
	serverOptions := &mcpargo.SDKServerOptions{Adapter: adapterOptions}
	if cfg.Transport == TransportHTTPStateless {
		serverOptions.StreamableHTTP = &sdkbridge.StreamableHTTPOptions{Stateless: true}
	}
	sdk, err := deps.newSDKServer(svc, serverOptions)
	if err != nil {
		return cleanupOnError(fmt.Errorf("build MCP server: %w", err))
	}
	app.sdk = sdk
	mux := http.NewServeMux()
	mux.Handle("/rpc", sdk.Handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	metricMode := observability.ConfigFromEnv(genargo.ServiceName, cfg.Version).MetricMode
	app.handler = runtime.HTTPMiddleware(genargo.ServiceName, metricMode)(requestLoggingMiddleware(runtime, mux))
	return app, nil
}

func (a *Application) Handler() http.Handler { return a.handler }

func (a *Application) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		if a.audit != nil {
			a.closeErr = errors.Join(a.closeErr, a.audit.Close())
		}
		if a.telemetry != nil {
			a.closeErr = errors.Join(a.closeErr, a.telemetry.Shutdown(ctx))
		}
	})
	return a.closeErr
}

func Run(ctx context.Context, cfg Config) error {
	return runWithDependencies(ctx, cfg, defaultDependencies())
}

func runWithDependencies(ctx context.Context, cfg Config, deps dependencies) (runErr error) {
	app, err := newApplication(ctx, cfg, deps)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout(cfg))
		defer cancel()
		runErr = errors.Join(runErr, app.Close(cleanupCtx))
	}()
	if cfg.Transport == TransportStdio {
		log.Printf("starting Argo Workflows MCP stdio server")
		err = deps.runStdio(ctx, app.sdk.Server)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return runHTTP(ctx, cfg, app, deps)
}

func runHTTP(ctx context.Context, cfg Config, app *Application, deps dependencies) error {
	listener, err := deps.listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	defer func() { _ = listener.Close() }()
	httpServer := &http.Server{Handler: app.handler, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()

	log.Printf("starting Argo Workflows MCP %s server on %s", cfg.Transport, listener.Addr())
	log.Printf("mcp endpoint: http://%s/rpc", listener.Addr())
	log.Printf("service: %s", genargo.ServiceName)
	log.Printf("default namespace: %s", cfg.DefaultNamespace)
	log.Printf("otel enabled: %t", app.telemetry.IsEnabled())
	app.telemetry.Emit(ctx, "go-argo-mcp.startup", "server starting",
		attribute.String("server.addr", listener.Addr().String()),
		attribute.String("service.name", genargo.ServiceName),
		attribute.String("default.namespace", cfg.DefaultNamespace),
		attribute.Bool("otel.enabled", app.telemetry.IsEnabled()),
	)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		app.telemetry.Emit(context.Background(), "go-argo-mcp.lifecycle", "server stopping")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout(cfg))
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
		}
		<-serveErr
		return nil
	}
}

func shutdownTimeout(cfg Config) time.Duration {
	if cfg.ShutdownTimeout > 0 {
		return cfg.ShutdownTimeout
	}
	return 5 * time.Second
}

func requestLoggingMiddleware(runtime telemetryRuntime, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtime.Emit(r.Context(), "go-argo-mcp.http", "http request received",
			attribute.String("http.method", r.Method),
			attribute.String("http.route", routeName(r)),
		)
		next.ServeHTTP(w, r)
	})
}

func routeName(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.URL.Path
}

func newArgoBaseHTTPClient(cfg Config) *http.Client {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	transport := defaultTransport.Clone()
	if cfg.ArgoInsecureSkipVerify || cfg.ArgoTLSServerName != "" {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: cfg.ArgoInsecureSkipVerify, // #nosec G402 -- explicit operator configuration
			ServerName:         cfg.ArgoTLSServerName,
		}
	}
	return &http.Client{Transport: transport}
}
