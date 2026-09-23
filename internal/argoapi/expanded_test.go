package argoapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestExpandedReadRoutesAndArchiveNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workflows/argo-ci/build":
			writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "build", "namespace": "argo-ci", "uid": "wf-uid"}, "status": map[string]any{"nodes": map[string]any{"node-b": map[string]any{"id": "node-b", "name": "b", "type": "Pod"}}}})
		case "/api/v1/archived-workflows":
			if r.URL.Query().Get("namespace") != "argo-ci" || r.URL.Query().Get("namePrefix") != "build-" || r.URL.Query().Get("listOptions.labelSelector") != "team=ci" || r.URL.Query().Get("listOptions.limit") != "2" || r.URL.Query().Get("listOptions.continue") != "opaque + token" {
				t.Fatalf("unexpected archive query: %#v", r.URL.Query())
			}
			writeJSON(t, w, map[string]any{
				"metadata": map[string]any{"continue": "next"},
				"items": []any{map[string]any{
					"metadata": map[string]any{"name": "build-1", "namespace": "argo-ci", "uid": "archive-1"},
					"status":   map[string]any{"phase": "Succeeded"},
				}},
			})
		case "/api/v1/archived-workflows/archive-1":
			if r.URL.Query().Get("namespace") != "argo-ci" {
				t.Fatalf("namespace missing: %#v", r.URL.Query())
			}
			writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "build-1", "namespace": "other", "uid": "archive-1"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	c := New(Config{BaseURL: server.URL})
	wf, err := c.GetWorkflowData(context.Background(), "argo-ci", "build")
	if err != nil || wf.UID != "wf-uid" || len(wf.Nodes) != 1 {
		t.Fatalf("workflow data=%#v err=%v", wf, err)
	}
	page, err := c.ListArchivedWorkflows(context.Background(), "argo-ci", "team=ci", "build-", 2, "opaque + token")
	if err != nil || page.Continue != "next" || len(page.Items) != 1 || page.Items[0].UID != "archive-1" {
		t.Fatalf("archive page=%#v err=%v", page, err)
	}
	if _, err := c.GetArchivedWorkflow(context.Background(), "argo-ci", "archive-1"); err == nil {
		t.Fatal("cross-namespace archive response must fail")
	}
}

func TestExpandedMutationRoutesAndDeterministicParameters(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, "bad body", 400)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			kind, _ := body["resourceKind"].(string)
			name, _ := body["resourceName"].(string)
			if r.Method != http.MethodPost || body["namespace"] != "argo-ci" || name == "" || (kind != "WorkflowTemplate" && kind != "ClusterWorkflowTemplate" && kind != "CronWorkflow") {
				t.Errorf("submit body/method: %#v %s", body, r.Method)
			}
			options, ok := body["submitOptions"].(map[string]any)
			if !ok || !reflect.DeepEqual(options["parameters"], []any{"a=1", "z=2"}) {
				t.Errorf("submit options missing/unsorted: %#v", body)
			}
		case strings.HasSuffix(r.URL.Path, "/resubmit"):
			if r.Method != http.MethodPut || body["name"] != "old" || body["namespace"] != "argo-ci" || body["memoized"] != true || !reflect.DeepEqual(body["parameters"], []any{"a=1", "z=2"}) {
				t.Errorf("resubmit body/method: %#v %s", body, r.Method)
			}
		case strings.HasSuffix(r.URL.Path, "/suspend"), strings.HasSuffix(r.URL.Path, "/resume"):
			if r.Method != http.MethodPut || body["name"] != "build" || body["namespace"] != "argo-ci" {
				t.Errorf("state body/method: %#v %s", body, r.Method)
			}
		}
		writeJSON(t, w, map[string]any{"metadata": map[string]any{"name": "created", "namespace": "argo-ci"}, "status": map[string]any{"phase": "Pending"}})
	}))
	defer server.Close()
	c := New(Config{BaseURL: server.URL})
	params := map[string]string{"z": "2", "a": "1"}
	if _, err := c.SubmitWorkflow(context.Background(), "argo-ci", "WorkflowTemplate", "tmpl", params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitWorkflow(context.Background(), "argo-ci", "ClusterWorkflowTemplate", "cluster", params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitWorkflow(context.Background(), "argo-ci", "CronWorkflow", "nightly", params); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResubmitWorkflow(context.Background(), "argo-ci", "old", true, params); err != nil {
		t.Fatal(err)
	}
	if err := c.SetWorkflowSuspended(context.Background(), "argo-ci", "build", true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetWorkflowSuspended(context.Background(), "argo-ci", "build", false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"POST /api/v1/workflows/argo-ci/submit", "POST /api/v1/workflows/argo-ci/submit", "POST /api/v1/workflows/argo-ci/submit", "PUT /api/v1/workflows/argo-ci/old/resubmit", "PUT /api/v1/workflows/argo-ci/build/suspend", "PUT /api/v1/workflows/argo-ci/build/resume"}) {
		t.Fatalf("routes=%#v", paths)
	}
}

func TestArtifactURLStaysUnderTrustedBase(t *testing.T) {
	c := New(Config{BaseURL: "https://argo.example/base"})
	got, err := c.ArtifactURL("team/a", "wf b", "node/x", "outputs", "report?.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://argo.example/base/artifact-files/team%2Fa/workflows/wf%20b/node%2Fx/outputs/report%3F.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	for _, bad := range []string{".", "..", "", "\n"} {
		if _, err := c.ArtifactURL("argo-ci", "wf", bad, "outputs", "a"); err == nil {
			t.Fatalf("accepted node %q", bad)
		}
	}
}

func TestListArchivedWorkflowsRejectsBackendOverReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"one","uid":"1"}},{"metadata":{"name":"two","uid":"2"}}]}`))
	}))
	defer server.Close()
	if _, err := New(Config{BaseURL: server.URL}).ListArchivedWorkflows(context.Background(), "argo-ci", "", "", 1, ""); err == nil || !strings.Contains(err.Error(), "without dropping items") {
		t.Fatalf("expected over-return error, got %v", err)
	}
}
