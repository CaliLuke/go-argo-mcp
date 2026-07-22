# Title

Loom OTEL bootstrap has no built-in local SQLite exporter

## Issue

The MCP migration workflow expects local traces and logs to persist to SQLite by default while production can redirect the same instrumentation to OTLP. Loom's OTEL bootstrap currently exposes OTLP HTTP exporters but no SQLite exporter or local persistent sink.

Adding an application-owned SQLite logging stack would duplicate framework bootstrap logic and add a database dependency to an otherwise stateless MCP server.

## Evidence

- `github.com/CaliLuke/loom/observability/otel` configures OTLP trace, metric, and log exporters.
- Searching the Loom and Loom-MCP repositories for `sqlite` and SQLite driver dependencies returns no implementation.
- `go-argo-mcp/internal/observability/otel.go` can enable OTLP through environment variables but cannot select a local SQLite sink.

## Repro

Create a Loom service, call `loomotel.New`, and attempt to configure a SQLite destination for local logs. The public configuration accepts OTLP endpoints and headers only.

Workaround: keep OTEL opt-in through `OTEL_EXPORTER_OTLP_ENDPOINT` and use the MCP JSONL audit log for local tool-call history.

## Correct Behavior

Loom should provide a supported local persistent exporter, or a documented exporter interface and SQLite implementation that can be selected without replacing runtime bootstrap.

## Version

- `github.com/CaliLuke/loom v1.7.0`
- `github.com/CaliLuke/loom-mcp v1.4.3`
- Go `1.26.1`

## Impact

This blocks the migration workflow's prescribed local SQLite OTEL default and encourages application-local observability glue that increases runtime complexity.

