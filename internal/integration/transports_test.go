package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appserver "github.com/CaliLuke/go-argo-mcp/internal/server"
)

func TestHTTPTransportsUseProductionBootstrap(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	for _, mode := range []appserver.Transport{appserver.TransportHTTP, appserver.TransportHTTPStateless} {
		t.Run(string(mode), func(t *testing.T) {
			app, httpServer := newTestApplication(t, mode, argo.URL)
			defer httpServer.Close()
			defer closeApplication(t, app)

			for attempt := 0; attempt < 2; attempt++ {
				session := connectHTTP(t, httpServer.URL+"/rpc")
				listed, err := session.ListTools(testContext(t), nil)
				if err != nil || len(listed.Tools) != 25 {
					t.Fatalf("ListTools: count=%d err=%v", len(listed.Tools), err)
				}
				result, err := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "get_workflow", Arguments: map[string]any{"name": "build-123"}})
				if err != nil || result.IsError || result.StructuredContent == nil {
					t.Fatalf("CallTool: result=%#v err=%v", result, err)
				}
				_ = session.Close()
			}
		})
	}
}

func TestStatelessMethodAndSessionPolicy(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	app, httpServer := newTestApplication(t, appserver.TransportHTTPStateless, argo.URL)
	defer closeApplication(t, app)
	defer httpServer.Close()

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, err := http.NewRequest(method, httpServer.URL+"/rpc", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
			t.Fatalf("%s status=%d Allow=%q", method, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	resp, err := http.Post(httpServer.URL+"/rpc", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("stateless response emitted session ID %q", got)
	}
}

func TestStdioEOFWithoutSignal(t *testing.T) {
	child := startStdioChild(t)
	session := connectChild(t, child)
	assertChildRead(t, session)
	if err := child.stdin.Close(); err != nil {
		t.Fatalf("close child stdin: %v", err)
	}
	if err := waitCommand(child.cmd, 10*time.Second); err != nil {
		t.Fatalf("child did not exit successfully on EOF: %v; stderr=%s", err, child.stderr.String())
	}
}

func TestStdioCommandTransport(t *testing.T) {
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	defer argo.Close()
	cmd := exec.Command(buildBinary(t))
	cmd.Env = cleanEnv(map[string]string{
		"ARGO_MCP_TRANSPORT": "stdio",
		"ARGO_BASE_URL":      argo.URL,
		"ARGO_NAMESPACE":     "argo-ci",
		"MCP_AUDIT_ENABLED":  "true",
		"MCP_AUDIT_FILE":     filepath.Join(t.TempDir(), "audit.jsonl"),
	})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "command-transport-test", Version: "1"}, nil)
	session, err := client.Connect(testContext(t), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect CommandTransport: %v; stderr=%s", err, stderr.String())
	}
	assertChildRead(t, session)
	if err := session.Close(); err != nil {
		t.Fatalf("close CommandTransport: %v; stderr=%s", err, stderr.String())
	}
}

func TestStdioSIGTERM(t *testing.T) {
	child := startStdioChild(t)
	session := connectChild(t, child)
	assertChildRead(t, session)
	if err := child.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal child: %v", err)
	}
	if err := waitCommand(child.cmd, 10*time.Second); err != nil {
		t.Fatalf("child did not exit successfully on SIGTERM: %v; stderr=%s", err, child.stderr.String())
	}
}

func TestStatelessRejectsSessionCompatibilityFlag(t *testing.T) {
	binary := buildBinary(t)
	for _, value := range []string{
		"allowsessionsinstateless=1",
		"allowsessionsinstateless =1",
		"allowsessionsinstateless= 1",
		"allowsessionsinstateless=0,allowsessionsinstateless=1",
	} {
		t.Run(value, func(t *testing.T) {
			cmd := exec.Command(binary)
			cmd.Env = cleanEnv(map[string]string{
				"ARGO_MCP_TRANSPORT": "http-stateless",
				"MCPGODEBUG":         value,
				"MCP_AUDIT_ENABLED":  "false",
			})
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil {
				t.Fatal("expected compatibility flag rejection")
			}
			if stdout.Len() != 0 {
				t.Fatalf("startup rejection wrote %d stdout bytes: %q", stdout.Len(), stdout.String())
			}
			if !strings.Contains(stderr.String(), "allowsessionsinstateless") {
				t.Fatalf("missing diagnostic: %s", stderr.String())
			}
		})
	}
}

