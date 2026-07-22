package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	loom "github.com/CaliLuke/loom/pkg"
	"go.opentelemetry.io/otel/attribute"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/mcpaudit"
	"github.com/CaliLuke/go-argo-mcp/internal/observability"
	"github.com/CaliLuke/go-argo-mcp/internal/service"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("go-argo-mcp %s\n", version)
		return
	}

	ctx := context.Background()
	addr := envOrDefault("ARGO_MCP_ADDR", "127.0.0.1:8080")
	otelRuntime, otelErr := observability.Start(ctx, observability.ConfigFromEnv(genargo.ServiceName, version))
	if otelErr != nil {
		log.Fatalf("start otel runtime: %v", otelErr)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := otelRuntime.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Printf("shutdown otel runtime: %v", shutdownErr)
		}
	}()

	argoHTTPClient := otelRuntime.WrapHTTPClient(newArgoBaseHTTPClient(), "argo-api", loomotel.HTTPMetricModeNone)
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{
			BaseURL:            os.Getenv("ARGO_BASE_URL"),
			Token:              os.Getenv("ARGO_TOKEN"),
			Username:           os.Getenv("ARGO_USERNAME"),
			Password:           os.Getenv("ARGO_PASSWORD"),
			InsecureSkipVerify: envBool("ARGO_INSECURE_SKIP_TLS_VERIFY", false),
			TLSServerName:      os.Getenv("ARGO_TLS_SERVER_NAME"),
			RequestTimeout:     envDurationSeconds("ARGO_REQUEST_TIMEOUT_SECONDS", 30),
			HTTPClient:         argoHTTPClient,
		}),
		DefaultNamespace: envOrDefault("ARGO_NAMESPACE", "default"),
		Policy: service.Policy{
			AllowMutations:      envBool("MCP_ALLOW_MUTATIONS", false),
			AllowDestructive:    envBool("MCP_ALLOW_DESTRUCTIVE", false),
			RequireConfirmation: envBool("MCP_REQUIRE_CONFIRMATION", true),
			AllowedNamespaces:   envCSV("MCP_NAMESPACES_ALLOW"),
			DeniedNamespaces:    envCSV("MCP_NAMESPACES_DENY"),
		},
	})

	adapterOptions := &mcpargo.MCPAdapterOptions{
		StructuredStreamJSON: true,
		ErrorMapper: func(err error) error {
			var named loom.LoomErrorNamer
			if errors.As(err, &named) {
				return err
			}
			return genargo.MakeArgoAPIError(err)
		},
	}
	if envBool("MCP_AUDIT_ENABLED", true) {
		audit, auditErr := mcpaudit.Open(envOrDefault("MCP_AUDIT_FILE", "./mcp-audit.log"))
		if auditErr != nil {
			log.Fatalf("open MCP audit log: %v", auditErr)
		}
		defer func() {
			if closeErr := audit.Close(); closeErr != nil {
				log.Printf("close MCP audit log: %v", closeErr)
			}
		}()
		adapterOptions.ToolCallInterceptors = append(adapterOptions.ToolCallInterceptors, audit.Interceptor())
	}

	server, serverErr := mcpargo.NewSDKServer(svc, &mcpargo.SDKServerOptions{Adapter: adapterOptions})
	if serverErr != nil {
		log.Fatalf("build mcp server: %v", serverErr)
	}

	mux := http.NewServeMux()
	mux.Handle("/rpc", server.Handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	handler := otelRuntime.HTTPMiddleware(genargo.ServiceName, observability.ConfigFromEnv(genargo.ServiceName, version).MetricMode)(requestLoggingMiddleware(otelRuntime, mux))
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	log.Printf("starting argo workflows mcp http server on %s", addr)
	log.Printf("mcp endpoint: http://%s/rpc", addr)
	log.Printf("service: %s", genargo.ServiceName)
	log.Printf("default namespace: %s", envOrDefault("ARGO_NAMESPACE", "default"))
	log.Printf("otel enabled: %t", otelRuntime.Enabled)
	otelRuntime.Emit(ctx, "go-argo-mcp.startup", "server starting",
		attribute.String("server.addr", addr),
		attribute.String("service.name", genargo.ServiceName),
		attribute.String("default.namespace", envOrDefault("ARGO_NAMESPACE", "default")),
		attribute.Bool("otel.enabled", otelRuntime.Enabled),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case serveErr := <-errCh:
		log.Fatalf("serve: %v", serveErr)
	case sig := <-sigCh:
		otelRuntime.Emit(context.Background(), "go-argo-mcp.lifecycle", "server stopping",
			attribute.String("signal", sig.String()),
		)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Fatalf("shutdown http server: %v", shutdownErr)
		}
	}
}

func requestLoggingMiddleware(runtime *observability.Runtime, next http.Handler) http.Handler {
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

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(key string, fallback int) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return time.Duration(fallback) * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func envCSV(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func newArgoBaseHTTPClient() *http.Client {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	transport := defaultTransport.Clone()
	if envBool("ARGO_INSECURE_SKIP_TLS_VERIFY", false) || os.Getenv("ARGO_TLS_SERVER_NAME") != "" {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: envBool("ARGO_INSECURE_SKIP_TLS_VERIFY", false), // #nosec G402 -- explicit operator configuration
			ServerName:         os.Getenv("ARGO_TLS_SERVER_NAME"),
		}
	}
	return &http.Client{Transport: transport}
}
