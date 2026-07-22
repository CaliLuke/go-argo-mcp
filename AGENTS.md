# Repository guidance

This is a Go-only MCP server for Argo Workflows.

- Treat `design/*.go` as the API and MCP source of truth.
- After design changes, run `loom gen github.com/CaliLuke/go-argo-mcp/design`.
- Never hand-edit files under `gen/`.
- Keep application logic in `internal/` and entrypoints in `cmd/`.
- Runtime configuration must use environment variables, not config files.
- Preserve the read-only defaults and test mutation/destructive safeguards.
- Run `prek run --all-files` before publishing changes.

