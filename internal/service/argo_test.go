package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	loom "github.com/CaliLuke/loom/pkg"

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

func TestGetWorkflowMapsArgoHTTPFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorName  string
		temporary  bool
	}{
		{"missing", http.StatusNotFound, "argo_not_found", false},
		{"forbidden", http.StatusForbidden, "argo_access_denied", false},
		{"conflict", http.StatusConflict, "argo_request_rejected", false},
		{"unavailable", http.StatusServiceUnavailable, "argo_api_error", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()
			svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
			_, err := svc.GetWorkflow(context.Background(), &genargo.GetWorkflowPayload{Name: "build-123"})
			var serviceErr *loom.ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.Name != tc.errorName || serviceErr.Temporary != tc.temporary {
				t.Fatalf("unexpected Argo error mapping: %#v", err)
			}
			remedy := loom.ExtractErrorRemedy(err)
			if remedy == nil || !strings.Contains(remedy.SafeMessage, `Workflow "build-123" in namespace "default"`) {
				t.Fatalf("error lacks exact resource context: %#v", remedy)
			}
			if tc.statusCode == http.StatusNotFound && !strings.Contains(remedy.RetryHint, "list_workflows") {
				t.Fatalf("not-found remedy should name the discovery tool: %#v", remedy)
			}
		})
	}
}

func TestGetCronHistoryUsesArgoWorkflows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/cron-workflows/argo-ci/daily-build" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metadata":{"name":"daily-build","namespace":"argo-ci"}}`))
			return
		}
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
	remedy := loom.ExtractErrorRemedy(err)
	if remedy == nil || !strings.Contains(remedy.SafeMessage, `Namespace "production"`) || !strings.Contains(remedy.RetryHint, "No Argo request was made") {
		t.Fatalf("namespace denial lacks actionable safe guidance: %#v", remedy)
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
			AllowMutations:      true,
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
	remedy := loom.ExtractErrorRemedy(err)
	if remedy == nil || !strings.Contains(remedy.SafeMessage, `Workflow "build-123" in namespace "argo-ci"`) || !strings.Contains(remedy.RetryHint, "same name, namespace, and reason") {
		t.Fatalf("confirmation error lacks scoped recovery guidance: %#v", remedy)
	}
	if calls != 0 {
		t.Fatalf("invalid token must not call Argo, got %d calls", calls)
	}
	changedReason := "different reason"
	_, err = svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{
		Namespace:         &namespace,
		Name:              "build-123",
		Reason:            changedReason,
		DryRun:            &dryRun,
		ConfirmationToken: preview.ConfirmationToken,
	})
	if err == nil {
		t.Fatal("token for a different reason must be rejected")
	}
	if calls != 0 {
		t.Fatalf("changed reason must not call Argo, got %d calls", calls)
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

func TestGetWorkflowLogsExplainsEmptyArgoResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: server.URL}),
		DefaultNamespace: "argo-ci",
	})

	result, err := svc.GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build-123"})
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if result.Note == nil || !strings.Contains(*result.Note, `Workflow "build-123" in namespace "argo-ci"`) || !strings.Contains(*result.Note, "may no longer be retained") {
		t.Fatalf("empty logs should explain likely causes: %#v", result.Note)
	}
}

func TestGetWorkflowLogsExplainsNoSearchMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"result\":{\"podName\":\"pod\",\"content\":\"healthy\"}}\n"))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
	search := "error"

	result, err := svc.GetWorkflowLogs(context.Background(), &genargo.GetWorkflowLogsPayload{WorkflowName: "build-123", Search: &search})
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if result.Note == nil || *result.Note != `No log entries matched "error" among the 1 entries returned by Argo.` {
		t.Fatalf("no-match logs should distinguish filtering from an empty Argo response: %#v", result.Note)
	}
}

func TestDeniedMutationNamesRequiredSettingAndConfirmsNoArgoCall(t *testing.T) {
	svc := NewArgoService(ArgoServiceConfig{})
	result, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build-123"})
	if err != nil {
		t.Fatalf("RetryWorkflow returned error: %v", err)
	}
	if result.Status != "denied" || result.Instructions == nil || !strings.Contains(*result.Instructions, "MCP_ALLOW_MUTATIONS=true") || !strings.Contains(*result.Instructions, "No Argo request was made") {
		t.Fatalf("denied mutation lacks actionable policy guidance: %#v", result)
	}
}
