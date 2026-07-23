package homebrew

import (
	"strings"
	"testing"
)

func TestRenderFormula(t *testing.T) {
	checksums, err := ParseChecksums(strings.NewReader(strings.Join([]string{
		strings.Repeat("a", 64) + "  " + darwinARM64,
		strings.Repeat("b", 64) + "  " + darwinAMD64,
		strings.Repeat("c", 64) + "  " + linuxARM64,
		strings.Repeat("d", 64) + "  " + linuxAMD64,
	}, "\n")))
	if err != nil {
		t.Fatalf("ParseChecksums returned error: %v", err)
	}
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
