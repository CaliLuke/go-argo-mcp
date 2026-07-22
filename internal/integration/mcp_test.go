package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/service"
)

func TestAllToolsAreAdvertisedAndCallable(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: argo.URL}),
		DefaultNamespace: "argo-ci",
		Policy: service.Policy{
			AllowedNamespaces: []string{"argo-ci"},
		},
	})
	server, err := mcpargo.NewSDKServer(svc, &mcpargo.SDKServerOptions{
		Adapter: &mcpargo.MCPAdapterOptions{StructuredStreamJSON: true},
	})
	if err != nil {
		t.Fatalf("NewSDKServer returned error: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		HTTPClient:           &http.Client{Timeout: 5 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(listed.Tools) != 13 {
		t.Fatalf("expected 13 tools, got %d", len(listed.Tools))
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{"list_workflows", map[string]any{"namespace": "argo-ci"}},
		{"get_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"get_workflow_logs", map[string]any{"namespace": "argo-ci", "workflow_name": "build-123"}},
		{"terminate_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123", "reason": "integration test"}},
		{"retry_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"list_cron_workflows", map[string]any{"namespace": "argo-ci"}},
		{"get_cron_workflow", map[string]any{"namespace": "argo-ci", "name": "nightly"}},
		{"get_cron_history", map[string]any{"namespace": "argo-ci", "name": "nightly"}},
		{"toggle_cron_suspension", map[string]any{"namespace": "argo-ci", "name": "nightly", "suspend": true}},
		{"list_workflow_templates", map[string]any{"namespace": "argo-ci"}},
		{"get_workflow_template", map[string]any{"namespace": "argo-ci", "name": "build-template"}},
		{"list_cluster_workflow_templates", map[string]any{}},
		{"get_cluster_workflow_template", map[string]any{"name": "global-template"}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
			if err != nil {
				t.Fatalf("CallTool returned transport error: %v", err)
			}
			if result.IsError {
				t.Fatalf("tool returned error: %#v", result.Content)
			}
			if result.StructuredContent == nil {
				t.Fatal("expected generated structured output")
			}
		})
	}
}

func simulatedArgo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/workflows/argo-ci":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"nightly-001","namespace":"argo-ci"},"status":{"phase":"Succeeded","progress":"1/1","startedAt":"2026-07-21T10:00:00Z","finishedAt":"2026-07-21T10:01:00Z"}}]}`))
	case "/api/v1/workflows/argo-ci/build-123":
		_, _ = w.Write([]byte(`{"metadata":{"name":"build-123","namespace":"argo-ci","labels":{"app":"build"}},"spec":{"arguments":{"parameters":[{"name":"image","value":"alpine"}]}},"status":{"phase":"Succeeded","progress":"1/1","startedAt":"2026-07-21T10:00:00Z","finishedAt":"2026-07-21T10:01:00Z"}}`))
	case "/api/v1/workflows/argo-ci/build-123/log":
		_, _ = w.Write([]byte("{\"result\":{\"podName\":\"build-123-main\",\"content\":\"done\"}}\n"))
	case "/api/v1/cron-workflows/argo-ci":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"nightly","namespace":"argo-ci"},"spec":{"schedule":"0 0 * * *","suspend":false}}]}`))
	case "/api/v1/cron-workflows/argo-ci/nightly":
		_, _ = w.Write([]byte(`{"metadata":{"name":"nightly","namespace":"argo-ci"},"spec":{"schedule":"0 0 * * *","suspend":false},"status":{"lastScheduledTime":"2026-07-21T00:00:00Z"}}`))
	case "/api/v1/workflow-templates/argo-ci":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"build-template","namespace":"argo-ci"},"spec":{"entrypoint":"main"}}]}`))
	case "/api/v1/workflow-templates/argo-ci/build-template":
		_, _ = w.Write([]byte(`{"metadata":{"name":"build-template","namespace":"argo-ci"},"spec":{"entrypoint":"main","templates":[{"name":"main"}]}}`))
	case "/api/v1/cluster-workflow-templates":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"global-template"},"spec":{"entrypoint":"main"}}]}`))
	case "/api/v1/cluster-workflow-templates/global-template":
		_, _ = w.Write([]byte(`{"metadata":{"name":"global-template"},"spec":{"entrypoint":"main","templates":[{"name":"main"}]}}`))
	default:
		http.Error(w, "unexpected Argo path: "+r.URL.Path, http.StatusNotFound)
	}
}
