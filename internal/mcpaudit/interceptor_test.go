package mcpaudit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	loom "github.com/CaliLuke/loom/pkg"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

type testInfo struct {
	tool string
	args loom.JSONValue
}

func (i testInfo) Service() string                    { return "argo" }
func (i testInfo) Method() string                     { return "tools/call" }
func (i testInfo) CallType() loom.InterceptorCallType { return loom.InterceptorUnary }
func (i testInfo) RawPayload() any                    { return nil }
func (i testInfo) Tool() string                       { return i.tool }
func (i testInfo) RawArguments() loom.JSONValue       { return i.args }

func TestInterceptorWritesJSONLAndRedactsConfirmationToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer audit.Close()
	info := testInfo{tool: "terminate_workflow", args: loom.JSONValue(`{"namespace":"argo-ci","confirmation_token":"secret"}`)}
	interceptor := audit.Interceptor()
	result, err := interceptor(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			message := "terminated"
			return &mcpargo.ToolsCallResult{Content: []*mcpargo.ContentItem{{Type: "text", Text: &message}}}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if result == nil {
		t.Fatal("interceptor returned nil result")
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("audit must redact confirmation token: %s", data)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode audit record: %v", err)
	}
	if record.Tool != "terminate_workflow" || record.Status != "SUCCESS" || record.Summary != "" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestInterceptorDoesNotLogConfirmationTokenFromResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer audit.Close()
	info := testInfo{tool: "terminate_workflow", args: loom.JSONValue(`{"namespace":"argo-ci","name":"build-123"}`)}
	_, err = audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			response := loom.JSONValue(`{"status":"dry_run","confirmation_token":"secret-token","preview":"private preview"}`)
			return &mcpargo.ToolsCallResult{StructuredContent: response}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "private preview") {
		t.Fatalf("audit included sensitive response content: %s", data)
	}
}

func TestInterceptorUsesStructuredContentAsSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer audit.Close()

	info := testInfo{tool: "list_workflows", args: loom.JSONValue(`{}`)}
	result, err := audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return &mcpargo.ToolsCallResult{StructuredContent: loom.JSONValue(`{"count":2}`)}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if result == nil {
		t.Fatal("interceptor returned nil result")
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode audit record: %v", err)
	}
	if record.Summary != `{"count":2}` {
		t.Fatalf("unexpected structured summary: %q", record.Summary)
	}
}

func TestInterceptorRedactsLintManifestFromSuccessfulJSONLRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	info := testInfo{
		tool: "lint_workflow",
		args: loom.JSONValue(`{
			"namespace":"argo-ci",
			"manifest_json":"{\"apiVersion\":\"argoproj.io/v1alpha1\",\"kind\":\"Workflow\",\"metadata\":{\"name\":\"private-build\"},\"spec\":{\"templates\":[{\"container\":{\"env\":[{\"name\":\"API_KEY\",\"value\":\"manifest-secret-value\"}]}}]}}"
		}`),
	}

	result, err := audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return &mcpargo.ToolsCallResult{StructuredContent: loom.JSONValue(`{
				"status":"valid",
				"namespace":"argo-ci",
				"name":"private-build",
				"manifest_json":"manifest-secret-value"
			}`)}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if result == nil {
		t.Fatal("interceptor returned nil result")
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	record := readAuditRecord(t, path)
	if record.Tool != "lint_workflow" || record.Status != "SUCCESS" {
		t.Fatalf("unexpected record context: %#v", record)
	}
	if record.Summary != `{"name":"private-build","namespace":"argo-ci","status":"valid"}` {
		t.Fatalf("unexpected safe summary: %q", record.Summary)
	}
	var arguments map[string]any
	if err := json.Unmarshal(record.Arguments, &arguments); err != nil {
		t.Fatalf("decode arguments: %v", err)
	}
	if arguments["namespace"] != "argo-ci" || arguments["manifest_json"] != "[REDACTED]" {
		t.Fatalf("unexpected redacted arguments: %#v", arguments)
	}
	assertAuditOmits(t, path, "manifest-secret-value", "argoproj.io/v1alpha1", "templates")
}

