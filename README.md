# go-argo-mcp

A lightweight Go MCP server for connecting AI tools to [Argo Workflows](https://argo-workflows.readthedocs.io/). It uses Loom and Loom-MCP to expose a typed Streamable HTTP MCP endpoint with environment-only configuration.

The server is read-only by default. Mutation and destructive tools require explicit environment flags, namespace policy is enforced before Argo calls, and workflow termination can require a scoped one-time confirmation token.

## Tools

| Area | Tools |
| --- | --- |
| Workflows | `list_workflows`, `get_workflow`, `get_workflow_logs`, `retry_workflow`, `terminate_workflow` |
| CronWorkflows | `list_cron_workflows`, `get_cron_workflow`, `get_cron_history`, `toggle_cron_suspension` |
| WorkflowTemplates | `list_workflow_templates`, `get_workflow_template` |
| ClusterWorkflowTemplates | `list_cluster_workflow_templates`, `get_cluster_workflow_template` |

All 13 tools call real Argo HTTP endpoints. There are no mock fallbacks.

## Install

Install the native binary with Homebrew:

```bash
brew install CaliLuke/tap/go-argo-mcp
```

Verify the installed binary:

```bash
go-argo-mcp --version
```

Or build it directly with Go 1.26.1:

```bash
go install github.com/CaliLuke/go-argo-mcp/cmd/go-argo-mcp@latest
```

## Run

Set the Argo endpoint and start the installed binary:

```bash
export ARGO_BASE_URL=http://localhost:2746
export ARGO_NAMESPACE=default
go-argo-mcp
```

The server listens on loopback by default. The MCP endpoint is `http://127.0.0.1:8080/rpc`; health is available at `http://127.0.0.1:8080/healthz`.

Configure Codex with Streamable HTTP:

```bash
codex mcp add argo_workflows --url http://127.0.0.1:8080/rpc
```

Or add the equivalent configuration manually:

```toml
[mcp_servers.argo_workflows]
url = "http://127.0.0.1:8080/rpc"
```

Confirm that the process and MCP registration are available:

```bash
curl --fail http://127.0.0.1:8080/healthz
codex mcp list
```

The binary is an HTTP server, so the MCP client does not launch it. Keep `go-argo-mcp` running with your preferred process supervisor. When running it as a service, set an absolute `MCP_AUDIT_FILE` path in a writable directory.

## Configuration

### Argo

| Variable | Default | Purpose |
| --- | --- | --- |
| `ARGO_MCP_ADDR` | `127.0.0.1:8080` | HTTP listen address; set explicitly to expose it beyond the local machine |
| `ARGO_BASE_URL` | required | Argo Server API base URL |
| `ARGO_NAMESPACE` | `default` | Namespace used when a tool omits one |
| `ARGO_TOKEN` | empty | Bearer token; takes precedence over Basic auth |
| `ARGO_USERNAME` / `ARGO_PASSWORD` | empty | Basic authentication |
| `ARGO_INSECURE_SKIP_TLS_VERIFY` | `false` | Disable certificate verification explicitly |
| `ARGO_TLS_SERVER_NAME` | empty | Override TLS server name/SNI |
| `ARGO_REQUEST_TIMEOUT_SECONDS` | `30` | Argo request timeout |

### Safety

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_ALLOW_MUTATIONS` | `false` | Enable retry and CronWorkflow suspension changes |
| `MCP_ALLOW_DESTRUCTIVE` | `false` | Enable workflow termination |
| `MCP_REQUIRE_CONFIRMATION` | `true` | Require a dry-run token before termination |
| `MCP_NAMESPACES_ALLOW` | empty/all | Comma-separated namespace allow list; `*` permits all |
| `MCP_NAMESPACES_DENY` | empty | Comma-separated deny list; deny takes precedence |

Termination confirmation tokens are cryptographically random, expire after five minutes, are valid once, and are scoped to the exact action, namespace, and workflow.

### Audit and observability

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_AUDIT_ENABLED` | `true` | Append MCP tool-call audit records |
| `MCP_AUDIT_FILE` | `./mcp-audit.log` | JSONL audit destination |
| `OTEL_ENABLED` | `false` | Enable Loom OpenTelemetry bootstrap |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP HTTP collector endpoint |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Use insecure OTLP transport |
| `OTEL_EXPORTER_OTLP_HEADERS` | empty | Comma-separated OTLP headers |

Audit arguments redact keys containing `token`, `password`, or `secret`.

## Agent safety model

- Read-only tools work with the defaults.
- `retry_workflow` and `toggle_cron_suspension` require `MCP_ALLOW_MUTATIONS=true`.
- `terminate_workflow` additionally requires `MCP_ALLOW_DESTRUCTIVE=true`.
- With confirmation enabled, call `terminate_workflow` in dry-run mode first, inspect the preview, then repeat the exact action with its one-time token.
- Use `MCP_NAMESPACES_ALLOW` in shared environments so agents cannot select an unintended namespace. Entries in `MCP_NAMESPACES_DENY` always take precedence.

Do not expose the HTTP listener to an untrusted network. The server relies on its network boundary and Argo credentials; it does not add client authentication to `/rpc`.

## Development

The Loom design is the source of truth:

```bash
loom gen github.com/CaliLuke/go-argo-mcp/design
```

Never edit generated files under `gen/` manually.

Quality gates:

```bash
prek run --all-files
./check.sh --fix
```

Validate the native release artifacts locally:

```bash
goreleaser check
make release-snapshot
```

Pushing a `v*` tag creates GitHub release archives for macOS, Linux, and Windows and updates `CaliLuke/homebrew-tap`. The repository must define a `HOMEBREW_TAP_GITHUB_TOKEN` Actions secret with write access to that tap.

The test suite includes focused HTTP client tests, confirmation and namespace-policy tests, audit interception tests, and an end-to-end official MCP Go SDK test that advertises and invokes all 13 tools against a simulated Argo API.

## Troubleshooting

- `configuration_error`: set `ARGO_BASE_URL` to the Argo Server URL, including its scheme and port.
- `401` or `403` from Argo: check `ARGO_TOKEN`, or `ARGO_USERNAME` and `ARGO_PASSWORD`.
- `namespace_denied`: choose a namespace permitted by `MCP_NAMESPACES_ALLOW` and not present in `MCP_NAMESPACES_DENY`.
- TLS hostname failures: set `ARGO_TLS_SERVER_NAME` to the certificate name. Use `ARGO_INSECURE_SKIP_TLS_VERIFY=true` only for an explicitly trusted development endpoint.

## Architecture

- `design/` — Loom service and MCP contracts
- `gen/` — committed Loom/Loom-MCP generated transport code
- `internal/argoapi/` — small direct Argo HTTP client
- `internal/service/` — tool behavior and safety policy
- `internal/confirmation/` — scoped one-time confirmations
- `internal/mcpaudit/` — generated MCP interceptor-backed JSONL audit
- `cmd/go-argo-mcp/` — server bootstrap
