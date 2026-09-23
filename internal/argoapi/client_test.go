package argoapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	loomotel "github.com/CaliLuke/loom/observability/otel"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	if _, err := client.ListWorkflows(context.Background(), "default", "", 50, ""); err != nil {
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
	if _, err := client.ListWorkflows(context.Background(), "default", "", 50, ""); err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
}

func TestClientDoesNotFollowRedirectsWithCredentials(t *testing.T) {
	const sentinel = "redirect-credential-secret"
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := New(Config{BaseURL: source.URL, Token: sentinel})
	_, err := client.ListWorkflows(context.Background(), "default", "", 50, "")
	if err == nil {
		t.Fatal("redirect returned success")
	}
	if targetCalls != 0 {
		t.Fatal("credential-bearing request followed redirect")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("redirect error leaked credential")
	}
}

func TestRedirectCredentialIsAbsentFromActualExportedTelemetry(t *testing.T) {
	const sentinel = "outbound-redirect-telemetry-secret"
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("redirect target called") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	wrapped := loomotel.WrapHTTPClient(&http.Client{}, loomotel.HTTPClientConfig{ServiceName: "argo-api", MetricMode: loomotel.HTTPMetricModeOTelOnly})
	client := New(Config{BaseURL: source.URL, Token: sentinel, HTTPClient: wrapped})
	if _, err := client.ListWorkflows(context.Background(), "default", "", 50, ""); err == nil {
		t.Fatal("redirect returned success")
	}
	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("wrapped outbound client exported no spans")
	}
	for _, span := range spans {
		if strings.Contains(span.Name(), sentinel) {
			t.Fatalf("span name leaked credential: %q", span.Name())
		}
		for _, attr := range span.Attributes() {
			if strings.Contains(attr.Value.String(), sentinel) {
				t.Fatalf("span attribute %s leaked credential", attr.Key)
			}
		}
		for _, event := range span.Events() {
			if strings.Contains(event.Name, sentinel) {
				t.Fatalf("span event leaked credential")
			}
			for _, attr := range event.Attributes {
				if strings.Contains(attr.Value.String(), sentinel) {
					t.Fatalf("event attribute leaked credential")
				}
			}
		}
	}
}