func TestInterceptorRedactsParameterValuesFromErroredJSONLRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	info := testInfo{
		tool: "submit_workflow_template",
		args: loom.JSONValue(`{
			"namespace":"argo-ci",
			"template_name":"release",
			"parameters":{
				"region":"value-containing-a-secret",
				"image":"registry.example/private-image"
			},
			"request":{"password":"nested-password","details":{"access_token":"nested-token"}}
		}`),
	}
	isError := true

	result, err := audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return &mcpargo.ToolsCallResult{
				IsError: &isError,
				StructuredContent: loom.JSONValue(`{
					"status":"ERROR",
					"namespace":"argo-ci",
					"name":"release",
					"parameters":{"region":"value-containing-a-secret"}
				}`),
			}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if result == nil {
		t.Fatal("interceptor returned nil result")
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	record := readAuditRecord(t, path)
	if record.Tool != "submit_workflow_template" || record.Status != "ERROR" {
		t.Fatalf("unexpected record context: %#v", record)
	}
	if record.Summary != `{"name":"release","namespace":"argo-ci","status":"ERROR"}` {
		t.Fatalf("unexpected safe summary: %q", record.Summary)
	}
	var arguments map[string]any
	if err := json.Unmarshal(record.Arguments, &arguments); err != nil {
		t.Fatalf("decode arguments: %v", err)
	}
	wantParameters := map[string]any{"count": float64(2), "names": []any{"image", "region"}}
	if !reflect.DeepEqual(arguments["parameters"], wantParameters) {
		t.Fatalf("unexpected parameter audit summary: got %#v, want %#v", arguments["parameters"], wantParameters)
	}
	request, ok := arguments["request"].(map[string]any)
	if !ok {
		t.Fatalf("request is not an object: %#v", arguments["request"])
	}
	details, ok := request["details"].(map[string]any)
	if !ok {
		t.Fatalf("request details are not an object: %#v", request["details"])
	}
	if request["password"] != "[REDACTED]" || details["access_token"] != "[REDACTED]" {
		t.Fatalf("nested secret fields were not redacted: %#v", request)
	}
	if arguments["namespace"] != "argo-ci" || arguments["template_name"] != "release" {
		t.Fatalf("safe operation context was not preserved: %#v", arguments)
	}
	assertAuditOmits(t, path, "value-containing-a-secret", "registry.example/private-image", "nested-password", "nested-token")
}

func TestInterceptorRedactsResubmitParametersWhenHandlerReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	info := testInfo{
		tool: "resubmit_workflow",
		args: loom.JSONValue(`{"namespace":"argo-ci","name":"build-123","parameters":{"safe_name":"raw-parameter-secret"}}`),
	}

	result, err := audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return nil, errors.New("upstream request failed with raw-parameter-secret")
		})
	if err == nil {
		t.Fatal("interceptor returned nil error")
	}
	if result != nil {
		t.Fatalf("interceptor returned unexpected result: %#v", result)
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	record := readAuditRecord(t, path)
	if record.Tool != "resubmit_workflow" || record.Status != "ERROR" || record.Summary != "tool call failed" {
		t.Fatalf("unexpected error record: %#v", record)
	}
	var arguments map[string]any
	if err := json.Unmarshal(record.Arguments, &arguments); err != nil {
		t.Fatalf("decode arguments: %v", err)
	}
	wantParameters := map[string]any{"count": float64(1), "names": []any{"safe_name"}}
	if !reflect.DeepEqual(arguments["parameters"], wantParameters) {
		t.Fatalf("unexpected parameter audit summary: got %#v, want %#v", arguments["parameters"], wantParameters)
	}
	if arguments["namespace"] != "argo-ci" || arguments["name"] != "build-123" {
		t.Fatalf("safe operation context was not preserved: %#v", arguments)
	}
	assertAuditOmits(t, path, "raw-parameter-secret")
}

