package argoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetResourceSpecPreservesRawSpecAndValidatesIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/argo-ci/build" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"build","namespace":"argo-ci"},"spec":{"templates":[{"name":"main","container":{"args":[9007199254740993]}}]}}`))
	}))
	defer server.Close()

	resource, err := New(Config{BaseURL: server.URL}).GetResourceSpec(context.Background(), "workflow", "argo-ci", "build")
	if err != nil {
		t.Fatal(err)
	}
	const expected = `{"templates":[{"name":"main","container":{"args":[9007199254740993]}}]}`
	if string(resource.Spec) != expected {
		t.Fatalf("spec changed: %s", resource.Spec)
	}
}
