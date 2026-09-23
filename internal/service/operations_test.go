package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loom "github.com/CaliLuke/loom/pkg"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/confirmation"
)

func TestRetryRequiresBothFlagsAndExplicitFalse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	dry := false
	for _, policy := range []Policy{{}, {AllowMutations: true}, {AllowDestructive: true}} {
		svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Policy: policy})
		result, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build", DryRun: &dry})
		if err != nil || result.Status != "denied" {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("denied/preview retries made %d calls", calls.Load())
	}
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Policy: Policy{AllowMutations: true, AllowDestructive: true, RequireConfirmation: false}})
	preview, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build"})
	if err != nil || preview.Status != "dry_run" || calls.Load() != 0 {
		t.Fatalf("preview=%#v calls=%d err=%v", preview, calls.Load(), err)
	}
	if _, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build", DryRun: &dry}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("explicit false made %d calls", calls.Load())
	}
}

func TestRetryConfirmationIsScopedAndSingleUse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	policy := Policy{AllowMutations: true, AllowDestructive: true, RequireConfirmation: true}
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Policy: policy})
	restart := true
	preview, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build", RestartSuccessful: &restart})
	if err != nil || preview.ConfirmationToken == nil {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	dry := false
	otherNS := "other"
	wrongNamespace := &genargo.RetryWorkflowPayload{Namespace: &otherNS, Name: "build", RestartSuccessful: &restart, DryRun: &dry, ConfirmationToken: preview.ConfirmationToken}
	if _, err := svc.RetryWorkflow(context.Background(), wrongNamespace); err == nil {
		t.Fatal("namespace scope accepted")
	}
	wrongName := &genargo.RetryWorkflowPayload{Name: "other", RestartSuccessful: &restart, DryRun: &dry, ConfirmationToken: preview.ConfirmationToken}
	if _, err := svc.RetryWorkflow(context.Background(), wrongName); err == nil {
		t.Fatal("name scope accepted")
	}
	restart = false
	wrongRestart := &genargo.RetryWorkflowPayload{Name: "build", RestartSuccessful: &restart, DryRun: &dry, ConfirmationToken: preview.ConfirmationToken}
	if _, err := svc.RetryWorkflow(context.Background(), wrongRestart); err == nil {
		t.Fatal("restart_successful scope accepted")
	}
	restart = true
	confirmed := &genargo.RetryWorkflowPayload{Name: "build", RestartSuccessful: &restart, DryRun: &dry, ConfirmationToken: preview.ConfirmationToken}
	if _, err := svc.RetryWorkflow(context.Background(), confirmed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RetryWorkflow(context.Background(), confirmed); err == nil {
		t.Fatal("replayed token accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestRetryConfirmationExpires(t *testing.T) {
	now := time.Unix(100, 0)
	manager := confirmation.New(confirmation.Config{TTL: time.Second, Now: func() time.Time { return now }})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("expired token reached Argo") }))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Policy: Policy{AllowMutations: true, AllowDestructive: true, RequireConfirmation: true}, Confirmations: manager})
	preview, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	dry := false
	if _, err := svc.RetryWorkflow(context.Background(), &genargo.RetryWorkflowPayload{Name: "build", DryRun: &dry, ConfirmationToken: preview.ConfirmationToken}); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestTerminationRequiresMutationAndDestructiveFlags(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	dry := false
	for _, policy := range []Policy{{AllowDestructive: true}, {AllowMutations: true}} {
		svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), Policy: policy})
		result, err := svc.TerminateWorkflow(context.Background(), &genargo.TerminateWorkflowPayload{Name: "build", Reason: "stuck", DryRun: &dry})
		if err != nil || result.Status != "denied" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("termination made %d calls", calls.Load())
	}
}

func TestSuspendAndResumeMessagesUseCompletedActions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build"}}`))
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{
		Client: argoapi.New(argoapi.Config{BaseURL: server.URL}),
		Policy: Policy{AllowMutations: true},
	})

	for _, tc := range []struct {
		name string
		call func() (*genargo.ActionResult, error)
		want string
	}{
		{"suspend", func() (*genargo.ActionResult, error) {
			return svc.SuspendWorkflow(context.Background(), &genargo.SuspendWorkflowPayload{Name: "build"})
		}, `Workflow "build" in namespace "default" was suspended.`},
		{"resume", func() (*genargo.ActionResult, error) {
			return svc.ResumeWorkflow(context.Background(), &genargo.ResumeWorkflowPayload{Name: "build"})
		}, `Workflow "build" in namespace "default" was resumed.`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.call()
			if err != nil {
				t.Fatal(err)
			}
			if result.Message != tc.want {
				t.Fatalf("message=%q want %q", result.Message, tc.want)
			}
		})
	}
}

