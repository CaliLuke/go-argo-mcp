package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestListWorkflowsWithoutArgoConfigurationReturnsError(t *testing.T) {
	svc := NewArgoService(ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{}),
		DefaultNamespace: "default",
	})

	_, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{})
	if err == nil {
		t.Fatal("expected missing Argo configuration to fail")
	}
}

func TestListWorkflowsUsesArgoAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/default" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{"metadata": {"name": "wf-a", "namespace": "default"}, "status": {"phase": "Succeeded"}},
				{"metadata": {"name": "wf-b", "namespace": "default"}, "status": {"phase": "Running"}}
			]
		}`))
	}))
	defer server.Close()

	svc := NewArgoService(ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: server.URL}),
		DefaultNamespace: "default",
	})
	status := "Running"

	result, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{Status: &status})
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	if result.Source != "argo" {
		t.Fatalf("expected argo source, got %q", result.Source)
	}
	if result.Count != 1 {
		t.Fatalf("expected one filtered workflow, got %d", result.Count)
	}
	if result.Workflows[0].Name != "wf-b" {
		t.Fatalf("expected wf-b, got %s", result.Workflows[0].Name)
	}
}

func TestListWorkflowsUsesConfiguredDefaultNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/argo-ci" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	svc := NewArgoService(ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: server.URL}),
		DefaultNamespace: "argo-ci",
	})

	result, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{})
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	if result.Namespace == nil || *result.Namespace != "argo-ci" {
		t.Fatalf("expected configured namespace argo-ci, got %#v", result.Namespace)
	}
}

func TestListCronWorkflowsPropagatesArgoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	svc := NewArgoService(ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{BaseURL: server.URL}),
	})

	_, err := svc.ListCronWorkflows(context.Background(), &genargo.ListCronWorkflowsPayload{})
	if err == nil {
		t.Fatal("expected Argo API failure to propagate")
	}
}

func TestListWorkflowTemplatesPropagatesArgoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	svc := NewArgoService(ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{BaseURL: server.URL}),
	})

	_, err := svc.ListWorkflowTemplates(context.Background(), &genargo.ListWorkflowTemplatesPayload{})
	if err == nil {
		t.Fatal("expected Argo API failure to propagate")
	}
}

func TestGetCronHistoryUsesArgoWorkflows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/argo-ci" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("listOptions.labelSelector"); got != "workflows.argoproj.io/cron-workflow=daily-build" {
			t.Fatalf("unexpected label selector: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"daily-build-001","namespace":"argo-ci"},"status":{"phase":"Succeeded","startedAt":"2026-07-21T10:00:00Z"}}]}`))
	}))
	defer server.Close()

	svc := NewArgoService(ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: server.URL}),
		DefaultNamespace: "argo-ci",
	})

	result, err := svc.GetCronHistory(context.Background(), &genargo.GetCronHistoryPayload{Name: "daily-build"})
	if err != nil {
		t.Fatalf("GetCronHistory returned error: %v", err)
	}
	if result.Source != "argo" || result.Count != 1 {
		t.Fatalf("expected one Argo history entry, got %#v", result)
	}
	if result.History[0].Name != "daily-build-001" {
		t.Fatalf("unexpected history entry: %#v", result.History[0])
	}
}

func TestNamespacePolicyDeniesReadBeforeCallingArgo(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	namespace := "production"
	svc := NewArgoService(ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{BaseURL: server.URL}),
		Policy: Policy{AllowedNamespaces: []string{"argo-ci"}},
	})

	_, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{Namespace: &namespace})
	if err == nil {
		t.Fatal("expected denied namespace error")
	}
	if calls != 0 {
		t.Fatalf("expected no Argo calls, got %d", calls)
	}
}

func TestTerminateWorkflowRequiresScopedOneTimeConfirmation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/workflows/argo-ci/build-123/terminate" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	namespace := "argo-ci"
	svc := NewArgoService(ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{BaseURL: server.URL}),
		Policy: Policy{
			AllowDestructive:    true,
			RequireConfirmation: true,
			AllowedNamespaces:   []string{"argo-ci"},
		},
	})

	preview, err := svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{
		Namespace: &namespace,
		Name:      "build-123",
		Reason:    "stuck",
	})
	if err != nil {
		t.Fatalf("dry run returned error: %v", err)
	}
	if preview.ConfirmationToken == nil || *preview.ConfirmationToken == "" {
		t.Fatal("dry run must return a confirmation token")
	}
	if calls != 0 {
		t.Fatalf("dry run must not call Argo, got %d calls", calls)
	}
	dryRun := false
	wrong := "wrong"
	_, err = svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{
		Namespace:         &namespace,
		Name:              "build-123",
		Reason:            "stuck",
		DryRun:            &dryRun,
		ConfirmationToken: &wrong,
	})
	if err == nil {
		t.Fatal("wrong token must be rejected")
	}
	if calls != 0 {
		t.Fatalf("invalid token must not call Argo, got %d calls", calls)
	}

	_, err = svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{
		Namespace:         &namespace,
		Name:              "build-123",
		Reason:            "stuck",
		DryRun:            &dryRun,
		ConfirmationToken: preview.ConfirmationToken,
	})
	if err != nil {
		t.Fatalf("confirmed termination returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one Argo call, got %d", calls)
	}
	_, err = svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{
		Namespace:         &namespace,
		Name:              "build-123",
		Reason:            "stuck",
		DryRun:            &dryRun,
		ConfirmationToken: preview.ConfirmationToken,
	})
	if err == nil {
		t.Fatal("reused token must be rejected")
	}
}

func TestGetWorkflowLogsZeroMaxLinesReturnsAllLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 201 {
			_, _ = w.Write([]byte("{\"result\":{\"podName\":\"pod\",\"content\":\"line\"}}\n"))
		}
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})

	result, err := svc.GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{
		WorkflowName: "build-123",
		MaxLines:     0,
	})
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if result.ReturnedLines != 201 || strings.Count(result.Logs, "line") != 201 {
		t.Fatalf("expected all 201 lines, got returned=%d", result.ReturnedLines)
	}
}
