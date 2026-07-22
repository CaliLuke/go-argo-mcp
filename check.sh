#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

fix="${1:-}"

echo "==> build static binary"
CGO_ENABLED=0 go build ./...

echo "==> vet"
go vet ./...

echo "==> imports and formatting"
if [[ "$fix" == "--fix" ]]; then
  goimports -w $(go list -f '{{.Dir}}' ./...)
else
  unformatted="$(goimports -l $(go list -f '{{.Dir}}' ./...))"
  if [[ -n "$unformatted" ]]; then
    printf '%s\n' "$unformatted"
    exit 1
  fi
fi

echo "==> module tidy drift"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
cp go.mod "$tmp_dir/go.mod"
cp go.sum "$tmp_dir/go.sum"
go mod tidy
diff -u "$tmp_dir/go.mod" go.mod
diff -u "$tmp_dir/go.sum" go.sum

echo "==> golangci-lint"
if [[ "$fix" == "--fix" ]]; then
  golangci-lint run --fix
else
  golangci-lint run
fi

echo "==> dead code"
dead_output="$(deadcode -test ./... || true)"
if [[ -n "$dead_output" ]]; then
  printf '%s\n' "$dead_output"
  exit 1
fi

echo "==> duplication"
dupl_output="$(find ./cmd ./design ./internal -name '*.go' ! -name '*_test.go' -print | dupl -plumbing -threshold 75 -files || true)"
if [[ -n "$dupl_output" ]]; then
  printf '%s\n' "$dupl_output"
  exit 1
fi

echo "==> race tests"
go test -race -count=1 -timeout=120s ./...

echo "All quality gates passed."
