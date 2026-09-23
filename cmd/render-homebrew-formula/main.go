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
	outputPath := flag.String("output", "-", "output path, or - for stdout")
	format := flag.String("format", "formula", "output format: formula or cask")
	flag.Parse()
	if err := run(*version, *checksumsPath, *outputPath, *format); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "render Homebrew package: %v\n", err)
		os.Exit(1)
	}
}

func run(version, checksumsPath, outputPath, format string) error {
	if format != "formula" && format != "cask" {
		return fmt.Errorf("unknown format %q (want formula or cask)", format)
	}
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
	var output []byte
	switch format {
	case "formula":
		output, err = homebrew.Render(version, checksums)
	case "cask":
		output, err = homebrew.RenderCask(version, checksums)
	}
	if err != nil {
		return err
	}
	if outputPath == "-" {
		_, err = os.Stdout.Write(output)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
