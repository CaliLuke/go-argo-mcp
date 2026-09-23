package homebrew

import (
	"strings"
	"testing"
)

func TestRenderFormula(t *testing.T) {
	checksums := completeChecksums()
	formula, err := Render("1.2.3", checksums)
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	for _, expected := range []string{
		`version "1.2.3"`,
		`releases/download/v1.2.3/go-argo-mcp_Darwin_arm64.tar.gz`,
		`sha256 "` + strings.Repeat("a", 64) + `"`,
		`system bin/"go-argo-mcp", "--version"`,
	} {
		if !strings.Contains(string(formula), expected) {
			t.Errorf("formula does not contain %q", expected)
		}
	}
}

func TestRenderCaskUsesDarwinArchivesForBothArchitectures(t *testing.T) {
	cask, err := RenderCask("1.2.3", map[string]string{
		darwinARM64: strings.Repeat("a", 64),
		darwinAMD64: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("RenderCask returned error: %v", err)
	}
	for _, expected := range []string{
		`cask "go-argo-mcp" do`,
		`arch arm: "arm64", intel: "x86_64"`,
		`version "1.2.3"`,
		`sha256 arm:   "` + strings.Repeat("a", 64) + `",`,
		`intel: "` + strings.Repeat("b", 64) + `"`,
		`releases/download/v#{version}/go-argo-mcp_Darwin_#{arch}.tar.gz`,
		`binary "go-argo-mcp"`,
	} {
		if !strings.Contains(string(cask), expected) {
			t.Errorf("cask does not contain %q", expected)
		}
	}
}

func TestRenderCaskDoesNotRequireLinuxChecksums(t *testing.T) {
	_, err := RenderCask("1.2.3", map[string]string{
		darwinARM64: strings.Repeat("a", 64),
		darwinAMD64: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("RenderCask returned error: %v", err)
	}
}

func TestRenderCaskRejectsInvalidInput(t *testing.T) {
	if _, err := RenderCask("v1.2.3", completeChecksums()); err == nil {
		t.Fatal("expected invalid version error")
	}
	if _, err := RenderCask("1.2.3", map[string]string{
		darwinARM64: strings.Repeat("a", 64),
	}); err == nil {
		t.Fatal("expected missing checksum error")
	}
	if _, err := RenderCask("1.2.3", map[string]string{
		darwinARM64: strings.Repeat("x", 64),
		darwinAMD64: strings.Repeat("b", 64),
	}); err == nil {
		t.Fatal("expected invalid checksum error")
	}
}

func TestRenderRejectsInvalidInput(t *testing.T) {
	if _, err := Render("v1.2.3", map[string]string{}); err == nil {
		t.Fatal("expected invalid version error")
	}
	if _, err := Render("1.2.3", map[string]string{}); err == nil {
		t.Fatal("expected missing checksum error")
	}
	if _, err := Render("1.2.3", map[string]string{
		darwinARM64: strings.Repeat("x", 64),
		darwinAMD64: strings.Repeat("b", 64),
		linuxARM64:  strings.Repeat("c", 64),
		linuxAMD64:  strings.Repeat("d", 64),
	}); err == nil {
		t.Fatal("expected invalid checksum error")
	}
	if _, err := ParseChecksums(strings.NewReader("not-a-checksum  file")); err == nil {
		t.Fatal("expected invalid checksum error")
	}
}

func completeChecksums() map[string]string {
	return map[string]string{
		darwinARM64: strings.Repeat("a", 64),
		darwinAMD64: strings.Repeat("b", 64),
		linuxARM64:  strings.Repeat("c", 64),
		linuxAMD64:  strings.Repeat("d", 64),
	}
}
