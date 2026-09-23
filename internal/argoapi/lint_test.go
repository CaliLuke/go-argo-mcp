package argoapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLintEnvelopesPreserveOpaqueManifest(t *testing.T) {
	manifest := json.RawMessage(`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x"},"spec":{"templates":[{"name":"main","container":{"image":"busybox","args":[9007199254740993]}}]}}`)
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodPost {
			t.Errorf("lint method=%s", r.Method)
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var envelope map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		key := "template"
		if r.URL.Path == "/api/v1/workflows/argo-ci/lint" {
			key = "workflow"
		}
		if r.URL.Path != "/api/v1/cluster-workflow-templates/lint" {
			var namespace string
			if err := json.Unmarshal(envelope["namespace"], &namespace); err != nil || namespace != "argo-ci" {
				t.Errorf("lint namespace envelope=%#v err=%v", envelope, err)
			}
		}
		if string(envelope[key]) != string(manifest) {
			t.Fatalf("manifest changed:\n%s\n%s", envelope[key], manifest)
		}
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "x", "namespace": "argo-ci"}})
	}))
	defer server.Close()
	c := New(Config{BaseURL: server.URL})
	if _, err := c.LintWorkflow(context.Background(), "argo-ci", manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LintWorkflowTemplate(context.Background(), "argo-ci", manifest, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LintWorkflowTemplate(context.Background(), "", manifest, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/workflows/argo-ci/lint", "/api/v1/workflow-templates/argo-ci/lint", "/api/v1/cluster-workflow-templates/lint"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths=%#v", paths)
	}
}
