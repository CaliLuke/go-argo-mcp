package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/CaliLuke/go-argo-mcp/internal/server"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("go-argo-mcp %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Printf("go-argo-mcp: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := server.ConfigFromEnv(version)
	if err != nil {
		return err
	}
	return server.Run(ctx, cfg)
}
