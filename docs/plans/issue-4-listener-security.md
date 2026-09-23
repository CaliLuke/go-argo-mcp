# Issue 4: authenticated and fail-closed HTTP listener

Implement [issue #4](https://github.com/CaliLuke/go-argo-mcp/issues/4) after transport bootstrap is shared in `internal/server/`. Protect both HTTP modes before tool dispatch using environment configuration. Read-only tool defaults remain intact.

## Status

- 2026-09-22 — Fresh Sol reviewer `review_code_4` approved all five review corrections; focused/race tests and parent publication gates passed. Ready for commit/push.
- 2026-09-22 — Draft, pending fresh review against completed transport code.
- 2026-09-22 — Fresh reviewer `review_plan_4` approved after exact authority, SDK, telemetry and test-first fixes; ready after dependencies land.
- 2026-09-22 — Fresh dependency-readiness reviewer `readiness_4` approved after explicit lookup and HTTP fixture migration steps were added against the actual issue-3 bootstrap; ready after issue 3 lands.
- 2026-09-22 — Execution sequencing deviation: implementation advanced beyond minimal compiling security interfaces before the required contract-test files existed. Work paused, the deviation was reported to the parent, and red contracts were added before continuing. Do not claim test-first execution for issue 4.
- 2026-09-22 — Implementation and local proof complete. Focused tests, race tests, generation, the full Go suite, and `prek run --all-files` pass; fresh Sol review and parent publication remain.
- 2026-09-22 — Fresh Sol review found five blockers: rejected-Host telemetry authority leakage, leading-zero port rejection, explicit empty query/fragment acceptance, tab-separated Bearer acceptance, and incomplete actual-export redirect proof. All five have focused regressions and fixes; focused/full/race/generation checks pass, with reviewer re-check and parent publication remaining.

## Contract

`ARGO_MCP_AUTH_TOKEN` is separate from outbound `ARGO_TOKEN`. Absent means disabled, but explicitly empty, surrounding whitespace, control characters, and characters outside RFC 6750 b64token syntax are startup errors. Compare SHA-256 digests of the supplied/configured token with constant-time comparison, never direct secret-length comparisons. Malformed, missing, duplicated, or wrong Authorization values receive 401 with an appropriate Bearer challenge when auth is enabled. Parse the Bearer scheme case-insensitively; reject ambiguous credentials. No credential values or headers enter log/audit/telemetry attributes or errors.

`ARGO_MCP_ALLOW_UNAUTHENTICATED` defaults false and must be a valid nonempty boolean when explicitly set. Non-loopback HTTP binds require inbound auth or this explicit acknowledgement of external proxy/mesh authentication; otherwise startup fails before listening. Recognize literal loopback IPs and the exact localhost name as loopback without DNS trust. Empty-host/wildcard binds are non-loopback. Parse addresses strictly and reject malformed host/port configuration. Stdio ignores HTTP security configuration because it never listens.

`ARGO_MCP_ALLOWED_HOSTS` contains comma-separated exact authorities (host plus explicit port, with IPv6 brackets); no schemes, paths, userinfo, wildcard patterns, empty entries or ambiguous characters. Canonicalize DNS case, IP formatting, and numeric ports. Default allowed authorities for a loopback bind are localhost, 127.0.0.1, and [::1] at the configured port; for an explicit non-loopback bind the default is its literal authority. Wildcard binds require an explicit usable allow list. Validate every `/rpc` request Host against this list; ignore forwarded headers. Invalid/unknown Host yields 403 before dispatch.

`ARGO_MCP_ALLOWED_ORIGINS` contains comma-separated exact HTTP(S) origins, no wildcard, opaque/null origins, userinfo, query, fragment, path (including trailing slash), or empty entries. Canonicalize scheme/host/default ports. A request without Origin is allowed subject to other checks; a single valid Origin must equal the request's direct origin (TLS controls scheme) or an explicitly allowed origin. Reject duplicates, malformed origins, cross-origin requests and hostile hosts with 403. Configure the underlying Loom/SDK checks consistently so explicit proxy origins work without allowing arbitrary origins. These settings validate inbound Origin; a trusted proxy supplies CORS/preflight handling for cross-origin browsers.

`/healthz` remains unauthenticated and returns only static `ok`; it does not reveal configuration, credentials, namespaces, connectivity or tool state. Security controls wrap `/rpc` before any app request logging. Never log raw request URL/query/header data or raw security configuration errors. Existing outbound credentials must also stay out of request error messages and telemetry; inspect Argo base-URL userinfo/query and redirect behavior so upstream credentials cannot leak via logged URLs. Reject credential-bearing configured base URLs rather than echoing them. Document explicit token env usage and proxy configuration.

## Milestones

### Review resolutions

After issue 3, `ConfigFromEnv` uses `os.Getenv` and `ConfigFromLookup` accepts `func(string) string`. Migrate that lookup boundary to `os.LookupEnv` and `func(string) (string, bool)` so security parsing can distinguish absent from explicitly empty values. Update helper signatures and every config test caller, preserving existing nonsecurity defaults. Establish the compiling lookup interface first, then add absent/empty contract tests before security parsing behavior. Apply HTTP security validation in `newApplication` so direct `New` and `Run` enforce the same rules, while stdio ignores HTTP-only settings.

Existing production-bootstrap HTTP integration helpers currently construct `server.New` with empty `Addr` before allocating an unrelated `httptest` port. Allocate an unstarted test server first, pass its listener's concrete address in `Config.Addr`, install `app.Handler()`, then start it. Reconcile `newTestApplication`, `defaultPolicySession`, `safetySession`, `TestHTTPTransportsUseProductionBootstrap`, and `TestStatelessMethodAndSessionPolicy` in `internal/integration/transports_test.go`. Keep all tests on the production handler and retain existing safety/audit assertions; never weaken strict authority checks for test convenience.

Adapt issue-3 lifecycle fixtures in `internal/server/server_test.go` to valid HTTP settings: use a valid loopback address for listen-failure coverage and the preallocated listener's actual address for shutdown-timeout coverage. Assert the injected listener was called and its sentinel failure propagated. Retain handler-entry and exactly-once cleanup assertions; invalid-security startup tests must instead assert zero listener calls. Run `go test ./internal/server ./internal/integration` immediately after this migration.

Absent allow-list variables select defaults; explicitly empty variables fail startup. Bind ports must be 1–65535 (port zero is rejected). Omitted request authority ports mean 80 for direct HTTP and 443 for direct HTTPS; configured allow-list entries still require explicit ports, and comparisons use canonical host+numeric port. Reject IPv6 zone identifiers, trailing DNS dots, escaped authority delimiters, userinfo and out-of-range ports. Canonicalize IPv4-mapped IPv6 consistently to IPv4 before loopback detection and matching. Add named config/authority table tests for every case in `internal/server/security_test.go`.

Reject Origin trailing slash as well as all other path components to match the installed bridge. Normalize accepted Origins (scheme/host case and default ports) before passing a cloned request to Loom; configure canonical serialized `TrustedOrigins` the same way. Set SDK `DisableLocalhostProtection=true` ONLY for handlers behind the mandatory exact-Host middleware; that middleware replaces this narrower built-in check and must run on every `/rpc` method. Prove loopback connection with allowed external proxy Host and HTTPS Origin over real HTTP. This issue validates Origin; it does not itself implement CORS. A trusted proxy supplies CORS/preflight handling for cross-origin browser use. Replace the earlier phrase 'authorize browser access' with this narrower contract; do not exempt actual MCP calls from authentication.

Outbound redirects are disabled, including same-origin redirects: do not forward configured Argo credentials to redirect targets; return a safe status failure. Reject Argo base URLs containing userinfo, any query or fragment at bootstrap before telemetry wrapping, without echoing their values. HTTP instrumentation must receive only sanitized route/method/URL data, including for accepted requests and health/unmatched routes; do not rely on the position of auth middleware. Unknown paths use a fixed route label, and raw queries never enter logging/export attributes. Keep original routing/protocol semantics intact while sanitizing the telemetry view. Capture actual exported spans/events/logs (not just call arguments) in `internal/observability/otel_test.go`, `internal/server/security_test.go` and `internal/argoapi/client_test.go`; use sentinel credentials in inbound query strings on accepted/rejected RPC, health and unmatched requests, malformed config, rejected upstream URLs, and redirect failures. Assert sentinels are absent from application, audit and telemetry output.

Before middleware behavior edits, establish minimal compiling security interfaces then add contract tests in `internal/integration/security_test.go`: both HTTP modes authenticated SDK invocation, missing/wrong/duplicate Authorization, accepted/rejected Host/Origin, canonical proxy access, static health, pre-listen startup rejection, and stdio ignoring malformed HTTP-only settings. Handler and Argo counters prove rejection before dispatch. The initial recital includes acceptance exit criteria and named commands in execution order. These sequencing requirements take precedence over the milestone grouping below.

### Milestone 1: configuration and middleware policy

Goal: Reject unusable security settings before listening and authenticate every MCP HTTP request.

Acceptance Criteria

- Unit tests cover the full env/bind/token/Host/Origin contract for IPv4, IPv6, localhost, explicit hosts and wildcard listeners.
- Rejected requests never invoke the MCP handler; liveness remains static and accessible.
- Credential values do not appear in captured application/audit/OTel output under success and error scenarios.

Checklist

- [x] Read this plan and `/Users/luca/.agents/skills/execution-plans/SKILL.md`; recite milestones, test-first requirements, proof commands, review gate, parent commit/push and repo constraints before editing.
- [x] Inventory `rg -n 'Authorization|ARGO_TOKEN|ARGO_BASE_URL|HTTPMiddleware|requestLogging|OriginProtection|DisableLocalhostProtection|ListenAndServe|log\.|Emit|ConfigFromLookup|ConfigFromEnv|envOrDefault|envBool|envCSV|envDurationSeconds' cmd internal gen/mcp_argo/sdk_server.go` and relevant installed SDK bridge security code; reconcile every credential/HTTP entry point and lookup caller in the handoff.
- [x] Migrate `ConfigFromLookup` and helpers to a presence-aware lookup, update all `internal/server/config_test.go` callers, and preserve existing nonsecurity defaults. Establish the compiling interface before adding absent/empty security parsing tests.
- [x] Adapt the production-bootstrap HTTP helpers in `internal/integration/transports_test.go` to preallocated concrete listener addresses and adapt HTTP lifecycle fixtures in `internal/server/server_test.go` with listener invocation/sentinel assertions. Preserve all transport/safety/cleanup coverage. Run `go test ./internal/server ./internal/integration`.
- [x] Add security configuration and middleware tests in `internal/server/` before implementation, including explicitly set empty env values, duplicate headers, token length differences, origin normalization, hostile forwarded headers, unusable wildcard config and health bypass.
- [x] Implement configuration parsing and `/rpc` middleware in `internal/server/`, integrate both HTTP modes, and coordinate Loom/SDK Host/Origin behavior. Validate configuration before listener creation. Run `go test ./internal/server`.
- [x] Add focused credential leak regressions for application/OTel/audit logging and Argo error paths; implement redaction or URL validation at the correct boundary without echoing secrets. Run `go test ./internal/argoapi ./internal/observability ./internal/mcpaudit ./internal/server`.

### Milestone 2: transport proof and deployment docs

Goal: Demonstrate the complete policy through real MCP transport and executable startup.

Acceptance Criteria

- Official SDK initializes/invokes authenticated stateful/stateless HTTP; wrong/missing auth, disallowed hosts and origins fail without Argo requests.
- Process tests prove unauthenticated non-loopback startup fails before listen and stdio is unaffected.
- README documents token separation, loopback compatibility, exact allow lists, authenticated/proxy deployment and static health semantics.
- Fresh Sol review approves; all gates pass before publication.

Checklist

- [x] Add SDK and child-process security tests under `internal/integration/`, including IPv6 where supported, wildcard configuration, allowed proxy origins, healthz, absent/wrong/correct tokens and legacy loopback defaults. Run `go test ./internal/integration`.
- [x] Update README environment/security/deployment tables and examples; remove the obsolete claim that inbound authentication does not exist.
- [x] Run `make generate`, `go test ./...`, and `prek run --all-files`; return review evidence to parent.
- [ ] Resolve fresh Sol review findings and rerun affected checks. Parent commits with `Fixes #4` and pushes after approval.
