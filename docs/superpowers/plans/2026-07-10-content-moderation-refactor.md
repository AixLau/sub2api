# Content Moderation Refactor Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the unsafe content-moderation decision core with a bounded, versioned text-moderation pipeline that runs before every upstream request and feeds upstream-account protection without retaining complete prompts.

**Architecture:** Keep the current moderated-route registrar and in-process gateway boundary. Introduce explicit extractor, detector, provider-adapter, policy-engine, executor, policy-runtime, evidence-store, and risk-manager contracts in the existing Go service layer, then migrate traffic through three independently deployable releases. Only deterministic `hard_deny` evidence short-circuits; all other enforce-mode text calls the moderation model.

**Tech Stack:** Go, Gin, PostgreSQL/raw SQL migrations, Redis, Wire, slog/zap, Vue 3, TypeScript, Vitest, Go race/fuzz/benchmark tooling.

**Status:** Independently reviewed; implementation has not started.

---

## Finite Boundary

This plan implements exactly three releases:

1. **Release 1 - containment:** close known bypasses and secret/raw-content leakage without a broad public API redesign.
2. **Release 2 - core replacement:** add bounded extraction, evidence combination, provider validation, immutable policy revisions, a scoped cache, and minimal audit data.
3. **Release 3 - account protection and cleanup:** apply route constraints/risk windows, finish admin governance, remove legacy runtime branches, and complete rollout gates.

Work stops when the final acceptance checklist passes. OCR/image/audio/video classification, complete-content forensics, and a remote moderation service are outside this plan.

## Rollout Protocol For Every Release

Every Release 1, 2, and 3 production rollout follows: staging corpus/replay, 5% for at least 30 minutes and 10k decisions, 25% under the same minimums, then 100% with two hours observation. Before each canary, record an immutable release manifest containing the baseline window, an absolute p95 moderation-latency SLA, and benchmark base output.

Promotion requires zero public fail-open, known-bypass miss, secret disclosure, route mismatch, queue drop, and unresolved dead letter. Pause promotion when total gateway error rate rises by 0.5 percentage points over the five-minute baseline, p95 moderation latency exceeds either the pre-approved absolute SLA or 120% of baseline, or CPU/RSS per request exceeds 120% of baseline. Roll back immediately when moderation-induced 503 rate rises by at least 1 percentage point over five minutes, or on any safety invariant/race/panic. Each release rehearses image/config or image/policy rollback in staging before its 5% gate.

## Locked File Structure

Keep and narrow:

- `backend/internal/service/content_moderation.go`: public façade, compatibility adapters, and admin use cases.
- `backend/internal/service/content_moderation_input.go`: protocol text extraction only; no body-text trust decisions.
- `backend/internal/handler/content_moderation_guard.go`: build one request and attach its decision/route constraint.
- `backend/internal/server/routes/moderated_route_registrar.go`: retain route inventory and admission enforcement.
- `backend/internal/server/routes/gateway_pipeline_dispatcher.go`: select admission from route metadata/protocol, not platform exclusion.
- `backend/internal/repository/content_moderation_repo.go`: minimal audit persistence.
- `backend/internal/service/content_moderation_outbox.go`: identifier-only side-effect events.
- `frontend/src/views/admin/RiskControlView.vue`: policy publication, health, minimal evidence, and review.

Create:

- `backend/internal/service/content_moderation_types.go`: domain types and failure taxonomy.
- `backend/internal/service/content_moderation_rules.go`: compiled rule evidence.
- `backend/internal/service/content_moderation_policy.go`: pure policy engine.
- `backend/internal/service/content_moderation_provider.go`: provider adapter and restricted egress.
- `backend/internal/service/content_moderation_runtime.go`: lifecycle and atomic policy snapshots.
- `backend/internal/service/content_moderation_evidence.go`: HMAC, span excerpts, redaction, audit records.
- `backend/internal/service/content_moderation_risk.go`: bounded risk windows and route constraints.
- `backend/internal/repository/content_moderation_policy_repo.go`: policy/credential persistence.
- `backend/internal/repository/content_moderation_legacy_migrator.go`: advisory-locked `import/verify/switch/export/scrub` workflow.
- `backend/internal/repository/content_moderation_risk_repo.go`: reversible actions/risk events.
- matching `_test.go` files plus `content_moderation_benchmark_test.go`.
- `backend/migrations/174_content_moderation_outbox_v2.sql` (Release 1).
- `backend/migrations/175_content_moderation_policy_runtime.sql` (Release 2).
- `backend/migrations/176_content_moderation_evidence.sql` (Release 2).
- `backend/migrations/177_content_moderation_actions_and_risk.sql` (Release 3).
- `backend/migrations/178_content_moderation_admin_roles.sql` (Release 3).
- `backend/migrations/179_content_moderation_legacy_scrub.sql` (final operator-approved scrub gate).

No local file deletion is part of this plan. Legacy code is removed from existing files after compatibility gates. Legacy database content is expired and scrubbed only by the explicit final operator gate; pre-containment binaries are never valid rollback targets.

## Chunk 1: Release 1 - Containment

### Task 1: Make The Test Runtime Race-Safe And Stoppable

**Files:**

- Modify: `backend/internal/service/content_moderation_test.go:27-75`
- Modify: `backend/internal/service/content_moderation.go:898-926,1910-1940`
- Modify: `backend/internal/service/wire.go:40-59`
- Create: `backend/internal/service/content_moderation_runtime_test.go`
- Create: `backend/internal/service/content_moderation_benchmark_test.go`
- Create: `backend/cmd/moderation-bench-guard/main.go`
- Create: `backend/cmd/moderation-bench-guard/main_test.go`
- Create: `backend/scripts/check-content-moderation-bench.sh`

- [ ] Reproduce the current fixture race:

```bash
cd backend
go test -race ./internal/service \
  -run '^TestContentModerationUpdateConfig_KeywordModeUpdateWithoutEngineModeKeepsLegacyUICompatible$' -count=1
```

Expected before the fix: concurrent map access or background-worker failures.

- [ ] Add `sync.RWMutex` to `contentModerationTestSettingRepo`; guard all reads and writes without sleeps.
- [ ] Re-run the exact single-test race command and require PASS before changing constructor behavior.
- [ ] Add behavior-neutral benchmarks for current extraction, rule matching, request-size rejection, and concurrent checks, plus a tested guard that compares same-host Go benchmark outputs and enforces 20% `ns/op`/`allocs/op` and 3x-input extraction `B/op` limits. Before production behavior changes run `go test ./internal/service -run '^$' -bench '^BenchmarkContentModeration' -benchmem -count=5 | tee /tmp/cm-release1-base.txt` from `backend/`.
- [ ] Add explicitly named `TestContentModerationRuntime_*` tests for idempotent `Start(context.Context)` and `Close()` that stop worker, cleanup, and outbox loops within one second.
- [ ] Remove goroutine startup from `NewContentModerationService`; start once from Wire, own a child context and `sync.WaitGroup`, and require async tests to register `t.Cleanup(svc.Close)`.
- [ ] Run the final lifecycle race gate:

```bash
cd backend
go test -race ./internal/service \
  -run '^(TestContentModerationRuntime_|TestContentModeration(UpdateConfig|Check|Outbox|Status))' -count=10
```

Expected: PASS, no race/leak and no vacuous regex.
- [ ] Commit only these lifecycle files with `fix: manage content moderation runtime lifecycle`.

### Task 2: Close Grok And Token-Count Admission Bypasses

**Files:**

