package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestLintWorkflowInsertsNamespaceWithoutLosingOpaqueFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(body["workflow"], &manifest); err != nil {
			t.Errorf("manifest: %v", err)
			return
		}
		const expectedSpec = `{"templates":[{"container":{"args":[9007199254740993]}}]}`
		if string(manifest["spec"]) != expectedSpec {
			t.Errorf("opaque spec changed: got %s want %s", manifest["spec"], expectedSpec)
		}
		var metadata map[string]any
		_ = json.Unmarshal(manifest["metadata"], &metadata)
		if metadata["namespace"] != "argo-ci" {
			t.Errorf("namespace=%#v", metadata)
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"}}`))
	}))
	defer server.Close()
	manifest := `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"build"},"spec":{"templates":[{"container":{"args":[9007199254740993]}}]}}`
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"}).LintWorkflow(context.Background(), &genargo.LintWorkflowPayload{ManifestJSON: manifest})
	if err != nil || !result.Valid || result.Namespace == nil || *result.Namespace != "argo-ci" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestLintWorkflowAcceptsGenerateNameAndReturnsArgoName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build-abc","namespace":"argo-ci"}}`))
	}))
	defer server.Close()
	manifest := `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"generateName":"build-"},"spec":{}}`
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"}).LintWorkflow(context.Background(), &genargo.LintWorkflowPayload{ManifestJSON: manifest})
	if err != nil || result.Name != "build-abc" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestLintRejectsInvalidManifestAndClusterNamespaceBeforeArgo(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"metadata":{"name":"x"}}`))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci", Policy: Policy{AllowedNamespaces: []string{"argo-ci"}, DeniedNamespaces: []string{"blocked"}}})
	invalid := []string{`null`, `[]`, `{"kind":"Workflow"}`, `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x","namespace":"other"}}`, `{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x"}} {}`}
	invalid = append(invalid,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x","namespace":null}}`,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x","namespace":0}}`,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x","namespace":{}}}`,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":"x","namespace":""}}`,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"name":7}}`,
		`{"apiVersion":"argoproj.io/v1alpha1","kind":"Workflow","metadata":{"generateName":null}}`,
	)
	for _, manifest := range invalid {
		if _, err := svc.LintWorkflow(context.Background(), &genargo.LintWorkflowPayload{ManifestJSON: manifest}); err == nil {
			t.Fatalf("accepted %s", manifest)
		}
	}
	ns := "argo-ci"
	cluster := `{"apiVersion":"argoproj.io/v1alpha1","kind":"ClusterWorkflowTemplate","metadata":{"name":"x"},"spec":{}}`
	if _, err := svc.LintWorkflowTemplate(context.Background(), &genargo.LintWorkflowTemplatePayload{Namespace: &ns, ClusterScope: true, ManifestJSON: cluster}); err == nil {
		t.Fatal("cluster namespace accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid lint made %d calls", calls.Load())
	}
	for _, namespaceValue := range []string{`""`, `null`, `0`, `{}`} {
		manifest := `{"apiVersion":"argoproj.io/v1alpha1","kind":"ClusterWorkflowTemplate","metadata":{"name":"x","namespace":` + namespaceValue + `},"spec":{}}`
		if _, err := svc.LintWorkflowTemplate(context.Background(), &genargo.LintWorkflowTemplatePayload{ClusterScope: true, ManifestJSON: manifest}); err == nil {
			t.Fatalf("cluster accepted namespace member %s", namespaceValue)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid cluster manifests made %d calls", calls.Load())
	}
	result, err := svc.LintWorkflowTemplate(context.Background(), &genargo.LintWorkflowTemplatePayload{ClusterScope: true, ManifestJSON: cluster})
	if err != nil || !result.Valid || result.Scope != "cluster" || calls.Load() != 1 {
		t.Fatalf("cluster lint result=%#v err=%v calls=%d", result, err, calls.Load())
	}
}
