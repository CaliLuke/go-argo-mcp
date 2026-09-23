package argoapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestCompatibilityWorkflowFixtures(t *testing.T) {
	client := fixtureClient(t, map[string]string{
		"/api/v1/workflows/argo-ci":           "workflow-list.json",
		"/api/v1/workflows/argo-ci/build-123": "workflow-detail.json",
	})

	page, err := client.ListWorkflows(context.Background(), "argo-ci", "", 20, "")
	if err != nil {
		t.Fatalf("ListWorkflows returned error: %v", err)
	}
	wantList := []WorkflowSummary{{Name: "build-123", Namespace: "argo-ci", Status: "Running", Progress: "1/2", StartedAt: "2026-09-22T16:00:00Z"}}
	if !reflect.DeepEqual(page.Items, wantList) {
		t.Fatalf("ListWorkflows mismatch:\n got %#v\nwant %#v", page.Items, wantList)
	}

	detail, err := client.GetWorkflow(context.Background(), "argo-ci", "build-123")
	if err != nil {
		t.Fatalf("GetWorkflow returned error: %v", err)
	}
	wantParams := map[string]string{
		"empty": "fallback", "null": "", "boolean": "false", "number": "42",
		"object": `{"a":1,"b":2}`, "array": `["one",2]`, "default": "chosen",
		"from": `{"parameter":"steps.make.outputs.result"}`,
	}
	if !reflect.DeepEqual(detail.Parameters, wantParams) {
		t.Fatalf("parameters mismatch:\n got %#v\nwant %#v", detail.Parameters, wantParams)
	}
	if !reflect.DeepEqual(detail.Outputs, map[string]string{"result": `{"ok":true}`}) ||
		!reflect.DeepEqual(detail.Labels, map[string]string{"team": "platform"}) ||
		len(detail.Annotations) != 0 || detail.Message != "complete" {
		t.Fatalf("unexpected workflow detail: %#v", detail)
	}
}

func TestCompatibilityCronWorkflowFixtures(t *testing.T) {
	client := fixtureClient(t, map[string]string{
		"/api/v1/cron-workflows/argo-ci":         "cron-list.json",
		"/api/v1/cron-workflows/argo-ci/nightly": "cron-detail.json",
	})

	page, err := client.ListCronWorkflows(context.Background(), "argo-ci", nil, 50, "")
	if err != nil {
		t.Fatalf("ListCronWorkflows returned error: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Schedule != "0 0 * * *" || page.Items[0].Namespace != "argo-ci" ||
		!slices.Equal(page.Items[1].Schedules, []string{"0 6 * * *", "0 18 * * *"}) || !page.Items[1].Suspended {
		t.Fatalf("unexpected CronWorkflow list: %#v", page.Items)
	}

	detail, err := client.GetCronWorkflow(context.Background(), "argo-ci", "nightly")
	if err != nil {
		t.Fatalf("GetCronWorkflow returned error: %v", err)
	}
	if detail.Namespace != "argo-ci" || detail.LastScheduledTime != "2026-09-22T06:00:00Z" ||
		detail.NextScheduledTime != "2026-09-23T06:00:00Z" {
		t.Fatalf("unexpected CronWorkflow detail: %#v", detail)
	}
}

func TestCronWorkflowCalculatesNextRunWhenServerOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"},"spec":{"schedule":"* * * * *"}}`))
	}))
	defer server.Close()
	detail, err := New(Config{BaseURL: server.URL}).GetCronWorkflow(context.Background(), "argo-ci", "nightly")
	if err != nil {
		t.Fatalf("GetCronWorkflow returned error: %v", err)
	}
	if detail.NextScheduledTime == "" {
		t.Fatal("active CronWorkflow without a server value must calculate its next run")
	}
}

