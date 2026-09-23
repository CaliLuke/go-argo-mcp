package argoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowDataMapsNodeDiagnosticOutputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"metadata": map[string]any{"name": "build", "namespace": "argo-ci", "uid": "uid-1"},
			"status": map[string]any{"nodes": map[string]any{"node-1": map[string]any{
				"id": "node-1", "name": "build", "type": "Pod",
				"inputs":  map[string]any{"parameters": []any{map[string]any{"name": "commit", "value": "abc"}}},
				"outputs": map[string]any{"parameters": []any{map[string]any{"name": "digest", "value": "sha256:x"}}, "result": "done", "exitCode": "0"},
			}}},
		})
	}))
	defer server.Close()

	data, err := New(Config{BaseURL: server.URL}).GetWorkflowData(context.Background(), "argo-ci", "build")
	if err != nil {
		t.Fatal(err)
	}
	node := data.Nodes[0]
	if node.InputParameters["commit"] != "abc" || node.OutputParameters["digest"] != "sha256:x" || node.OutputResult != "done" || node.ExitCode != "0" {
		t.Fatalf("unexpected node outputs: %#v", node)
	}
}

func TestGetArchivedWorkflowDataRejectsIdentityMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "other", "namespace": "argo-ci", "uid": "archive-1"}})
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).GetArchivedWorkflowData(context.Background(), "argo-ci", "build", "archive-1")
	if err == nil {
		t.Fatal("expected archived workflow name mismatch")
	}
}
