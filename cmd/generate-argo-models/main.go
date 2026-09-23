package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/CaliLuke/go-argo-mcp/internal/argomodelgen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	schemaPath := filepath.FromSlash("api/argo/v3.7.3/swagger.json")
	projectionPath := filepath.FromSlash("api/argo/projection.json")
	outputPath := filepath.FromSlash("internal/argoapi/models/models.gen.go")

	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", schemaPath, err)
	}
	projection, err := os.ReadFile(projectionPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", projectionPath, err)
	}
	generated, err := argomodelgen.Generate(schema, projection)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, generated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}
