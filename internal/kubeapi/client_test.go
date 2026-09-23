package kubeapi

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type namedError interface{ LoomErrorName() string }

func TestNewValidatesExplicitConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{BaseURL: "https://user:pass@example.test"},
		{BaseURL: "https://example.test?x=1"},
		{BaseURL: "http://example.test"},
		{BaseURL: "https://example.test", CAPEM: "not pem"},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("New(%#v) succeeded", cfg)
		}
	}
}

func TestNewUsesSuppliedCAForTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := New(Config{BaseURL: server.URL, CAPEM: string(ca)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 1, "")
	if err != nil || result.Count != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := x509.ParseCertificate(server.Certificate().Raw); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnoseMapsMainRequestErrorsWithoutResponseBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{name: "denied", status: http.StatusForbidden, want: "kubernetes_access_denied"},
		{name: "missing", status: http.StatusNotFound, want: "kubernetes_not_found"},
		{name: "retry", status: http.StatusTooManyRequests, want: "kubernetes_api_error"},
		{name: "bad response", status: http.StatusBadRequest, want: "kubernetes_response_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("secret-body"))
			}))
			defer server.Close()
			client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			_, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 1, "")
			var named namedError
			if !errors.As(err, &named) || named.LoomErrorName() != tc.want || strings.Contains(err.Error(), "secret-body") {
				t.Fatalf("error=%v name=%v", err, named)
			}
		})
	}
}

func TestDiagnoseRejectsUpstreamAndSerializedSizeBounds(t *testing.T) {
	t.Run("pod page exceeds requested limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"items":[{},{}]}`)) }))
		defer server.Close()
		client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
		if _, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 1, ""); err == nil {
			t.Fatal("expected page bound error")
		}
	})
	t.Run("result exceeds one MiB", func(t *testing.T) {
		page := oversizedPodPage()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/events") {
				_, _ = w.Write([]byte(`{"items":[]}`))
				return
			}
			_, _ = w.Write(page)
		}))
		defer server.Close()
		client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
		_, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 50, "")
		var named namedError
		if !errors.As(err, &named) || named.LoomErrorName() != "kubernetes_response_error" || !strings.Contains(err.Error(), "lower limit") {
			t.Fatalf("error=%v", err)
		}
	})
}

func oversizedPodPage() []byte {
	items := make([]pod, 50)
	for i := range items {
		items[i].Metadata = objectMeta{Name: fmt.Sprintf("pod-%d", i), Namespace: "n", UID: fmt.Sprintf("uid-%d", i), Labels: map[string]string{"workflows.argoproj.io/workflow": "w"}, OwnerReferences: []ownerReference{{Kind: "Workflow", UID: "wf"}}}
		items[i].Status.Conditions = []condition{{Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 4096)}}
	}
	body, _ := json.Marshal(podList{Items: items})
	return body
}

func TestDiagnoseFiltersOwnerAndEventsByUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/namespaces/argo-ci/pods":
			if r.URL.Query().Get("labelSelector") != "workflows.argoproj.io/workflow=build" {
				t.Errorf("labelSelector = %q", r.URL.Query().Get("labelSelector"))
			}
			_, _ = w.Write([]byte(`{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"owned","namespace":"argo-ci","uid":"pod-1","labels":{"workflows.argoproj.io/workflow":"build"},"ownerReferences":[{"kind":"Workflow","uid":"wf-1"}]},"spec":{"nodeName":"node-a"},"status":{"phase":"Failed","conditions":[{"type":"Ready","status":"False","message":"broken"}],"containerStatuses":[{"name":"main","restartCount":2,"ready":false,"state":{"terminated":{"exitCode":1,"reason":"Error"}}}]}},{"metadata":{"name":"foreign","namespace":"argo-ci","uid":"pod-2","labels":{"workflows.argoproj.io/workflow":"build"},"ownerReferences":[{"kind":"Workflow","uid":"other"}]}}]}`))
		case "/api/v1/namespaces/argo-ci/events":
			if r.URL.Query().Get("fieldSelector") != "involvedObject.uid=pod-1" {
				t.Errorf("fieldSelector = %q", r.URL.Query().Get("fieldSelector"))
			}
			_, _ = w.Write([]byte(`{"metadata":{"continue":"more"},"items":[{"metadata":{"namespace":"argo-ci"},"involvedObject":{"uid":"pod-1","namespace":"argo-ci"},"type":"Warning","reason":"Failed","message":"image pull","count":2},{"metadata":{"namespace":"argo-ci"},"involvedObject":{"uid":"pod-1","namespace":"foreign"},"message":"secret"},{"metadata":{"namespace":"argo-ci"},"involvedObject":{"uid":"other","namespace":"argo-ci"},"message":"secret"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Diagnose(context.Background(), "argo-ci", "build", "wf-1", "", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || result.Pods[0].Name != "owned" || len(result.Pods[0].Events) != 1 || !result.Pods[0].EventsTruncated || !result.HasMore || result.Continue == nil || *result.Continue != "next" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDiagnoseOwnedDeadlineAbortsDuringEventsAndStopsRequests(t *testing.T) {
	var eventRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pods") {
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"p1","namespace":"n","uid":"p1","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}},{"metadata":{"name":"p2","namespace":"n","uid":"p2","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}}]}`))
			return
		}
		eventRequests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	client.diagnoseTimeout = 20 * time.Millisecond
	_, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 2, "")
	var named namedError
	if !errors.As(err, &named) || named.LoomErrorName() != "kubernetes_api_error" || eventRequests.Load() != 1 {
		t.Fatalf("error=%v eventRequests=%d", err, eventRequests.Load())
	}
}

func TestDiagnoseRejectsInvalidPodIdentityBeforeEvents(t *testing.T) {
	for _, tc := range []struct{ name, podName, body string }{
		{name: "missing name", body: `{"items":[{"metadata":{"namespace":"n","uid":"p1","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}}]}`},
		{name: "missing uid", body: `{"items":[{"metadata":{"name":"p1","namespace":"n","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}}]}`},
		{name: "direct name mismatch", podName: "wanted", body: `{"metadata":{"name":"other","namespace":"n","uid":"p1","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/events") {
					eventRequests++
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
			_, err := client.Diagnose(context.Background(), "n", "w", "wf", tc.podName, 1, "")
			var named namedError
			if !errors.As(err, &named) || named.LoomErrorName() != "kubernetes_response_error" || eventRequests != 0 {
				t.Fatalf("error=%v eventRequests=%d", err, eventRequests)
			}
		})
	}
}

func TestDiagnoseEventFailureIsPerPodButCancellationAborts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pods") {
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"p","namespace":"n","uid":"p1","labels":{"workflows.argoproj.io/workflow":"w"},"ownerReferences":[{"kind":"Workflow","uid":"wf"}]}}]}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client, _ := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	result, err := client.Diagnose(context.Background(), "n", "w", "wf", "", 1, "")
	if err != nil || result.Pods[0].EventsError == nil || !strings.Contains(*result.Pods[0].EventsError, "denied") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Diagnose(canceled, "n", "w", "wf", "", 1, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