func TestListWorkflowsAppliesLimitAfterLocalStatusFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("listOptions.limit"); got != "1" {
			t.Fatalf("status filtering must request remaining capacity, got %q", got)
		}
		writeJSON(t, w, map[string]any{"items": []any{
			map[string]any{"metadata": map[string]any{"name": "active", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Running"}},
		}})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	page, err := client.ListWorkflows(context.Background(), "argo-ci", "Running", 1, "")
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "active" {
		t.Fatalf("unexpected workflows: %#v", page.Items)
	}
}

func TestListWorkflowsFindsMatchingStatusOnLaterPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("listOptions.continue") == "next-page" {
			writeJSON(t, w, map[string]any{"items": []any{
				map[string]any{"metadata": map[string]any{"name": "active", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Running"}},
			}})
			return
		}
		writeJSON(t, w, map[string]any{
			"metadata": map[string]any{"continue": "next-page"},
			"items":    []any{map[string]any{"metadata": map[string]any{"name": "done", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Succeeded"}}},
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	page, err := client.ListWorkflows(context.Background(), "argo-ci", "Running", 1, "")
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "active" {
		t.Fatalf("matching workflow on later page was lost: %#v", page.Items)
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
		if r.URL.Path == "/api/v1/cron-workflows/argo-ci/nightly" {
			writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "nightly", "namespace": "argo-ci"}})
			return
		}
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
	history, err := client.GetCronHistory(context.Background(), "argo-ci", "nightly", 5, "")
	if err != nil {
		t.Fatalf("GetCronHistory returned error: %v", err)
	}
	if len(history.Items) != 2 || history.Items[0].Name != "nightly-002" || history.Items[0].Status != "Failed" {
		t.Fatalf("unexpected history: %#v", history)
	}
}

func TestGetCronHistoryDistinguishesMissingCronWorkflowFromNoRuns(t *testing.T) {
	workflowListCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/cron-workflows/argo-ci/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		workflowListCalled = true
		writeJSON(t, w, map[string]any{"items": []any{}})
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	_, err := client.GetCronHistory(context.Background(), "argo-ci", "missing", 5, "")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected not-found error, got %v", err)
	}
	if workflowListCalled {
		t.Fatal("missing CronWorkflow should not trigger a workflow list")
	}
}

func TestCronWorkflowReadsModernSchedulesAndTimezone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := map[string]any{
			"metadata": map[string]any{"name": "nightly", "namespace": "argo-ci"},
			"spec":     map[string]any{"schedules": []string{"0 0 * * *", "0 12 * * *"}, "timezone": "America/Los_Angeles"},
			"status":   map[string]any{"lastScheduledTime": "2026-09-21T19:00:00Z"},
		}
		if r.URL.Path == "/api/v1/cron-workflows/argo-ci" {
			writeJSON(t, w, map[string]any{"items": []any{item}})
			return
		}
		writeJSON(t, w, item)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	listed, err := client.ListCronWorkflows(context.Background(), "argo-ci", nil, 50, "")
	if err != nil {
		t.Fatalf("ListCronWorkflows returned error: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Schedule != "0 0 * * *" ||
		!slices.Equal(listed.Items[0].Schedules, []string{"0 0 * * *", "0 12 * * *"}) ||
		listed.Items[0].Timezone != "America/Los_Angeles" {
		t.Fatalf("modern cron schedule lost in list result: %#v", listed)
	}
	detail, err := client.GetCronWorkflow(context.Background(), "argo-ci", "nightly")
	if err != nil {
		t.Fatalf("GetCronWorkflow returned error: %v", err)
	}
	if !slices.Equal(detail.Schedules, []string{"0 0 * * *", "0 12 * * *"}) ||
		detail.Timezone != "America/Los_Angeles" || detail.LastScheduledTime != "2026-09-21T19:00:00Z" {
		t.Fatalf("modern cron schedule lost in detail result: %#v", detail)
	}
}

func TestNextCronRunUsesEarliestScheduleInConfiguredTimezone(t *testing.T) {
	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)
	got := nextCronRun([]string{"0 12 * * *", "0 10 * * *"}, "America/Los_Angeles", now)
	if got != "2026-09-22T17:00:00Z" {
		t.Fatalf("unexpected next cron run: %q", got)
	}
}

func TestWorkflowLogsDecodesSSEFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"result\":{\"podName\":\"build-pod\",\"content\":\"done\"}}\n\n"))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	entries, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main")
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].PodName != "build-pod" || entries[0].Content != "done" {
		t.Fatalf("unexpected log entries: %#v", entries)
	}
}

func TestWorkflowLogsReportsStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"error\":{\"message\":\"permission denied\"}}\n"))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	if _, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main"); err == nil {
		t.Fatal("stream error must not look like empty logs")
	}
}

func TestWorkflowLogsReportsNullStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"error\":null,\"content\":\"must not be returned\"}\n"))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	if _, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main"); err == nil {
		t.Fatal("a present error key must fail even when its value is null")
	}
}

func TestWorkflowLogsFallsBackForEmptyOrNullResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"result\":{},\"podName\":\"empty\",\"content\":\"one\"}\n{\"result\":null,\"podName\":\"null\",\"content\":\"two\"}\n"))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	entries, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main")
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	want := []WorkflowLogEntry{{PodName: "empty", Content: "one"}, {PodName: "null", Content: "two"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("unexpected log entries: got %#v want %#v", entries, want)
	}
}

func TestWorkflowLogsDoNotMergeOuterFieldsIntoNonemptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			`{"podName":"outer-pod","content":"outer-content","result":{"podName":"inner-pod"}}` + "\n" +
				`{"podName":"outer-pod","result":{"content":"inner-content"}}` + "\n",
		))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	entries, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main")
	if err != nil {
		t.Fatalf("GetWorkflowLogs returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].PodName != "" || entries[0].Content != "inner-content" {
		t.Fatalf("nonempty result inherited outer fields: %#v", entries)
	}
}

func TestWorkflowLogsRejectMalformedNestedResultStructures(t *testing.T) {
	for _, result := range []string{`"scalar"`, `[]`, `42`} {
		t.Run(result, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"podName":"outer","content":"must-not-fallback","result":` + result + `}` + "\n"))
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL})
			if _, err := client.GetWorkflowLogs(context.Background(), "argo-ci", "build", "", "main"); err == nil {
				t.Fatalf("malformed nested result %s used outer fallback", result)
			}
		})
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
