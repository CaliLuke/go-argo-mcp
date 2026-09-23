package mcpaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

const maxSummaryLength = 2000

type Record struct {
	Tool       string          `json:"tool"`
	Arguments  json.RawMessage `json:"arguments"`
	Status     string          `json:"status"`
	Summary    string          `json:"summary,omitempty"`
	DurationMS int64           `json:"duration_ms"`
	ExecutedAt time.Time       `json:"executed_at"`
}

type Logger struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{file: file}, nil
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.file.Close()
}

func (l *Logger) Interceptor() mcpargo.ToolCallInterceptor {
	return func(ctx context.Context, info mcpargo.ToolCallInterceptorInfo, payload *mcpargo.ToolsCallPayload, next mcpargo.ToolCallHandler) (*mcpargo.ToolsCallResult, error) {
		start := time.Now()
		result, err := next(ctx, payload)
		status := "SUCCESS"
		if err != nil || result != nil && result.IsError != nil && *result.IsError {
			status = "ERROR"
		}
		summary := resultSummary(result)
		if summary == "" && err != nil {
			summary = "tool call failed"
		}
		_ = l.write(Record{
			Tool:       info.Tool(),
			Arguments:  redactArguments(info.Tool(), info.RawArguments()),
			Status:     status,
			Summary:    truncate(summary, maxSummaryLength),
			DurationMS: time.Since(start).Milliseconds(),
			ExecutedAt: time.Now().UTC(),
		})
		return result, err
	}
}

func (l *Logger) write(record Record) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("audit logger is closed")
	}
	return json.NewEncoder(l.file).Encode(record)
}

func resultSummary(result *mcpargo.ToolsCallResult) string {
	if result == nil {
		return ""
	}
	raw := bytes.TrimSpace(result.StructuredContent)
	if len(raw) == 0 {
		for _, item := range result.Content {
			if item != nil && item.Text != nil && json.Valid([]byte(*item.Text)) {
				raw = []byte(*item.Text)
				break
			}
		}
	}
	var structured map[string]any
	if err := json.Unmarshal(raw, &structured); err != nil {
		return ""
	}
	safe := make(map[string]any)
	for _, key := range []string{"status", "count", "namespace", "name"} {
		if value, ok := structured[key]; ok {
			safe[key] = value
		}
	}
	if len(safe) == 0 {
		return ""
	}
	encoded, err := json.Marshal(safe)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func redactArguments(tool string, raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{"redacted":"invalid JSON"}`)
	}
	redactValue(value, auditRedactionPolicy(tool))
	redacted, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"redacted":"unavailable"}`)
	}
	return redacted
}

type redactionPolicy struct {
	manifest   bool
	parameters bool
}

func auditRedactionPolicy(tool string) redactionPolicy {
	switch tool {
	case "lint_workflow", "lint_workflow_template":
		return redactionPolicy{manifest: true}
	case "submit_workflow_template", "resubmit_workflow", "trigger_cron_workflow":
		return redactionPolicy{parameters: true}
	default:
		return redactionPolicy{}
	}
}

func redactValue(value any, policy redactionPolicy) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if policy.manifest && lower == "manifest_json" {
				typed[key] = "[REDACTED]"
				continue
			}
			if policy.parameters && lower == "parameters" {
				typed[key] = summarizeParameters(child)
				continue
			}
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
				typed[key] = "[REDACTED]"
				continue
			}
			redactValue(child, policy)
		}
	case []any:
		for _, child := range typed {
			redactValue(child, policy)
		}
	}
}

func summarizeParameters(value any) any {
	parameters, ok := value.(map[string]any)
	if !ok {
		return "[REDACTED]"
	}
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return map[string]any{
		"count": len(names),
		"names": names,
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
