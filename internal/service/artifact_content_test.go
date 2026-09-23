package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestReadWorkflowArtifactRequiresExactDeclaredArtifact(t *testing.T) {
	var artifactCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/build":
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"live-1"},"status":{"nodes":{"node-1":{"id":"node-1","outputs":{"artifacts":[{"name":"report"}]}}}}}`))
		default:
			artifactCalls.Add(1)
			_, _ = w.Write([]byte("secret"))
		}
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"})

	_, err := svc.ReadWorkflowArtifact(context.Background(), &genargo.ReadWorkflowArtifactPayload{Name: "build", NodeID: "node-1", ArtifactName: "other", Direction: "outputs", MaxBytes: 64})
	if err == nil || !strings.Contains(err.Error(), "declared") {
		t.Fatalf("expected exact declaration failure, got %v", err)
	}
	if artifactCalls.Load() != 0 {
		t.Fatalf("undeclared artifact triggered %d artifact requests", artifactCalls.Load())
	}
}

func TestReadWorkflowArtifactReturnsBoundedDeclaredText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/build":
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"live-1"},"status":{"nodes":{"node-1":{"id":"node-1","outputs":{"artifacts":[{"name":"report"}]}}}}}`))
		case "/artifact-files/argo-ci/workflows/build/node-1/outputs/report":
			_, _ = w.Write([]byte("abcdef"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"})

	result, err := svc.ReadWorkflowArtifact(context.Background(), &genargo.ReadWorkflowArtifactPayload{Name: "build", NodeID: "node-1", ArtifactName: "report", Direction: "outputs", OffsetBytes: 1, MaxBytes: 4})
	if err != nil {
		t.Fatalf("ReadWorkflowArtifact returned error: %v", err)
	}
	if result.Text != "bcde" || result.ReturnedBytes != 4 || !result.HasMore || result.NextOffset == nil || *result.NextOffset != 5 || result.Source != "argo" {
		t.Fatalf("unexpected artifact result: %#v", result)
	}
}

func TestReadWorkflowArtifactArchiveUsesUIDRouteAndChecksName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/archived-workflows/archive-1":
			_, _ = w.Write([]byte(`{"metadata":{"name":"different","namespace":"argo-ci","uid":"archive-1"},"status":{"nodes":{}}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	uid := "archive-1"
	_, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"}).ReadWorkflowArtifact(context.Background(), &genargo.ReadWorkflowArtifactPayload{Name: "build", NodeID: "node-1", ArtifactName: "report", Direction: "outputs", ArchiveUID: &uid, MaxBytes: 64})
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("expected archived name mismatch, got %v", err)
	}
}

func TestReadWorkflowArtifactRejectsContinuationBeyondMaximumOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/build":
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"live-1"},"status":{"nodes":{"node-1":{"id":"node-1","outputs":{"artifacts":[{"name":"report"}]}}}}}`))
		case "/artifact-files/argo-ci/workflows/build/node-1/outputs/report":
			_, _ = w.Write(bytes.Repeat([]byte{'a'}, 16777224))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"})

	_, err := svc.ReadWorkflowArtifact(context.Background(), &genargo.ReadWorkflowArtifactPayload{Name: "build", NodeID: "node-1", ArtifactName: "report", Direction: "outputs", OffsetBytes: 16777215, MaxBytes: 4})
	if err == nil || !strings.Contains(err.Error(), "get_workflow_artifacts") || !strings.Contains(err.Error(), "download_url") {
		t.Fatalf("expected large-artifact download guidance, got %v", err)
	}
}
