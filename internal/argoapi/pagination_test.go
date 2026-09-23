package argoapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCollectionMethodsPreserveOpaqueContinuationTokens(t *testing.T) {
	const cursor = "  next+/=?& token\t"
	tests := []struct {
		name string
		path string
		body string
		call func(*Client) (string, error)
	}{
		{"workflows", "/api/v1/workflows/argo-ci", `{"metadata":{"continue":"later"},"items":[{"metadata":{"name":"wf"}}]}`, func(c *Client) (string, error) {
			page, err := c.ListWorkflows(context.Background(), "argo-ci", "", 1, cursor)
			return page.Continue, err
		}},
		{"CronWorkflows", "/api/v1/cron-workflows/argo-ci", `{"metadata":{"continue":"later"},"items":[{"metadata":{"name":"cron"}}]}`, func(c *Client) (string, error) {
			page, err := c.ListCronWorkflows(context.Background(), "argo-ci", nil, 1, cursor)
			return page.Continue, err
		}},
		{"WorkflowTemplates", "/api/v1/workflow-templates/argo-ci", `{"metadata":{"continue":"later"},"items":[{"metadata":{"name":"template"}}]}`, func(c *Client) (string, error) {
			page, err := c.ListWorkflowTemplates(context.Background(), "argo-ci", "team=ci", 1, cursor)
			return page.Continue, err
		}},
		{"ClusterWorkflowTemplates", "/api/v1/cluster-workflow-templates", `{"metadata":{"continue":"later"},"items":[{"metadata":{"name":"template"}}]}`, func(c *Client) (string, error) {
			page, err := c.ListClusterWorkflowTemplates(context.Background(), "team=ci", 1, cursor)
			return page.Continue, err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path || r.URL.Query().Get("listOptions.continue") != cursor || r.URL.Query().Get("listOptions.limit") != "1" {
					t.Fatalf("opaque cursor or limit changed: %s %#v", r.URL.Path, r.URL.Query())
				}
				if strings.Contains(test.name, "Template") && r.URL.Query().Get("listOptions.labelSelector") != "team=ci" {
					t.Fatalf("label selector missing: %#v", r.URL.Query())
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			got, err := test.call(New(Config{BaseURL: server.URL}))
			if err != nil || got != "later" {
				t.Fatalf("unexpected page: cursor=%q err=%v", got, err)
			}
		})
	}
}

func TestGetCronHistoryPreservesCursorAndSortsReturnedPage(t *testing.T) {
	const cursor = " history+/=? & "
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cron-workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
			return
		}
		if r.URL.Query().Get("listOptions.continue") != cursor || r.URL.Query().Get("listOptions.limit") != "2" {
			t.Fatalf("history cursor or limit changed: %#v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"metadata":{"continue":"later"},"items":[` +
			`{"metadata":{"name":"older"},"status":{"startedAt":"2026-01-01T00:00:00Z"}},` +
			`{"metadata":{"name":"newer"},"status":{"startedAt":"2026-01-02T00:00:00Z"}}]}`))
	}))
	defer server.Close()
	page, err := New(Config{BaseURL: server.URL}).GetCronHistory(context.Background(), "argo-ci", "nightly", 2, cursor)
	if err != nil || page.Continue != "later" || !slices.Equal(names(page.Items), []string{"newer", "older"}) {
		t.Fatalf("unexpected history page: %#v err=%v", page, err)
	}
}

func TestListWorkflowsFilteredPagesDoNotDropMatchingTail(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			if r.URL.Query().Get("listOptions.limit") != "2" || r.URL.Query().Has("listOptions.continue") {
				t.Fatalf("unexpected first query: %#v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"metadata":{"continue":" page 2 +"},"items":[]}`))
			return
		}
		if call == 2 {
			if r.URL.Query().Get("listOptions.limit") != "2" || r.URL.Query().Get("listOptions.continue") != ` page 2 +` {
				t.Fatalf("unexpected second query: %#v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"metadata":{"continue":"tail"},"items":[` +
				`{"metadata":{"name":"done"},"status":{"phase":"Succeeded"}},` +
				`{"metadata":{"name":"active-1"},"status":{"phase":"Running"}}]}`))
			return
		}
		if r.URL.Query().Get("listOptions.limit") != "1" || r.URL.Query().Get("listOptions.continue") != "tail" {
			t.Fatalf("remaining capacity or tail cursor wrong: %#v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"metadata":{"continue":"after"},"items":[{"metadata":{"name":"active-2"},"status":{"phase":"Running"}}]}`))
	}))
	defer server.Close()
	page, err := New(Config{BaseURL: server.URL}).ListWorkflows(context.Background(), "argo-ci", "Running", 2, "")
	if err != nil || !slices.Equal(names(page.Items), []string{"active-1", "active-2"}) || page.Continue != "after" || calls.Load() != 3 {
		t.Fatalf("filtered tail was lost: page=%#v calls=%d err=%v", page, calls.Load(), err)
	}
}

