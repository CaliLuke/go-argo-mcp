package releaseverify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVerifyRejectsCLIIdentityMismatchAndSanitizesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "server")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'go-argo-mcp 1.2.3'; exit; fi\necho \"$SECRET_VALUE\" >&2\nexit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SECRET_VALUE", "must-not-leak")
	err := Verify(context.Background(), binary, "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "expected 9.9.9") || !strings.Contains(err.Error(), "actual 1.2.3") || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyHonorsTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	binary := filepath.Join(t.TempDir(), "server")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Verify(ctx, binary, "1")
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("error=%v elapsed=%v", err, time.Since(start))
	}
}

func TestSanitizedEnvironmentExcludesCredentialsAndForcesSafeStdio(t *testing.T) {
	t.Setenv("ARGO_TOKEN", "argo-secret")
	t.Setenv("KUBERNETES_TOKEN", "kube-secret")
	t.Setenv("SECRET_VALUE", "other-secret")
	env := strings.Join(sanitizedEnvironment(), "\n")
	for _, forbidden := range []string{"ARGO_TOKEN=", "KUBERNETES_TOKEN=", "SECRET_VALUE="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("sanitized environment contains %s", forbidden)
		}
	}
	for _, required := range []string{"ARGO_MCP_TRANSPORT=stdio", "MCP_AUDIT_ENABLED=false", "MCP_ALLOW_MUTATIONS=false", "MCP_ALLOW_DESTRUCTIVE=false"} {
		if !strings.Contains(env, required) {
			t.Fatalf("sanitized environment omits %s", required)
		}
	}
}

func TestVerifyOfficialSDKHandshakeAcceptsMatchingAndRejectsMCPMismatch(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	matching := buildCandidate(t, root, "0.3.0")
	if verifyErr := Verify(context.Background(), matching, "0.3.0"); verifyErr != nil {
		t.Fatalf("matching candidate: %v", verifyErr)
	}
	mismatch := buildCandidate(t, root, "9.9.9")
	err = Verify(context.Background(), mismatch, "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "expected go-argo-mcp 9.9.9") || !strings.Contains(err.Error(), "actual go-argo-mcp 0.3.0") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func buildCandidate(t *testing.T, root, version string) string {
	t.Helper()
	name := "go-argo-mcp-" + version
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", binary, "./cmd/go-argo-mcp")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build candidate: %v: %s", err, output)
	}
	return binary
}
