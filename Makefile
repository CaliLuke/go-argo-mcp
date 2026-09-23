.PHONY: build check formula-snapshot generate release-snapshot run test

FORMULA_VERSION ?= 0.0.0

generate:
	go run ./cmd/generate-argo-models
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

formula-snapshot: release-snapshot
	go run ./cmd/render-homebrew-formula \
		-version "$(FORMULA_VERSION)" \
		-checksums dist/checksums.txt \
		-output dist/homebrew/Formula/go-argo-mcp.rb
	go run ./cmd/render-homebrew-formula \
		-format cask \
		-version "$(FORMULA_VERSION)" \
		-checksums dist/checksums.txt \
		-output dist/homebrew/Casks/go-argo-mcp.rb
