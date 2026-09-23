package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

type fakeKubernetesDiagnostics struct {
	called bool
	uid    string
}

func (f *fakeKubernetesDiagnostics) Diagnose(_ context.Context, namespace, name, uid, pod string, limit int, token string) (*genargo.WorkflowPodDiagnosticsResult, error) {
	f.called = true
	f.uid = uid
	return &genargo.WorkflowPodDiagnosticsResult{Namespace: namespace, Name: name, Pods: []*genargo.WorkflowPodDiagnostic{}, Source: "kubernetes"}, nil
}

func TestPodDiagnosticsAuthorizesBeforeIOAndUsesLiveWorkflowUID(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"wf-1"}}`))
	}))
	defer server.Close()
	fake := &fakeKubernetesDiagnostics{}
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci", Policy: Policy{DeniedNamespaces: []string{"blocked"}}, Kubernetes: fake})
	blocked := "blocked"
	if _, err := svc.GetWorkflowPodDiagnostics(context.Background(), &genargo.GetWorkflowPodDiagnosticsPayload{Namespace: &blocked, Name: "build", Limit: 20}); err == nil || requests != 0 || fake.called {
		t.Fatalf("namespace denial performed I/O: requests=%d called=%t err=%v", requests, fake.called, err)
	}
	if _, err := svc.GetWorkflowPodDiagnostics(context.Background(), &genargo.GetWorkflowPodDiagnosticsPayload{Name: "build", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if !fake.called || fake.uid != "wf-1" {
		t.Fatalf("fake=%#v", fake)
	}
}

func TestPodDiagnosticsUnconfiguredFailsBeforeArgo(t *testing.T) {
	svc := NewArgoService(ArgoServiceConfig{})
	if _, err := svc.GetWorkflowPodDiagnostics(context.Background(), &genargo.GetWorkflowPodDiagnosticsPayload{Name: "build", Limit: 20}); err == nil {
		t.Fatal("expected configuration error")
	}
}

func TestTypedNilKubernetesDiagnosticsIsUnconfiguredBeforeArgoIO(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	var backend *fakeKubernetesDiagnostics
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Kubernetes: backend})
	if _, err := svc.GetWorkflowPodDiagnostics(context.Background(), &genargo.GetWorkflowPodDiagnosticsPayload{Name: "build", Limit: 20}); err == nil || requests != 0 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
	contextResult, err := svc.GetServerContext(context.Background())
	if err != nil || contextResult.KubernetesConfigured {
		t.Fatalf("context=%#v err=%v", contextResult, err)
	}
}
