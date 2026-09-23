package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDefaultsRemainFormulaCompatible(t *testing.T) {
	checksumsPath := writeChecksums(t)
	outputPath := filepath.Join(t.TempDir(), "Formula", "go-argo-mcp.rb")

	if err := run("1.2.3", checksumsPath, outputPath, "formula"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(output), "class GoArgoMcp < Formula") {
		t.Fatalf("default format did not render a formula:\n%s", output)
	}
}

func TestRunRendersCask(t *testing.T) {
	checksumsPath := writeChecksums(t)
	outputPath := filepath.Join(t.TempDir(), "Casks", "go-argo-mcp.rb")

	if err := run("1.2.3", checksumsPath, outputPath, "cask"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(output), `cask "go-argo-mcp" do`) {
		t.Fatalf("cask format did not render a cask:\n%s", output)
	}
}

func TestRunRejectsUnknownFormatBeforeReadingChecksums(t *testing.T) {
	err := run("1.2.3", filepath.Join(t.TempDir(), "missing-checksums.txt"), "-", "bottle")
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("run error = %v, want unknown format error", err)
	}
}

func writeChecksums(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checksums.txt")
	contents := strings.Join([]string{
		strings.Repeat("a", 64) + "  go-argo-mcp_Darwin_arm64.tar.gz",
		strings.Repeat("b", 64) + "  go-argo-mcp_Darwin_x86_64.tar.gz",
		strings.Repeat("c", 64) + "  go-argo-mcp_Linux_arm64.tar.gz",
		strings.Repeat("d", 64) + "  go-argo-mcp_Linux_x86_64.tar.gz",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write checksums: %v", err)
	}
	return path
}
