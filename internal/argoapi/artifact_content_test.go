package argoapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadArtifactContentPaginatesAtUTF8Boundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artifact-files/argo-ci/workflows/build/node-1/outputs/report" {
			t.Fatalf("unexpected artifact path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("artifact request must disable content encoding: %q", r.Header.Get("Accept-Encoding"))
		}
		_, _ = w.Write([]byte("ab€cd"))
	}))
	defer server.Close()

	result, err := New(Config{BaseURL: server.URL}).ReadArtifactContent(context.Background(), "argo-ci", "build", "", "node-1", "outputs", "report", 0, 4)
	if err != nil {
		t.Fatalf("ReadArtifactContent returned error: %v", err)
	}
	if result.Text != "ab" || result.ReturnedBytes != 2 || !result.HasMore || result.NextOffset != 2 {
		t.Fatalf("unexpected first page: %#v", result)
	}
}

func TestReadArtifactContentToleratesBoundedLookaheadEndingInsideRune(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("€", 100)))
	}))
	defer server.Close()

	result, err := New(Config{BaseURL: server.URL}).ReadArtifactContent(context.Background(), "argo-ci", "build", "", "node-1", "outputs", "report", 0, 4)
	if err != nil {
		t.Fatalf("bounded lookahead cut valid UTF-8: %v", err)
	}
	if result.Text != "€" || !result.HasMore || result.NextOffset != 3 {
		t.Fatalf("unexpected Unicode page: %#v", result)
	}
}

func TestReadArtifactContentRejectsMalformedByteAtBoundedLookaheadEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("abcdefg"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte{0xff, 'm', 'o', 'r', 'e'})
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).ReadArtifactContent(context.Background(), "argo-ci", "build", "", "node-1", "outputs", "report", 0, 4)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected malformed bounded byte to fail, got %v", err)
	}
}

func TestReadArtifactContentRejectsSplitOffsetAndBinaryText(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		offset int
	}{
		{name: "split UTF-8 offset", body: []byte("a€b"), offset: 2},
		{name: "NUL", body: []byte("a\x00b"), offset: 0},
		{name: "invalid UTF-8", body: []byte{'a', 0xff, 'b'}, offset: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tc.body) }))
			defer server.Close()
			_, err := New(Config{BaseURL: server.URL}).ReadArtifactContent(context.Background(), "argo-ci", "build", "", "node-1", "outputs", "report", tc.offset, 64)
			if err == nil {
				t.Fatal("expected non-text or split offset to fail")
			}
		})
	}
}

func TestReadArtifactContentRejectsEncodingAndCrossOriginRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalled = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "encoding") {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("not actually gzip"))
			return
		}
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, Token: "secret"})

	if _, err := client.readArtifactURL(context.Background(), server.URL+"/artifact?encoding=1", 0, 64); err == nil {
		t.Fatal("expected encoded artifact response to fail")
	}
	if _, err := client.readArtifactURL(context.Background(), server.URL+"/artifact", 0, 64); err == nil {
		t.Fatal("expected cross-origin redirect to fail")
	}
	if targetCalled {
		t.Fatal("cross-origin redirect reached target")
	}
}

func TestReadArtifactContentRejectsRedirectUserinfo(t *testing.T) {
	server := httptest.NewServer(nil)
	defer server.Close()
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := strings.Replace(server.URL, "://", "://attacker:secret@", 1)
		http.Redirect(w, r, target+"/artifact", http.StatusFound)
	})
	_, err := New(Config{BaseURL: server.URL, Token: "token"}).readArtifactURL(context.Background(), server.URL+"/start", 0, 64)
	if err == nil {
		t.Fatal("expected redirect userinfo rejection")
	}
}

func TestArtifactURLRejectsUnsafeSegmentsAndBuildsArchiveRoute(t *testing.T) {
	client := New(Config{BaseURL: "https://argo.example"})
	for _, segment := range []string{"a/b", `a\\b`, ".", "..", "%2f", "%2e%2e", "x\n"} {
		if _, err := client.ArtifactURL(segment, "build", "node", "outputs", "report"); err == nil {
			t.Fatalf("unsafe segment %q accepted", segment)
		}
	}
	got, err := client.ArchivedArtifactURL("argo-ci", "archive-1", "node", "outputs", "main-logs")
	if err != nil {
		t.Fatalf("ArchivedArtifactURL returned error: %v", err)
	}
	if got != "https://argo.example/artifact-files/argo-ci/archived-workflows/archive-1/node/outputs/main-logs" {
		t.Fatalf("unexpected archived artifact URL: %q", got)
	}
}
