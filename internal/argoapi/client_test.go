package argoapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientUsesBearerAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		writeJSON(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Token: "secret-token"})
	if _, err := client.ListWorkflows(context.Background(), "default", "", 50); err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
}

func TestClientUsesBasicAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "argo-user" || password != "argo-pass" {
			t.Fatalf("unexpected basic auth: username=%q password=%q ok=%t", username, password, ok)
		}
		writeJSON(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Username: "argo-user", Password: "argo-pass"})
	if _, err := client.ListWorkflows(context.Background(), "default", "", 50); err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
}

func TestListWorkflowsAppliesLimitAfterLocalStatusFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("listOptions.limit"); got != "" {
			t.Fatalf("server limit would truncate before status filtering: %q", got)
		}
		writeJSON(t, w, map[string]any{"items": []any{
			map[string]any{"metadata": map[string]any{"name": "done", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Succeeded"}},
			map[string]any{"metadata": map[string]any{"name": "active", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Running"}},
		}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	workflows, err := client.ListWorkflows(context.Background(), "argo-ci", "Running", 1)
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != "active" {
		t.Fatalf("unexpected workflows: %#v", workflows)
	}
}

func TestRetryWorkflowSendsCompleteRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/workflows/argo-ci/build-123/retry" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["name"] != "build-123" || body["namespace"] != "argo-ci" || body["restartSuccessful"] != true {
			t.Fatalf("unexpected body: %#v", body)
		}
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "build-123"}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	if err := client.RetryWorkflow(context.Background(), "argo-ci", "build-123", true); err != nil {
		t.Fatalf("RetryWorkflow returned error: %v", err)
	}
}

func TestTerminateWorkflowSendsCompleteRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/workflows/argo-ci/build-123/terminate" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["name"] != "build-123" || body["namespace"] != "argo-ci" {
			t.Fatalf("unexpected body: %#v", body)
		}
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "build-123"}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	if err := client.TerminateWorkflow(context.Background(), "argo-ci", "build-123"); err != nil {
		t.Fatalf("TerminateWorkflow returned error: %v", err)
	}
}

func TestToggleCronSuspensionSendsCompleteRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/cron-workflows/argo-ci/nightly/suspend" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["name"] != "nightly" || body["namespace"] != "argo-ci" {
			t.Fatalf("unexpected body: %#v", body)
		}
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "nightly"}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	if err := client.ToggleCronSuspension(context.Background(), "argo-ci", "nightly", true); err != nil {
		t.Fatalf("ToggleCronSuspension returned error: %v", err)
	}
}

func TestGetCronHistoryUsesCronLabelAndLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/argo-ci" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("listOptions.labelSelector"); got != "workflows.argoproj.io/cron-workflow=nightly" {
			t.Fatalf("unexpected label selector: %q", got)
		}
		if got := r.URL.Query().Get("listOptions.limit"); got != "5" {
			t.Fatalf("unexpected limit: %q", got)
		}
		writeJSON(t, w, map[string]any{"items": []any{
			map[string]any{"metadata": map[string]any{"name": "nightly-002", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Failed", "startedAt": "2026-07-21T11:00:00Z"}},
			map[string]any{"metadata": map[string]any{"name": "nightly-001", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Succeeded", "startedAt": "2026-07-21T10:00:00Z"}},
		}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	history, err := client.GetCronHistory(context.Background(), "argo-ci", "nightly", 5)
	if err != nil {
		t.Fatalf("GetCronHistory returned error: %v", err)
	}
	if len(history) != 2 || history[0].Name != "nightly-002" || history[0].Status != "Failed" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