- Create: `backend/internal/server/routes/gateway_pipeline_dispatcher_test.go`
- Modify: `backend/internal/server/routes/gateway_pipeline_dispatcher.go:59-136`
- Modify: `backend/internal/server/routes/moderated_route_registrar_test.go`
- Modify: `backend/internal/handler/openai_gateway_pipeline_integration_test.go`
- Modify: `backend/internal/handler/openai_moderation_guard_coverage_test.go`
- Modify if necessary: `backend/internal/handler/openai_gateway_count_tokens.go:19-145`

- [ ] Table-drive dispatcher tests for OpenAI, Grok, Anthropic, Gemini, and forced-platform groups. Every `moderation_required=true` text route must enter exactly one pipeline.
- [ ] Include Grok Chat/Responses/Messages aliases and streaming variants, OpenAI `/v1/messages/count_tokens`, OpenAI embeddings, and Responses WebSocket admission. Assert Grok media/video/embeddings keep their existing dedicated behavior and are not double-moderated.
- [ ] Add executable handler tests proving moderation occurs before billing/account selection and a block prevents all downstream calls. Responses WebSocket tests cover initial and subsequent client frames, exactly one guard call per accepted frame, and zero forwarding after a block.
- [ ] Run the red tests:

```bash
cd backend
go test ./internal/server/routes ./internal/handler ./internal/pkg/moderationcoverage \
  -run 'GatewayPipelineEntrypointDispatcher|Grok.*Moderation|CountTokens.*Moderation|OpenAIResponsesWebSocket.*Moderation|ModeratedRoute|ModerationCoverage|Pipeline.*Coverage' -count=1
```

- [ ] Replace platform exclusion with a precise route-capability matrix. Grok Chat/Messages/Responses use the OpenAI HTTP admission implementation. Add the unique generic-pipeline exception for `GatewayHandler.CountTokens + anthropic_messages` on OpenAI groups; Grok count-tokens remains unsupported.
- [ ] Make `OpenAIGatewayHandler.CountTokens` consume the audited pre-forward body from request context when present, retaining its direct-handler fallback for tests. Auditing, parsing, and forwarding must use identical bytes.
- [ ] Keep post-handler admission enforcement as defense in depth and fail tests if any required branch has neither admission nor a block.
- [ ] Verify all manifest routes:

```bash
cd backend
go test ./internal/server/routes ./internal/handler ./internal/pkg/moderationcoverage \
  -run 'ModeratedRoute|ModerationCoverage|Pipeline.*Coverage|Grok|CountTokens' -count=1
```

Expected: PASS and all current manifest entries remain covered; the count may grow when legitimate routes are added.

- [ ] Commit with `fix: enforce moderation on every text gateway route`.

### Task 3: Enforce Failure Semantics And Provider Results

**Files:**

- Modify: `backend/internal/service/content_moderation.go:1189-1480,3105-3168,4100-4123,4563-4592`
- Modify: `backend/internal/service/content_moderation_test.go`
- Create: `backend/internal/service/content_moderation_metrics.go`
- Create: `backend/internal/service/content_moderation_metrics_test.go`
- Modify: `backend/internal/service/ops_metrics_collector.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service_test.go`
- Modify: `docs/risk-control/content-moderation-semantics.md`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`
- Modify: `Makefile`

- [ ] Add a matrix for `pre_block` x `rule_only/api_only/hybrid` x `public/trusted` x switch/config error, missing/all-frozen credentials, timeout/429/5xx/bad response, cache error, and classifier timeout/saturation/bad response.
- [ ] Assert public traffic receives `503`, `action=error`, and `blocked=true` for every required safety dependency failure.
- [ ] In Release 1, assert switch/config load failures return 503 for every group because no trustworthy config exists. After config loads successfully, listed trusted groups may fail open only for typed provider/classifier/completeness failures; deterministic hard rules still block them.
- [ ] Reverse unsafe expectations around `content_moderation_test.go:3122-3153` and status expectations around `:5887-5916`.
- [ ] Change `isRiskControlEnabled` to `(bool, error)`. Explicit `false` is disabled; repository failure enters the safety failure path.
- [ ] Make `UpdateConfig` reject enabling `api_only/hybrid` without at least one configured credential, and keep a request-time guard for rotation/freeze failures. Versioned publication validation replaces this compatibility check in Release 2.
- [ ] Treat provider `flagged=true` as blocking evidence, preserve unknown categories, and classify empty/malformed results as provider errors.
- [ ] Put all keys/retries under one overall deadline; `RetryCount=5` must not multiply the request budget into minutes.
- [ ] Add bounded reason-labeled counters/gauges before any production canary: decisions/failures/truncations, usable credentials, route mismatch, queue drop, outbox dead letter/age, and pre-block duration. Include instance/boot identity in persisted deltas so process restart cannot create negative or duplicate aggregation.
- [ ] Add direct critical safety events for public fail-open, admission mismatch, no usable required credential, queue drop, dead letter, and unavailable policy/config. Persist/aggregate across instances through the existing ops path; stale/no-data after two collector intervals is unhealthy. Define maximum critical notification latency as 90 seconds because the collector minimum is 60 seconds.
- [ ] Test firing/resolution, restart/reset, multi-instance aggregation, stale/no-data behavior, label allowlists/cardinality, and <=90-second notification timing. Update `RiskControlView` with the critical banner and add its spec to `FRONTEND_CRITICAL_VITEST` in the root `Makefile`.
- [ ] Run:

```bash
cd backend
go test ./internal/service \
  -run 'TestContentModeration(Check|Status).*?(Fail|API|Key|Provider|Trusted|Flagged|Category)' -count=1
go test ./internal/service \
  -run 'ContentModerationMetrics|OpsAlertEvaluator.*Moderation' -count=1
```

Expected: PASS and no public required-dependency failure returns allow.

- [ ] Commit with `fix: make moderation failures fail closed`.

### Task 4: Remove Text-Trust Bypasses And Silent Truncation

**Files:**

- Modify: `backend/internal/service/content_moderation_input.go:14-110,1020-1181`
- Modify: `backend/internal/service/content_moderation_input_test.go`
- Modify: `backend/internal/service/content_moderation.go:1288-1376,3574-3586,4942-5033`
- Modify: `backend/internal/service/content_moderation_test.go`

- [ ] Add stable `TestContentModerationBypassCorpus` and `TestContentModerationTrustedFailureControls` tables for Chat, Responses, OpenAI Messages/Anthropic, Gemini, embeddings, token-count, and multimodal requests. Seed unsafe tail after 12k runes, forged reminders/agent markers, unsafe tool content, mixed intent, max depth/strings/total runes, oversized base64, multiple images beyond inspection capacity, unsupported audio/video parts, and Unicode/zero-width/spacing variants. Public enforce mode must mark every unsupported/partially inspected modality incomplete and fail closed.
- [ ] Keep neutral educational and real internal-scaffold samples as negative controls, but pass internal provenance outside the body.
- [ ] Confirm the corpus fails on current behavior:

```bash
cd backend
go test ./internal/service \
  -run '^(TestContentModerationBypassCorpus|TestContentModerationTrustedFailureControls)$' -count=1
```

- [ ] Remove behavior that strips reminder text or trusts body marker combinations. Client body fields are always untrusted.
- [ ] Replace educational/meta automatic downgrade with contextual evidence. Only `hard_deny` is terminal; contextual/observe continues to the provider.
- [ ] Report every limit with a stable completeness reason. Enforce mode returns 413/422 for input limits or 503 for internal extraction failure; it never silently trims an unscanned suffix.
- [ ] Add `FuzzExtractContentModerationInput_ChaosCorpus`; assert bounded output and a non-empty completeness reason whenever content is omitted. Run:

```bash
cd backend
go test ./internal/service -run '^$' \
  -fuzz '^FuzzExtractContentModerationInput_ChaosCorpus$' -fuzztime=60s -timeout=90s
