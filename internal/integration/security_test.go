package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appserver "github.com/CaliLuke/go-argo-mcp/internal/server"
)

type securityRoundTripper struct {
	next                http.RoundTripper
	token, host, origin string
}

func (rt securityRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if rt.token != "" {
		clone.Header.Set("Authorization", "Bearer "+rt.token)
	}
	if rt.host != "" {
		clone.Host = rt.host
	}
	if rt.origin != "" {
		clone.Header.Set("Origin", rt.origin)
	}
	return rt.next.RoundTrip(clone)
}

func TestAuthenticatedHTTPModesUseOfficialSDK(t *testing.T) {
	for _, mode := range []appserver.Transport{appserver.TransportHTTP, appserver.TransportHTTPStateless} {
		t.Run(string(mode), func(t *testing.T) {
			var argoCalls atomic.Int32
			argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { argoCalls.Add(1); simulatedArgo(w, r) }))
			defer argo.Close()
			server := httptest.NewUnstartedServer(nil)
			auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
			app, err := appserver.New(context.Background(), appserver.Config{
				Transport: mode, Addr: server.Listener.Addr().String(), AuthToken: "integration-secret",
				ArgoBaseURL: argo.URL, ArgoRequestTimeout: 5 * time.Second, DefaultNamespace: "argo-ci",
				AuditEnabled: true, AuditFile: auditPath,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer closeApplication(t, app)
			server.Config.Handler = app.Handler()
			server.Start()
			defer server.Close()

			client := mcp.NewClient(&mcp.Implementation{Name: "security-test", Version: "1"}, nil)
			httpClient := &http.Client{Transport: securityRoundTripper{next: http.DefaultTransport, token: "integration-secret"}}
			session, err := client.Connect(testContext(t), &mcp.StreamableClientTransport{Endpoint: server.URL + "/rpc", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
			if err != nil {
				t.Fatalf("authenticated connect: %v", err)
			}
			defer session.Close()
			if _, listErr := session.ListTools(testContext(t), nil); listErr != nil {
				t.Fatal(listErr)
			}
			if result, callErr := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "get_workflow", Arguments: map[string]any{"name": "build-123"}}); callErr != nil || result.IsError {
				t.Fatalf("authenticated tool call: result=%#v err=%v", result, callErr)
			}

			before := argoCalls.Load()
			for _, token := range []string{"", "wrong"} {
				req, _ := http.NewRequest(http.MethodPost, server.URL+"/rpc?credential=inbound-query-secret", strings.NewReader(`{}`))
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				resp, requestErr := http.DefaultClient.Do(req)
				if requestErr != nil {
					t.Fatal(requestErr)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("token %q status=%d", token, resp.StatusCode)
				}
			}
			if argoCalls.Load() != before {
				t.Fatal("rejected authentication reached Argo")
			}
			_ = session.Close()
			if closeErr := app.Close(context.Background()); closeErr != nil {
				t.Fatal(closeErr)
			}
			audit, err := os.ReadFile(auditPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, sentinel := range []string{"integration-secret", "inbound-query-secret", "wrong"} {
				if strings.Contains(string(audit), sentinel) {
					t.Fatalf("audit leaked %q", sentinel)
				}
			}
		})
	}
}

func TestAllowedProxyHostAndOriginOverHTTP(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	server := httptest.NewUnstartedServer(nil)
	app, err := appserver.New(context.Background(), appserver.Config{
		Transport: appserver.TransportHTTPStateless, Addr: server.Listener.Addr().String(), AuthToken: "proxy-secret",
		AllowedHosts: []string{"proxy.example:443"}, AllowedOrigins: []string{"https://proxy.example"},
		ArgoBaseURL: argo.URL, DefaultNamespace: "argo-ci", AuditEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeApplication(t, app)
	server.Config.Handler = app.Handler()
	server.Start()
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: securityRoundTripper{next: http.DefaultTransport, token: "proxy-secret", host: "proxy.example:443", origin: "https://PROXY.example:443"}}
	session, err := client.Connect(testContext(t), &mcp.StreamableClientTransport{Endpoint: server.URL + "/rpc", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("proxy connect: %v", err)
	}
	defer session.Close()
	if _, err := session.ListTools(testContext(t), nil); err != nil {
		t.Fatal(err)
	}
}

func TestExecutableRejectsUnauthenticatedExternalBindAndStdioIgnoresHTTPSettings(t *testing.T) {
	binary := buildBinary(t)
	cmd := exec.Command(binary)
	cmd.Env = cleanEnv(map[string]string{"ARGO_MCP_TRANSPORT": "http", "ARGO_MCP_ADDR": "0.0.0.0:18080", "MCP_AUDIT_ENABLED": "false"})
	if err := cmd.Run(); err == nil {
		t.Fatal("external unauthenticated process started")
	}
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	child := startStdioChildWithEnv(t, argo.URL, map[string]string{"ARGO_MCP_AUTH_TOKEN": " bad ", "ARGO_MCP_ALLOWED_HOSTS": "", "ARGO_MCP_ALLOWED_ORIGINS": "/bad"})
	session := connectChild(t, child)
	if _, err := session.ListTools(testContext(t), nil); err != nil {
		t.Fatal(err)
	}
	_ = child.stdin.Close()
	if err := waitCommand(child.cmd, 10*time.Second); err != nil {
		t.Fatalf("stdio failed: %v", err)
	}
}
