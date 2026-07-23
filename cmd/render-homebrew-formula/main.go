package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CaliLuke/go-argo-mcp/internal/homebrew"
)

func main() {
	version := flag.String("version", "", "release version without a v prefix")
	checksumsPath := flag.String("checksums", "", "path to GoReleaser checksums.txt")
	outputPath := flag.String("output", "-", "formula output path, or - for stdout")
	flag.Parse()
	if err := run(*version, *checksumsPath, *outputPath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "render Homebrew formula: %v\n", err)
		os.Exit(1)
	}
}

func run(version, checksumsPath, outputPath string) error {
	if checksumsPath == "" {
		return fmt.Errorf("-checksums is required")
	}
	checksumsFile, err := os.Open(checksumsPath)
	if err != nil {
		return fmt.Errorf("open checksums: %w", err)
	}
	defer func() { _ = checksumsFile.Close() }()
	checksums, err := homebrew.ParseChecksums(checksumsFile)
	if err != nil {
		return err
	}
	formula, err := homebrew.Render(version, checksums)
	if err != nil {
		return err
	}
	if outputPath == "-" {
		_, err = os.Stdout.Write(formula)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, formula, 0o644); err != nil {
		return fmt.Errorf("write formula: %w", err)
	}
	return nil
}