func TestInterceptorRedactsWorkflowTemplateLintManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	info := testInfo{
		tool: "lint_workflow_template",
		args: loom.JSONValue(`{"cluster_scope":true,"manifest_json":"{\"kind\":\"ClusterWorkflowTemplate\",\"private\":\"template-manifest-secret\"}"}`),
	}

	_, err = audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return &mcpargo.ToolsCallResult{StructuredContent: loom.JSONValue(`{"status":"valid","name":"release"}`)}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	record := readAuditRecord(t, path)
	var arguments map[string]any
	if err := json.Unmarshal(record.Arguments, &arguments); err != nil {
		t.Fatalf("decode arguments: %v", err)
	}
	if arguments["cluster_scope"] != true || arguments["manifest_json"] != "[REDACTED]" {
		t.Fatalf("unexpected redacted arguments: %#v", arguments)
	}
	assertAuditOmits(t, path, "template-manifest-secret", "ClusterWorkflowTemplate")
}

func TestInterceptorRedactsCronTriggerParameterValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	info := testInfo{
		tool: "trigger_cron_workflow",
		args: loom.JSONValue(`{"namespace":"argo-ci","name":"nightly","parameters":{"ordinary_name":"cron-parameter-secret"}}`),
	}

	_, err = audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: info.tool},
		func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
			return &mcpargo.ToolsCallResult{StructuredContent: loom.JSONValue(`{"status":"submitted","namespace":"argo-ci","name":"nightly-abc"}`)}, nil
		})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if closeErr := audit.Close(); closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}

	record := readAuditRecord(t, path)
	var arguments map[string]any
	if err := json.Unmarshal(record.Arguments, &arguments); err != nil {
		t.Fatalf("decode arguments: %v", err)
	}
	wantParameters := map[string]any{"count": float64(1), "names": []any{"ordinary_name"}}
	if !reflect.DeepEqual(arguments["parameters"], wantParameters) {
		t.Fatalf("unexpected parameter audit summary: got %#v, want %#v", arguments["parameters"], wantParameters)
	}
	if arguments["namespace"] != "argo-ci" || arguments["name"] != "nightly" {
		t.Fatalf("safe operation context was not preserved: %#v", arguments)
	}
	assertAuditOmits(t, path, "cron-parameter-secret")
}

func readAuditRecord(t *testing.T, path string) Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode audit record: %v", err)
	}
	return record
}

func assertAuditOmits(t *testing.T, path string, forbidden ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(data), value) {
			t.Fatalf("audit record contains %q: %s", value, data)
		}
	}
}

func TestDiagnosticContentsNeverEnterAuditSummary(t *testing.T) {
	for _, tool := range []string{"read_workflow_artifact", "get_workflow_logs", "get_resource_spec", "get_workflow_nodes", "get_workflow_pod_diagnostics"} {
		t.Run(tool, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "audit.jsonl")
			audit, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer audit.Close()
			info := testInfo{tool: tool, args: loom.JSONValue(`{"namespace":"argo-ci","name":"build"}`)}
			_, err = audit.Interceptor()(context.Background(), info, &mcpargo.ToolsCallPayload{Name: tool}, func(context.Context, *mcpargo.ToolsCallPayload) (*mcpargo.ToolsCallResult, error) {
				return &mcpargo.ToolsCallResult{StructuredContent: loom.JSONValue(`{"name":"build","namespace":"argo-ci","count":1,"text":"private-artifact","logs":"private-log","spec_json":"private-spec","input_parameters":{"a":"private-input"},"output_parameters":{"b":"private-output"},"pods":[{"events":[{"message":"private-event"}]}]}`)}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private-") {
				t.Fatal("diagnostic payload leaked to audit")
			}
			var record Record
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			if record.Status != "SUCCESS" || !strings.Contains(record.Summary, "build") {
				t.Fatalf("safe audit outcome missing: %+v", record)
			}
		})
	}
}
