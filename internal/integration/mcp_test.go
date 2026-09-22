package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	toolsByName := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		toolsByName[tool.Name] = tool
	}
	if strings.Contains(toolsByName["list_workflows"].Description, "namespace(s)") || strings.Contains(toolsByName["list_cron_workflows"].Description, "namespace(s)") {
		t.Fatalf("list tool descriptions should describe the single namespace input: workflows=%q cron=%q", toolsByName["list_workflows"].Description, toolsByName["list_cron_workflows"].Description)
	}
	if strings.Contains(toolsByName["get_cron_workflow"].Description, "last execution") || !strings.Contains(toolsByName["get_cron_workflow"].Description, "next scheduled") {
		t.Fatalf("CronWorkflow description should match the returned scheduling fields: %q", toolsByName["get_cron_workflow"].Description)
	}
	getWorkflowSchema, err := json.Marshal(toolsByName["get_workflow"].InputSchema)
	if err != nil {
		t.Fatalf("encode get_workflow input schema: %v", err)
	}
	if !strings.Contains(string(getWorkflowSchema), "ARGO_NAMESPACE") || !strings.Contains(string(getWorkflowSchema), "list_workflows") {
		t.Fatalf("get_workflow schema should explain its default and discovery path: %s", getWorkflowSchema)
	}
	actionSchema, err := json.Marshal(toolsByName["terminate_workflow"].OutputSchema)
	if err != nil {
		t.Fatalf("encode terminate_workflow output schema: %v", err)
	}
	if strings.Contains(string(actionSchema), "confirmation_required") || !strings.Contains(string(actionSchema), `"enum":["ok","dry_run","denied"]`) {
		t.Fatalf("action status schema should contain only statuses the server returns: %s", actionSchema)
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
			if call.name == "terminate_workflow" || call.name == "retry_workflow" || call.name == "toggle_cron_suspension" {
				encoded, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatalf("encode action result: %v", err)
				}
				var action map[string]any
				if err := json.Unmarshal(encoded, &action); err != nil {
					t.Fatalf("decode action result: %v", err)
				}
				if action["status"] != "denied" {
					t.Fatalf("default policy must deny mutation: %#v", action)
				}
				setting := "MCP_ALLOW_MUTATIONS=true"
				if call.name == "terminate_workflow" {
					setting = "MCP_ALLOW_DESTRUCTIVE=true"
				}
				instructions, _ := action["instructions"].(string)
				if !strings.Contains(instructions, setting) || !strings.Contains(instructions, "No Argo request was made") {
					t.Fatalf("denied mutation should name the setting and side-effect status: %#v", action)
				}
			}
			if call.name == "list_cron_workflows" || call.name == "get_cron_workflow" {
				encoded, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatalf("encode structured result: %v", err)
				}
				var output map[string]any
				if err := json.Unmarshal(encoded, &output); err != nil {
					t.Fatalf("decode structured result: %v", err)
				}
				if call.name == "list_cron_workflows" {
					items, ok := output["cron_workflows"].([]any)
					if !ok || len(items) != 1 {
						t.Fatalf("missing cron workflows: %#v", output)
					}
					output = items[0].(map[string]any)
				}
				if output["schedule"] != "0 0 * * *" || output["timezone"] != "America/Los_Angeles" {
					t.Fatalf("cron schedule missing from MCP response: %#v", output)
				}
				schedules, ok := output["schedules"].([]any)
				if !ok || len(schedules) != 2 || schedules[1] != "0 12 * * *" {
					t.Fatalf("multiple schedules missing from MCP response: %#v", output)
				}
			}
		})
	}
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"get_workflow", map[string]any{"namespace": "argo-ci", "name": "missing"}},
		{"get_cron_history", map[string]any{"namespace": "argo-ci", "name": "missing"}},
	} {
		t.Run(call.name+"_missing", func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
			if err != nil {
				t.Fatalf("CallTool returned transport error: %v", err)
			}
			if !result.IsError || len(result.Content) == 0 {
				t.Fatalf("missing resource should return not-found remedy: %#v", result)
			}
			message := result.Content[0].(*mcp.TextContent).Text
			resource, discoveryTool := `Workflow "missing"`, "list_workflows"
			if call.name == "get_cron_history" {
				resource, discoveryTool = `CronWorkflow "missing"`, "list_cron_workflows"
			}
			if !strings.Contains(message, "argo.resource.not_found") || !strings.Contains(message, resource) || !strings.Contains(message, `namespace "argo-ci"`) || !strings.Contains(message, discoveryTool) {
				t.Fatalf("missing resource remedy lacks exact context and recovery tool: %s", message)
			}
		})
	}
}

func TestMutationToolsCallArgoOnlyWhenEnabledAndConfirmed(t *testing.T) {
	var argoCalls atomic.Int32
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		argoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer argo.Close()
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: argo.URL}),
		DefaultNamespace: "argo-ci",
		Policy: service.Policy{
			AllowMutations:      true,
			AllowDestructive:    true,
			RequireConfirmation: true,
			AllowedNamespaces:   []string{"argo-ci"},
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
	client := mcp.NewClient(&mcp.Implementation{Name: "mutation-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		HTTPClient:           &http.Client{Timeout: 5 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer session.Close()

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s failed: result=%#v err=%v", name, result, err)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("encode %s result: %v", name, err)
		}
		var output map[string]any
		if err := json.Unmarshal(encoded, &output); err != nil {
			t.Fatalf("decode %s result: %v", name, err)
		}
		return output
	}

	preview := call("terminate_workflow", map[string]any{"name": "build-123", "reason": "stuck"})
	if preview["status"] != "dry_run" || argoCalls.Load() != 0 {
		t.Fatalf("termination preview changed Argo: %#v, calls=%d", preview, argoCalls.Load())
	}
	token, ok := preview["confirmation_token"].(string)
	if !ok || token == "" {
		t.Fatalf("termination preview lacked token: %#v", preview)
	}
	confirmed := call("terminate_workflow", map[string]any{"name": "build-123", "reason": "stuck", "dry_run": false, "confirmation_token": token})
	if confirmed["status"] != "ok" || argoCalls.Load() != 1 {
		t.Fatalf("confirmed termination did not call Argo once: %#v, calls=%d", confirmed, argoCalls.Load())
	}
	retried := call("retry_workflow", map[string]any{"name": "build-123", "restart_successful": true})
	if retried["status"] != "ok" || argoCalls.Load() != 2 {
		t.Fatalf("retry did not call Argo once: %#v, calls=%d", retried, argoCalls.Load())
	}
	toggled := call("toggle_cron_suspension", map[string]any{"name": "nightly", "suspend": true})
	if toggled["status"] != "ok" || argoCalls.Load() != 3 {
		t.Fatalf("cron suspension did not call Argo once: %#v, calls=%d", toggled, argoCalls.Load())
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
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"nightly","namespace":"argo-ci"},"spec":{"schedules":["0 0 * * *","0 12 * * *"],"timezone":"America/Los_Angeles","suspend":false}}]}`))
	case "/api/v1/cron-workflows/argo-ci/nightly":
		_, _ = w.Write([]byte(`{"metadata":{"name":"nightly","namespace":"argo-ci"},"spec":{"schedules":["0 0 * * *","0 12 * * *"],"timezone":"America/Los_Angeles","suspend":false},"status":{"lastScheduledTime":"2026-07-21T00:00:00Z"}}`))
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