func TestStdioRejectsStdoutAudit(t *testing.T) {
	paths := []string{"/dev/stdout", "/dev/fd/1"}
	if runtime.GOOS == "linux" {
		paths = append(paths, "/proc/self/fd/1")
	}
	symlink := filepath.Join(t.TempDir(), "stdout-audit")
	if err := os.Symlink("/dev/stdout", symlink); err == nil {
		paths = append(paths, symlink)
	}
	binary := buildBinary(t)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			cmd := exec.Command(binary)
			cmd.Env = cleanEnv(map[string]string{
				"ARGO_MCP_TRANSPORT": "stdio",
				"MCP_AUDIT_ENABLED":  "true",
				"MCP_AUDIT_FILE":     path,
			})
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			if err := cmd.Run(); err == nil {
				t.Fatalf("expected %q rejection", path)
			}
			if stdout.Len() != 0 {
				t.Fatalf("rejection wrote %d stdout bytes", stdout.Len())
			}
		})
	}
	if runtime.GOOS == "linux" {
		t.Run("proc-child-pid", func(t *testing.T) {
			cmd := exec.Command("/bin/sh", "-c", `exec env MCP_AUDIT_FILE="/proc/$$/fd/1" "$TEST_BINARY"`)
			cmd.Env = cleanEnv(map[string]string{
				"ARGO_MCP_TRANSPORT": "stdio",
				"MCP_AUDIT_ENABLED":  "true",
				"TEST_BINARY":        binary,
			})
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			if err := cmd.Run(); err == nil {
				t.Fatal("expected child PID stdout alias rejection")
			}
			if stdout.Len() != 0 {
				t.Fatalf("rejection wrote %d stdout bytes", stdout.Len())
			}
		})
	}
}

