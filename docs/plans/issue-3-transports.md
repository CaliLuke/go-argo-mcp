# Issue 3: stdio and stateless HTTP transports

Implement [issue #3](https://github.com/CaliLuke/go-argo-mcp/issues/3) through the existing generated SDK server and shared service/adapter. The installed SDK supports `Server.Run` with `StdioTransport`; Loom's `SDKServerOptions.StreamableHTTP` exposes stateless mode. Keep all application bootstrap logic in `internal/` and the executable entrypoint in `cmd/`.

## Status

- 2026-09-22 — Fresh Sol reviewer `review_code_3` approved the corrected implementation; parent `prek run --all-files`, `git diff --check`, and `actionlint` passed. Committed and pushed as `c267514` with `Fixes #3`; GitHub issue closed and CI run `35813766039` succeeded.
- 2026-09-22 — Draft for fresh review after issues 1 and 5.
- 2026-09-22 — Fresh reviewer `review_plan_3` approved after lifecycle/test sequencing fixes; ready after dependencies land.
- 2026-09-22 — Implementation completed and all named proof commands pass; awaiting fresh Sol code review. Configuration and lifecycle tests preceded their implementation, but transport dispatch/bootstrap was implemented before the required real-transport and safety-parity integration contracts were added. The deviation is recorded here rather than represented as fully test-first.
- 2026-09-22 — Fresh Sol review found `MCPGODEBUG` whitespace and duplicate-key parsing differed from SDK v1.8.0. Added failing unit/child-process regressions first, then matched the SDK's trimmed key/value and last-duplicate-wins behavior; focused proofs pass and full gates are being rerun.

## Contract

Environment variable `ARGO_MCP_TRANSPORT` selects `http` (default), `http-stateless`, or `stdio`; reject other values at startup. Empty/unset retains stateful HTTP. All modes use one generated adapter/service setup with identical namespace policy, mutation gates, confirmation policy, structured results, error mapping, and audit interception. HTTP retains `/rpc` and `/healthz`, address defaults and current logging. Stateless HTTP uses Loom's existing bridge option, emits no session ID, accepts POST protocol traffic, rejects GET/DELETE with 405, and has no subscriptions, replay, or server-to-client requests. The application exposes no such session features today.

Stdio starts no listener and ignores HTTP address configuration; stdout contains only MCP JSON-RPC frames. Operational logs go to stderr and audit/telemetry retain their configured destinations; explicitly reject audit destinations that resolve to stdout in stdio mode. Version output remains the existing explicit CLI exception. EOF and SIGINT/SIGTERM cleanly close the SDK transport, audit writer, and telemetry runtime; HTTP signals drain in-flight requests within the existing five-second shutdown budget. Initialization errors must unwind resources before main exits.

Destructive confirmation storage is currently in-process. Stateless HTTP removes MCP session affinity, but a dry-run confirmation remains scoped to one process; this limitation must be explicit in deployment documentation (shared confirmation persistence is deferred). Do not weaken one-time confirmations or namespace gates to claim cross-replica equivalence. Tests prove equivalent safety on the same instance in all three modes.

## Milestones

### Verified client documentation

Parent verified configuration sources on 2026-09-22: [Codex MCP commands](https://learn.chatgpt.com/docs/developer-commands#codex-mcp), [Claude Desktop local servers](https://modelcontextprotocol.io/docs/2026-07-28/develop/connect-local-servers), [Cursor MCP configuration](https://prod.cursor.com/help/customization/mcp), and [VS Code MCP configuration](https://code.visualstudio.com/docs/agents/reference/mcp-configuration). Installed `codex mcp add --help` also confirms repeated `--env KEY=VALUE`, `-- COMMAND...`, and HTTP `--bearer-token-env-var` support. Codex stdio examples can set `ARGO_MCP_TRANSPORT=stdio` and `ARGO_BASE_URL` through repeated `--env` flags. Claude Desktop and Cursor use `mcpServers` with command/args/env; VS Code uses `servers` with type=stdio plus command/args/env. Use an absolute installed binary path in desktop examples and an explicit writable absolute audit path. Include these source links in the final README examples.

### Review resolutions

Issue 5 adds `internal/mcpvalidation.PaginationLimits()` to `MCPAdapterOptions.ToolCallInterceptors` because the generated adapter does not enforce numeric bounds before the service interprets an internal zero as the default. The shared bootstrap must retain this interceptor in all transports and prove explicit zero is rejected before any Argo request in its parity tests.

Bootstrap files are `internal/server/config.go`, `internal/server/server.go` and their `_test.go` siblings; executable tests run a built `./cmd/go-argo-mcp`. Establish only minimal compiling configuration/run interfaces first, then add transport AND safety-parity contract tests before implementing dispatch/lifecycle behavior. Existing milestone text is ordered by outcome, but this test-first sequence takes precedence. HTTP tests must exercise shared production bootstrap rather than recreate a different adapter.

The initial recital includes observable exit criteria and named proof commands in execution order. Normal stdin EOF and expected signal cancellation return success, including normalization of `context.Canceled` from SDK `Server.Run`; startup/listener failures return failure after resource cleanup. Add injectable lifecycle seams to prove audit Close and telemetry Shutdown run exactly once after adapter construction failure, listen failure, transport failure and normal exit.

`internal/integration/transports_test.go` must include separate `TestStdioEOFWithoutSignal` and `TestStdioSIGTERM` subprocess regressions. The EOF test manually closes child stdin and requires zero exit before sending any signal; `CommandTransport.Close` is not evidence because it can escalate to SIGTERM/SIGKILL. `TestHTTPShutdownDrainsRequests` covers BOTH modes with an in-flight Argo call: signal server, finish upstream work, assert successful MCP result and clean exit. A bounded shutdown-timeout test proves cleanup even if work never completes; inject a shorter timeout in unit tests rather than slowing every suite run.

Reject `MCPGODEBUG` enabling `allowsessionsinstateless=1` in stateless mode before SDK startup, since the installed SDK uses that flag to restore session headers/DELETE. Prove this in an isolated child process (`TestStatelessRejectsSessionCompatibilityFlag`); changing environment after package initialization is not sufficient proof. Stdio audit destination checks cover `/dev/stdout`, `/dev/fd/1`, `/proc/self/fd/1`, `/proc/<pid>/fd/1` where supported, and symlinks/file identity (`TestStdioRejectsStdoutAudit`). Rejection must produce zero stdout bytes. Record official client configuration source links in README or completion evidence.

### Milestone 1: shared bootstrap and transport lifecycle

Goal: Run all three transports with the same generated tool pipeline and clean shutdown.

Acceptance Criteria

- Unit tests prove valid/default/invalid transport configuration and resource cleanup; existing stateful integration behavior passes.
- A real child-process stdio SDK test initializes, lists tools, invokes a read, and observes clean EOF without stdout noise.
- Stateless SDK tests initialize and invoke independently without a reused session ID and prove GET/DELETE policy.

Checklist

- [x] Read this plan and `/Users/luca/.agents/skills/execution-plans/SKILL.md`; recite milestone order, tests before behavior edits, proof commands, fresh review gate, parent commit/push, and AGENTS.md constraints.
- [x] Inventory `rg -n 'NewSDKServer|adapterOptions|ListenAndServe|signal.Notify|MCP_AUDIT|Emit|Stdout' cmd internal gen/mcp_argo/sdk_server.go`; reconcile shared setup, logging, audit and shutdown call sites in the handoff.
- [x] Add `internal/server/` configuration and lifecycle test surface, then tests for mode parsing/defaults, invalid values, cleanup and stdout safeguards; move reusable bootstrap behavior from `cmd/go-argo-mcp/main.go` into that package and leave version/exit handling in main.
- [x] Configure `mcpargo.NewSDKServer` once per process using the shared adapter and policy; use `.Server.Run` with SDK stdio transport and `.Handler` with stateful/stateless bridge options. Use cancellation and resource unwinding rather than fatal exits inside helpers.
- [x] Run `go test ./internal/server ./cmd/go-argo-mcp ./internal/integration`.
- [x] Add `internal/integration/transports_test.go` with official SDK tests against both HTTP modes and a real executable child using `CommandTransport`; test EOF and SIGTERM, independent stateless requests, absent session headers, and unsupported methods. Run `go test ./internal/integration -run 'Transport|Stdio|Stateless'`.

### Milestone 2: safety parity and client documentation

Goal: Preserve all safeguards and give local clients executable configuration examples.

Acceptance Criteria

- SDK tests exercise namespace denial, default mutation denial, allowed mutation, destructive dry-run/confirmation/replay rejection and redacted audit records in each mode.
- README describes all transport choices and Codex, Claude Desktop, Cursor, and VS Code local command configurations from official documentation.
- Fresh Sol review approves the final diff and quality gates pass.

Checklist

- [x] Extend transport SDK tests with safety, structured-result, error and audit parity assertions; use actual upstream call counters to prove denied calls never reach Argo. Run `go test ./internal/integration ./internal/server ./internal/mcpaudit`.
- [x] Update README mode/env/deployment guidance, stdout behavior, graceful shutdown, stateless method restrictions and per-process confirmation limitation. Verify client configuration formats against official docs; for Codex follow openai-docs skill and local evidence first.
- [ ] Run `make generate`, `go test ./...`, and `prek run --all-files`; return review evidence and accurate plan status to parent.
- [x] Resolve fresh Sol review findings and rerun affected checks. Parent commits with `Fixes #3` and pushes only after approval.
