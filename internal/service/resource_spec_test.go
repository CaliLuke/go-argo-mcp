package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func TestGetResourceSpecSelectsNamedTemplateWithoutRoundingNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"},"spec":{"templates":[{"name":"main","container":{"args":[9007199254740993]}},{"name":"other"}]}}`))
	}))
	defer server.Close()
	templateName := "main"

	result, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL}), DefaultNamespace: "argo-ci"}).GetResourceSpec(context.Background(), &genargo.GetResourceSpecPayload{Kind: "workflow", Name: "build", Section: "templates", TemplateName: &templateName, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	var selected struct {
		Name      string `json:"name"`
		Container struct {
			Args []json.Number `json:"args"`
		} `json:"container"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(result.SpecJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&selected); err != nil {
		t.Fatalf("decode selected template: %v", err)
	}
	if selected.Name != "main" || len(selected.Container.Args) != 1 || selected.Container.Args[0].String() != "9007199254740993" {
		t.Fatalf("unexpected selected template: %#v", selected)
	}
}

func TestSelectResourceSpecCronFullSpecAndDuplicateTemplate(t *testing.T) {
	full, err := selectResourceSpec([]byte(`{"schedule":"* * * * *","suspend":true,"workflowSpec":{"entrypoint":"main"}}`), "cron_workflow", "spec", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(full) != `{"schedule":"* * * * *","suspend":true,"workflowSpec":{"entrypoint":"main"}}` {
		t.Fatalf("cron spec was unwrapped: %s", full)
	}
	_, err = selectResourceSpec([]byte(`{"templates":[{"name":"main"},{"name":"main"}]}`), "workflow", "templates", "main")
	if err == nil {
		t.Fatal("duplicate template names must fail")
	}
}

func TestGetResourceSpecRejectsUnsafeNameBeforeIO(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	_, err := NewArgoService(ArgoServiceConfig{Client: argoapi.New(argoapi.Config{BaseURL: server.URL})}).GetResourceSpec(context.Background(), &genargo.GetResourceSpecPayload{Kind: "workflow", Name: "%2e%2e", Section: "spec", MaxBytes: 65536})
	if err == nil || calls.Load() != 0 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