func TestTransportSafetyParity(t *testing.T) {
	for _, mode := range []appserver.Transport{appserver.TransportHTTP, appserver.TransportHTTPStateless, appserver.TransportStdio} {
		t.Run(string(mode), func(t *testing.T) {
			var argoCalls atomic.Int32
			argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				argoCalls.Add(1)
				if r.Method == http.MethodPut {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
					return
				}
				simulatedArgo(w, r)
			}))
			defer argo.Close()
			session, finish := safetySession(t, mode, argo.URL)

			read := callTool(t, session, "get_workflow", map[string]any{"name": "build-123"})
			if read.IsError || read.StructuredContent == nil || argoCalls.Load() != 1 {
				t.Fatalf("read contract failed: result=%#v calls=%d", read, argoCalls.Load())
			}

			before := argoCalls.Load()
			denied := callTool(t, session, "get_workflow", map[string]any{"namespace": "forbidden", "name": "build-123"})
			if !denied.IsError || argoCalls.Load() != before {
				t.Fatalf("namespace denial reached Argo: result=%#v calls=%d", denied, argoCalls.Load())
			}

			invalidLimit, invalidErr := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "list_workflows", Arguments: map[string]any{"limit": 0}})
			if (invalidErr == nil && (invalidLimit == nil || !invalidLimit.IsError)) || argoCalls.Load() != before {
				t.Fatalf("explicit zero limit reached Argo or was accepted: result=%#v err=%v calls=%d", invalidLimit, invalidErr, argoCalls.Load())
			}

			nullDryRun, nullDryRunErr := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "terminate_workflow", Arguments: map[string]any{"name": "build-123", "reason": "cleanup", "dry_run": nil}})
			if (nullDryRunErr == nil && (nullDryRun == nil || !nullDryRun.IsError)) || argoCalls.Load() != before {
				t.Fatalf("explicit null dry_run reached Argo or was accepted: result=%#v err=%v calls=%d", nullDryRun, nullDryRunErr, argoCalls.Load())
			}

			retryPreview := callTool(t, session, "retry_workflow", map[string]any{"name": "build-123"})
			retryToken, _ := structuredMap(t, retryPreview)["confirmation_token"].(string)
			if retryPreview.IsError || structuredStatus(t, retryPreview) != "dry_run" || retryToken == "" || argoCalls.Load() != before {
				t.Fatalf("retry preview failed: result=%#v calls=%d", retryPreview, argoCalls.Load())
			}
			retry := callTool(t, session, "retry_workflow", map[string]any{"name": "build-123", "dry_run": false, "confirmation_token": retryToken})
			if retry.IsError || structuredStatus(t, retry) != "ok" || argoCalls.Load() != before+1 {
				t.Fatalf("confirmed retry failed: result=%#v calls=%d", retry, argoCalls.Load())
			}

			preview := callTool(t, session, "terminate_workflow", map[string]any{"name": "build-123", "reason": "cleanup"})
			if preview.IsError || structuredStatus(t, preview) != "dry_run" || argoCalls.Load() != before+1 {
				t.Fatalf("destructive preview changed Argo: result=%#v calls=%d", preview, argoCalls.Load())
			}
			previewMap := structuredMap(t, preview)
			token, _ := previewMap["confirmation_token"].(string)
			if token == "" {
				t.Fatalf("preview lacked confirmation token: %#v", previewMap)
			}
			confirmedArgs := map[string]any{"name": "build-123", "reason": "cleanup", "dry_run": false, "confirmation_token": token}
			confirmed := callTool(t, session, "terminate_workflow", confirmedArgs)
			if confirmed.IsError || structuredStatus(t, confirmed) != "ok" || argoCalls.Load() != before+2 {
				t.Fatalf("confirmed destructive action failed: result=%#v calls=%d", confirmed, argoCalls.Load())
			}
			replayed := callTool(t, session, "terminate_workflow", confirmedArgs)
			if !replayed.IsError || argoCalls.Load() != before+2 {
				t.Fatalf("confirmation replay reached Argo: result=%#v calls=%d", replayed, argoCalls.Load())
			}

			missing := callTool(t, session, "get_workflow", map[string]any{"name": "missing"})
			if !missing.IsError || !strings.Contains(textResult(missing), "argo.resource.not_found") {
				t.Fatalf("error mapping missing: %#v", missing)
			}

			audit := finish()
			if bytes.Contains(audit, []byte(token)) || !bytes.Contains(audit, []byte(`"confirmation_token":"[REDACTED]"`)) {
				t.Fatalf("audit confirmation token was not redacted: %s", audit)
			}
		})
	}
}

func TestTransportDefaultMutationDenial(t *testing.T) {
	for _, mode := range []appserver.Transport{appserver.TransportHTTP, appserver.TransportHTTPStateless, appserver.TransportStdio} {
		t.Run(string(mode), func(t *testing.T) {
			var argoCalls atomic.Int32
			argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				argoCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer argo.Close()
			session, finish := defaultPolicySession(t, mode, argo.URL)
			defer finish()

			for _, call := range []struct {
				name string
				args map[string]any
			}{
				{name: "retry_workflow", args: map[string]any{"name": "build-123"}},
				{name: "terminate_workflow", args: map[string]any{"name": "build-123", "reason": "test"}},
			} {
				result := callTool(t, session, call.name, call.args)
				if result.IsError || structuredStatus(t, result) != "denied" {
					t.Fatalf("%s was not denied by default: %#v", call.name, result)
				}
			}
			if got := argoCalls.Load(); got != 0 {
				t.Fatalf("default-denied mutations made %d Argo requests", got)
			}
		})
	}
}