```

Stop on panic, hang, unbounded allocation, or missing incomplete state.
- [ ] Commit with `fix: remove moderation text trust bypasses`.

### Task 5: Stop Raw Retention And Secret-Bearing Outbox Events

**Files:**

- Create: `backend/migrations/174_content_moderation_outbox_v2.sql`
- Modify: `backend/internal/service/content_moderation_outbox.go:79-149`
- Modify: `backend/internal/service/content_moderation.go:590-612,817-819,940-946,2044-2064,6319-6419`
- Modify: `backend/internal/service/wire.go:40-59`
- Modify: `backend/internal/repository/content_moderation_repo.go:86-217,291-340`
- Modify: `backend/internal/repository/content_moderation_outbox_repo.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/server/routes/admin.go:120-135`
- Modify: `frontend/src/api/admin/riskControl.ts`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`
- Test: moderation service/repository/admin handler/request parse-log tests.

- [ ] Add idempotent migration 174 and update the schema integration test:

```sql
ALTER TABLE content_moderation_outbox ADD COLUMN IF NOT EXISTS payload_version SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE content_moderation_outbox ADD COLUMN IF NOT EXISTS log_id BIGINT REFERENCES content_moderation_logs(id) ON DELETE CASCADE;
ALTER TABLE content_moderation_outbox ADD COLUMN IF NOT EXISTS policy_version_id BIGINT;
ALTER TABLE content_moderation_outbox ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE content_moderation_raw_request_snapshots ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_content_moderation_outbox_payload_version
  ON content_moderation_outbox(payload_version, status, id);
CREATE INDEX IF NOT EXISTS idx_content_moderation_raw_request_expiry
  ON content_moderation_raw_request_snapshots(expires_at, id);
CREATE TABLE IF NOT EXISTS content_moderation_outbox_state (
  id SMALLINT PRIMARY KEY CHECK (id = 1),
  write_version SMALLINT NOT NULL DEFAULT 1 CHECK (write_version IN (1,2)),
  lock_version BIGINT NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO content_moderation_outbox_state(id) VALUES (1) ON CONFLICT (id) DO NOTHING;
```

- [ ] Add `CreateDecisionWithEvents(ctx, log, events)` in the repository. In one transaction, upsert the minimal log by `decision_id`, capture `log_id`, and insert v2 side-effect events with per-event safe scalars. Retire `content_moderation_log_write` for new decisions.
- [ ] Define typed v2 event payloads; common IDs live in outbox columns, not duplicated JSON:

```go
type violationCountV2 struct { WindowHours int; ExcludeCyberPolicy bool }
type autoBanV2 struct { WindowHours int; ExcludeCyberPolicy bool; Threshold int }
type emailV2 struct { Kind string; WindowHours int; ExcludeCyberPolicy bool; BanThreshold int }
type hashRecordV2 struct { InputHash string; ExpiresAt time.Time }
type adminAlertV2 struct{}
```

Workers load the sanitized log/user by `log_id`; email addresses, names, excerpts, policy objects, and credentials never enter payload JSON. `autoBanV2` is emitted only when auto-ban is enabled and recalculates its own bounded-window count; email recalculates/loads the count it displays. Neither event depends on the violation-count event running first.
- [ ] Implement dual v1/v2 outbox readers. Convert pending/retry/dead-letter v1 rows in bounded idempotent batches; keep v1 decoding until all v1 rows drain. A v1-only binary is not a valid rollback target once v2 writes begin.
- [ ] V1 conversion decodes `payload.log/config`, blanks legacy excerpt/email/name/error fields before idempotent log upsert, discards all credentials, and maps event fields exactly: violation window/exclusion; auto-ban window/exclusion/threshold when enabled (disabled pending rows become succeeded no-ops); email kind/window/exclusion/threshold; input hash+expiry; or empty admin alert. Update the existing row to v2 in the same transaction while preserving ID, decision/event keys, priority, status, retry/max-retry counts, next retry, lock/dead-letter timestamps, and uniqueness semantics.
- [ ] Add explicit table tests for pending, retry, and dead-letter v1 source rows. Assert every typed field mapping and exact preservation of status, retry/max-retry, next-retry, lock, dead-letter, priority, event key, and dedup behavior across an interrupted/repeated conversion.
- [ ] Replay violation, auto-ban, email, hash, and admin-alert events independently and in every relevant order. Assert identical final count/ban/email state and no duplicate side effect; a missing/succeeded sibling event cannot change correctness.
- [ ] Deploy Release 1 in two bounded steps: first deploy dual-read code everywhere while `write_version=1` and stop raw writes; then CAS `write_version` to 2 only after every old node exits. The v1 mixed window is explicitly temporary and blocks Release 1 completion. After the switch, rollback keeps `write_version=2` and may use only the dual-reader containment image or newer.
- [ ] Seed one unique canary set covering bearer/JWT, password, email, phone, URL query token, base64 form, malformed JSON head/tail, prompt text, and echoed provider errors. Assert absence from slog/zap fields, SQL args and real JSONB rows, converted v1-to-v2 rows, email/admin-alert events, dead-letter/status DTOs, admin HTTP responses, and raw snapshots. Plaintext credentials are allowed only at the exact `settings.key='content_moderation_config'` legacy row until Release 2 import/scrub; no broader allowlist is permitted.
- [ ] Add raw-store spies for cyber-policy and session-block paths; expected new writes: zero.
- [ ] Replace new outbox payloads with `decision_id`, `log_id`, and event-specific non-sensitive scalars. Workers load sanitized state by ID; dead-letter APIs never return arbitrary payload maps.
- [ ] Stop copying new `user_email`, API-key/group names, and arbitrary input prefixes into moderation rows; list DTOs resolve current display fields through foreign keys. Only a local-rule matched window may be added by the later evidence schema.
- [ ] Stop injecting/calling the raw snapshot store. Return `410 Gone` from the old raw endpoint and remove the raw-view UI control in Release 1. Set existing raw rows to a bounded expiry and add a batched cleanup job; do not drop the table in this task.
- [ ] Backfill `expires_at = created_at + interval '30 days'` for legacy raw rows with null expiry. Cleanup uses small `id` batches and reports deleted/error counts; an operator may shorten retention only after rollback evidence is no longer needed.
- [ ] Centralize redaction. Malformed JSON and provider errors log length/hash plus bounded sanitized diagnostics, never request head/tail or arbitrary upstream bodies.
- [ ] Run:

```bash
cd backend
go test -tags=unit ./internal/handler -run TestLogRequestBodyParseFailure -count=1
go test ./internal/service ./internal/repository ./internal/handler/admin \
  -run 'Outbox|DeadLetter|Redact|RawRequest|Secret' -count=1
go test -tags=integration ./internal/repository \
  -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1
```

Expected: PASS; no seed appears outside the explicitly asserted legacy-settings exception, no new raw row is written, and v1/v2 replay is idempotent.

- [ ] Commit with `fix: minimize moderation audit data`.

### Release 1 Gate

