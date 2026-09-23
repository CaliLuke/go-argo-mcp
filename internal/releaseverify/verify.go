package releaseverify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const commandTimeout = 15 * time.Second

func Verify(ctx context.Context, binary, expectedVersion string) error {
	if strings.TrimSpace(binary) == "" || strings.TrimSpace(expectedVersion) == "" {
		return errors.New("binary and version are required")
	}
	if err := verifyCLI(ctx, binary, expectedVersion); err != nil {
		return err
	}
	if err := verifyMCP(ctx, binary, expectedVersion); err != nil {
		return err
	}
	return nil
}

func verifyCLI(parent context.Context, binary, expected string) error {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	configureCommand(cmd)
	cmd.Env = sanitizedEnvironment()
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("CLI version check timed out or was canceled: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("CLI version check failed")
	}
	expectedOutput := "go-argo-mcp " + expected + "\n"
	if string(output) != expectedOutput {
		actual := strings.TrimSpace(string(output))
		actual = strings.TrimPrefix(actual, "go-argo-mcp ")
		if actual == "" {
			actual = "<empty>"
		}
		return fmt.Errorf("CLI version mismatch: expected %s, actual %s", expected, actual)
	}
	return nil
}

func verifyMCP(parent context.Context, binary, expected string) error {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	configureCommand(cmd)
	cmd.Env = sanitizedEnvironment()
	client := mcp.NewClient(&mcp.Implementation{Name: "go-argo-mcp-release-verifier", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("MCP initialize timed out or was canceled: %w", ctx.Err())
		}
		return fmt.Errorf("MCP initialize failed")
	}
	info := session.InitializeResult().ServerInfo
	var identityErr error
	if info == nil || info.Name != "go-argo-mcp" || info.Version != expected {
		actualName, actualVersion := "<missing>", "<missing>"
		if info != nil {
			actualName, actualVersion = info.Name, info.Version
		}
		identityErr = fmt.Errorf("MCP identity mismatch: expected go-argo-mcp %s, actual %s %s", expected, actualName, actualVersion)
	}
	closeErr := session.Close()
	if identityErr != nil && closeErr != nil {
		return errors.Join(identityErr, fmt.Errorf("close MCP session: %w", closeErr))
	}
	if identityErr != nil {
		return identityErr
	}
	if closeErr != nil {
		return fmt.Errorf("close MCP session: %w", closeErr)
	}
	return nil
}

func sanitizedEnvironment() []string {
	keys := []string{"PATH", "TMPDIR", "TMP", "TEMP"}
	if runtime.GOOS == "windows" {
		keys = append(keys, "SystemRoot", "WINDIR", "PATHEXT")
	}
	env := make([]string, 0, len(keys)+6)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env,
		"ARGO_MCP_TRANSPORT=stdio",
		"MCP_AUDIT_ENABLED=false",
		"MCP_ALLOW_MUTATIONS=false",
		"MCP_ALLOW_DESTRUCTIVE=false",
		"MCP_REQUIRE_CONFIRMATION=true",
	)
}
