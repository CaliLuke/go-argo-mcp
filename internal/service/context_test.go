package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/version"
)

func TestGetServerContextReturnsSanitizedIdentityAndPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v3.7.3","token":"secret"}`))
	}))
	defer server.Close()

	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL, Token: "secret"}), DefaultNamespace: "argo-ci", BuildVersion: "0.3.0", Transport: "http", Policy: Policy{AllowedNamespaces: []string{"argo-ci"}, DeniedNamespaces: []string{"blocked"}}}).GetServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.McpVersion != version.MCPVersion || result.ArgoVersion == nil || *result.ArgoVersion != "v3.7.3" || result.ArgoStatus != "available" {
		t.Fatalf("unexpected context: %#v", result)
	}
}

func TestGetServerContextTreatsMissingVersionAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"must-not-appear"}`))
	}))
	defer server.Close()

	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetServerContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.ArgoStatus != "unavailable" || result.ArgoVersion != nil {
		t.Fatalf("unexpected context: %#v", result)
	}
}

func TestGetServerContextPropagatesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := NewArgoService(ArgoServiceConfig{}).GetServerContext(ctx)
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("expected parent cancellation, got result=%#v err=%v", result, err)
	}
}