func TestParameterValidationBounds(t *testing.T) {
	tests := []map[string]string{{"": "x"}, {"bad=name": "x"}, {"bad\nname": "x"}, {"x": strings.Repeat("v", 64<<10)}}
	tooMany := map[string]string{}
	for i := 0; i < 129; i++ {
		tooMany[fmt.Sprintf("p%d", i)] = "v"
	}
	tests = append(tests, tooMany)
	for _, params := range tests {
		if err := validateNameAndParameters("source", params); err == nil {
			t.Fatalf("accepted invalid parameters with %d entries", len(params))
		}
	}
	if err := validateNameAndParameters("source", map[string]string{"z": "2", "a": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestNewOperationErrorClasses(t *testing.T) {
	for _, tc := range []struct {
		status int
		name   string
	}{{400, "invalid_input"}, {422, "invalid_input"}, {409, "invalid_state"}, {404, "argo_not_found"}, {403, "argo_access_denied"}, {429, "argo_api_error"}, {503, "argo_api_error"}} {
		err := mapNewArgoError(&argoapi.HTTPError{StatusCode: tc.status, Endpoint: "safe"}, argoTarget{action: "submit", resource: "Workflow", name: "build"})
		var serviceErr *loom.ServiceError
		if !errors.As(err, &serviceErr) || serviceErr.Name != tc.name {
			t.Fatalf("status %d error=%#v", tc.status, err)
		}
	}
}

func TestNewReadMethodsUseNewErrorClasses(t *testing.T) {
	for _, tc := range []struct {
		status int
		name   string
	}{{400, "invalid_input"}, {422, "invalid_input"}, {409, "invalid_state"}, {404, "argo_not_found"}, {401, "argo_access_denied"}, {403, "argo_access_denied"}, {429, "argo_api_error"}, {503, "argo_api_error"}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "SECRET UPSTREAM BODY", tc.status) }))
			defer server.Close()
			_, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetWorkflowNodes(context.Background(), &genargo.GetWorkflowNodesPayload{Name: "build", Limit: 50})
			var serviceErr *loom.ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.Name != tc.name {
				t.Fatalf("error=%#v", err)
			}
			remedy := loom.ExtractErrorRemedy(err)
			if remedy == nil || remedy.SafeMessage == "" || remedy.RetryHint == "" || strings.Contains(remedy.SafeMessage, "SECRET") {
				t.Fatalf("unsafe/nonactionable remedy=%#v", remedy)
			}
		})
	}
}

func TestEveryNewReadMethodMapsConflictToInvalidState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "SECRET UPSTREAM BODY", http.StatusConflict)
	}))
	defer server.Close()
	svc := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})})
	tests := []struct {
		name string
		call func() error
	}{
		{"nodes", func() error {
			_, err := svc.GetWorkflowNodes(context.Background(), &genargo.GetWorkflowNodesPayload{Name: "build", Limit: 50})
			return err
		}},
		{"events", func() error {
			_, err := svc.GetWorkflowEvents(context.Background(), &genargo.GetWorkflowEventsPayload{Name: "build", Limit: 50, DurationSeconds: 2})
			return err
		}},
		{"archive list", func() error {
			_, err := svc.ListArchivedWorkflows(context.Background(), &genargo.ListArchivedWorkflowsPayload{Limit: 50})
			return err
		}},
		{"archive detail", func() error {
			_, err := svc.GetArchivedWorkflow(context.Background(), &genargo.GetArchivedWorkflowPayload{UID: "archive-1"})
			return err
		}},
		{"artifacts", func() error {
			_, err := svc.GetWorkflowArtifacts(context.Background(), &genargo.GetWorkflowArtifactsPayload{Name: "build", Limit: 50})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var serviceErr *loom.ServiceError
			if err := tc.call(); !errors.As(err, &serviceErr) || serviceErr.Name != "invalid_state" {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}
