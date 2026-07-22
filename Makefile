.PHONY: build check generate release-snapshot run test

generate:
	loom gen github.com/CaliLuke/go-argo-mcp/design

build:
	CGO_ENABLED=0 go build -trimpath -o go-argo-mcp ./cmd/go-argo-mcp

test:
	go test -race ./...

check:
	prek run --all-files

run:
	go run ./cmd/go-argo-mcp

release-snapshot:
	goreleaser release --snapshot --clean
