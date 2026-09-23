# Issue 5: bounded collection pagination

Implement [issue #5](https://github.com/CaliLuke/go-argo-mcp/issues/5) after typed Argo models land. Extend the existing collection contracts in `design/design.go`; use opaque upstream continuation strings without decoding or reinterpreting them.

## Status

- 2026-09-22 — Fresh Sol reviewer approved all three review fixes; parent publication gates passed. Committed and pushed as `8439656` with `Fixes #5`; GitHub issue closed and CI run `35811607176` succeeded.
- 2026-09-22 — Draft; requires review against the completed issue 1 implementation.
- 2026-09-22 — Fresh reviewer `review_plan_5` approved after fixes; ready once issue 1 lands, subject to checking its actual model names.
- 2026-09-22 — Implementation complete; focused/full tests, deterministic generation, and `prek run --all-files` pass. After contract generation, the shared scanner was implemented before its complete regression suite, so the original test-first sequencing check remains open. Awaiting fresh Sol review and parent delivery.

## Contract

`list_workflows`, `list_cron_workflows`, `list_workflow_templates`, and `list_cluster_workflow_templates` accept `limit` (default 50, range 1–200) and optional `continue`. `get_cron_history` gains the same continuation contract, retaining its default limit 10 and adding maximum 200. Results always include `has_more` and optionally `continue`; `has_more` is exactly whether the returned token is nonempty. Agents replay identical filters and limit. Empty and exhausted collections use empty arrays and no continuation. Preserve backend order; history retains its existing per-result start-time sorting and documents that it is not a global ordering guarantee.

At each backend request, request at most the remaining output capacity, including when local filters apply. Consume the entire backend page before returning its continuation so matching items are never skipped. Continue past empty/nonmatching pages. Track the input cursor and all returned cursors to reject cycles, even on a full final result page. Argo cursors remain opaque; URL encoding handles arbitrary string content. Reject structurally invalid cursor JSON via typed decoding. A 1,000 backend-page budget fails explicitly rather than hanging on endless distinct tokens. If an upstream page exceeds the requested capacity, return an actionable error rather than silently skipping its tail. HTTP failures/expired cursors remain actionable errors; no silent restart. Validate bounds at the service/client boundary as well as generated MCP input validation. A missing/zero internal limit means the documented default; negative and oversized limits fail.

## Milestones

### Review resolutions

The pinned [workflow implementation](https://github.com/argoproj/argo-workflows/blob/v3.7.3/server/workflow/workflow_server.go) emits the next offset independently of page size; [BuildListOptions](https://github.com/argoproj/argo-workflows/blob/v3.7.3/server/utils/list_options.go) parses offset and limit separately. Kubernetes [continuation implementation](https://github.com/kubernetes/apiserver/blob/v0.33.1/pkg/storage/continue.go) carries start key and resource version, not page size; CronWorkflow/template APIs forward Kubernetes ListOptions. Therefore remaining-capacity requests are supported by these pinned implementations despite Swagger's conservative identical-query wording. Do not claim compatibility with arbitrary backends that reject changed page size; surface their errors. Callers should still keep their MCP limit/filters unchanged.

`metadata.continue` must be a JSON string or absent/null (null means no continuation); reject number/object/array/bool types. Never parse the opaque string or use `internal/service/argo.go`'s trimming `stringPtrValue` helper on it. Explicit MCP limit 0 is invalid; a nil or zero internal limit means the documented default. Generated input/output `Continue` is optional `*string`, output `HasMore` is required `bool`, and input `Limit` is optional `*int`. Empty domain/output collections are non-nil slices. Runtime defaults remain 50 for collection lists and 10 for history, and schema descriptions advertise them. The machine-readable schema `default` keyword is temporarily absent because Loom-MCP v2.1.0-alpha.27 otherwise generates invalid recovery examples with `limit: 0`; track that generator fix in [loom-mcp#302](https://github.com/CaliLuke/loom-mcp/issues/302). The application interceptor rejects explicit MCP bounds before service dispatch until generated adapters enforce numeric schema bounds; track that fix in [loom-mcp#303](https://github.com/CaliLuke/loom-mcp/issues/303). Inspect resulting types in `gen/argo/service.go` and `gen/mcp_argo/adapter_server.go` after generation.

Create service regressions in `internal/service/argo_test.go` before editing service mappings: cover all five methods, exact cursor bytes, metadata/count/empty arrays, defaults, and invalid limits before any upstream request (including history's CronWorkflow existence check). Update `internal/argoapi/client_test.go` regression `TestListWorkflowsAppliesLimitAfterLocalStatusFilter`, which currently requires backend limit 100. Add opaque cursor whitespace/URL-special-character fixtures, invalid response cursor types, over-capacity backend pages, full-page cursor cycles, scan-budget boundaries, cross-page order and per-page history sorting. SDK regressions belong in `internal/integration/mcp_test.go` and assert explicit 0, -1, and 201 limits cause zero Argo requests.

Prove deterministic regeneration by capturing a sorted path/checksum manifest of `gen/` after the first `make generate`, repeating `make generate`, and comparing the second manifest. Do not mistake the expected uncommitted contract diff for generator drift.

### Milestone 1: contract and pagination engine

Goal: Make every collection bounded and resumable without dropping filtered items.

Acceptance Criteria

- Generated schemas expose bounded limit and continuation inputs plus consistent output metadata.
- Client tests prove first/later/empty/exhausted pages, local filters, exact query parameters, cycle rejection, scan budget, cancellation, and no skipped items.

Checklist

- [x] Read this plan and `/Users/luca/.agents/skills/execution-plans/SKILL.md`, then recite milestone order, proof commands, test-first rules, fresh review gate, parent commit/push handoff, and repository constraints before editing.
- [x] Inventory `rg -n 'ListWorkflows|ListCronWorkflows|ListWorkflowTemplates|ListClusterWorkflowTemplates|GetCronHistory|listWorkflows' design internal`; reconcile client/service/tests and regenerated interfaces in the completion report.
- [x] Update `design/design.go` inputs/results, descriptions, defaults and maxima; run `loom gen github.com/CaliLuke/go-argo-mcp/design` before consuming new generated fields. Never hand-edit `gen/`.
- [ ] Add client page result types and tests in `internal/argoapi` before implementing shared pagination and each collection mapping. Keep typed upstream models and use `listOptions.limit`/`listOptions.continue` on supported endpoints. (Coverage is complete; the required test-first sequence was not followed.)
- [x] Implement service mapping of `continue` and `has_more` plus validation; update affected service/client tests. Run `go test ./internal/argoapi ./internal/service`.

### Milestone 2: MCP contract proof and delivery

Goal: Prove agents can intentionally resume all collections and document the contract.

Acceptance Criteria

- Official SDK tests cover all collection tools across first/later/empty/exhausted results and filtered workflow/CronWorkflow pagination, including invalid limits rejected before Argo calls.
- README demonstrates next-page calls and states defaults/maxima and cursor/filter limitations.
- Fresh Sol review approves the diff and all quality gates pass before publishing.

Checklist

- [x] Add `internal/integration` official SDK regressions for collection schemas and page sequences; run `go test ./internal/integration`.
- [x] Update README collection documentation with concrete first-page and continuation JSON examples.
- [x] Run `make generate`, `go test ./...`, and `prek run --all-files`; confirm a second generation is deterministic.
- [x] Return evidence to the parent; resolve fresh Sol review findings and rerun affected checks. Parent commits with `Fixes #5` and pushes after approval.