func TestCollectionPaginationRejectsOverCapacityAndFullPageCycles(t *testing.T) {
	t.Run("over capacity", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"metadata":{"continue":"tail"},"items":[{"metadata":{"name":"one"}},{"metadata":{"name":"two"}}]}`))
		}))
		defer server.Close()
		_, err := New(Config{BaseURL: server.URL}).ListWorkflows(context.Background(), "argo-ci", "", 1, "")
		if err == nil || !strings.Contains(err.Error(), "without dropping items") {
			t.Fatalf("expected actionable over-capacity error, got %v", err)
		}
	})
	t.Run("full page cycle", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"metadata":{"continue":"same"},"items":[{"metadata":{"name":"one"}}]}`))
		}))
		defer server.Close()
		_, err := New(Config{BaseURL: server.URL}).ListWorkflows(context.Background(), "argo-ci", "", 1, "same")
		if err == nil || !strings.Contains(err.Error(), "continuation token") {
			t.Fatalf("expected full-page cursor cycle error, got %v", err)
		}
	})
}

func TestCollectionPaginationBackendPageBudget(t *testing.T) {
	for _, test := range []struct {
		name      string
		endAt1000 bool
		wantError bool
	}{
		{"one thousand pages may complete", true, false},
		{"one thousand distinct cursors may not request page 1001", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				call := calls.Add(1)
				if test.endAt1000 && call == maximumBackendPages {
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"metadata":{"continue":"cursor-%d"},"items":[]}`, call)
			}))
			defer server.Close()
			page, err := New(Config{BaseURL: server.URL}).ListWorkflows(context.Background(), "argo-ci", "", 1, "")
			if (err != nil) != test.wantError || calls.Load() != maximumBackendPages || (!test.wantError && (page.Items == nil || page.Continue != "")) {
				t.Fatalf("budget result: page=%#v calls=%d err=%v", page, calls.Load(), err)
			}
		})
	}
}

func TestCollectionPaginationPropagatesCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(Config{BaseURL: server.URL}).ListWorkflows(ctx, "argo-ci", "", 1, "")
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if calls.Load() > 1 {
		t.Fatalf("canceled scan made %d requests", calls.Load())
	}
}

func TestCollectionMethodsRejectInvalidLimitsBeforeArgo(t *testing.T) {
	for _, limit := range []int{-1, 201} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL})
			requests := []func() error{
				func() error {
					_, err := client.ListWorkflows(context.Background(), "argo-ci", "", limit, "")
					return err
				},
				func() error {
					_, err := client.ListCronWorkflows(context.Background(), "argo-ci", nil, limit, "")
					return err
				},
				func() error {
					_, err := client.ListWorkflowTemplates(context.Background(), "argo-ci", "", limit, "")
					return err
				},
				func() error {
					_, err := client.ListClusterWorkflowTemplates(context.Background(), "", limit, "")
					return err
				},
				func() error {
					_, err := client.GetCronHistory(context.Background(), "argo-ci", "nightly", limit, "")
					return err
				},
			}
			for i, request := range requests {
				if err := request(); err == nil {
					t.Fatalf("request %d accepted invalid limit", i)
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("invalid limit made %d Argo requests, including history existence checks", calls.Load())
			}
		})
	}
}

func TestCollectionMethodsApplyInternalZeroDefaults(t *testing.T) {
	wantLimit := "50"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cron-workflows/") && strings.HasSuffix(r.URL.Path, "/nightly") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
			return
		}
		if strings.Contains(r.URL.Query().Get("listOptions.labelSelector"), "cron-workflow") {
			wantLimit = "10"
		}
		if r.URL.Query().Get("listOptions.limit") != wantLimit {
			t.Fatalf("wrong zero default: got %q want %q", r.URL.Query().Get("listOptions.limit"), wantLimit)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL})
	if _, err := client.ListWorkflows(context.Background(), "argo-ci", "", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListCronWorkflows(context.Background(), "argo-ci", nil, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListWorkflowTemplates(context.Background(), "argo-ci", "", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListClusterWorkflowTemplates(context.Background(), "", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetCronHistory(context.Background(), "argo-ci", "nightly", 0, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionMethodsRejectNonStringResponseCursors(t *testing.T) {
	for _, cursorJSON := range []string{`1`, `{}`, `[]`, `true`} {
		t.Run(cursorJSON, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/cron-workflows/") && strings.HasSuffix(r.URL.Path, "/nightly") {
					_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"}}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"metadata":{"continue":%s},"items":[]}`, cursorJSON)
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL})
			requests := []func() error{
				func() error { _, err := client.ListWorkflows(context.Background(), "argo-ci", "", 1, ""); return err },
				func() error {
					_, err := client.ListCronWorkflows(context.Background(), "argo-ci", nil, 1, "")
					return err
				},
				func() error {
					_, err := client.ListWorkflowTemplates(context.Background(), "argo-ci", "", 1, "")
					return err
				},
				func() error {
					_, err := client.ListClusterWorkflowTemplates(context.Background(), "", 1, "")
					return err
				},
				func() error {
					_, err := client.GetCronHistory(context.Background(), "argo-ci", "nightly", 1, "")
					return err
				},
			}
			for i, request := range requests {
				if err := request(); err == nil {
					t.Fatalf("request %d accepted cursor %s", i, cursorJSON)
				}
			}
		})
	}
}

func names(items []WorkflowSummary) []string {
	result := make([]string, len(items))
	for i := range items {
		result[i] = items[i].Name
	}
	return result
}
