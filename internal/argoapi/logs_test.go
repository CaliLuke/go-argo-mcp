package argoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowLogsBoundedPrefixKeepsOnlyCompleteFrames(t *testing.T) {
	first := "data: {\"result\":{\"podName\":\"pod\",\"content\":\"one\"}}\n\n"
	second := "data: {\"result\":{\"podName\":\"pod\",\"content\":\"two\"}}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(first + second)) }))
	defer server.Close()

	entries, truncated, err := New(Config{BaseURL: server.URL}).GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main", len(first)+8)
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "one" || !truncated {
		t.Fatalf("unexpected bounded logs: entries=%#v truncated=%v", entries, truncated)
	}
}

func TestWorkflowLogsOversizedFirstFrameFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`data: {"result":{"podName":"pod","content":"` + strings.Repeat("x", 200) + `"}}` + "\n\n"))
	}))
	defer server.Close()

	_, _, err := New(Config{BaseURL: server.URL}).GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main", 64)
	if err == nil || !strings.Contains(err.Error(), "increase") {
		t.Fatalf("expected bounded-frame guidance, got %v", err)
	}
}

func TestWorkflowLogsBoundIncludesSSEFraming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"result\":{\"podName\":\"pod\",\"content\":\"x\"}}\n\n"))
	}))
	defer server.Close()

	_, truncated, err := New(Config{BaseURL: server.URL}).GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main", 10)
	if err == nil || !truncated {
		t.Fatalf("framing must count toward max bytes: truncated=%v err=%v", truncated, err)
	}
}