func TestSuspendedCronWorkflowDoesNotReportNextRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"nightly"},"spec":{"schedule":"* * * * *","suspend":true},"status":{"nextScheduledTime":"2099-01-01T00:00:00Z"}}`))
	}))
	defer server.Close()
	detail, err := New(Config{BaseURL: server.URL}).GetCronWorkflow(context.Background(), "argo-ci", "nightly")
	if err != nil {
		t.Fatalf("GetCronWorkflow returned error: %v", err)
	}
	if detail.NextScheduledTime != "" {
		t.Fatalf("suspended CronWorkflow reported next run %q", detail.NextScheduledTime)
	}
}

func TestCompatibilityTemplateFixtures(t *testing.T) {
	client := fixtureClient(t, map[string]string{
		"/api/v1/workflow-templates/argo-ci":       "template-list.json",
		"/api/v1/workflow-templates/argo-ci/build": "template-detail.json",
		"/api/v1/cluster-workflow-templates":       "template-list.json",
		"/api/v1/cluster-workflow-templates/build": "template-detail.json",
	})

	workflowTemplates, err := client.ListWorkflowTemplates(context.Background(), "argo-ci", "", 50, "")
	if err != nil || !reflect.DeepEqual(workflowTemplates.Items, []TemplateSummary{{Name: "build", Namespace: "argo-ci", Entrypoint: "main"}}) {
		t.Fatalf("unexpected WorkflowTemplate list: %#v, %v", workflowTemplates, err)
	}
	workflowTemplate, err := client.GetWorkflowTemplate(context.Background(), "argo-ci", "build")
	if err != nil || !slices.Equal(workflowTemplate.TemplateNames, []string{"main", "cleanup"}) || workflowTemplate.Namespace != "argo-ci" {
		t.Fatalf("unexpected WorkflowTemplate detail: %#v, %v", workflowTemplate, err)
	}
	clusterTemplates, err := client.ListClusterWorkflowTemplates(context.Background(), "", 50, "")
	if err != nil || !reflect.DeepEqual(clusterTemplates.Items, []ClusterTemplateSummary{{Name: "build", Entrypoint: "main"}}) {
		t.Fatalf("unexpected ClusterWorkflowTemplate list: %#v, %v", clusterTemplates, err)
	}
	clusterTemplate, err := client.GetClusterWorkflowTemplate(context.Background(), "build")
	if err != nil || !slices.Equal(clusterTemplate.TemplateNames, []string{"main", "cleanup"}) {
		t.Fatalf("unexpected ClusterWorkflowTemplate detail: %#v, %v", clusterTemplate, err)
	}
}

func TestMalformedKnownStructuralFieldsReturnDecodeErrors(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		request func(*Client) error
	}{
		{name: "workflow list items", path: "/api/v1/workflows/argo-ci", body: `{"items":{}}`, request: func(c *Client) error {
			_, err := c.ListWorkflows(context.Background(), "argo-ci", "", 20, "")
			return err
		}},
		{name: "workflow metadata", path: "/api/v1/workflows/argo-ci/build", body: `{"metadata":[],"status":{}}`, request: func(c *Client) error { _, err := c.GetWorkflow(context.Background(), "argo-ci", "build"); return err }},
		{name: "cron schedules", path: "/api/v1/cron-workflows/argo-ci/nightly", body: `{"metadata":{"name":"nightly"},"spec":{"schedules":{}}}`, request: func(c *Client) error {
			_, err := c.GetCronWorkflow(context.Background(), "argo-ci", "nightly")
			return err
		}},
		{name: "template entries", path: "/api/v1/workflow-templates/argo-ci/build", body: `{"metadata":{"name":"build"},"spec":{"templates":["bad"]}}`, request: func(c *Client) error {
			_, err := c.GetWorkflowTemplate(context.Background(), "argo-ci", "build")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			if err := test.request(New(Config{BaseURL: server.URL})); err == nil {
				t.Fatal("malformed known field must return a decode error")
			}
		})
	}
}

func TestMutationRequestCompatibility(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
		path string
		want map[string]any
	}{
		{name: "retry false", call: func(c *Client) error { return c.RetryWorkflow(context.Background(), "argo-ci", "build", false) }, path: "/api/v1/workflows/argo-ci/build/retry", want: map[string]any{"name": "build", "namespace": "argo-ci", "restartSuccessful": false}},
		{name: "retry true", call: func(c *Client) error { return c.RetryWorkflow(context.Background(), "argo-ci", "build", true) }, path: "/api/v1/workflows/argo-ci/build/retry", want: map[string]any{"name": "build", "namespace": "argo-ci", "restartSuccessful": true}},
		{name: "suspend", call: func(c *Client) error { return c.ToggleCronSuspension(context.Background(), "argo-ci", "nightly", true) }, path: "/api/v1/cron-workflows/argo-ci/nightly/suspend", want: map[string]any{"name": "nightly", "namespace": "argo-ci"}},
		{name: "resume", call: func(c *Client) error {
			return c.ToggleCronSuspension(context.Background(), "argo-ci", "nightly", false)
		}, path: "/api/v1/cron-workflows/argo-ci/nightly/resume", want: map[string]any{"name": "nightly", "namespace": "argo-ci"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if !reflect.DeepEqual(got, test.want) {
					t.Fatalf("request body mismatch: got %#v want %#v", got, test.want)
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			if err := test.call(New(Config{BaseURL: server.URL})); err != nil {
				t.Fatalf("request returned error: %v", err)
			}
		})
	}
}

func fixtureClient(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture, ok := routes[r.URL.Path]
		if !ok {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		data, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return New(Config{BaseURL: server.URL})
}
