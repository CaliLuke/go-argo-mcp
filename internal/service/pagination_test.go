package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestCollectionServicesMapPaginationMetadata(t *testing.T) {
	const cursor = "  opaque+/=?& token\t"
	tests := []struct {
		name       string
		arrayField string
		item       string
		call       func(*ArgoService) (any, error)
	}{
		{"workflows", "workflows", `{"metadata":{"name":"wf"},"status":{"phase":"Running"}}`, func(s *ArgoService) (any, error) {
			return s.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{Limit: intPtr(1), Continue: stringPointer(cursor)})
		}},
		{"CronWorkflows", "cron_workflows", `{"metadata":{"name":"cron"}}`, func(s *ArgoService) (any, error) {
			return s.ListCronWorkflows(context.Background(), &genargo.ListCronWorkflowsPayload{Limit: intPtr(1), Continue: stringPointer(cursor)})
		}},
		{"WorkflowTemplates", "templates", `{"metadata":{"name":"template"}}`, func(s *ArgoService) (any, error) {
			return s.ListWorkflowTemplates(context.Background(), &genargo.ListWorkflowTemplatesPayload{Limit: intPtr(1), Continue: stringPointer(cursor)})
		}},
		{"ClusterWorkflowTemplates", "templates", `{"metadata":{"name":"template"}}`, func(s *ArgoService) (any, error) {
			return s.ListClusterWorkflowTemplates(context.Background(), &genargo.ListClusterWorkflowTemplatesPayload{Limit: intPtr(1), Continue: stringPointer(cursor)})
		}},
		{"history", "history", `{"metadata":{"name":"run"}}`, func(s *ArgoService) (any, error) {
			return s.GetCronHistory(context.Background(), &genargo.GetCronHistoryPayload{Name: "nightly", Limit: intPtr(1), Continue: stringPointer(cursor)})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/cron-workflows/") && strings.HasSuffix(r.URL.Path, "/nightly") {
					_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
					return
				}
				if r.URL.Query().Get("listOptions.continue") != cursor || r.URL.Query().Get("listOptions.limit") != "1" {
					t.Fatalf("service changed cursor or limit: %#v", r.URL.Query())
				}
				_, _ = fmt.Fprintf(w, `{"metadata":{"continue":"next"},"items":[%s]}`, test.item)
			}))
			defer server.Close()
			svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"})
			value, err := test.call(svc)
			if err != nil {
				t.Fatalf("service call failed: %v", err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(encoded, &result); err != nil {
				t.Fatal(err)
			}
			items, ok := result[test.arrayField].([]any)
			if !ok || len(items) != 1 || result["count"] != float64(1) || result["continue"] != "next" || result["has_more"] != true {
				t.Fatalf("pagination metadata lost: %s", encoded)
			}
		})
	}
}

func TestCollectionServiceEmptyResultsUseArraysWithoutContinuation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
	result, err := svc.ListWorkflowTemplates(context.Background(), &genargo.ListWorkflowTemplatesPayload{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || !strings.Contains(string(encoded), `"templates":[]`) || strings.Contains(string(encoded), `"continue"`) || result.HasMore {
		t.Fatalf("empty result contract lost: %s", encoded)
	}
}

func TestCollectionServicesApplyDocumentedInternalDefaults(t *testing.T) {
	tests := []struct {
		name      string
		wantLimit string
		call      func(*ArgoService) error
	}{
		{"workflows", "50", func(s *ArgoService) error {
			_, err := s.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{})
			return err
		}},
		{"CronWorkflows", "50", func(s *ArgoService) error {
			_, err := s.ListCronWorkflows(context.Background(), &genargo.ListCronWorkflowsPayload{})
			return err
		}},
		{"WorkflowTemplates", "50", func(s *ArgoService) error {
			_, err := s.ListWorkflowTemplates(context.Background(), &genargo.ListWorkflowTemplatesPayload{})
			return err
		}},
		{"ClusterWorkflowTemplates", "50", func(s *ArgoService) error {
			_, err := s.ListClusterWorkflowTemplates(context.Background(), &genargo.ListClusterWorkflowTemplatesPayload{})
			return err
		}},
		{"history", "10", func(s *ArgoService) error {
			_, err := s.GetCronHistory(context.Background(), &genargo.GetCronHistoryPayload{Name: "nightly"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/cron-workflows/") && strings.HasSuffix(r.URL.Path, "/nightly") {
					_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
					return
				}
				if got := r.URL.Query().Get("listOptions.limit"); got != test.wantLimit {
					t.Fatalf("default limit: got %q want %q", got, test.wantLimit)
				}
				_, _ = w.Write([]byte(`{"items":[]}`))
			}))
			defer server.Close()
			svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
			if err := test.call(svc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCollectionServiceTreatsInternalZeroLimitAsDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("listOptions.limit"); got != "50" {
			t.Fatalf("internal zero did not use default: %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
	if _, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{Limit: intPtr(0)}); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionServicesRejectInvalidLimitsBeforeAnyArgoRequest(t *testing.T) {
	for _, limit := range []int{-1, 201} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
			requests := []func() error{
				func() error {
					_, err := svc.ListWorkflows(context.Background(), &genargo.ListWorkflowsPayload{Limit: intPtr(limit)})
					return err
				},
				func() error {
					_, err := svc.ListCronWorkflows(context.Background(), &genargo.ListCronWorkflowsPayload{Limit: intPtr(limit)})
					return err
				},
				func() error {
					_, err := svc.ListWorkflowTemplates(context.Background(), &genargo.ListWorkflowTemplatesPayload{Limit: intPtr(limit)})
					return err
				},
				func() error {
					_, err := svc.ListClusterWorkflowTemplates(context.Background(), &genargo.ListClusterWorkflowTemplatesPayload{Limit: intPtr(limit)})
					return err
				},
				func() error {
					_, err := svc.GetCronHistory(context.Background(), &genargo.GetCronHistoryPayload{Name: "nightly", Limit: intPtr(limit)})
					return err
				},
			}
			for i, request := range requests {
				if err := request(); err == nil {
					t.Fatalf("request %d accepted invalid limit", i)
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("invalid limits made %d Argo requests, including a history existence request", calls.Load())
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