- [ ] Run focused race tests across service, handler, route coverage, and repositories.
- [ ] Run backend unit/integration targets and `RiskControlView.spec.ts`.
- [ ] Require aggregated counters/critical alerts and the 90-second notification SLA to pass firing/resolution, restart, stale-data, and multi-instance tests before any production traffic.
- [ ] Capture `/tmp/cm-release1-candidate.txt` on the same host with the same benchmark command and run `backend/scripts/check-content-moderation-bench.sh /tmp/cm-release1-base.txt /tmp/cm-release1-candidate.txt`; require exit 0.
- [ ] Verify zero known route bypass, zero public fail-open, zero raw snapshot write, no raw-view UI/API, and zero seeded secret disclosure outside the temporary legacy-settings exception.
- [ ] Drain/convert all v1 outbox rows and rehearse replay before rollout. Keep only Release 1 containment images as rollback artifacts; pre-containment images are forbidden because they resume raw/secret writes.
- [ ] Execute the common staging/5%/25%/100% protocol and containment-image rollback rehearsal.

## Chunk 2: Release 2 - Core Replacement

### Task 6: Introduce Domain Types And A Pure Policy Engine

**Files:**

- Create: `backend/internal/service/content_moderation_types.go`
- Create: `backend/internal/service/content_moderation_policy.go`
- Create: `backend/internal/service/content_moderation_policy_test.go`
- Modify: `backend/internal/service/content_moderation.go`

- [ ] Before modifying behavior, capture same-host `/tmp/cm-release2-base.txt` with `go test ./internal/service -run '^$' -bench '^BenchmarkContentModeration' -benchmem -count=5 | tee /tmp/cm-release2-base.txt` from `backend/`.
- [ ] Write table tests for extraction completeness, hard/contextual/observe evidence, provider flagged/scores/errors, public/trusted subjects, enforce/shadow modes, and existing risk state.
- [ ] Define immutable `ModerationRequest`, `ExtractedText`, `ModerationEvidence`, `PolicySnapshot`, `ModerationDecision`, and `RouteConstraint` values from the architecture spec.
- [ ] Implement a pure interface with no repository, HTTP, logger, or global-state dependency:

```go
type ModerationPolicyEngine interface {
    Decide(PolicySnapshot, DecisionInput) ModerationDecision
}
```

- [ ] Apply precedence in this order: invalid/incomplete input, local hard deny, provider failure policy, provider flagged/threshold/unknown-category evidence, contextual/observe evidence, risk action, allow.
- [ ] Add a compatibility converter so existing handlers preserve status/message fields while migrating.
- [ ] Run `go test ./internal/service -run TestModerationPolicyEngine -count=1`. Expected: PASS without mocks or I/O.
- [ ] Commit with `refactor: add moderation policy engine`.

### Task 7: Build Bounded Extraction And Compiled Rule Evidence

**Files:**

- Modify: `backend/internal/service/content_moderation_input.go`
- Modify: `backend/internal/service/content_moderation_input_test.go`
- Create: `backend/internal/service/content_moderation_rules.go`
- Create: `backend/internal/service/content_moderation_rules_test.go`
- Modify: `backend/internal/service/content_moderation_benchmark_test.go`
- Modify: `backend/internal/service/content_moderation.go`

- [ ] Assert exact ordered source/role/index metadata for Chat, Responses, Messages, Embeddings, Anthropic, Gemini, token count, and tool/function content. Unknown client roles remain untrusted and included.
- [ ] Parse once into bounded segments and report `max_body_bytes`, `max_segments`, `max_depth`, `max_segment_runes`, `max_total_runes`, and `unsupported_media` explicitly.
- [ ] Test all-match behavior: rule order cannot suppress higher severity, disabled/observe rules are non-terminal, normalization preserves boundaries, and spans map to original sources.
- [ ] Compile/normalize rules only when a policy revision loads. Use a deterministic indexed or multi-pattern matcher with explicit boundaries; do not normalize 10k rules per source/request.
- [ ] Route the new evidence into the pure policy engine. During shadow comparison, an old-engine allow can never override a new hard deny.
- [ ] Run benchmarks:

```bash
cd backend
go test ./internal/service -run '^$' \
  -bench 'BenchmarkContentModeration(Extract|Rules|Concurrent)' -benchmem -count=5
```

Cover 12k runes x 10k rules for no-hit/tail-hit, a 1 MiB multi-message body, and concurrent decisions.

- [ ] Commit with `refactor: add bounded moderation extraction and rules`.

### Task 8: Add The Provider Adapter And Restricted Egress

**Files:**

- Create: `backend/internal/service/content_moderation_provider.go`
- Create: `backend/internal/service/content_moderation_provider_test.go`
- Modify: `backend/internal/service/content_moderation.go:3171-3450,4548-4592`
- Modify: `backend/internal/config/config.go`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`

- [ ] Use `httptest` for success, flagged, unknown categories, empty/multiple results, chunk/result count or index mismatch, invalid JSON, 401/429/5xx, timeout, frozen credentials, retry budget, and cancellation.
- [ ] Add SSRF tests for HTTP, localhost, loopback, link-local, RFC1918, metadata ranges, DNS rebinding, embedded credentials, and redirect-to-private targets.
- [ ] Implement:

```go
type ModerationProvider interface {
    Evaluate(context.Context, ProviderPolicy, []TextChunk) (ModelEvidence, error)
}
```

- [ ] Enforce one overall deadline, response-size limits, a restricted `http.Transport`, HTTPS/host allowlists, and typed errors. Provider test APIs cannot choose arbitrary destinations.
- [ ] Preserve deterministic result-to-input-chunk mapping. Any count/index mismatch is `schema_mismatch`; only a mapped flagged chunk may later produce a model evidence excerpt.
- [ ] Keep credential plaintext inside this boundary only. Logs/status expose a fingerprint/mask, never ciphertext or plaintext.
- [ ] Run `go test ./internal/service -run TestModerationProvider -count=1`. Expected: no private-network attempt and bounded wall time.
- [ ] Commit with `refactor: isolate moderation provider access`.

### Task 9: Add Versioned Policy, Credentials, And Atomic Runtime Snapshots

**Files:**

- Create: `backend/migrations/175_content_moderation_policy_runtime.sql`
- Create: `backend/internal/repository/content_moderation_policy_repo.go`
- Create: `backend/internal/repository/content_moderation_policy_repo_test.go`
- Create: `backend/internal/repository/content_moderation_legacy_migrator.go`
- Create: `backend/internal/repository/content_moderation_legacy_migrator_test.go`
- Create: `backend/cmd/content-moderation-migrate/main.go`
- Create: `backend/internal/service/content_moderation_runtime.go`
- Extend: `backend/internal/service/content_moderation_runtime_test.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_metrics.go`
- Modify: `backend/internal/service/content_moderation_metrics_test.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service_test.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/service/setting_public.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler_test.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/middleware/admin_auth.go:149-201`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`
- Regenerate: `backend/cmd/server/wire_gen.go` with `make -C backend generate` from the repository root

- [ ] Write repository tests for immutable versions, schema validation, expected-version/lock CAS, one active plus optional shadow/canary candidates, stable cohort selection, rollback, encrypted secrets, advisory-locked migration, and transaction rollback.
- [ ] Create idempotent raw-SQL objects and register every table/column/index/constraint in `migrations_schema_integration_test.go`:

