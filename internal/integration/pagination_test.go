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
	"github.com/CaliLuke/go-argo-mcp/internal/mcpvalidation"
	"github.com/CaliLuke/go-argo-mcp/internal/service"
)

const integrationCursor = "  opaque+/=?& token\t"

func TestCollectionToolSchemasAndPageSequences(t *testing.T) {
	var argoCalls atomic.Int32
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		argoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/cron-workflows/argo-ci/nightly" {
			_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
			return
		}
		if got := r.URL.Query().Get("listOptions.limit"); got != "1" {
			t.Fatalf("MCP limit did not reach Argo: %q", got)
		}
		cursor := r.URL.Query().Get("listOptions.continue")
		if cursor != "" && cursor != integrationCursor && cursor != "empty" {
			t.Fatalf("MCP changed opaque cursor: %q", cursor)
		}
		item := `{"metadata":{"name":"first","namespace":"argo-ci"},"status":{"phase":"Running"}}`
		switch r.URL.Path {
		case "/api/v1/cron-workflows/argo-ci":
			item = `{"metadata":{"name":"first","namespace":"argo-ci"},"spec":{"suspend":true}}`
		case "/api/v1/workflow-templates/argo-ci", "/api/v1/cluster-workflow-templates":
			item = `{"metadata":{"name":"first","namespace":"argo-ci"},"spec":{"entrypoint":"main"}}`
		}
		if cursor == "empty" {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		if cursor == integrationCursor {
			item = strings.Replace(item, "first", "second", 1)
			_, _ = w.Write([]byte(`{"items":[` + item + `]}`))
			return
		}
		cursorJSON, err := json.Marshal(integrationCursor)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"metadata":{"continue":` + string(cursorJSON) + `},"items":[` + item + `]}`))
	}))
	defer argo.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	tests := []struct {
		name        string
		arrayField  string
		baseArgs    map[string]any
		defaultText string
	}{
		{"list_workflows", "workflows", map[string]any{"namespace": "argo-ci", "status": "Running"}, "defaults to 50 when omitted"},
		{"list_cron_workflows", "cron_workflows", map[string]any{"namespace": "argo-ci", "suspended": true}, "defaults to 50 when omitted"},
		{"list_workflow_templates", "templates", map[string]any{"namespace": "argo-ci", "label_selector": "team=ci"}, "defaults to 50 when omitted"},
		{"list_cluster_workflow_templates", "templates", map[string]any{"label_selector": "team=ci"}, "defaults to 50 when omitted"},
		{"get_cron_history", "history", map[string]any{"namespace": "argo-ci", "name": "nightly"}, "defaults to 10 when omitted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := json.Marshal(tools[test.name].InputSchema)
			if err != nil || !strings.Contains(string(schema), test.defaultText) || strings.Contains(string(schema), `"default":`) || !strings.Contains(string(schema), `"minimum":1`) || !strings.Contains(string(schema), `"maximum":200`) || !strings.Contains(string(schema), `"continue"`) {
				t.Fatalf("pagination schema mismatch: %s err=%v", schema, err)
			}
			firstArgs := cloneArgs(test.baseArgs)
			firstArgs["limit"] = 1
			first := callCollectionTool(t, ctx, session, test.name, firstArgs)
			assertCollectionPage(t, first, test.arrayField, 1, true, integrationCursor)

			laterArgs := cloneArgs(firstArgs)
			laterArgs["continue"] = integrationCursor
			later := callCollectionTool(t, ctx, session, test.name, laterArgs)
			assertCollectionPage(t, later, test.arrayField, 1, false, "")

			emptyArgs := cloneArgs(firstArgs)
			emptyArgs["continue"] = "empty"
			empty := callCollectionTool(t, ctx, session, test.name, emptyArgs)
			assertCollectionPage(t, empty, test.arrayField, 0, false, "")
		})
	}
	if argoCalls.Load() == 0 {
		t.Fatal("page sequence did not call Argo")
	}
}

func TestCollectionToolsRejectExplicitInvalidLimitsBeforeArgo(t *testing.T) {
	var argoCalls atomic.Int32
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		argoCalls.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer argo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)
	tools := []struct {
		name string
		args map[string]any
	}{
		{"list_workflows", map[string]any{}},
		{"list_cron_workflows", map[string]any{}},
		{"list_workflow_templates", map[string]any{}},
		{"list_cluster_workflow_templates", map[string]any{}},
		{"get_cron_history", map[string]any{"name": "nightly"}},
	}
	for _, limit := range []int{0, -1, 201} {
		for _, tool := range tools {
			args := cloneArgs(tool.args)
			args["limit"] = limit
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool.name, Arguments: args})
			if err == nil && !result.IsError {
				t.Fatalf("%s accepted explicit limit %d: %#v", tool.name, limit, result.StructuredContent)
			}
		}
	}
	if argoCalls.Load() != 0 {
		t.Fatalf("invalid MCP limits made %d Argo requests, including history existence checks", argoCalls.Load())
	}
}

func TestOptionalCollectionToolsAcceptNormalizedEmptyArguments(t *testing.T) {
	var argoCalls atomic.Int32
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		argoCalls.Add(1)
		if got := r.URL.Query().Get("listOptions.limit"); got != "50" {
			t.Fatalf("default limit did not reach Argo: %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)
	tools := []string{"list_workflows", "list_cron_workflows", "list_workflow_templates", "list_cluster_workflow_templates"}
	inputs := []struct {
		name string
		args any
	}{
		{"omitted", nil},
		{"empty object", map[string]any{}},
		{"null", json.RawMessage("null")},
	}
	for _, input := range inputs {
		for _, tool := range tools {
			t.Run(input.name+"_"+tool, func(t *testing.T) {
				params := &mcp.CallToolParams{Name: tool}
				if input.name != "omitted" {
					params.Arguments = input.args
				}
				result, err := session.CallTool(ctx, params)
				if err != nil || result.IsError {
					t.Fatalf("normalized empty arguments failed: result=%#v err=%v", result, err)
				}
			})
		}
	}
	if argoCalls.Load() != int32(len(tools)*len(inputs)) {
		t.Fatalf("normalized inputs made %d Argo calls, want %d", argoCalls.Load(), len(tools)*len(inputs))
	}
}

func TestCollectionRecoveryExamplesAreValidInputs(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cron-workflows/") && strings.HasSuffix(r.URL.Path, "/example") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"example"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := newPaginationSession(t, ctx, argo.URL)
	for _, tool := range []string{"list_workflows", "list_cron_workflows", "list_workflow_templates", "list_cluster_workflow_templates", "get_cron_history"} {
		t.Run(tool, func(t *testing.T) {
			invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{"unexpected": true}})
			if err != nil || !invalid.IsError || len(invalid.Content) == 0 {
				t.Fatalf("expected structured input error: result=%#v err=%v", invalid, err)
			}
			message := invalid.Content[0].(*mcp.TextContent).Text
			marker := "Example: "
			index := strings.LastIndex(message, marker)
			if index < 0 {
				t.Fatalf("recovery lacks example: %s", message)
			}
			exampleJSON := strings.TrimSpace(message[index+len(marker):])
			var example map[string]any
			if decodeErr := json.Unmarshal([]byte(exampleJSON), &example); decodeErr != nil {
				t.Fatalf("decode recovery example %q: %v", exampleJSON, decodeErr)
			}
			if _, hasLimit := example["limit"]; hasLimit {
				t.Fatalf("recovery example includes an invalid synthesized limit: %s", exampleJSON)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: example})
			if err != nil || result.IsError {
				t.Fatalf("recovery example is not replayable: %s result=%#v err=%v", exampleJSON, result, err)
			}
		})
	}
}

func newPaginationSession(t *testing.T, ctx context.Context, argoURL string) *mcp.ClientSession {
	t.Helper()
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: argoURL}),
		DefaultNamespace: "argo-ci",
	})
	server, err := mcpargo.NewSDKServer(svc, &mcpargo.SDKServerOptions{Adapter: &mcpargo.MCPAdapterOptions{
		StructuredStreamJSON: true,
		ToolCallInterceptors: []mcpargo.ToolCallInterceptor{mcpvalidation.PaginationLimits()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "pagination-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callCollectionTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s failed: result=%#v err=%v", name, result, err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func assertCollectionPage(t *testing.T, output map[string]any, field string, count int, hasMore bool, cursor string) {
	t.Helper()
	items, ok := output[field].([]any)
	if !ok || len(items) != count || output["count"] != float64(count) || output["has_more"] != hasMore {
		t.Fatalf("unexpected collection output: %#v", output)
	}
	if cursor == "" {
		if _, exists := output["continue"]; exists {
			t.Fatalf("exhausted output retained a cursor: %#v", output)
		}
	} else if output["continue"] != cursor {
		t.Fatalf("cursor changed: %#v", output)
	}
}

func cloneArgs(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source)+2)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
