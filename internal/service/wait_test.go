package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestWaitWorkflowReturnsTerminalFailureAsSuccessfulRead(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		phase := "Running"
		if calls.Add(1) > 1 {
			phase = "Failed"
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"},"status":{"phase":"` + phase + `"}}`))
	}))
	defer server.Close()

	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).WaitWorkflow(context.Background(), &genargo.WaitWorkflowPayload{Name: "build", DurationSeconds: 3, PollIntervalSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.TimedOut || result.Workflow.Status != "Failed" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
