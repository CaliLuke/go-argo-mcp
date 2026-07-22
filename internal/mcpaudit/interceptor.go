package mcpaudit

import (
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
	return func(ctx context.Context, info mcpargo.ToolCallInterceptorInfo, payload *mcpargo.ToolsCallPayload, stream mcpargo.ToolsCallServerStream, next mcpargo.ToolCallHandler) (bool, error) {
		start := time.Now()
		capture := &captureStream{ToolsCallServerStream: stream}
		toolError, err := next(ctx, payload, capture)
		status := "SUCCESS"
		if err != nil || toolError || capture.isError {
			status = "ERROR"
		}
		summary := capture.summary
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
		return toolError, err
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

type captureStream struct {
	mcpargo.ToolsCallServerStream
	summary string
	isError bool
}

func (s *captureStream) Send(ctx context.Context, event mcpargo.ToolsCallEvent) error {
	s.capture(event)
	return s.ToolsCallServerStream.Send(ctx, event)
}

func (s *captureStream) SendAndClose(ctx context.Context, event mcpargo.ToolsCallEvent) error {
	s.capture(event)
	return s.ToolsCallServerStream.SendAndClose(ctx, event)
}

func (s *captureStream) SendError(ctx context.Context, id any, err error) error {
	s.isError = true
	if err != nil {
		s.summary = err.Error()
	}
	return s.ToolsCallServerStream.SendError(ctx, id, err)
}

func (s *captureStream) capture(event mcpargo.ToolsCallEvent) {
	result, ok := event.(*mcpargo.ToolsCallResult)
	if !ok || result == nil {
		return
	}
	if result.IsError != nil && *result.IsError {
		s.isError = true
	}
	var parts []string
	for _, item := range result.Content {
		if item != nil && item.Text != nil && *item.Text != "" {
			parts = append(parts, *item.Text)
		}
	}
	if len(parts) > 0 {
		s.summary = strings.Join(parts, "\n")
	} else if len(result.StructuredContent) > 0 {
		s.summary = string(result.StructuredContent)
	}
}

func redactArguments(raw json.RawMessage) json.RawMessage {
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
