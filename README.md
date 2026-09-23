# go-argo-mcp

A lightweight Go MCP server for connecting AI tools to [Argo Workflows](https://argo-workflows.readthedocs.io/). It uses Loom and Loom-MCP to expose typed stdio, stateful Streamable HTTP, and stateless Streamable HTTP transports with environment-only configuration.

The server is read-only by default. Mutation and destructive tools require explicit environment flags. Namespace policy applies before each Argo call. Workflow retry and termination can require a scoped, one-time confirmation token.

## Tools

The table is the complete tool catalog. The integration test computes its total from the generated SDK catalog and checks every row.

<!-- tool-catalog:start -->

| Area | Tool | Purpose | readOnlyHint | destructiveHint | idempotentHint | Required access |
| --- | --- | --- | --- | --- | --- | --- |
| Workflows | `list_workflows` | List workflows with filters and cursor pagination | true | false | n/a | `none` |
| Workflows | `get_workflow` | Get compact workflow details | true | false | n/a | `none` |
| Workflows | `get_workflow_logs` | Get bounded workflow logs | true | false | n/a | `none` |
| Workflows | `get_workflow_nodes` | Get filtered node summaries with offset pagination | true | false | n/a | `none` |
| Workflows | `get_workflow_events` | Observe a bounded window of workflow events | true | false | n/a | `none` |
| Workflows | `get_workflow_artifacts` | Get artifact metadata and trusted Argo download links | true | false | n/a | `none` |
| Workflows | `suspend_workflow` | Suspend a workflow | false | false | false | `MCP_ALLOW_MUTATIONS` |
| Workflows | `resume_workflow` | Resume a whole workflow | false | false | false | `MCP_ALLOW_MUTATIONS` |
| Workflows | `resubmit_workflow` | Create a workflow from an existing workflow | false | false | false | `MCP_ALLOW_MUTATIONS` |
| Workflows | `retry_workflow` | Preview or retry a workflow | false | true | n/a | `MCP_ALLOW_MUTATIONS+MCP_ALLOW_DESTRUCTIVE` |
| Workflows | `terminate_workflow` | Preview or terminate a workflow | false | true | n/a | `MCP_ALLOW_MUTATIONS+MCP_ALLOW_DESTRUCTIVE` |
| CronWorkflows | `list_cron_workflows` | List CronWorkflows with cursor pagination | true | false | n/a | `none` |
| CronWorkflows | `get_cron_workflow` | Get compact CronWorkflow details | true | false | n/a | `none` |
| CronWorkflows | `get_cron_history` | List workflow history for a CronWorkflow | true | false | n/a | `none` |
| CronWorkflows | `toggle_cron_suspension` | Change CronWorkflow suspension | false | false | n/a | `MCP_ALLOW_MUTATIONS` |
| CronWorkflows | `trigger_cron_workflow` | Create a workflow from a CronWorkflow | false | false | false | `MCP_ALLOW_MUTATIONS` |
| WorkflowTemplates | `list_workflow_templates` | List WorkflowTemplates with cursor pagination | true | false | n/a | `none` |
| WorkflowTemplates | `get_workflow_template` | Get compact WorkflowTemplate details | true | false | n/a | `none` |
| WorkflowTemplates | `submit_workflow_template` | Create a workflow from a template | false | false | false | `MCP_ALLOW_MUTATIONS` |
| ClusterWorkflowTemplates | `list_cluster_workflow_templates` | List ClusterWorkflowTemplates with cursor pagination | true | false | n/a | `none` |
| ClusterWorkflowTemplates | `get_cluster_workflow_template` | Get compact ClusterWorkflowTemplate details | true | false | n/a | `none` |
| Archive | `list_archived_workflows` | List archived workflows with cursor pagination | true | false | n/a | `none` |
| Archive | `get_archived_workflow` | Get bounded archived workflow details | true | false | n/a | `none` |
| Validation | `lint_workflow` | Validate a complete Workflow manifest with Argo | true | false | n/a | `none` |
| Validation | `lint_workflow_template` | Validate a complete workflow template manifest with Argo | true | false | n/a | `none` |

<!-- tool-catalog:end -->

All tools call real Argo HTTP endpoints. There are no mock fallbacks.

CronWorkflow reads include every configured schedule and its timezone. `get_cron_workflow` reports the next nominal run when active; `when` and stop conditions can still prevent that run.

### Collection pagination

`list_workflows`, `list_cron_workflows`, `list_workflow_templates`,
`list_cluster_workflow_templates`, and `list_archived_workflows` return at most
50 items by default. `get_cron_history` returns at most 10. These tools accept
`limit` from 1 to 200 and an optional `continue` token.

Start with the filters and limit you want:

```json
{
  "name": "list_workflows",
  "arguments": {
    "namespace": "argo-ci",
    "status": "Running",
    "limit": 25
  }
}
```

When the result has `"has_more": true`, pass its exact `continue` value back
with the same limit and filters:

```json
{
  "name": "list_workflows",
  "arguments": {
    "namespace": "argo-ci",
    "status": "Running",
    "limit": 25,
    "continue": "opaque-token-from-the-previous-result"
  }
}
```

Continuation values are opaque Argo tokens. Do not trim, decode, modify, or
reuse them with different filters or a different limit. `has_more` is true
exactly when a nonempty continuation token is returned. Empty and exhausted
results contain an empty array, set `has_more` to false, and omit `continue`.
Expired or rejected tokens return an error; the server does not restart the
scan. `get_cron_history` sorts each returned result by start time, newest first,
but pagination does not provide a global ordering guarantee across pages.

`get_workflow_nodes` and `get_workflow_artifacts` use offset pagination. Both
tools default to `offset: 0` and `limit: 50`. Their maximum limit is 200. Pass
`next_offset` unchanged to get the next page. An absent `next_offset` means
that the filtered result is complete. Node children can refer to nodes outside
the current page.

### Bounded diagnostics

`get_workflow_events` observes the Argo event stream for a short interval. It
does not query event history. The default interval is two seconds, and the
allowed range is one to ten seconds. The default event limit is 50, and the
maximum is 200. The result states whether the limit ended the observation.

Node, archive, artifact, and event results apply field and collection bounds.
Use `truncated`, `fields_truncated`, and `next_offset` to decide whether to
request another page or account for shortened fields. Artifact results contain
metadata and links on the configured Argo server. They do not contain object
store credentials, signed URLs, or artifact bytes.

Archive tools require the Argo workflow archive and suitable archive RBAC.
Every archive request includes the authorized namespace. The server rejects an
archived workflow response from a different namespace.

### Lint and submit

Lint sends one complete JSON object to Argo. Supply that object as the
`manifest_json` string. Its maximum UTF-8 size is 256 KiB. Lint is read-only,
although Argo uses a POST endpoint.

```json
{
  "name": "lint_workflow",
  "arguments": {
    "namespace": "argo-ci",
    "manifest_json": "{\"apiVersion\":\"argoproj.io/v1alpha1\",\"kind\":\"Workflow\",\"metadata\":{\"generateName\":\"build-\"},\"spec\":{\"entrypoint\":\"main\",\"templates\":[{\"name\":\"main\",\"container\":{\"image\":\"alpine:3.22\",\"command\":[\"echo\"],\"args\":[\"ok\"]}}]}}"
  }
}
```

For `lint_workflow_template`, set `cluster_scope: true` for a
ClusterWorkflowTemplate. A cluster-scoped manifest must not contain a namespace,
and the request must omit `namespace`.

Template submission accepts only a template name, scope, namespace, and string
parameters. It does not accept a service account override or an arbitrary
submission body.

```json
{
  "name": "submit_workflow_template",
  "arguments": {
    "namespace": "argo-ci",
    "template_name": "build-template",
    "parameters": {
      "revision": "main"
    }
  }
}
```

## Install

Install the native binary with Homebrew:

```bash
brew install CaliLuke/tap/go-argo-mcp
```

Verify the installed binary:

```bash
go-argo-mcp --version
```

Or build it directly with Go 1.27.1:

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

For an authenticated listener, set a separate inbound token and configure the client to send it as a Bearer token:

```bash
export ARGO_MCP_AUTH_TOKEN='replace-with-a-random-secret'
```

`ARGO_MCP_AUTH_TOKEN` protects MCP clients connecting to `/rpc`. It is separate from `ARGO_TOKEN`, which the server sends only to the Argo API.

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

In HTTP mode, the MCP client does not launch the binary. Keep `go-argo-mcp` running with your preferred process supervisor. When running it as a service, set an absolute `MCP_AUDIT_FILE` path in a writable directory.

### Transport modes

`ARGO_MCP_TRANSPORT` selects one of three modes:

| Value | Behavior |
| --- | --- |
| `http` | Default. Stateful Streamable HTTP at `/rpc`, plus `/healthz` |
| `http-stateless` | Sessionless Streamable HTTP at `/rpc`, plus `/healthz` |
| `stdio` | MCP JSON-RPC over stdin/stdout; no network listener |

Stateless HTTP accepts protocol traffic through `POST` only. It returns `405 Method Not Allowed` for `GET` and `DELETE`, emits no MCP session ID, and does not provide subscriptions, replay, or server-to-client requests. Startup rejects `MCPGODEBUG=allowsessionsinstateless=1`, because that SDK compatibility flag restores session behavior.

In stdio mode, `ARGO_MCP_ADDR` is ignored. Stdout is reserved for MCP JSON-RPC frames; operational logs use stderr. Audit and telemetry retain their configured destinations, and audit paths that resolve to stdout are rejected before the SDK starts. EOF, SIGINT, and SIGTERM close the transport, audit writer, and telemetry runtime. HTTP signals stop accepting new work and allow in-flight requests up to five seconds to finish before forced shutdown.

Destructive confirmation tokens are stored in the server process. A token remains one-time and scoped to the exact action, but stateless HTTP does not make it portable across replicas. Route a preview and its confirmed action to the same process or use one replica until shared confirmation persistence is available.

### Local stdio clients

Use an absolute installed binary path and an absolute writable audit path in desktop client configuration. Replace the sample paths when Homebrew or your home directory uses a different location.

Codex CLI:

```bash
codex mcp add argo_workflows \
  --env ARGO_MCP_TRANSPORT=stdio \
  --env ARGO_BASE_URL=http://localhost:2746 \
  --env ARGO_NAMESPACE=default \
  --env MCP_AUDIT_FILE=/Users/you/Library/Logs/go-argo-mcp/audit.jsonl \
  -- /opt/homebrew/bin/go-argo-mcp
```

Claude Desktop and Cursor use the same `mcpServers` entry in their respective JSON configuration files:

```json
{
  "mcpServers": {
    "argo_workflows": {
      "command": "/opt/homebrew/bin/go-argo-mcp",
      "args": [],
      "env": {
        "ARGO_MCP_TRANSPORT": "stdio",
        "ARGO_BASE_URL": "http://localhost:2746",
        "ARGO_NAMESPACE": "default",
        "MCP_AUDIT_FILE": "/Users/you/Library/Logs/go-argo-mcp/audit.jsonl"
      }
    }
  }
}
```

VS Code `.vscode/mcp.json`:

```json
{
  "servers": {
    "argo_workflows": {
      "type": "stdio",
      "command": "/opt/homebrew/bin/go-argo-mcp",
      "args": [],
      "env": {
        "ARGO_MCP_TRANSPORT": "stdio",
        "ARGO_BASE_URL": "http://localhost:2746",
        "ARGO_NAMESPACE": "default",
        "MCP_AUDIT_FILE": "/Users/you/Library/Logs/go-argo-mcp/audit.jsonl"
      }
    }
  }
}
```

Configuration references: [Codex MCP commands](https://learn.chatgpt.com/docs/developer-commands#codex-mcp), [Claude Desktop local servers](https://modelcontextprotocol.io/docs/2026-07-28/develop/connect-local-servers), [Cursor MCP configuration](https://prod.cursor.com/help/customization/mcp), and [VS Code MCP configuration](https://code.visualstudio.com/docs/agents/reference/mcp-configuration).

## Configuration

### Argo

| Variable | Default | Purpose |
| --- | --- | --- |
| `ARGO_MCP_TRANSPORT` | `http` | `http`, `http-stateless`, or `stdio` |
| `ARGO_MCP_ADDR` | `127.0.0.1:8080` | HTTP listen address; set explicitly to expose it beyond the local machine |
| `ARGO_MCP_AUTH_TOKEN` | disabled | Bearer token required on every `/rpc` request when set |
| `ARGO_MCP_ALLOW_UNAUTHENTICATED` | `false` | Explicitly acknowledge that a non-loopback listener is authenticated by a trusted proxy or mesh |
| `ARGO_MCP_ALLOWED_HOSTS` | bind-derived | Comma-separated exact request authorities, each with an explicit port |
| `ARGO_MCP_ALLOWED_ORIGINS` | none | Comma-separated exact HTTP(S) origins accepted in addition to the direct origin |
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
| `MCP_ALLOW_MUTATIONS` | `false` | Enable mutation tools, including retry and termination when their other safeguards pass |
| `MCP_ALLOW_DESTRUCTIVE` | `false` | Enable retry and termination when mutation access is also enabled |
| `MCP_REQUIRE_CONFIRMATION` | `true` | Require a scoped dry-run token before retry or termination |
| `MCP_NAMESPACES_ALLOW` | empty/all | Comma-separated namespace allow list; `*` permits all |
| `MCP_NAMESPACES_DENY` | empty | Comma-separated deny list; deny takes precedence |

Confirmation tokens are cryptographically random and expire after five minutes.
Each token is valid once. A retry token binds the action, namespace, workflow,
and `restart_successful` value. A termination token also binds the reason.

### Audit and observability

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_AUDIT_ENABLED` | `true` | Append MCP tool-call audit records |
| `MCP_AUDIT_FILE` | `./mcp-audit.log` | JSONL audit destination |
| `OTEL_ENABLED` | `false` | Enable Loom OpenTelemetry bootstrap |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP HTTP collector endpoint |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Use insecure OTLP transport |
| `OTEL_EXPORTER_OTLP_HEADERS` | empty | Comma-separated OTLP headers |

Audit arguments redact keys containing `token`, `password`, or `secret`. Audit
records replace lint manifests with `REDACTED`. For template submission,
resubmission, and CronWorkflow triggers, they retain only sorted parameter names
and the parameter count. They never retain parameter values. Response summaries
retain only status, count, namespace, and name. Workflow logs and confirmation
tokens are not written to the audit file.

## Agent safety model

- Read-only tools work with the defaults.
- `toggle_cron_suspension`, `submit_workflow_template`, `suspend_workflow`, `resume_workflow`, `resubmit_workflow`, and `trigger_cron_workflow` require `MCP_ALLOW_MUTATIONS=true`.
- `retry_workflow` and `terminate_workflow` require both `MCP_ALLOW_MUTATIONS=true` and `MCP_ALLOW_DESTRUCTIVE=true`.
- `retry_workflow` defaults to preview mode. Only an explicit `dry_run: false` can dispatch the retry.
- With confirmation enabled, preview a retry or termination first. Inspect the preview, then repeat the exact action with its one-time token.
- Use `MCP_NAMESPACES_ALLOW` in shared environments so agents cannot select an unintended namespace. Entries in `MCP_NAMESPACES_DENY` always take precedence.

For example, first preview a retry:

```json
{
  "name": "retry_workflow",
  "arguments": {
    "namespace": "argo-ci",
    "name": "build-123",
    "restart_successful": false
  }
}
```

Then copy the returned token into the exact confirmed request:

```json
{
  "name": "retry_workflow",
  "arguments": {
    "namespace": "argo-ci",
    "name": "build-123",
    "restart_successful": false,
    "dry_run": false,
    "confirmation_token": "token-from-the-preview"
  }
}
```

When confirmation is disabled, retry still requires both access flags and an
explicit `dry_run: false`. The server sends each mutation request once. A
network or server failure after dispatch can have an uncertain outcome. Check
the workflow state before you retry any mutation.

HTTP startup fails closed. A non-loopback bind requires `ARGO_MCP_AUTH_TOKEN` or the explicit `ARGO_MCP_ALLOW_UNAUTHENTICATED=true` acknowledgement for deployments where a trusted proxy or service mesh authenticates clients. Wildcard binds also require `ARGO_MCP_ALLOWED_HOSTS`.

Host entries are exact authorities such as `mcp.internal.example:443`. Origin entries are exact origins such as `https://console.internal.example`; wildcards are rejected. The server validates the direct `Host` and ignores forwarded headers. An allowed cross-origin request still needs valid `/rpc` authentication. This origin setting validates MCP requests; configure CORS and preflight handling on the trusted proxy when a browser connects across origins.

`/healthz` remains unauthenticated for probes and returns only `ok`. It does not expose configuration, Argo connectivity, namespaces, or credential state. Stdio opens no listener and ignores all HTTP-only security variables.

## Development

The Loom design remains the MCP contract source of truth. The Argo HTTP client
uses a small generated projection of the pinned Argo Workflows v3.7.3 Swagger
schema. Generate both sets of models with:

```bash
make generate
```

Never edit generated files under `gen/` or
`internal/argoapi/models/models.gen.go` manually. Argo model generation is
offline: it reads `api/argo/v3.7.3/swagger.json` and
`api/argo/projection.json`, validates every selected field against the pinned
schema, and writes the typed projection deterministically.

To update the Argo API pin, replace the Swagger file from the exact upstream
tag, update `api/argo/v3.7.3/PROVENANCE.md` with its URL and SHA-256, adjust the
projection only for intentional compatibility changes, then run:

```bash
go test ./internal/argomodelgen
make generate
git diff --exit-code
```

The client ignores unknown response fields and keeps omitted or null optional
fields at their existing empty defaults. It rejects malformed known structural
fields instead of silently treating them as absent. Compatibility fixtures
cover workflow and CronWorkflow list/detail responses, both template families,
legacy and multiple schedules, and parameter rendering. These checks establish
the supported response behavior; they do not claim compatibility with
untested Argo Server versions.

Quality gates:

```bash
prek run --all-files
./check.sh --fix
```

Validate the native release artifacts locally:

```bash
goreleaser check
make formula-snapshot
brew style ./dist/homebrew/Formula/go-argo-mcp.rb
```

Pushing a `v*` tag creates GitHub release archives for macOS, Linux, and Windows. The release workflow then renders a checksummed multi-platform formula with `cmd/render-homebrew-formula` and commits it to `CaliLuke/homebrew-tap`. The repository must define a `HOMEBREW_TAP_GITHUB_TOKEN` Actions secret. Use a fine-grained personal access token limited to the `CaliLuke/homebrew-tap` repository with Contents read and write permission and Metadata read permission.

If the binary release succeeded but formula publication failed, publish the existing release again with:

```bash
gh workflow run publish-homebrew.yml -f tag=v0.2.0
```

This recovery workflow downloads the existing release checksums and does not rebuild or replace the binary release.

The test suite includes focused HTTP client tests, confirmation and namespace-policy tests, audit interception tests, and official MCP Go SDK coverage for stateful HTTP, stateless HTTP, and a real stdio child process. Transport parity tests exercise reads, structured results, mapped errors, namespace and pagination rejection before Argo, default mutation denial, allowed mutation, destructive confirmation and replay rejection, and audit redaction.

## Troubleshooting

- `configuration_error`: set `ARGO_BASE_URL` to the Argo Server URL, including its scheme and port.
- `argo_not_found`: check the resource name and namespace.
- `argo_access_denied`: check Argo credentials and RBAC permissions.
- `argo_request_rejected`: check the tool inputs and current resource state.
- `argo_api_error`: a network or server failure may be temporary; retry after checking Argo availability.
- An error after mutation dispatch can have an uncertain outcome. Check the workflow state before you retry the mutation.
- `namespace_denied`: choose a namespace permitted by `MCP_NAMESPACES_ALLOW` and not present in `MCP_NAMESPACES_DENY`.
- TLS hostname failures: set `ARGO_TLS_SERVER_NAME` to the certificate name. Use `ARGO_INSECURE_SKIP_TLS_VERIFY=true` only for an explicitly trusted development endpoint.

## Architecture

- `design/` — Loom service and MCP contracts
- `gen/` — committed Loom/Loom-MCP generated transport code
- `api/argo/` — pinned upstream Swagger, provenance, and projection selection
- `internal/argoapi/` — small direct Argo HTTP client
- `internal/argoapi/models/` — generated typed Argo response/request projection
- `internal/argomodelgen/` — offline projection validator and generator
- `internal/service/` — tool behavior and safety policy
- `internal/confirmation/` — scoped one-time confirmations
- `internal/mcpaudit/` — generated MCP interceptor-backed JSONL audit
- `internal/server/` — shared service, adapter, transport, and lifecycle bootstrap
- `cmd/go-argo-mcp/` — version, signal, and process exit entrypoint
