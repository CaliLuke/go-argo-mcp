package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appserver "github.com/CaliLuke/go-argo-mcp/internal/server"
)

func TestDiagnosticToolsAcrossProductionTransports(t *testing.T) {
	for _, mode := range []appserver.Transport{appserver.TransportStdio, appserver.TransportHTTP, appserver.TransportHTTPStateless} {
		t.Run(string(mode), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/version":
					_, _ = w.Write([]byte(`{"version":"v4.1.4"}`))
				case "/api/v1/workflows/argo-ci/build":
					_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci","uid":"workflow-uid"},"spec":{"entrypoint":"main","arguments":{"parameters":[{"name":"revision","default":"main"}]},"templates":[{"name":"main","container":{"image":"alpine","args":[9007199254740993]}}]},"status":{"phase":"Succeeded","nodes":{"build-node":{"id":"build-node","name":"build.main","type":"Pod","phase":"Succeeded","inputs":{"parameters":[{"name":"input","value":"in"}]},"outputs":{"parameters":[{"name":"output","value":"out"}],"result":"result-value","exitCode":"0","artifacts":[{"name":"report"},{"name":"main-logs"}]}}}}}`))
				case "/artifact-files/argo-ci/workflows/build/build-node/outputs/report":
					_, _ = w.Write([]byte("report-value"))
				case "/artifact-files/argo-ci/workflows/build/build-node/outputs/main-logs":
					_, _ = w.Write([]byte("retained-line\n"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			session, cleanup := defaultPolicySession(t, mode, upstream.URL)
			defer cleanup()
			defer session.Close()
			info := session.InitializeResult().ServerInfo
			if info.Name != "go-argo-mcp" || info.Version != "0.3.0" {
				t.Fatalf("identity=%+v", info)
			}
			listed, err := session.ListTools(testContext(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(listed.Tools) != 30 {
				t.Fatalf("catalog=%d", len(listed.Tools))
			}
			names := map[string]bool{}
			for _, tool := range listed.Tools {
				names[tool.Name] = true
			}
			for _, name := range []string{"read_workflow_artifact", "get_resource_spec", "get_workflow_pod_diagnostics", "wait_workflow", "get_server_context"} {
				if !names[name] {
					t.Errorf("missing %s", name)
				}
			}
			cases := []struct {
				name  string
				args  map[string]any
				field string
				want  any
			}{
				{"read_workflow_artifact", map[string]any{"name": "build", "node_id": "build-node", "artifact_name": "report"}, "text", "report-value"},
				{"wait_workflow", map[string]any{"name": "build"}, "completed", true},
				{"get_server_context", map[string]any{}, "argo_version", "v4.1.4"},
				{"get_workflow_logs", map[string]any{"workflow_name": "build", "source": "archive", "node_id": "build-node"}, "source", "archive"},
			}
			for _, tc := range cases {
				res := callTool(t, session, tc.name, tc.args)
				if res.IsError {
					t.Fatalf("%s failed: %+v", tc.name, res)
				}
				data := diagnosticResult(t, res)
				if data[tc.field] != tc.want {
					t.Fatalf("%s: %s=%v want %v", tc.name, tc.field, data[tc.field], tc.want)
				}
			}
			spec := diagnosticResult(t, callTool(t, session, "get_resource_spec", map[string]any{"kind": "workflow", "name": "build", "section": "templates"}))
			var templates []struct {
				Container struct {
					Args []json.Number `json:"args"`
				} `json:"container"`
			}
			if err := json.Unmarshal([]byte(spec["spec_json"].(string)), &templates); err != nil {
				t.Fatal(err)
			}
			if len(templates) != 1 || templates[0].Container.Args[0].String() != "9007199254740993" {
				t.Fatalf("spec lost precision: %#v", templates)
			}
			before := calls.Load()
			unconfigured := callTool(t, session, "get_workflow_pod_diagnostics", map[string]any{"name": "build"})
			if !unconfigured.IsError || calls.Load() != before {
				t.Fatal("unconfigured Kubernetes must fail before upstream calls")
			}
		})
	}
}

func TestDiagnosticInputBoundsRejectBeforeIO(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	app, server := newTestApplication(t, appserver.TransportHTTP, upstream.URL)
	defer server.Close()
	defer closeApplication(t, app)
	session := connectHTTP(t, server.URL+"/rpc")
	defer session.Close()
	cases := []struct {
		name string
		args map[string]any
	}{
		{"read_workflow_artifact", map[string]any{"name": "build", "node_id": "node", "artifact_name": "a", "max_bytes": 3}},
		{"read_workflow_artifact", map[string]any{"name": "build", "node_id": "node", "artifact_name": "a", "offset_bytes": -1}},
		{"get_resource_spec", map[string]any{"name": "build", "kind": "invalid"}},
		{"get_resource_spec", map[string]any{"name": "build", "kind": "workflow", "max_bytes": 262145}},
		{"wait_workflow", map[string]any{"name": "build", "duration_seconds": 31}},
		{"wait_workflow", map[string]any{"name": "build", "poll_interval_seconds": 0}},
		{"get_workflow_logs", map[string]any{"workflow_name": "build", "source": "invalid"}},
		{"get_workflow_logs", map[string]any{"workflow_name": "build", "max_bytes": 0}},
		{"get_workflow_pod_diagnostics", map[string]any{"name": "build", "limit": 51}},
	}
	for _, tc := range cases {
		result, err := session.CallTool(testContext(t), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil || !result.IsError {
			t.Fatalf("%s invalid args accepted: %+v %v", tc.name, result, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input made %d requests", calls.Load())
	}
}

func diagnosticResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool error: %+v", result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDiagnosticNamespaceDenialBeforeIO(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	app, server := newTestApplication(t, appserver.TransportHTTP, upstream.URL)
	defer server.Close()
	defer closeApplication(t, app)
	session := connectHTTP(t, server.URL+"/rpc")
	defer session.Close()
	cases := []struct {
		name string
		args map[string]any
	}{
		{"read_workflow_artifact", map[string]any{"name": "build", "node_id": "node", "artifact_name": "a"}},
		{"get_resource_spec", map[string]any{"name": "build", "kind": "workflow"}},
		{"wait_workflow", map[string]any{"name": "build"}},
		{"get_workflow_nodes", map[string]any{"name": "build", "archive_uid": "archive-uid"}},
		{"get_workflow_artifacts", map[string]any{"name": "build", "archive_uid": "archive-uid"}},
		{"get_workflow_logs", map[string]any{"workflow_name": "build", "source": "archive"}},
		{"get_workflow_pod_diagnostics", map[string]any{"name": "build"}},
	}
	for _, tc := range cases {
		tc.args["namespace"] = "denied"
		result := callTool(t, session, tc.name, tc.args)
		if !result.IsError {
			t.Fatalf("namespace denial missing for %s", tc.name)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("denied namespace caused %d requests", calls.Load())
	}
}
