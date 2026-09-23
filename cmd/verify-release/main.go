package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/CaliLuke/go-argo-mcp/internal/releaseverify"
)

func main() {
	binary := flag.String("binary", "", "path to the release candidate binary")
	version := flag.String("version", "", "expected release version")
	flag.Parse()
	if err := releaseverify.Verify(context.Background(), *binary, *version); err != nil {
		fmt.Fprintf(os.Stderr, "verify release: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("verified go-argo-mcp %s CLI and MCP identity\n", *version)
}
