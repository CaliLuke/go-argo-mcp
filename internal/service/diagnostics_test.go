package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestWorkflowNodesFilterSortPageAndTruncate(t *testing.T) {
	long := strings.Repeat("é", 3000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"},"status":{"nodes":{"b":{"id":"b","name":"b","type":"Pod","phase":"Running"},"a":{"id":"a","name":"a","type":"Pod","phase":"Running","message":` + quoteJSON(long) + `}}}}`))
	}))
	defer server.Close()
	ns := "argo-ci"
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowNodes(context.Background(), &genargo.GetWorkflowNodesPayload{Namespace: &ns, Name: "build", Phase: strPtr("Running"), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Count != 1 || result.Nodes[0].ID != "a" || result.NextOffset == nil || !result.Truncated || !result.FieldsTruncated || len(*result.Nodes[0].Message) > 4096 || !utf8.ValidString(*result.Nodes[0].Message) {
		t.Fatalf("result=%#v node=%#v", result, result.Nodes[0])
	}
}

func TestWorkflowArtifactsFilterBeforePagingAndTrustedLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"},"status":{"nodes":{"b":{"id":"b","name":"b","type":"Pod","outputs":{"artifacts":[{"name":"z","path":"/tmp/z"}]}},"a":{"id":"a","name":"a","type":"Pod","inputs":{"artifacts":[{"name":"x","path":"/tmp/x","s3":{"key":"secret"}}]}}}}}`))
	}))
	defer server.Close()
	ns, node := "argo-ci", "a"
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowArtifacts(context.Background(), &genargo.GetWorkflowArtifactsPayload{Namespace: &ns, Name: "build", NodeID: &node, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Count != 1 || result.Artifacts[0].Name != "x" || result.Artifacts[0].DownloadURL == nil || !strings.HasPrefix(*result.Artifacts[0].DownloadURL, server.URL+"/artifact-files/") || strings.Contains(*result.Artifacts[0].DownloadURL, "secret") {
		t.Fatalf("result=%#v", result)
	}
}

func TestWorkflowNodeChildrenAreAlwaysNonNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build"},"status":{"nodes":{"a":{"id":"a","name":"a","type":"Pod"}}}}`))
	}))
	defer server.Close()
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowNodes(context.Background(), &genargo.GetWorkflowNodesPayload{Name: "build", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].Children == nil {
		t.Fatalf("children must be nonnil: %#v", result)
	}
}

func TestDiagnosticPaginationAndFieldBoundsRemainDistinct(t *testing.T) {
	children := make([]string, 201)
	for i := range children {
		children[i] = fmt.Sprintf("child-%03d", i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/default/")
		var nodes map[string]any
		switch name {
		case "paged":
			nodes = map[string]any{
				"a": map[string]any{"id": "a", "name": "a", "type": "Pod"},
				"b": map[string]any{"id": "b", "name": "b", "type": "Pod"},
			}
		case "bounded":
			nodes = map[string]any{"a": map[string]any{"id": "a", "name": "a", "type": "Pod", "message": strings.Repeat("m", 4097), "children": children}}
		default:
			nodes = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"name": name}, "status": map[string]any{"nodes": nodes}})
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})

	tests := []struct {
		name       string
		payload    *genargo.GetWorkflowNodesPayload
		count      int
		next       bool
		truncated  bool
		fieldShort bool
	}{
		{"page only", &genargo.GetWorkflowNodesPayload{Name: "paged", Limit: 1}, 1, true, true, false},
		{"field only", &genargo.GetWorkflowNodesPayload{Name: "bounded", Limit: 50}, 1, false, true, true},
		{"exhausted offset", &genargo.GetWorkflowNodesPayload{Name: "paged", Offset: 2, Limit: 1}, 0, false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.GetWorkflowNodes(context.Background(), tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			if result.Count != tc.count || (result.NextOffset != nil) != tc.next || result.Truncated != tc.truncated || result.FieldsTruncated != tc.fieldShort {
				t.Fatalf("result=%#v", result)
			}
			if tc.name == "field only" {
				if len(result.Nodes[0].Children) != 200 || result.Nodes[0].Message == nil || len(*result.Nodes[0].Message) != 4096 {
					t.Fatalf("bounded node=%#v", result.Nodes[0])
				}
			}
		})
	}
}

func TestArchivedWorkflowDetailBoundsEveryDiagnosticCollection(t *testing.T) {
	bounded := make(map[string]string, 21)
	for i := 0; i < 21; i++ {
		bounded[fmt.Sprintf("key-%02d", i)] = "value"
	}
	bounded[strings.Repeat("!", 257)] = strings.Repeat("v", 1025)
	parameters := make([]map[string]any, 0, len(bounded))
	for key, value := range bounded {
		parameters = append(parameters, map[string]any{"name": key, "value": value})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"uid": "archive-1", "name": "build", "namespace": "default", "labels": bounded, "annotations": bounded},
			"spec":     map[string]any{"arguments": map[string]any{"parameters": parameters}},
			"status":   map[string]any{"phase": "Failed", "message": strings.Repeat("m", 4097), "outputs": map[string]any{"parameters": parameters}},
		})
	}))
	defer server.Close()
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetArchivedWorkflow(context.Background(), &genargo.GetArchivedWorkflowPayload{UID: "archive-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.Message == nil || len(*result.Message) != 4096 {
		t.Fatalf("detail=%#v", result)
	}
	for name, values := range map[string]map[string]string{"labels": result.Labels, "annotations": result.Annotations, "parameters": result.Parameters, "outputs": result.Outputs} {
		if len(values) != 20 {
			t.Fatalf("%s has %d entries, want 20", name, len(values))
		}
		sawBoundedKey, sawBoundedValue := false, false
		for key, value := range values {
			if len(key) > 256 || len(value) > 1024 {
				t.Fatalf("%s contains oversized %d-byte key or %d-byte value", name, len(key), len(value))
			}
			sawBoundedKey = sawBoundedKey || len(key) == 256
			sawBoundedValue = sawBoundedValue || len(value) == 1024
		}
		if !sawBoundedKey || !sawBoundedValue {
			t.Fatalf("%s did not retain bounded key/value evidence: %#v", name, values)
		}
	}
}

func TestArtifactLinksOmittedForInvalidOrOversizedIdentities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"name": "build"},
			"status": map[string]any{"nodes": map[string]any{"a": map[string]any{
				"id": "a", "name": "a", "type": "Pod", "outputs": map[string]any{"artifacts": []map[string]any{{"name": "."}, {"name": strings.Repeat("x", 4097)}}},
			}}},
		})
	}))
	defer server.Close()
	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowArtifacts(context.Background(), &genargo.GetWorkflowArtifactsPayload{Name: "build", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 || !result.Truncated || !result.FieldsTruncated {
		t.Fatalf("result=%#v", result)
	}
	for _, artifact := range result.Artifacts {
		if artifact.DownloadURL != nil {
			t.Fatalf("unsafe link emitted: %#v", artifact)
		}
	}
}

func quoteJSON(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
