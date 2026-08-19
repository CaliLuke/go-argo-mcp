package mcpaudit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loom "github.com/CaliLuke/loom/pkg"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

type testInfo struct {
	tool string
	args json.RawMessage
}

func (i testInfo) Service() string                    { return "argo" }
func (i testInfo) Method() string                     { return "tools/call" }
func (i testInfo) CallType() loom.InterceptorCallType { return loom.InterceptorUnary }
func (i testInfo) RawPayload() any                    { return nil }
func (i testInfo) Tool() string                       { return i.tool }
func (i testInfo) RawArguments() json.RawMessage      { return i.args }

func TestInterceptorWritesJSONLAndRedactsConfirmationToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer audit.Close()
	info := testInfo{tool: "terminate_workflow", args: json.RawMessage(`{"namespace":"argo-ci","confirmation_token":"secret"}`)}
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
	if record.Tool != "terminate_workflow" || record.Status != "SUCCESS" || record.Summary != "terminated" {
		t.Fatalf("unexpected record: %#v", record)
	}
}
