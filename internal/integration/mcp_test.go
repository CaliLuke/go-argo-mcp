package integration

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestNewReadAndLintToolsReportMissingArgoConfigurationThroughSDK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sdkServer, err := mcpargo.NewSDKServer(service.NewArgoService(service.ArgoServiceConfig{DefaultNamespace: "argo-ci"}), &mcpargo.SDKServerOptions{
		Adapter: &mcpargo.MCPAdapterOptions{StructuredStreamJSON: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(sdkServer.Handler)
	defer httpServer.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "missing-config-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools := []struct {
		name string
		args map[string]any
	}{
		{"get_workflow_nodes", map[string]any{"name": "build"}},
		{"get_workflow_events", map[string]any{"name": "build"}},
		{"list_archived_workflows", map[string]any{}},
		{"get_archived_workflow", map[string]any{"uid": "archive-1"}},
		{"get_workflow_artifacts", map[string]any{"name": "build"}},
		{"lint_workflow", map[string]any{"manifest_json": `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"generateName":"build-"},"spec":{}}`}},
		{"lint_workflow_template", map[string]any{"manifest_json": `{"apiVersion":"argoproj.io/v1alpha1","kind":"WorkflowTemplate","metadata":{"name":"build"},"spec":{}}`}},
	}
	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil || result == nil || !result.IsError {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			encoded, _ := json.Marshal(result.Content)
			if !strings.Contains(string(encoded), "ARGO_BASE_URL") {
				t.Fatalf("missing configuration guidance: %s", encoded)
			}
		})
	}
}

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
	if len(listed.Tools) != 30 {
		t.Fatalf("expected 30 tools, got %d", len(listed.Tools))
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
		{"get_workflow_nodes", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"get_workflow_events", map[string]any{"namespace": "argo-ci", "name": "build-123", "duration_seconds": 1}},
		{"list_archived_workflows", map[string]any{"namespace": "argo-ci"}},
		{"get_archived_workflow", map[string]any{"namespace": "argo-ci", "uid": "archive-1"}},
		{"get_workflow_artifacts", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"lint_workflow", map[string]any{"namespace": "argo-ci", "manifest_json": `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"generateName":"build-"},"spec":{"entrypoint":"main","templates":[{"name":"main","container":{"image":"alpine:3.22","command":["echo"],"args":["ok"]}}]}}`}},
		{"lint_workflow_template", map[string]any{"namespace": "argo-ci", "manifest_json": `{"apiVersion":"argoproj.io/v1alpha1","kind":"WorkflowTemplate","metadata":{"name":"build"},"spec":{}}`}},
		{"submit_workflow_template", map[string]any{"namespace": "argo-ci", "template_name": "build-template"}},
		{"suspend_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"resume_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"resubmit_workflow", map[string]any{"namespace": "argo-ci", "name": "build-123"}},
		{"trigger_cron_workflow", map[string]any{"namespace": "argo-ci", "name": "nightly"}},
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
			if call.name == "lint_workflow" {
				if got := structuredMap(t, result)["name"]; got != "build" {
					t.Fatalf("lint result name=%#v, want Argo response name", got)
				}
			}
			if call.name == "terminate_workflow" || call.name == "retry_workflow" || call.name == "toggle_cron_suspension" || call.name == "submit_workflow_template" || call.name == "suspend_workflow" || call.name == "resume_workflow" || call.name == "resubmit_workflow" || call.name == "trigger_cron_workflow" {
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
				if call.name != "submit_workflow_template" && call.name != "resubmit_workflow" && call.name != "trigger_cron_workflow" {
					setting := "MCP_ALLOW_MUTATIONS=true"
					if call.name == "terminate_workflow" || call.name == "retry_workflow" {
						setting = "MCP_ALLOW_DESTRUCTIVE=true"
					}
					instructions, _ := action["instructions"].(string)
					if !strings.Contains(instructions, setting) || !strings.Contains(instructions, "No Argo request was made") {
						t.Fatalf("denied mutation should name the setting and side-effect status: %#v", action)
					}
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
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		argoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"name":"created-123","namespace":"argo-ci"},"status":{"phase":"Pending"}}`))
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
	retryPreview := call("retry_workflow", map[string]any{"name": "build-123", "restart_successful": true})
	retryToken, _ := retryPreview["confirmation_token"].(string)
	if retryPreview["status"] != "dry_run" || retryToken == "" || argoCalls.Load() != 1 {
		t.Fatalf("retry preview failed: %#v calls=%d", retryPreview, argoCalls.Load())
	}
	retried := call("retry_workflow", map[string]any{"name": "build-123", "restart_successful": true, "dry_run": false, "confirmation_token": retryToken})
	if retried["status"] != "ok" || argoCalls.Load() != 2 {
		t.Fatalf("confirmed retry did not call Argo once: %#v, calls=%d", retried, argoCalls.Load())
	}
	toggled := call("toggle_cron_suspension", map[string]any{"name": "nightly", "suspend": true})
	if toggled["status"] != "ok" || argoCalls.Load() != 3 {
		t.Fatalf("cron suspension did not call Argo once: %#v, calls=%d", toggled, argoCalls.Load())
	}
	for index, mutation := range []struct {
		name string
		args map[string]any
	}{
		{"submit_workflow_template", map[string]any{"template_name": "build-template", "parameters": map[string]any{"image": "alpine"}}},
		{"suspend_workflow", map[string]any{"name": "build-123"}},
		{"resume_workflow", map[string]any{"name": "build-123"}},
		{"resubmit_workflow", map[string]any{"name": "build-123", "memoized": true}},
		{"trigger_cron_workflow", map[string]any{"name": "nightly"}},
	} {
		output := call(mutation.name, mutation.args)
		if output["status"] != "ok" || argoCalls.Load() != int32(4+index) {
			t.Fatalf("%s output=%#v calls=%d", mutation.name, output, argoCalls.Load())
		}
		workflow, ok := output["workflow"].(map[string]any)
		if mutation.name != "suspend_workflow" && mutation.name != "resume_workflow" && (!ok || workflow["name"] != "created-123") {
			t.Fatalf("%s lost Argo-created name: %#v", mutation.name, output["workflow"])
		}
	}
}

func TestExpandedGeneratedBoundsRejectBeforeArgo(t *testing.T) {
	var calls atomic.Int32
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); simulatedArgo(w, r) }))
	defer argo.Close()
	svc := service.NewArgoService(service.ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: argo.URL}), DefaultNamespace: "argo-ci", Policy: service.Policy{AllowMutations: true, AllowDestructive: true, RequireConfirmation: false}})
	server, err := mcpargo.NewSDKServer(svc, &mcpargo.SDKServerOptions{Adapter: &mcpargo.MCPAdapterOptions{StructuredStreamJSON: true}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "bounds", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, tool := range []string{"get_workflow_nodes", "get_workflow_events", "list_archived_workflows", "get_workflow_artifacts"} {
		for _, invalid := range []any{0, -1, 201, nil} {
			before := calls.Load()
			args := map[string]any{"limit": invalid}
			if tool != "list_archived_workflows" {
				args["name"] = "build-123"
			}
			result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
			if callErr != nil || result == nil || !result.IsError || calls.Load() != before {
				t.Fatalf("%s limit=%#v result=%#v err=%v calls=%d/%d", tool, invalid, result, callErr, calls.Load(), before)
			}
		}
	}
	for _, invalid := range []any{0, -1, 11, nil} {
		before := calls.Load()
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_workflow_events", Arguments: map[string]any{"name": "build-123", "duration_seconds": invalid}})
		if callErr != nil || result == nil || !result.IsError || calls.Load() != before {
			t.Fatalf("duration=%#v result=%#v err=%v calls=%d/%d", invalid, result, callErr, calls.Load(), before)
		}
	}
	before := calls.Load()
	result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "retry_workflow", Arguments: map[string]any{"name": "build-123", "dry_run": nil}})
	if callErr != nil || result == nil || !result.IsError || calls.Load() != before {
		t.Fatalf("null retry dry_run result=%#v err=%v calls=%d/%d", result, callErr, calls.Load(), before)
	}
}

func TestExpandedDefaultsAndRecoveryExamplesAreReplayable(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/example":
			_, _ = w.Write([]byte(`{"metadata":{"name":"example","namespace":"argo-ci","uid":"u-1"},"status":{"nodes":{}}}`))
		case "/api/v1/stream/events/argo-ci":
			if r.URL.Query().Get("listOptions.timeoutSeconds") != "7" {
				t.Errorf("event default timeout=%q", r.URL.Query().Get("listOptions.timeoutSeconds"))
			}
		case "/api/v1/archived-workflows":
			if r.URL.Query().Get("listOptions.limit") != "50" {
				t.Errorf("archive default limit=%q", r.URL.Query().Get("listOptions.limit"))
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, 404)
		}
	}))
	defer argo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		catalog[tool.Name] = tool
	}
	tools := []struct {
		name     string
		args     map[string]any
		defaults []string
	}{{"get_workflow_nodes", map[string]any{"name": "example"}, []string{`"default":0`, `"default":50`}}, {"get_workflow_events", map[string]any{"name": "example"}, []string{`"default":50`, `"default":2`}}, {"list_archived_workflows", map[string]any{}, []string{`"default":50`}}, {"get_workflow_artifacts", map[string]any{"name": "example"}, []string{`"default":0`, `"default":50`}}}
	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			schema, _ := json.Marshal(catalog[tc.name].InputSchema)
			for _, want := range tc.defaults {
				if !strings.Contains(string(schema), want) {
					t.Fatalf("schema missing %s: %s", want, schema)
				}
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil || result.IsError {
				t.Fatalf("omitted defaults result=%#v err=%v", result, err)
			}
			invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: map[string]any{"unexpected": true}})
			if err != nil || !invalid.IsError || len(invalid.Content) == 0 {
				t.Fatalf("invalid result=%#v err=%v", invalid, err)
			}
			message := invalid.Content[0].(*mcp.TextContent).Text
			index := strings.LastIndex(message, "Example: ")
			if index < 0 {
				t.Fatalf("missing recovery example: %s", message)
			}
			var example map[string]any
			if decodeErr := json.Unmarshal([]byte(strings.TrimSpace(message[index+len("Example: "):])), &example); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			replayed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: example})
			if err != nil || replayed.IsError {
				t.Fatalf("recovery=%#v result=%#v err=%v", example, replayed, err)
			}
		})
	}
}

func TestDiagnosticEmptyChildrenAndNewErrorClassThroughSDK(t *testing.T) {
	status := http.StatusOK
	children := make([]string, 201)
	for i := range children {
		children[i] = fmt.Sprintf("child-%03d", i)
	}
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			http.Error(w, "SECRET", status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"name": "build"}, "status": map[string]any{"nodes": map[string]any{
			"a": map[string]any{"id": "a", "name": "a", "type": "Pod"},
			"b": map[string]any{"id": "b", "name": "b", "type": "Pod", "message": strings.Repeat("m", 4097), "children": children},
		}}})
	}))
	defer argo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_workflow_nodes", Arguments: map[string]any{"name": "build"}})
	if err != nil || result.IsError {
		t.Fatalf("nodes result=%#v err=%v", result, err)
	}
	nodes := structuredMap(t, result)["nodes"].([]any)
	emptyChildren, ok := nodes[0].(map[string]any)["children"].([]any)
	if !ok || emptyChildren == nil || len(emptyChildren) != 0 {
		t.Fatalf("children=%#v", nodes[0])
	}
	boundedNode := nodes[1].(map[string]any)
	boundedChildren, ok := boundedNode["children"].([]any)
	if !ok || len(boundedChildren) != 200 || len(boundedNode["message"].(string)) != 4096 || structuredMap(t, result)["fields_truncated"] != true {
		t.Fatalf("bounded node=%#v result=%#v", boundedNode, structuredMap(t, result))
	}
	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "argo.input.invalid"},
		{http.StatusUnprocessableEntity, "argo.input.invalid"},
		{http.StatusConflict, "argo.state.invalid"},
		{http.StatusNotFound, "argo.resource.not_found"},
		{http.StatusUnauthorized, "argo.access.denied"},
		{http.StatusForbidden, "argo.access.denied"},
		{http.StatusTooManyRequests, "argo.api.retry"},
		{http.StatusServiceUnavailable, "argo.api.retry"},
	} {
		status = tc.status
		result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_workflow_nodes", Arguments: map[string]any{"name": "build"}})
		if err != nil || result == nil || !result.IsError {
			t.Fatalf("status %d result=%#v err=%v", tc.status, result, err)
		}
		encoded, _ := json.Marshal(result.Content)
		if !strings.Contains(string(encoded), tc.code) || !strings.Contains(string(encoded), "Recovery:") || strings.Contains(string(encoded), "SECRET") {
			t.Fatalf("status %d unsafe/wrong error: %s", tc.status, encoded)
		}
	}
}

func simulatedArgo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/workflows/argo-ci":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"nightly-001","namespace":"argo-ci"},"status":{"phase":"Succeeded","progress":"1/1","startedAt":"2026-07-21T10:00:00Z","finishedAt":"2026-07-21T10:01:00Z"}}]}`))
	case "/api/v1/workflows/argo-ci/build-123":
		_, _ = w.Write([]byte(`{"metadata":{"name":"build-123","namespace":"argo-ci","uid":"wf-uid","labels":{"app":"build"}},"spec":{"arguments":{"parameters":[{"name":"image","value":"alpine"}]}},"status":{"phase":"Succeeded","progress":"1/1","startedAt":"2026-07-21T10:00:00Z","finishedAt":"2026-07-21T10:01:00Z","nodes":{}}}`))
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
	case "/api/v1/stream/events/argo-ci":
		_, _ = w.Write([]byte("{\"result\":{\"type\":\"Normal\",\"reason\":\"Completed\",\"count\":1}}\n"))
	case "/api/v1/archived-workflows":
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"archived","namespace":"argo-ci","uid":"archive-1"},"status":{"phase":"Succeeded"}}]}`))
	case "/api/v1/archived-workflows/archive-1":
		_, _ = w.Write([]byte(`{"metadata":{"name":"archived","namespace":"argo-ci","uid":"archive-1"},"status":{"phase":"Succeeded"}}`))
	case "/api/v1/workflows/argo-ci/lint":
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"}}`))
	case "/api/v1/workflow-templates/argo-ci/lint":
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"}}`))
	default:
		http.Error(w, "unexpected Argo path: "+r.URL.Path, http.StatusNotFound)
	}
}