func TestHTTPShutdownDrainsRequests(t *testing.T) {
	for _, mode := range []appserver.Transport{appserver.TransportHTTP, appserver.TransportHTTPStateless} {
		t.Run(string(mode), func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/workflows/argo-ci/build-123" {
					http.Error(w, "unexpected path", http.StatusNotFound)
					return
				}
				close(started)
				<-release
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"metadata":{"name":"build-123","namespace":"argo-ci"},"status":{"phase":"Running"}}`))
			}))
			defer argo.Close()

			addr := unusedAddress(t)
			ctx, cancel := context.WithCancel(context.Background())
			runDone := make(chan error, 1)
			go func() {
				runDone <- appserver.Run(ctx, appserver.Config{
					Transport:          mode,
					Addr:               addr,
					ArgoBaseURL:        argo.URL,
					ArgoRequestTimeout: 5 * time.Second,
					DefaultNamespace:   "argo-ci",
					AllowedNamespaces:  []string{"argo-ci"},
					AuditEnabled:       false,
					ShutdownTimeout:    2 * time.Second,
				})
			}()
			waitHealthy(t, "http://"+addr+"/healthz")
			session := connectHTTP(t, "http://"+addr+"/rpc")
			callDone := make(chan struct {
				result *mcp.CallToolResult
				err    error
			}, 1)
			go func() {
				result, err := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "get_workflow", Arguments: map[string]any{"name": "build-123"}})
				callDone <- struct {
					result *mcp.CallToolResult
					err    error
				}{result, err}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("Argo request did not start")
			}
			cancel()
			close(release)
			call := <-callDone
			if call.err != nil || call.result == nil || call.result.IsError {
				t.Fatalf("in-flight call did not drain successfully: result=%#v err=%v", call.result, call.err)
			}
			if err := <-runDone; err != nil {
				t.Fatalf("server shutdown failed: %v", err)
			}
		})
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitHealthy(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not become healthy at %s", endpoint)
}

func defaultPolicySession(t *testing.T, mode appserver.Transport, argoURL string) (*mcp.ClientSession, func()) {
	t.Helper()
	if mode == appserver.TransportStdio {
		child := startStdioChildWithEnv(t, argoURL, nil)
		return connectChild(t, child), func() {
			_ = child.stdin.Close()
			if err := waitCommand(child.cmd, 10*time.Second); err != nil {
				t.Fatalf("finish stdio child: %v; stderr=%s", err, child.stderr.String())
			}
		}
	}
	app, httpServer := newTestApplication(t, mode, argoURL)
	session := connectHTTP(t, httpServer.URL+"/rpc")
	return session, func() {
		_ = session.Close()
		httpServer.Close()
		closeApplication(t, app)
	}
}

func safetySession(t *testing.T, mode appserver.Transport, argoURL string) (*mcp.ClientSession, func() []byte) {
	t.Helper()
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	if mode == appserver.TransportStdio {
		child := startStdioChildWithEnv(t, argoURL, map[string]string{
			"MCP_ALLOW_MUTATIONS":   "true",
			"MCP_ALLOW_DESTRUCTIVE": "true",
			"MCP_NAMESPACES_ALLOW":  "argo-ci",
			"MCP_AUDIT_FILE":        auditPath,
		})
		session := connectChild(t, child)
		return session, func() []byte {
			_ = child.stdin.Close()
			if err := waitCommand(child.cmd, 10*time.Second); err != nil {
				t.Fatalf("finish stdio child: %v; stderr=%s", err, child.stderr.String())
			}
			data, err := os.ReadFile(auditPath)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	httpServer := httptest.NewUnstartedServer(nil)
	app, err := appserver.New(testContext(t), appserver.Config{
		Transport:           mode,
		Addr:                httpServer.Listener.Addr().String(),
		ArgoBaseURL:         argoURL,
		ArgoRequestTimeout:  5 * time.Second,
		DefaultNamespace:    "argo-ci",
		AllowedNamespaces:   []string{"argo-ci"},
		AllowMutations:      true,
		AllowDestructive:    true,
		RequireConfirmation: true,
		AuditEnabled:        true,
		AuditFile:           auditPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer.Config.Handler = app.Handler()
	httpServer.Start()
	session := connectHTTP(t, httpServer.URL+"/rpc")
	return session, func() []byte {
		_ = session.Close()
		httpServer.Close()
		closeApplication(t, app)
		data, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(testContext(t), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport error: %v", name, err)
	}
	return result
}

func structuredMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func structuredStatus(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	value, _ := structuredMap(t, result)["status"].(string)
	return value
}

func textResult(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if text, ok := result.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

func newTestApplication(t *testing.T, mode appserver.Transport, argoURL string) (*appserver.Application, *httptest.Server) {
	t.Helper()
	httpServer := httptest.NewUnstartedServer(nil)
	app, err := appserver.New(testContext(t), appserver.Config{
		Transport:           mode,
		Addr:                httpServer.Listener.Addr().String(),
		ArgoBaseURL:         argoURL,
		ArgoRequestTimeout:  5 * time.Second,
		DefaultNamespace:    "argo-ci",
		AllowedNamespaces:   []string{"argo-ci"},
		RequireConfirmation: true,
		AuditEnabled:        true,
		AuditFile:           filepath.Join(t.TempDir(), "audit.jsonl"),
	})
	if err != nil {
		httpServer.Close()
		t.Fatalf("server.New: %v", err)
	}
	httpServer.Config.Handler = app.Handler()
	httpServer.Start()
	return app, httpServer
}

func closeApplication(t *testing.T, app *appserver.Application) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Close(ctx); err != nil {
		t.Errorf("close application: %v", err)
	}
}

func connectHTTP(t *testing.T, endpoint string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "transport-test", Version: "1"}, nil)
	session, err := client.Connect(testContext(t), &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Timeout: 5 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect HTTP: %v", err)
	}
	return session
}

type stdioChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer
}

func startStdioChild(t *testing.T) *stdioChild {
	t.Helper()
	argo := httptest.NewServer(http.HandlerFunc(simulatedArgo))
	t.Cleanup(argo.Close)
	return startStdioChildWithEnv(t, argo.URL, nil)
}

func startStdioChildWithEnv(t *testing.T, argoURL string, extra map[string]string) *stdioChild {
	t.Helper()
	cmd := exec.Command(buildBinary(t))
	values := map[string]string{
		"ARGO_MCP_TRANSPORT": "stdio",
		"ARGO_BASE_URL":      argoURL,
		"ARGO_NAMESPACE":     "argo-ci",
		"MCP_AUDIT_ENABLED":  "true",
		"MCP_AUDIT_FILE":     filepath.Join(t.TempDir(), "audit.jsonl"),
	}
	for key, value := range extra {
		values[key] = value
	}
	cmd.Env = cleanEnv(values)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child := &stdioChild{cmd: cmd, stdin: stdin, stdout: stdout}
	cmd.Stderr = &child.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return child
}

func connectChild(t *testing.T, child *stdioChild) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, err := client.Connect(testContext(t), &mcp.IOTransport{Reader: child.stdout, Writer: child.stdin}, nil)
	if err != nil {
		t.Fatalf("connect stdio: %v; stderr=%s", err, child.stderr.String())
	}
	return session
}

func assertChildRead(t *testing.T, session *mcp.ClientSession) {
	t.Helper()
	listed, err := session.ListTools(testContext(t), nil)
	if err != nil || len(listed.Tools) != 25 {
		t.Fatalf("ListTools: count=%d err=%v", len(listed.Tools), err)
	}
	result, err := session.CallTool(testContext(t), &mcp.CallToolParams{Name: "get_workflow", Arguments: map[string]any{"name": "build-123"}})
	if err != nil || result.IsError {
		encoded, _ := json.Marshal(result)
		t.Fatalf("CallTool: result=%s err=%v", encoded, err)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go-argo-mcp")
	cmd := exec.Command("go", "build", "-o", path, "./cmd/go-argo-mcp")
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}
	return path
}

func cleanEnv(values map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != "MCPGODEBUG" && !strings.HasPrefix(key, "ARGO_") && !strings.HasPrefix(key, "MCP_") && !strings.HasPrefix(key, "OTEL_") {
			env = append(env, item)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func waitCommand(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return context.DeadlineExceeded
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
