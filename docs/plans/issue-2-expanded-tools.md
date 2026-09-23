# Issue 2: diagnostic and guarded workflow tools

Implement [issue #2](https://github.com/CaliLuke/go-argo-mcp/issues/2) after issues 1, 5, 3, and 4. Expand the generated MCP contract with the concrete tools below. Reuse typed upstream models, pagination, shared transport bootstrap, existing policy, and real Argo endpoints. No mock fallback or arbitrary remote URL fetch is permitted.

## Status

- 2026-09-22 — Fresh Sol reviewer `review_code_2` approved the combined fixes with no remaining blockers. Independent client/service race tests, SDK/audit/generator tests, and staged whitespace checks passed; all ten findings are resolved. Ready for parent commit and push.
- 2026-09-22 — Draft for fresh review once preceding issues land.
- 2026-09-22 — Fresh reviewer `review_plan_2` approved after event, retry safety, bounds and manifest fixes; ready after dependencies land.
- 2026-09-22 — Fresh dependency-readiness reviewer `readiness_2` approved against the actual typed generator, pagination, transport and security bootstrap, including retry null-safety and transport-test migration; ready after issue 4 lands.
- 2026-09-22 — Fresh dependency-readiness review approved the amended generated-validation contract: defaults, scalar bounds, tool-error responses, optional dry-run semantics, and transport/confirmation tests align with the installed generator.
- 2026-09-22 — Implementation complete for fresh Sol review. Focused client/service/audit/SDK tests, full tests, targeted race tests, generated catalog checks, two-run generation checksums, and `prek run --all-files` pass. Client behavior and core service diagnostic/guard regressions were written red before implementation; service lint edge tests were added after the initial implementation, a sequencing deviation recorded for review.
- 2026-09-22 — Fresh Sol review identified ten blockers. Focused red-to-green fixes cover manifest `generateName` and namespace member validation, explicit destructive-tool read annotations, archive over-return rejection, non-null node children, new-read error classes, suspend/resume wording, diagnostic/archive/SDK bounds, missing-configuration behavior, active event-watch cancellation/body closure, and typed event envelopes generated from the pinned projection.
- 2026-09-22 — Post-review-fix verification passes: combined generated-model/client/service/audit/SDK packages, `git diff --check`, two identical sorted SHA-256 generation manifests covering 10 generated files, and `prek run --all-files` including the full race-enabled Go quality gate. An isolated read-only SDK smoke against an attached Argo advertised 25 tools and returned bounded nodes with `children=[]`, safe artifact links, and UID-scoped events that stopped at the requested limit. Fresh re-review and parent delivery remain pending.

## Contract

All namespaced operations resolve `ARGO_NAMESPACE` then authorize namespace before every upstream request. Archive lookup also sends the authorized namespace and rejects a response from another namespace. Read tools use readOnlyHint=true and destructiveHint=false. Mutations use readOnlyHint=false; creation/resume/suspend/resubmit are not destructive and use destructiveHint=false, idempotentHint=false conservatively. Existing termination must require BOTH mutation and destructive flags; audit the present destructive-only gate and add regressions for this safety fix.

| New MCP tool | Inputs and output | Actual Argo route |
| --- | --- | --- |
| `get_workflow_nodes` | namespace, name, optional phase/node_id, offset default 0, limit 50 (1–200); bounded node summaries with ID/name/display_name/type/phase/template_name/boundary_id/children/timing/message; total/count/next_offset/truncated | GET `/api/v1/workflows/{namespace}/{name}`; project status.nodes |
| `get_workflow_events` | namespace, name, limit 50 (1–200), duration_seconds 2 (1–10); observed event type/reason/message/count/timestamps, count and collection window note | GET workflow for UID, then GET `/api/v1/stream/events/{namespace}` with `listOptions.fieldSelector` scoped to involvedObject UID/kind/name and timeoutSeconds |
| `list_archived_workflows` | namespace, optional label_selector/name_prefix, limit 50 (1–200), continue; workflow summaries including archive UID, count/continue/has_more | GET `/api/v1/archived-workflows` with `namespace`, `namePrefix`, listOptions label/limit/continue |
| `get_archived_workflow` | namespace, uid; compact workflow detail plus UID | GET `/api/v1/archived-workflows/{uid}` with namespace query |
| `get_workflow_artifacts` | namespace, name, optional node_id, offset 0, limit 50 (1–200); metadata name/node_id/direction/path/optional and safe Argo download link, total/count/next_offset/truncated | GET workflow; output links to `/artifact-files/{namespace}/workflows/{name}/{nodeID}/{inputs-or-outputs}/{artifactName}` |
| `lint_workflow` | namespace, manifest_json (JSON object, max 256 KiB); valid=true and compact name/namespace/source on successful validation | POST `/api/v1/workflows/{namespace}/lint` with typed namespace/workflow envelope |
| `lint_workflow_template` | namespace for namespaced template, cluster_scope default false, manifest_json max 256 KiB; valid=true/name/scope/source | POST `/api/v1/workflow-templates/{namespace}/lint` or `/api/v1/cluster-workflow-templates/lint` with typed template envelope |
| `submit_workflow_template` | namespace, template_name, cluster_scope false, optional map parameters; created workflow summary plus action status | POST `/api/v1/workflows/{namespace}/submit`, resourceKind WorkflowTemplate or ClusterWorkflowTemplate |
| `suspend_workflow` | namespace, name; action result | PUT `/api/v1/workflows/{namespace}/{name}/suspend` |
| `resume_workflow` | namespace, name; action result (whole-workflow resume, node selector deferred) | PUT `/api/v1/workflows/{namespace}/{name}/resume` |
| `resubmit_workflow` | namespace, name, memoized false, optional parameter map; created workflow summary plus action status | PUT `/api/v1/workflows/{namespace}/{name}/resubmit` |
| `trigger_cron_workflow` | namespace, name, optional parameter map; created workflow summary plus action status | POST `/api/v1/workflows/{namespace}/submit`, resourceKind CronWorkflow |

The event API is a watch, not historical-event listing. Describe it honestly as a bounded observation window, parse NDJSON/SSE incrementally, stop at limit/deadline/EOF, and close the response. Its own time-window expiry is successful collection; parent cancellation, HTTP error and stream error remain failures. Bound stream bytes to 1 MiB and each frame to 64 KiB. Use typed event envelopes and reject any error-key presence. Do not request an unbounded body through `doText`.

Node/artifact pagination sorts by stable node ID and direction/name before offset slicing. Filtering happens before counting/slicing. Links in children may point outside the current page and are documented. Truncate long diagnostic messages to 4 KiB and expose a truncation note. Cap child IDs returned per node at 200 with a marker. Node status offloading must use the normal hydrated workflow endpoint; a missing nodes map is empty, not fabricated. Artifact links use only the configured trusted Argo base URL plus escaped validated resource segments, never embedded object-store URLs/credentials or signed query strings. Return metadata/links only; binary retrieval is deferred.

Lint manifests are opaque complete JSON objects carried as `json.RawMessage` within typed envelopes so projection does not silently strip template fields. Validate kind/API version, metadata namespace agreement (set absent namespace to resolved namespace for namespaced kinds), object shape, maximum size and no trailing JSON before sending. A cluster template must have no metadata namespace and no supplied namespace input. Lint is read-only even though the endpoint uses POST; do not gate it as a mutation. Audit must not persist the manifest or parameter values: redact these argument fields explicitly, retaining names/counts and normal operation identifiers.

Guarded operations first enforce mutation and namespace policy, validate names and parameter counts/sizes, then make one real request; never automatically retry a mutation. Parameter maps become deterministically sorted `name=value` lists, at most 128 entries/64 KiB combined, rejecting empty names, equals/control characters in names. No serviceAccount override or arbitrary submission body is exposed. Created workflow results contain the new Argo-assigned name, not the source template name. Termination gains its missing mutation gate; destructive retry also gains the safeguards specified below.

Add typed errors/remedies that distinguish invalid input (local validation and upstream 400/422) from invalid state (409), not-found (404), access denied (401/403), temporary failures (429/5xx/network). Preserve existing error names where backward compatibility requires them; new tool paths may select new invalid-input/state errors without silently renaming existing tool contracts. Safe errors never echo upstream response bodies, manifests or credentials. Explain that mutation failures after dispatch may have an uncertain outcome and require checking workflow state before retry.

## Milestones

### Review resolutions

Official SDK tests prove generated numeric validation and valid recovery examples without an application interceptor; the five existing collection inputs advertise source-defined defaults. Use `Default`, `Minimum`, and `Maximum` for new limit-bearing tools (`get_workflow_nodes`, `get_workflow_events`, `list_archived_workflows`, `get_workflow_artifacts`) and event `duration_seconds` (default 2, range 1–10). Inspect generated scalar/pointer types before service mapping. Preserve service/client defensive bounds and internal default behavior, but rely on generated input validation to distinguish omitted MCP values from explicit zero/null. Add SDK cases proving explicit 0/-1/201/null limits and invalid durations cause no Argo requests, including event workflow existence lookups. Prove omitted defaults and canonical recovery examples work for each new bounded input. Invalid generated arguments return a tool error (`CallToolResult.IsError`).

Event collection owns a local timer started immediately before watch dispatch; upstream `timeoutSeconds` is local duration plus 5 seconds, preventing the pinned [watch implementation](https://github.com/argoproj/argo-workflows/blob/v3.7.3/server/workflow/workflow_server.go) from racing normal local expiry with its ResourceExhausted closure. Own timer expiry succeeds even before headers or during a blocked read; earlier parent cancellation fails. Missing workflow UID fails before watch dispatch. Skip framing heartbeat lines, but a JSON event with missing/null result is malformed and fails; any error key fails, including null. Byte/frame overflow fails explicitly with no successful partial response. Count limit ends successfully with `limit_reached=true` and an observation-window note; otherwise false. Always close the body. Add `internal/argoapi/events_test.go` cases for all these paths, especially closure and bounded hangs.

All existing `retry_workflow` forms are destructive because retry may delete/recreate pods and it is already annotated destructive. Require BOTH mutation and destructive flags. Add optional `dry_run` (default true) and `confirmation_token` to its design payload; use existing ActionResult preview/token fields. With confirmation enabled, an actual retry requires a scoped one-time token binding action, namespace, name, and restart_successful. With confirmation disabled, both flags and explicit dry_run=false still apply. Keep the retry annotation destructive. This is an intentional stricter safety contract: document migration examples and update existing allowed-retry tests. Termination also requires both flags. `internal/service/operations_test.go` must cover all flag combinations, preview without upstream requests, wrong scope, expiry/replay, restart_successful scope, confirmation-disabled mode, and uncertain mutation errors without blind retry advice.

Follow the existing termination payload's optional `*bool` representation for retry `DryRun`: describe the default rather than adding `Default(true)`, and apply nil-to-true at the service boundary. This preserves explicit-false semantics for direct service callers as well as MCP. Generated validation rejects explicit MCP null; prove omitted input previews and null input fails without executing a retry even when confirmation is disabled. Only explicit false may execute. Update `TestTransportSafetyParity` in `internal/integration/transports_test.go`, whose current successful retry assertion precedes the new confirmation contract, to preview and confirm the retry while retaining its all-transport dispatch and audit checks.

Archived detail returns existing summary fields and UID plus message (4 KiB), labels/annotations/parameters/outputs (at most 20 entries each, selected by sorted key, key 256 bytes/value 1 KiB) and required `truncated` marker when fields were shortened or entries omitted. Node name/display_name/template_name and artifact path cap at 1 KiB; node/artifact identifiers cap at 256 bytes in displayed fields. Do not truncate identity used in requests or links: validate input identities, build links from full validated values, and omit an invalid/over-4-KiB link with a truncation note. UTF-8 truncation preserves valid text. Diagnostic collection arrays are always non-nil; `next_offset` is optional and present only when more filtered items exist. Required `truncated` means either additional pages or shortened fields; separate required `fields_truncated` disambiguates field loss, and `next_offset` disambiguates pagination. Archive list uses optional opaque `continue` and required `has_more` exactly as issue 5. Add `internal/service/diagnostics_test.go` for bounds, stable ordering, filtering-before-count, exhausted offsets and field/page marker distinctions.

`manifest_json` is an MCP string containing one JSON object. Enforce its UTF-8 byte length (256 KiB) at the service boundary, not only a character-count schema constraint. Decode only validation metadata into typed fields; use `map[string]json.RawMessage` narrowly for lossless namespace insertion into the manifest envelope without converting numbers to float64 or projecting away unknown nested data. This exception is for an opaque user-authored manifest, not normal Argo response decoding. Cluster-template lint rejects any supplied namespace, does not default/authorize a namespace, and relies on upstream cluster RBAC like existing cluster-template reads; test under configured allow/deny lists. `internal/argoapi/lint_test.go` locks full nested-field/large-integer preservation and all three lint envelope shapes; service tests lock namespace agreement/insertion and invalid input before calls.

Use the repeated-generation sorted path/checksum manifest procedure from `docs/plans/issue-5-pagination.md`; compare two generated directory manifests including upstream models. The new typed request/result DTOs must exist before test imports, but behavioral tests precede client/service implementation.

### Milestone 1: generated contracts and typed upstream capabilities

Goal: Advertise bounded, accurately annotated tools and implement exact Argo requests.

Acceptance Criteria

- All 12 new tools have generated payload/result schemas with explicit validation, errors and annotations.
- Client tests cover exact methods/routes/queries/bodies, typed response projection, pagination/stream bounds and error mapping.
- Upstream model regeneration is deterministic and existing API output remains compatible.

Checklist

- [x] Read this plan and `/Users/luca/.agents/skills/execution-plans/SKILL.md`, then recite milestones, test-first order, named commands, peer review gate, parent commit/push and AGENTS.md constraints before editing.
- [x] Inventory `rg -n 'Method\(|readOnlyHint|destructiveHint|mapArgoError|AllowMutations|AllowDestructive|redact|summar' design internal` and the pinned schema definitions/routes; reconcile affected contracts, models, service, audit and SDK tests in the handoff.
- [x] Extend `design/*.go` and `api/argo/projection.json` for the 12 tools, typed request envelopes and selected diagnostic fields. Keep outputs compact; generate via `make generate` before tests import new symbols. Do not hand-edit generated code.
- [x] Add `internal/argoapi` request/response tests before client implementations, using the pinned Swagger and upstream event/artifact implementations as endpoint evidence. Implement new client methods in focused files with application logic under `internal/`.
- [x] Run `go test ./internal/argomodelgen ./internal/argoapi`; cover streaming deadline/EOF/cancellation, archive namespace mismatch, artifact-link escaping, deterministic parameter ordering and lint envelope preservation.

### Milestone 2: service safeguards and actionable errors

Goal: Enforce policy uniformly and shape diagnostics for agent consumption.

Acceptance Criteria

- Service tests prove denied namespaces/mutations make no Argo requests; termination requires both flags and one-time confirmation remains scoped.
- Node/artifact filters/pagination/size caps and lint validation reject invalid inputs before requests.
- Audit never stores raw manifest/parameter values; errors distinguish invalid input/state and temporary conditions.

Checklist

- [x] Add tests to `internal/service/` for every new method and the termination mutation-gate regression before implementing policy/mapping behavior; update existing termination tests to explicitly opt into mutations for successful destructive actions. Core diagnostic and guard tests preceded implementation; the status records the lint-test sequencing deviation.
- [x] Implement bounded result shaping, parameter and manifest validation, namespace checks, safe action outcomes, and new error mapping in focused `internal/service/` files. Run `go test ./internal/service`.
- [x] Add `internal/mcpaudit` regressions for manifest/parameter redaction before implementing redaction; run `go test ./internal/mcpaudit`.

### Milestone 3: SDK advertisement, invocation and documented coverage

Goal: Prove every added tool works through the MCP interface and the documentation remains synchronized.

Acceptance Criteria

- Official SDK advertises and invokes every new tool with structured results; annotation, validation and error assertions match generated contract.
- README tool/safety tables are checked against the live generated catalog by a test, with no hardcoded stale tool total.
- Shared transport safety tests still pass across stateful HTTP, stateless HTTP and stdio.
- Fresh Sol review approves and all quality gates pass before publishing.

Checklist

- [x] Extend `internal/integration/mcp_test.go` and focused new SDK tests to invoke all 12 tools, cover real mutation dispatch under enabled policy, default-denied behavior, diagnostic bounds, validation/remedies and archive paging; use simulated Argo only as test fixtures.
- [x] Update README tools, safety, diagnostic limitations, pagination, archive requirements, lint/submit examples and uncertain mutation outcomes. Add a catalog-based test under `internal/integration/` to validate table membership/annotations and computed total.
- [x] Run `go test ./internal/integration`, `make generate`, `go test ./...`, and `prek run --all-files`; compare generated manifests across repeated generation and report evidence.
- [x] Resolve fresh Sol review findings and rerun affected checks.
- [ ] Parent commits with `Fixes #2`, pushes after approval, and verifies GitHub CI.
