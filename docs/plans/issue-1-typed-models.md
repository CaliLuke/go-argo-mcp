# Issue 1: reproducible typed Argo API models

Implement [issue #1](https://github.com/CaliLuke/go-argo-mcp/issues/1) without changing the MCP contract. The selected direction is a small typed projection of the pinned Argo Workflows v3.7.3 Swagger schema, kept separately from Loom output. Preserve the existing compact results and safety behavior.

## Status

- 2026-09-22 — Delivered in `37cf13b` and CI follow-up `3b3df00`; GitHub CI run `35809168543` passed all steps.

- 2026-09-22 — Commit `37cf13b` pushed and issue 1 closed. CI follow-up received fresh Sol approval from `review_ci_followup`; all configured checks remain enabled.

- 2026-09-22 — Post-publication CI iteration: baseline run `35768359486` failed because golangci-lint v2.12.2's staticcheck crashed on Go 1.27 with `unexpected expr: *ast.KeyValueExpr`. CI now pins golangci-lint v2.13.2 (embedding `honnef.co/go/tools` v0.8.1). In an isolated detached checkout of `37cf13b`, `actionlint` and `prek run --all-files` passed with no new linter findings; ready for fresh Sol review and parent publication.
- 2026-09-22 — Fresh Sol reviewer `review_code_1` approved without findings; parent publication checks (`prek`, `git diff --check`, `actionlint`) passed.

- 2026-09-22 — Draft for fresh-agent review. Baseline is commit `9bd88ed`.
- 2026-09-22 — Fresh reviewer `review_plan_1` approved readiness after compatibility and reproducibility fixes; ready for Sol implementation.
- 2026-09-22 — Sol implementation complete. Targeted tests, deterministic online/offline generation (`822b5b403c4eac1c53fbf2ab05669798029d1130c5112d20c84a1a0c2f4620f2`), `make generate`, `go test ./...`, and `prek run --all-files` pass; ready for fresh Sol code review.

## Contract

The upstream source is https://raw.githubusercontent.com/argoproj/argo-workflows/v3.7.3/api/openapi-spec/swagger.json. Check in the schema plus provenance and checksum, and generate a deliberately selected subset of fields into `internal/argoapi/models/`. A Go generator owns those files; a checked-in projection specification selects fields from actual upstream definitions and rejects missing definitions or changed types. Regeneration is offline and deterministic. Unknown response fields are ignored, missing/null optional fields retain current empty/default semantics, malformed known structural fields fail decoding. Parameter values/defaults/valueFrom must preserve current string/JSON presentation through narrowly scoped raw JSON fields, rather than reintroducing object-wide dynamic extraction. Preserve the current `status.nextScheduledTime` fallback extension explicitly even though it may not be in Swagger. Normal responses and mutation request envelopes are typed. Streaming logs retain their supported SSE/NDJSON framing and error semantics, with typed envelopes where practical. No runtime dependency on the full Kubernetes client is needed.

## Milestones

### Review resolutions

The concrete authored inputs are `api/argo/v3.7.3/swagger.json`, `api/argo/v3.7.3/PROVENANCE.md`, and `api/argo/projection.json`; the command is `go run ./cmd/generate-argo-models`, reusable logic/tests belong in `internal/argomodelgen/`, and output is `internal/argoapi/models/models.gen.go`. Projection entries explicitly label compatibility overrides: raw parameter scalar/object representations still validate their upstream source properties; the `CronWorkflowStatus.nextScheduledTime` extension is explicitly marked as an extension, not asserted to exist upstream. Validate nested references, array element types, and map value types; negative generator tests cover each.

`internal/argoapi/compatibility_test.go` and `internal/argoapi/testdata/` must lock down public client methods for workflow list/detail, CronWorkflow list/detail, and both template families, including omitted metadata, nil/empty collections and unknown fields. Preserve the existing parameter renderer's normalization and precedence: test empty/whitespace value, default, null, boolean, number, object, array, and valueFrom fallback, including canonical JSON formatting. Raw JSON is a representation, not permission to return its bytes unchanged. Preserve `nextScheduledTime` when supplied, calculate it when absent, and return empty while suspended. The only intended decode behavior change is explicit rejection of malformed known structural fields; document and test it separately.

Before changing streaming envelopes, add regressions that an `error` key fails even with null value, and that empty/null result falls back to outer content. Typed mutation request tests cover both restartSuccessful boolean values and both CronWorkflow suspend/resume paths.

Exact generation proof: run `go run ./cmd/generate-argo-models`, save `shasum -a 256 internal/argoapi/models/models.gen.go` output to a temporary file, run `GOPROXY=off GOSUMDB=off go run ./cmd/generate-argo-models`, and compare a second checksum file with `diff -u`. The generator itself must use only local input files and no network APIs. Run `go test ./internal/argomodelgen` for generator proof. CI must invoke `make generate`, then `git diff --exit-code` and a nonempty check on `git ls-files --others --exclude-standard -- gen internal/argoapi/models` to detect both tracked drift and new outputs.

### Milestone 1: schema projection and compatibility proof

Goal: Establish reproducible typed upstream models and lock existing response behavior down.

Acceptance Criteria

- The implementer recites the workflow before code edits.
- Generator tests prove deterministic output and rejection of missing or incompatible selected fields; provenance records the exact version and SHA-256.
- Fixture tests in `internal/argoapi` cover legacy singular CronWorkflow schedules, current multiple schedules, optional omissions/nulls, additional fields, and parameter rendering.

Checklist

- [x] Read this plan and `/Users/luca/.agents/skills/execution-plans/SKILL.md`, then recite milestone order, acceptance criteria, proof commands, test-first requirements, independent review gate, parent-owned commit/push handoff, and AGENTS.md constraints before editing code.
- [x] Inventory `rg -n 'map\[string\]any|objectValue|stringValue|jsonItems|anySlice|doJSON|extractParameters|cronSchedules' internal/argoapi internal/service internal/integration`; account for each production helper and fixture/test-only use in the completion report. Public client result types, service call sites, and generated adapters keep their signatures.
- [x] Add compatibility fixtures and regression tests before replacing decoding; run `go test ./internal/argoapi`.
- [x] Add the pinned schema/provenance, projection specification, Go generator entrypoint under `cmd/`, and typed model output under `internal/argoapi/models/`; keep reusable generation logic in `internal/`. Add generator tests before implementation of generation behavior. Generated fields use pointers/raw JSON only where omission must be distinguished; collections retain empty-result output through the existing domain mappers.
- [x] Run the documented generator twice and prove identical output with checksums; run its package tests.

### Milestone 2: typed HTTP boundary and reproducibility gate

Goal: Replace dynamic response traversal and mutation envelopes without changing tool behavior.

Acceptance Criteria

- Production normal API decoding has no general-purpose map extraction; retry, termination, and CronWorkflow suspend/resume payloads are typed.
- Existing shaped results, HTTP methods/paths/query/body, status errors, pagination, timeout/TLS/auth, and streaming-log tests pass.
- CI and `make generate` regenerate upstream models and fail on drift; README documents support/compatibility and regeneration.
- Fresh Sol review approves the final diff and `prek run --all-files` passes before commit/push.

Checklist

- [x] Replace `internal/argoapi/client.go` JSON decoding with a typed destination helper and explicit typed-to-domain mappings; remove obsolete traversal helpers and preserve stream framing behavior.
- [x] Run `go test ./internal/argoapi ./internal/service ./internal/integration` and fix failures.
- [x] Update `Makefile`, `.github/workflows/ci.yml`, and README with offline generation, pin/update instructions, optional/unknown-field behavior, and checked fixture compatibility rather than claiming untested server versions.
- [x] Run `make generate`, `go test ./...`, and `prek run --all-files`; inspect the diff for unintended Loom contract changes.
- [x] Return files changed and proof results to the parent. Obtain a fresh Sol code review; resolve findings and rerun affected checks. Parent commits with `Fixes #1` and pushes after approval.
