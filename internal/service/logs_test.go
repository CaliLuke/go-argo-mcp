package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestGetWorkflowLogsAutoFallsBackFromEmptyLiveToExactArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/build/log":
			// Empty live logs permit retained-artifact fallback.
		case "/api/v1/workflows/argo-ci/build":
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"live-1"},"status":{"nodes":{"b":{"id":"b","name":"pod-b","outputs":{"artifacts":[{"name":"sidecar-logs"}]}},"a":{"id":"a","name":"pod-a","outputs":{"artifacts":[{"name":"main-logs"}]}}}}}`))
		case "/artifact-files/argo-ci/workflows/build/a/outputs/main-logs":
			_, _ = w.Write([]byte("Error one\nhealthy\n"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	search := "error"
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"}).GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", Container: "main", Source: "auto", Search: &search, MaxBytes: 1024})
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if result.Source != "archive" || result.TotalLines != 2 || result.MatchingLines != 1 || !strings.Contains(result.Logs, "Error one") || result.Note == nil || !strings.Contains(*result.Note, "empty") {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
}

func TestGetWorkflowLogsAutoDoesNotFallbackOnLiveFailure(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", Source: "auto", MaxBytes: 1024})
	if err == nil {
		t.Fatal("expected live temporary failure")
	}
	if calls != 1 {
		t.Fatalf("temporary live failure triggered %d calls", calls)
	}
}

func TestGetWorkflowLogsAutoWithPodRejectsEmptyOrMissingLiveFallback(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "empty", status: http.StatusOK},
		{name: "not found", status: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			artifactCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/workflows/default/build/log" {
					w.WriteHeader(tc.status)
					return
				}
				artifactCalls++
			}))
			defer server.Close()
			pod := "build-pod"

			_, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", PodName: &pod, Source: "auto", MaxBytes: 1024})
			if err == nil || !strings.Contains(err.Error(), "node_id") {
				t.Fatalf("expected node_id fallback guidance, got %v", err)
			}
			if artifactCalls != 0 {
				t.Fatalf("pod-scoped fallback made %d artifact requests", artifactCalls)
			}
		})
	}
}

func TestGetWorkflowLogsArchiveRejectsPodAndUnknownNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"default","uid":"live-1"},"status":{"nodes":{"node-1":{"id":"node-1","outputs":{"artifacts":[{"name":"main-logs"}]}}}}}`))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
	pod, node := "build-pod", "missing"
	if _, err := svc.GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", Source: "archive", PodName: &pod, MaxBytes: 1024}); err == nil || !strings.Contains(err.Error(), "node_id") {
		t.Fatalf("expected pod fallback guidance, got %v", err)
	}
	if _, err := svc.GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", Source: "archive", NodeID: &node, MaxBytes: 1024}); err == nil || !strings.Contains(err.Error(), "node") {
		t.Fatalf("expected unknown node failure, got %v", err)
	}
}

func TestGetWorkflowLogsArchiveReportsAbsentExactContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"default","uid":"live-1"},"status":{"nodes":{"node-1":{"id":"node-1","outputs":{"artifacts":[{"name":"main-logs"}]}}}}}`))
	}))
	defer server.Close()
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build", Container: "wait", Source: "archive", MaxBytes: 1024})
	if err != nil {
		t.Fatalf("absent exact container should be a successful empty result: %v", err)
	}
	if result.Source != "archive" || result.TotalLines != 0 || result.Note == nil || !strings.Contains(*result.Note, "wait-logs") {
		t.Fatalf("absence was not explicit: %#v", result)
	}
}