```sql
CREATE TABLE IF NOT EXISTS content_moderation_policy_versions (
  id BIGSERIAL PRIMARY KEY,
  status VARCHAR(16) NOT NULL,
  schema_version VARCHAR(32) NOT NULL,
  policy JSONB NOT NULL,
  content_hash VARCHAR(64) NOT NULL,
  base_version_id BIGINT REFERENCES content_moderation_policy_versions(id),
  draft_revision BIGINT NOT NULL DEFAULT 1,
  created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  published_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  published_at TIMESTAMPTZ,
  CONSTRAINT moderation_policy_status_check
    CHECK (status IN ('draft','shadow','canary','published','archived','rejected'))
);

CREATE INDEX IF NOT EXISTS idx_moderation_policy_status_created
  ON content_moderation_policy_versions(status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_moderation_policy_base_draft
  ON content_moderation_policy_versions(base_version_id, draft_revision)
  WHERE base_version_id IS NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_moderation_outbox_policy_version') THEN
    ALTER TABLE content_moderation_outbox
      ADD CONSTRAINT fk_moderation_outbox_policy_version
      FOREIGN KEY (policy_version_id) REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS content_moderation_secrets (
  id BIGSERIAL PRIMARY KEY,
  kind VARCHAR(32) NOT NULL,
  ciphertext TEXT NOT NULL,
  key_id VARCHAR(64) NOT NULL,
  fingerprint VARCHAR(64) NOT NULL,
  masked VARCHAR(128) NOT NULL,
  created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  retired_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_moderation_secret_active_fingerprint
  ON content_moderation_secrets(kind, fingerprint)
  WHERE retired_at IS NULL;

CREATE TABLE IF NOT EXISTS content_moderation_policy_secret_refs (
  policy_version_id BIGINT NOT NULL REFERENCES content_moderation_policy_versions(id) ON DELETE CASCADE,
  secret_id BIGINT NOT NULL REFERENCES content_moderation_secrets(id) ON DELETE RESTRICT,
  position INT NOT NULL,
  PRIMARY KEY (policy_version_id, secret_id),
  UNIQUE (policy_version_id, position)
);

CREATE TABLE IF NOT EXISTS content_moderation_policy_state (
  id SMALLINT PRIMARY KEY CHECK (id = 1),
  authority VARCHAR(16) NOT NULL DEFAULT 'legacy' CHECK (authority IN ('legacy','policy')),
  writes_frozen BOOLEAN NOT NULL DEFAULT FALSE,
  risk_control_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  active_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE RESTRICT,
  shadow_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL,
  canary_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL,
  canary_percent SMALLINT NOT NULL DEFAULT 0 CHECK (canary_percent BETWEEN 0 AND 100),
  cohort_salt VARCHAR(64) NOT NULL DEFAULT '',
  rollout_started_at TIMESTAMPTZ,
  lock_version BIGINT NOT NULL DEFAULT 1,
  updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS content_moderation_policy_activations (
  id BIGSERIAL PRIMARY KEY,
  previous_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL,
  target_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL,
  action VARCHAR(16) NOT NULL CHECK (action IN ('shadow','canary','publish','rollback','enable','disable','authority_switch')),
  enabled_before BOOLEAN NOT NULL,
  enabled_after BOOLEAN NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  operator_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_moderation_policy_activation_created
  ON content_moderation_policy_activations(created_at DESC, id DESC);
INSERT INTO content_moderation_policy_state(id)
  VALUES (1) ON CONFLICT (id) DO NOTHING;
```

- [ ] Add `ContentModerationSecurityConfig` and examples for `CONTENT_MODERATION_ENCRYPTION_KEY_ID`, `CONTENT_MODERATION_ENCRYPTION_KEY`, `CONTENT_MODERATION_PREVIOUS_ENCRYPTION_KEY_ID`, and `CONTENT_MODERATION_PREVIOUS_ENCRYPTION_KEY`. Keys are explicit 32-byte values; never auto-generate or reuse TOTP material. Reserve a secret ID with `nextval` inside the transaction before encryption, then use AES-GCM AAD `sub2api/content-moderation/secret/v1:<secret_id>:<kind>` and insert with that ID.
- [ ] Validate policy JSON recursively rejects `api_key`, `api_keys`, ciphertext, and unknown schema fields. Validate/compile before publication; CAS every state change with expected active version and lock version, returning 409 on conflict.
- [ ] Store active, shadow, and canary compiled snapshots in one atomic runtime value. Select canary cohorts stably from an HMAC of cohort salt plus user/API-key identity. Requests perform no policy DB read.
- [ ] Extend persisted metrics before Release 2 canary with active/candidate policy version, cohort, outcome, failure reason, and latency labels from bounded allowlists. Add unavailable/invalid snapshot critical alerts and tests for active-vs-candidate comparison, multi-instance aggregation, restart, stale data, firing, and resolution.
- [ ] Add backend admin endpoints now for policy list/create/validate/shadow/canary/publish/rollback and secret create/retire. Existing admin auth protects them until granular RBAC lands in Release 3; every state-changing call emits an admin audit event.
- [ ] Make `auth_method=admin_api_key` read-only for all risk-control endpoints before exposing policy/secret writes. Until Release 3 roles land, only authenticated human JWT administrators may write compatibility config, secrets, or policy state; add forbidden and audit tests.
- [ ] Make existing `GET /config` project the authoritative active policy. Before authority switch, `PUT /config` writes legacy settings through its current path; after switch it creates/publishes a new version with optional `If-Match` and never writes plaintext credentials. Route `risk_control_enabled` updates in `setting_update.go` through the same policy-state transaction and mirror only the public projection in settings.
- [ ] Implement an explicit command with PostgreSQL advisory locking:
  - `import`: copy legacy config/secrets into draft/version rows without changing authority or deleting settings;
  - `verify`: compare normalized legacy and versioned policies, decrypt credentials, and scan for missing references;
  - `freeze`/`unfreeze`: return 423 from both legacy config write paths during mixed-version cutover;
  - `switch`: require every node to be policy-capable, then atomically set `authority=policy`;
  - `export`: repopulate legacy settings only before a deliberate rollback to the containment baseline;
  - `rekey`: under a separate PostgreSQL advisory lock, batch-select rows whose `key_id` is the configured previous key, decrypt with previous, encrypt with current using the same identity-based AAD, and update atomically; selecting by old `key_id` makes interrupted runs resumable;
  - `scrub`: available only after the rollback window and final gate.
- [ ] Rekey covers both credential rows and encrypted evidence. Rotation sequence is: deploy new-current/old-previous to every node, run resumable `rekey`, verify zero old-key rows, retain the previous key through the longest evidence rollback/retention window, then remove it. A second rotation is forbidden while any row still uses the older key.
- [ ] Cutover sequence: deploy dual-capable code on all nodes, freeze writes, `import`, `verify`, exercise shadow/canary APIs, `switch`, verify projections, then unfreeze. Before switch, rollback may use the containment baseline; after switch, rollback may use only policy-aware images unless `export` is run first. After scrub, pre-policy images are permanently forbidden.
- [ ] Add migration tests for writer-version CAS, interrupted v1 conversion resume, advisory-lock contention, import/export round-trip, authority switch, reserved-ID AAD, wrong/absent previous key, interrupted rekey resume for secrets/evidence, zero-old-key verification, and scrub dry-run refusal. Run:

```bash
cd backend
go test -tags=integration ./internal/repository \
  -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1
go test ./internal/service ./internal/repository ./internal/handler/admin -run 'ContentModeration(Policy|Runtime|Migration)' -count=1
cd ..
make -C backend generate
cd backend
go test ./cmd/server ./internal/service ./internal/repository ./internal/handler/admin -count=1
```

- [ ] Inspect generated `backend/cmd/server/wire_gen.go` diffs; unexpected providers block the task.
- [ ] Commit with `feat: version content moderation policies`.

