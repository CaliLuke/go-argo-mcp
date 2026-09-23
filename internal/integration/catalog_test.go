package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/service"
)

const (
	toolCatalogStart = "<!-- tool-catalog:start -->"
	toolCatalogEnd   = "<!-- tool-catalog:end -->"
)

type documentedTool struct {
	readOnly    bool
	destructive bool
	idempotent  *bool
	guard       string
}

func TestREADMECatalogMatchesGeneratedSDK(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	documented := parseDocumentedTools(t, string(readme))

	argo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("listing tools must not call Argo")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer argo.Close()
	svc := service.NewArgoService(service.ArgoServiceConfig{
		Client:           argoapi.New(argoapi.Config{BaseURL: argo.URL}),
		DefaultNamespace: "argo-ci",
	})
	server, err := mcpargo.NewSDKServer(svc, nil)
	if err != nil {
		t.Fatalf("create SDK server: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session := connectHTTP(t, httpServer.URL)
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list generated SDK tools: %v", err)
	}

	liveNames := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		liveNames = append(liveNames, tool.Name)
		doc, ok := documented[tool.Name]
		if !ok {
			t.Errorf("generated tool %q is absent from the README catalog", tool.Name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("generated tool %q has no annotations", tool.Name)
			continue
		}
		if tool.Annotations.ReadOnlyHint != doc.readOnly {
			t.Errorf("%s readOnlyHint = %t, README says %t", tool.Name, tool.Annotations.ReadOnlyHint, doc.readOnly)
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Errorf("%s omits destructiveHint; README says %t", tool.Name, doc.destructive)
		} else if *tool.Annotations.DestructiveHint != doc.destructive {
			t.Errorf("%s destructiveHint = %t, README says %t", tool.Name, *tool.Annotations.DestructiveHint, doc.destructive)
		}
		if doc.idempotent != nil {
			if tool.Annotations.IdempotentHint != *doc.idempotent {
				t.Errorf("%s idempotentHint = %t, README says %t", tool.Name, tool.Annotations.IdempotentHint, *doc.idempotent)
			}
		}
		for _, flag := range strings.Split(doc.guard, "+") {
			if flag != "none" && !strings.Contains(tool.Description, flag) {
				t.Errorf("%s description does not advertise required guard %s: %q", tool.Name, flag, tool.Description)
			}
		}
	}

	documentedNames := make([]string, 0, len(documented))
	for name := range documented {
		documentedNames = append(documentedNames, name)
	}
	sort.Strings(liveNames)
	sort.Strings(documentedNames)
	if strings.Join(liveNames, "\n") != strings.Join(documentedNames, "\n") {
		t.Errorf("README and generated catalog differ\nREADME:\n%s\ngenerated:\n%s", strings.Join(documentedNames, "\n"), strings.Join(liveNames, "\n"))
	}
	t.Logf("README documents all %d generated SDK tools", len(liveNames))
}

func TestDestructiveToolsDeclareExplicitReadOnlyFalseInGeneratedCatalog(t *testing.T) {
	generated, err := os.ReadFile("../../gen/mcp_argo/register.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(generated)
	for _, tool := range []string{"terminate_workflow", "retry_workflow"} {
		marker := `Name: "` + tool + `"`
		end := strings.Index(source, marker)
		if end < 0 {
			t.Fatalf("missing %s", tool)
		}
		start := strings.LastIndex(source[:end], "Meta:")
		if start < 0 {
			t.Fatalf("missing metadata for %s", tool)
		}
		metadata := source[start:end]
		if !strings.Contains(metadata, `"readOnlyHint":    []string{"false"}`) {
			t.Fatalf("%s generated metadata omits explicit readOnlyHint=false: %s", tool, metadata)
		}
	}
}

func parseDocumentedTools(t *testing.T, readme string) map[string]documentedTool {
	t.Helper()
	if strings.Count(readme, toolCatalogStart) != 1 || strings.Count(readme, toolCatalogEnd) != 1 {
		t.Fatalf("README must contain exactly one tool catalog")
	}
	start := strings.Index(readme, toolCatalogStart)
	end := strings.Index(readme, toolCatalogEnd)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("README must contain one tool catalog between %s and %s", toolCatalogStart, toolCatalogEnd)
	}

	tools := make(map[string]documentedTool)
	for _, line := range strings.Split(readme[start+len(toolCatalogStart):end], "\n") {
		if !strings.HasPrefix(line, "| ") || strings.Contains(line, "---") || strings.Contains(line, "| Area |") {
			continue
		}
		columns := strings.Split(strings.Trim(line, "| "), " | ")
		if len(columns) != 7 {
			t.Fatalf("tool catalog row must have seven columns: %q", line)
		}
		name := strings.Trim(columns[1], "`")
		readOnly, err := strconv.ParseBool(columns[3])
		if err != nil {
			t.Fatalf("parse %s readOnlyHint: %v", name, err)
		}
		destructive, err := strconv.ParseBool(columns[4])
		if err != nil {
			t.Fatalf("parse %s destructiveHint: %v", name, err)
		}
		if _, duplicate := tools[name]; duplicate {
			t.Fatalf("README catalog contains duplicate tool %q", name)
		}
		var idempotent *bool
		if columns[5] != "n/a" {
			value, err := strconv.ParseBool(columns[5])
			if err != nil {
				t.Fatalf("parse %s idempotentHint: %v", name, err)
			}
			idempotent = &value
		}
		guard := strings.Trim(columns[6], "`")
		if readOnly && guard != "none" {
			t.Fatalf("read-only tool %s must not require a mutation guard", name)
		}
		if !readOnly && !strings.Contains(guard, "MCP_ALLOW_MUTATIONS") {
			t.Fatalf("mutation tool %s must require MCP_ALLOW_MUTATIONS", name)
		}
		if destructive != strings.Contains(guard, "MCP_ALLOW_DESTRUCTIVE") {
			t.Fatalf("tool %s has inconsistent destructiveHint and guard %q", name, guard)
		}
		tools[name] = documentedTool{readOnly: readOnly, destructive: destructive, idempotent: idempotent, guard: guard}
	}
	if len(tools) == 0 {
		t.Fatal("README tool catalog is empty")
	}
	return tools
}
