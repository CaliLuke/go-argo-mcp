package mcpaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
			summary = err.Error()
		}
		_ = l.write(Record{
			Tool:       info.Tool(),
			Arguments:  redactArguments(info.RawArguments()),
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
	var parts []string
	for _, item := range result.Content {
		if item != nil && item.Text != nil && *item.Text != "" {
			parts = append(parts, *item.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	raw := bytes.TrimSpace(result.StructuredContent)
	if len(raw) == 0 {
		return ""
	}
	var structured any
	if err := json.Unmarshal(raw, &structured); err != nil {
		return ""
	}
	encoded, err := json.Marshal(structured)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func redactArguments(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{"redacted":"invalid JSON"}`)
	}
	redactValue(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"redacted":"unavailable"}`)
	}
	return redacted
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
				typed[key] = "[REDACTED]"
				continue
			}
			redactValue(child)
		}
	case []any:
		for _, child := range typed {
			redactValue(child)
		}
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