### Task 10: Add Minimal Evidence And A Scoped Decision Cache

**Files:**

- Create: `backend/migrations/176_content_moderation_evidence.sql`
- Create: `backend/internal/service/content_moderation_evidence.go`
- Create: `backend/internal/service/content_moderation_evidence_test.go`
- Modify: `backend/internal/repository/content_moderation_repo.go`
- Modify: `backend/internal/repository/content_moderation_repo_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Replace behavior in: `backend/internal/repository/content_moderation_hash_cache.go`
- Modify: `backend/internal/service/content_moderation_test.go`

- [ ] Test that blocked records contain policy version, risk, detector evidence, HMAC, source, completeness, and only a redacted matched window. Allow/shadow records contain no input text.
- [ ] Add migration 176 idempotently and register it in the schema integration test:

```sql
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS policy_version_id BIGINT REFERENCES content_moderation_policy_versions(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS risk_level VARCHAR(16) NOT NULL DEFAULT 'none';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS evidence JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS input_hmac VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS input_hmac_key_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS matched_source VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS input_complete BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS provider_outcome VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS error_code VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS content_moderation_evidence (
  log_id BIGINT PRIMARY KEY REFERENCES content_moderation_logs(id) ON DELETE CASCADE,
  source VARCHAR(255) NOT NULL,
  evidence_scope VARCHAR(32) NOT NULL CHECK (evidence_scope IN ('exact_span','flagged_chunk')),
  excerpt_encrypted TEXT NOT NULL,
  encryption_key_id VARCHAR(64) NOT NULL,
  excerpt_bytes INT NOT NULL CHECK (excerpt_bytes >= 0),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_policy_created
  ON content_moderation_logs(policy_version_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_expiry
  ON content_moderation_logs(expires_at, id);
CREATE INDEX IF NOT EXISTS idx_content_moderation_evidence_expiry
  ON content_moderation_evidence(expires_at, log_id);
```

- [ ] `account_action_id` belongs to Release 3 migration 177.
- [ ] Local rules store only a concrete matched window. A model block may store the redacted highest-risk chunk only when the adapter maps a result to that chunk; aggregate-only results store structured evidence and no excerpt.
- [ ] Limit local matched windows to 160 Unicode characters by default and 512 maximum. Redact before limiting. Default retention is 30 days, maximum 90.
- [ ] Encrypt evidence with AAD `sub2api/content-moderation/evidence/v1:<log_id>:<policy_version_id>`. Derive a separate rotation-aware input-HMAC key using HKDF info `sub2api/content-moderation/input-hmac/v1`; persist its key ID.
- [ ] Extend the Release 1 transaction to insert encrypted evidence with the decision/outbox. New allow/shadow rows store aggregate/HMAC sampling only and never text. Clear legacy allow/shadow excerpts and expire old blocked excerpts/raw rows through bounded cleanup after the rollback window.
- [ ] Replace the permanent set with a scoped TTL key:

```text
moderation:decision:v2:<policy_version>:<provider_policy_hash>:<input_hmac>
```

Use short TTLs, no cache for incomplete/provider-error results, and non-terminal cache failures.

- [ ] In Release 2, false-positive review revokes only scoped cache entries and must not change user status. Reversal of attributable moderation actions begins after migration 177 in Task 11.
- [ ] Run `go test ./internal/service ./internal/repository -run 'ModerationEvidence|DecisionCache|FalsePositive|ContentModerationRepository' -count=1`.
- [ ] Run `go test -tags=integration ./internal/repository -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1`.
- [ ] Commit with `feat: persist minimal moderation evidence`.

### Release 2 Gate

- [ ] Complete `import/verify/freeze/switch/unfreeze` on staging and rehearse pre-scrub `export` rollback. No mixed-version node may write configuration during the freeze.
- [ ] Verify active/shadow/canary state, stable cohort selection, ETag conflicts, and backend publish/rollback APIs before traffic canary; the Release 3 UI is not required for this gate.
- [ ] Run old/new engines in shadow on a synthetic/redacted staging replay; classify every disagreement.
- [ ] Block release if the new engine allows any known bypass or public safety failure.
- [ ] Confirm request-path policy DB reads are zero under trace/load.
- [ ] Confirm no policy JSON, outbox, log, or admin response contains credentials or complete bodies.
- [ ] Capture `/tmp/cm-release2-candidate.txt` on the same host and require `backend/scripts/check-content-moderation-bench.sh /tmp/cm-release2-base.txt /tmp/cm-release2-candidate.txt` to exit 0.
- [ ] Execute the common staging/5%/25%/100% protocol using immutable versions; verify rollback by version plus a policy-aware image at or above the containment baseline.

## Chunk 3: Release 3 - Account Protection, Governance, And Cleanup

### Task 11: Feed Moderation Risk Into Account Selection

**Files:**

- Create: `backend/migrations/177_content_moderation_actions_and_risk.sql`
- Create: `backend/internal/service/content_moderation_risk.go`
- Create: `backend/internal/service/content_moderation_risk_test.go`
- Create: `backend/internal/repository/content_moderation_risk_repo.go`
- Create: `backend/internal/repository/content_moderation_risk_repo_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/handler/content_moderation_guard.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/gateway_scheduling.go`
- Modify: `backend/internal/handler/openai_gateway_executable_pipeline.go`
- Modify: `backend/internal/handler/openai_gateway_count_tokens.go`
- Modify: `backend/internal/handler/grok_media.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go:2310-2715` for upstream safety-signal attribution

- [ ] Before modifying behavior, capture same-host `/tmp/cm-release3-base.txt` with `go test ./internal/service -run '^$' -bench '^BenchmarkContentModeration' -benchmem -count=5 | tee /tmp/cm-release3-base.txt` from `backend/`.
- [ ] Test repeated high/critical attempts, expiration/cooldown, isolation between users/API keys/groups, upstream warnings, fragile-account exclusion, concurrency, and review reversal.
- [ ] Assert a policy-rejected request is never retried across additional upstream accounts.
- [ ] Add idempotent migration 177 and register it in the schema integration test:

```sql
CREATE TABLE IF NOT EXISTS content_moderation_actions (
  id BIGSERIAL PRIMARY KEY,
  decision_id VARCHAR(128) NOT NULL,
  user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  action VARCHAR(32) NOT NULL,
  prior_user_status VARCHAR(32) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL CHECK (status IN ('active','reversed','expired')),
  expires_at TIMESTAMPTZ,
  reversed_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  reversed_reason TEXT NOT NULL DEFAULT '',
  reversed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_moderation_action_decision_action
  ON content_moderation_actions(decision_id, action);
CREATE INDEX IF NOT EXISTS idx_moderation_action_user_status_expiry
  ON content_moderation_actions(user_id, status, expires_at);

CREATE TABLE IF NOT EXISTS content_moderation_risk_events (
  id BIGSERIAL PRIMARY KEY,
  dimension VARCHAR(16) NOT NULL CHECK (dimension IN ('user','api_key','group','channel','account')),
  dimension_key_hash VARCHAR(128) NOT NULL,
  provider VARCHAR(64) NOT NULL DEFAULT '',
  account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
  signal VARCHAR(32) NOT NULL CHECK (signal IN ('local_block','model_block','upstream_reject','provider_warning','manual_confirm')),
  score INT NOT NULL CHECK (score BETWEEN 0 AND 100),
  decision_id VARCHAR(128) NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_moderation_risk_dimension_expiry
  ON content_moderation_risk_events(dimension, dimension_key_hash, expires_at);
CREATE INDEX IF NOT EXISTS idx_moderation_risk_account_expiry
  ON content_moderation_risk_events(provider, account_id, expires_at)
  WHERE account_id IS NOT NULL;

ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS account_action_id BIGINT REFERENCES content_moderation_actions(id) ON DELETE SET NULL;
```
- [ ] Use TTL-bounded Redis windows for user, client API key, group/channel, and upstream account. Keys include provider/policy scope.
- [ ] Store a typed `RouteConstraint` in request context; schedulers receive only constraints, never raw evidence or content.
- [ ] Map request-specific safety rejection first to subject/API-key risk. Shared-account health changes only for account-specific provider status/warnings or corroborated signals from multiple independent subjects in one bounded window; one malicious user can never quarantine an account.
- [ ] Add reversible moderation-action rows with decision ID, prior user state, action, expiry, and reversal metadata. Unban changes only an attributable moderation action.
- [ ] Run:

```bash
cd backend
go test -race ./internal/service ./internal/handler \
  -run 'ModerationRisk|RouteConstraint|AccountScheduler|SafetyReject|ModerationAction' -count=1
go test -tags=integration ./internal/repository \
  -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1
```

- [ ] Commit with `feat: protect upstream accounts with moderation risk`.

### Task 12: Finish Admin Policy Governance And Minimal Review UI

**Files:**

- Create: `backend/migrations/178_content_moderation_admin_roles.sql`
- Create: `backend/internal/server/middleware/content_moderation_rbac.go`
- Create: `backend/internal/server/middleware/content_moderation_rbac_test.go`
- Create: `backend/internal/handler/admin/content_moderation_audit.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler_test.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/middleware/admin_auth.go:149-201`
- Modify: `backend/internal/service/admin_audit.go`
- Modify: `backend/internal/handler/admin/admin_audit_coverage_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `docs/risk-control/admin-audit-coverage.json`
- Modify: `frontend/src/api/admin/riskControl.ts`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`
- Modify carefully: `frontend/src/i18n/locales/en/admin/channels.ts`
- Modify carefully: `frontend/src/i18n/locales/en/admin/extensions.ts`
- Modify carefully: `frontend/src/i18n/locales/zh/admin/channels.ts`
- Modify carefully: `frontend/src/i18n/locales/zh/admin/extensions.ts`

- [ ] Test draft creation, validation details, shadow/canary/publish with expected version, stale-version conflict, rollback, secret masks, evidence reads, and audit attempt/success/failure.
- [ ] Add idempotent migration 178 and register it in the schema integration test:

```sql
CREATE TABLE IF NOT EXISTS content_moderation_admin_roles (
  user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  role VARCHAR(32) NOT NULL CHECK (role IN ('viewer','reviewer','operator','policy_admin')),
  granted_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_moderation_admin_role
  ON content_moderation_admin_roles(role, user_id);
```

- [ ] Backfill existing human admins as `policy_admin`; service transactions prevent removal of the final policy admin. Role order is `viewer < reviewer < operator < policy_admin` with no implicit permissions outside risk-control routes.
- [ ] Map routes explicitly: viewer = status/config/log list/outbox status; reviewer = viewer + evidence read/review; operator = reviewer + attributed unban/cache eviction/outbox replay-cleanup/provider test; policy_admin = operator + policy/secret/role changes and publish/rollback.
- [ ] Add policy-admin-only `PUT /admin/risk-control/access/:user_id` and `DELETE /admin/risk-control/access/:user_id` endpoints with stale-role/last-policy-admin conflict tests. Add an operator cache-eviction endpoint scoped by policy/input HMAC; no global permanent-hash clear remains.
- [ ] In middleware, force `auth_method=admin_api_key` to `viewer` before any user-row/role lookup; it must never inherit the first administrator's role. Human JWT admins use the table, with super-admin compatibility only during migration.
- [ ] Keep `GET/PUT /admin/risk-control/config` as a one-release compatibility projection over the Task 9 policy API. All writes use ETag/expected lock version and encrypted secret references.
- [ ] Make the old `/raw-request` endpoint remain `410`; add an audited `/evidence` endpoint. Dead-letter endpoints return safe DTOs only.
- [ ] Update the UI for active version, draft/validation, shadow/canary state, provider health, completeness failures, and minimal local-rule evidence. Remove raw request viewing, `public_group_ids`, permanent hash clearing, and misleading healthy/protection claims.
- [ ] Preserve the existing user-owned changes in `frontend/src/components/admin/usage/__tests__/UsageStatsCards.spec.ts`, the six modified `frontend/src/i18n/locales/{en,zh}/...` files, and untracked `frontend/src/i18n/__tests__/localCustomizationLocales.spec.ts`. Read/merge overlapping extension-locale hunks; never revert them. Stage only reviewed moderation hunks.
- [ ] Emit attempt/success/failure audit actions named `risk_control.policy.create|validate|shadow|canary|publish|rollback`, `risk_control.secret.create|retire|test`, `risk_control.evidence.read`, `risk_control.review.update`, `risk_control.user_action.reverse`, `risk_control.outbox.replay|cleanup`, `risk_control.access.update`, `risk_control.config.update`, `risk_control.state.enable|disable`, and `risk_control.cache.evict`. Redact policy text, excerpts, credentials, HMACs, emails, and notes according to the manifest.
- [ ] Extend route/manifest tests so every risk-control write/read-sensitive route declares its minimum role and exact audit action; admin API key attempts at reviewer/operator/policy-admin routes must return forbidden and emit no privileged success event.
- [ ] Run:

```bash
cd backend
go test ./internal/handler/admin ./internal/server/routes \
  -run 'ContentModeration|AdminAudit|Policy' -count=1
cd ..
pnpm --dir frontend exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
cd backend
go test -tags=integration ./internal/repository \
  -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1
```

- [ ] Inspect `git diff --cached` to ensure unrelated locale work is absent, then commit with `feat: govern moderation policy releases`.

### Task 13: Add Metrics, Alerts, Benchmarks, And Evaluation Corpus

**Files:**

- Modify: `backend/internal/service/content_moderation_metrics.go`
- Modify: `backend/internal/service/content_moderation_metrics_test.go`
- Modify: `backend/internal/service/ops_metrics_collector.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service_test.go`
- Create: `backend/internal/service/testdata/content_moderation/corpus.jsonl`
- Modify: `backend/internal/service/content_moderation_benchmark_test.go`
- Modify: `backend/cmd/moderation-bench-guard/main.go`
- Modify: `backend/cmd/moderation-bench-guard/main_test.go`
- Modify: `backend/scripts/check-content-moderation-bench.sh`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`
- Modify: `Makefile`

- [ ] Test that every action and typed failure increments one bounded-cardinality metric. Distinguish public fail-closed from trusted fail-open; missing key/config/provider failures cannot appear as ordinary allow/disabled.
- [ ] Extend the Release 1 critical safety alerts for public fail-open, route/pipeline mismatch, no usable required credential, queue drop/dead letter, and unavailable/invalid policy snapshot; retain the defined maximum 90-second notification latency.
- [ ] Add sustained alerts for pre-block error ratio over 1% for five minutes, oldest outbox item over 120 seconds, and p95 latency over the approved budget for five minutes.
- [ ] Add a versioned synthetic/redacted JSONL corpus containing protocol, body fixture/reference, expected action/risk, rationale, and tags. Cover harmful, benign, multilingual, encoding, tool, unsafe-tail, spoofed-marker, educational, and ambiguous cases.
- [ ] Add a local replay test that fails, rather than only prints: known-bypass false negatives must be zero; confirmed hard-deny false negatives must be zero; benign false-positive rate may increase by at most 0.5 percentage points from the checked-in approved corpus baseline.
- [ ] Keep `RiskControlView.spec.ts` in `FRONTEND_CRITICAL_VITEST` and test critical banner firing/recovery without displaying secrets.
- [ ] Add a benchmark guard that parses two standard Go benchmark outputs captured on the same host. It exits nonzero when matched benchmarks regress over 20% in `ns/op` or `allocs/op`, or when extraction `B/op` exceeds 3x the fixture input bytes. Fixtures: small text, 12k runes x 10k rules, 1 MiB multi-message, configured max-body rejection, and parallel decisions.
- [ ] Reuse the exact same-host `/tmp/cm-release3-base.txt` captured at Task 11. After Task 13 changes, capture `/tmp/cm-release3-task13-candidate.txt` with the command below and run the checked-in guard; no self-reported benchmark prose substitutes for its exit status.
- [ ] Run race and performance gates:

```bash
cd backend
go test -race ./internal/service ./internal/handler ./internal/server/routes ./internal/repository \
  -run 'ContentModeration|Moderation|ModeratedRoute' -count=1
go test ./internal/service \
  -run 'ContentModerationMetrics|OpsAlertEvaluator.*Moderation|ContentModerationCorpus' -count=1
go test ./internal/service -run '^$' \
  -bench '^BenchmarkContentModeration' -benchmem -count=5 \
  | tee /tmp/cm-release3-task13-candidate.txt
cd ..
pnpm --dir frontend exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts
pnpm --dir frontend run typecheck
backend/scripts/check-content-moderation-bench.sh \
  /tmp/cm-release3-base.txt /tmp/cm-release3-task13-candidate.txt
```

Stop on any race, panic, nonlinear allocation, extraction memory above 3x input body, or unexplained >20% ns/op/allocs regression.

- [ ] Commit with `feat: observe content moderation safety`.

### Task 14: Remove Legacy Runtime Behavior And Complete Rollout

**Files:**

- Create: `backend/migrations/179_content_moderation_legacy_scrub.sql`
- Modify: `backend/cmd/content-moderation-migrate/main.go`
- Modify: `backend/internal/repository/content_moderation_legacy_migrator.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_input.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/service/setting_public.go`
- Modify: `backend/internal/repository/content_moderation_hash_cache.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `frontend/src/api/admin/riskControl.ts`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `docs/risk-control/content-moderation-semantics.md`

- [ ] Prove with tests/metrics that every request uses a compiled version and none reads legacy settings/global hashes. After authority cutover and rollback rehearsal, remove the startup importer from the request-serving binary; retain migration logic only in the operator command.
- [ ] Remove educational/meta auto-downgrade, body-text scaffold trust, terminal observe/warn paths, request-path settings reads, global permanent hash decisions, raw snapshot writer/view code, duplicate legacy mode/public-group runtime branches, false healthy status, and `antigravity.DefaultSafetySettings` if a final repository-wide `rg` confirms it remains unreferenced.
- [ ] Before scrub, rehearse image rollback and `export`; verify the containment baseline can serve the exported config without raw/outbox leakage. After scrub begins, pre-policy images are permanently disallowed and rollback is policy-version plus policy-aware image only.
- [ ] Generate migration 179 only after `content-moderation-migrate scrub --dry-run` reports no blocking rows; applying it is the operator-approved final gate. Use idempotent SQL equivalent to:

```sql
DELETE FROM content_moderation_raw_request_snapshots
  WHERE expires_at IS NULL OR expires_at <= NOW();
DELETE FROM content_moderation_outbox
  WHERE payload_version = 1 AND status IN ('succeeded','dead_letter');
UPDATE content_moderation_logs
  SET input_excerpt = '', user_email = '', api_key_name = '', group_name = '', error = ''
  WHERE input_excerpt <> '' OR user_email <> '' OR api_key_name <> '' OR group_name <> '' OR error <> '';
DELETE FROM settings WHERE key = 'content_moderation_config';
UPDATE content_moderation_policy_state
  SET authority = 'policy', writes_frozen = FALSE, updated_at = NOW()
  WHERE id = 1;
```

Keep `risk_control_enabled` as a public projection and do not drop tables. The command must process large deletes/updates in bounded batches before the final small migration assertion, rather than holding one unbounded transaction.
- [ ] Run a database-wide seed/shape scan: zero known credential seeds, test prompts, non-empty legacy excerpts, unexpired raw snapshots, v1 payloads, or JSON paths `payload.config`, `payload.log`, `api_key`, `api_keys`. Block completion on any hit.
- [ ] Run repository-wide verification:

```bash
make test-backend
make test-frontend
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
```

- [ ] Also run focused race, corpus, privacy, route coverage, and benchmark gates from prior tasks.
- [ ] Capture `/tmp/cm-release3-candidate.txt` on the same host and require `backend/scripts/check-content-moderation-bench.sh /tmp/cm-release3-base.txt /tmp/cm-release3-candidate.txt` to exit 0.
- [ ] Run `cd backend && go test -tags=integration ./internal/repository -run 'TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestContentModerationMigrations' -count=1` after generating and dry-running migration 179.
- [ ] Execute the common staging/5%/25%/100% protocol with its fixed error, latency, CPU, RSS, and safety thresholds.
- [ ] Exercise rollback by activating the prior policy version and a policy-aware containment-or-newer image. Never set moderation off/fail-open as rollback.
- [ ] Commit with `refactor: retire legacy moderation behavior`.

## Final Acceptance And Stop Checklist

- [ ] Every upstream HTTP text request reaches exactly one moderation admission path, and each accepted WebSocket client turn/frame is checked once, before forwarding.
- [ ] Grok and OpenAI token-count bypass tests pass.
- [ ] Public traffic never fails open for missing/invalid policy, missing required credentials, provider failure, or incomplete extraction.
- [ ] Only `hard_deny` short-circuits; contextual/observe evidence calls the moderation model.
- [ ] Provider `flagged` and unknown categories are never silently ignored.
- [ ] Client body text cannot establish trusted scaffold provenance.
- [ ] Enforce mode has no unreported truncation/unsupported text.
- [ ] New outbox/log/admin data contains no complete body or credential; evidence is redacted/bounded, and model-only excerpts exist only for deterministically mapped flagged chunks.
- [ ] Policy versions are immutable, CAS-published, atomically loaded, canaried, and rollback-tested.
- [ ] Permanent global hashes no longer affect decisions; scoped cache expires and review can revoke it.
- [ ] Account selection honors route constraints and never rotates a safety-rejected request through more accounts.
- [ ] Moderation actions are attributable/reversible without changing unrelated user status.
- [ ] Reason-labeled metrics aggregate across instances/restarts, stale data is unhealthy, and critical notification latency is at most 90 seconds.
- [ ] Same-host benchmark guards pass for each release under the fixed 20% CPU/allocation and 3x extraction-memory limits.
- [ ] Race, fuzz, corpus, privacy, route coverage, unit/integration, frontend, and benchmark gates pass.
- [ ] Staging, 5%, 25%, and 100% rollout gates pass, and rollback is exercised.
- [ ] Final scrub scan passes; all remaining rollback artifacts are policy-aware and at or above the containment baseline.

When every checkbox passes, the refactor is complete. Any later multimodal classifier, remote-service split, physical legacy-table drop, or analytics expansion requires a new approved design and plan.
