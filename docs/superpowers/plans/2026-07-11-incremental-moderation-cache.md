# Incremental Content Moderation Cache Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add provider-aware, fail-closed content moderation that sends stable 1,800-rune overlapping chunks to Zhipu or OpenAI and reuses only scoped, short-lived PASS verdicts for unchanged chunks.

**Architecture:** Keep gateway admission and local rules in `ContentModerationService`, while moving canonicalization/chunking, provider adaptation, Redis caching, and bounded batch execution into focused units. Redis stores only HMAC-scoped PASS/quarantine metadata. Existing JSON configuration stays backward compatible and defaults to OpenAI with caching disabled.

**Tech Stack:** Go 1.24, Gin, go-redis v9, miniredis, `golang.org/x/text/unicode/norm`, Vue 3, TypeScript, vue-i18n, Vitest.

**Specification:** `docs/superpowers/specs/2026-07-11-incremental-moderation-cache-design.md`

**Worktree:** `/Users/lushiwu/dev/sub2api/.worktrees/incremental-moderation-cache`

**Baseline:** Focused Go moderation tests and `RiskControlView.spec.ts` pass. Committed frontend package overrides and lockfile configuration disagree at baseline, so use installed worktree dependencies and do not rewrite the lockfile unless feature dependencies change.

---

## File Structure

Create focused backend units:

- `backend/internal/service/content_moderation_canonical.go`: canonical text, source framing, fixed-stride chunks, policy scope, HMAC messages.
- `backend/internal/service/content_moderation_identity.go`: deterministic policy-scope serialization and versioned HMAC cache/request identities.
- `backend/internal/service/content_moderation_provider.go`: provider-neutral results plus OpenAI/Zhipu adapters.
- `backend/internal/service/content_moderation_transport.go`: restricted HTTPS transport and host/IP validation.
- `backend/internal/service/content_moderation_batch.go`: cache lookup, bounded provider calls, pure aggregation.
- `backend/internal/repository/content_moderation_pass_cache.go`: Redis PASS/quarantine/comparison/feedback epoch.

Create one adjacent `_test.go` file for each new backend unit. Modify existing content moderation service/input/config/wiring/admin handler files and the Risk Control API/view/tests/locales. Add the Prometheus Go client for durable scrapeable rollout metrics; add no database migration. Modify deployment examples and Compose environment wiring only.

---

## Chunk 1: Canonical Text And Provider Foundations

### Task 1: Backend moderation security configuration

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

- [ ] **Step 1:** Write failing table tests for `MODERATION_CACHE_HMAC_KEY`, `MODERATION_CACHE_HMAC_KEY_VERSION`, and `MODERATION_ALLOWED_HOSTS`. Assert default version `1` and default allowlist hosts exactly `api.openai.com` and `open.bigmodel.cn`; reject version `0`; accept exactly 64 hex characters; reject empty, malformed, odd-length, 62/66-character keys; normalize lowercase DNS names; reject schemes, ports, paths, trailing dots that normalize ambiguously, empty labels, wildcard names, IP literals, localhost, and duplicates after normalization.
- [ ] **Step 2:** Run `cd backend && go test ./internal/config -run ModerationSecurityConfig -count=1`. Expected: FAIL because `Config.Moderation` does not exist.
- [ ] **Step 3:** Add `ModerationSecurityConfig` with `CacheHMACKey string`, `CacheHMACKeyVersion uint64`, and `AllowedHosts []string`; bind the exact environment variables and add a helper that decodes exactly 32 hex bytes without logging the value. Missing/invalid keys report cache unavailable while moderation remains callable.
- [ ] **Step 4:** Run `cd backend && gofmt -w internal/config/config.go internal/config/config_test.go && go test ./internal/config -run ModerationSecurityConfig -count=1`. Expected: PASS.
- [ ] **Step 5:** Commit only these files with `git commit -m "feat: add moderation security configuration"`.

### Task 2: Exact canonicalization and stable chunks

**Files:**
- Create: `backend/internal/service/content_moderation_canonical.go`
- Create: `backend/internal/service/content_moderation_canonical_test.go`
- Create: `backend/internal/service/content_moderation_identity.go`
- Create: `backend/internal/service/content_moderation_identity_test.go`
- Modify: `backend/internal/service/content_moderation_input.go`
- Modify: `backend/internal/service/content_moderation_input_test.go`

- [ ] **Step 1:** Write canonicalization golden vectors with complete expected bytes/runes: NFKC-equivalent forms; each of exactly `U+200B`, `U+200C`, `U+200D`, `U+2060`, and `U+FEFF` removed while adjacent non-listed format characters remain; Go Unicode whitespace mapped/collapsed; leading/trailing ASCII spaces trimmed per source; invalid UTF-8; Chinese, emoji, combining marks; source text containing separator-like strings; exact LF source joins; lowercased/trimmed server-derived roles; byte-exact server-derived source names; role/source changes; and ordered source preservation. Assert the exact canonical chunk text sent to the provider is also the normalized text field used by cache identity without secondary normalization.
- [ ] **Step 2:** Write context/HMAC golden vectors. Assert exact `uvarint` bytes for span count, role/source byte lengths, and source-local rune offsets. Separately assert outer HMAC bytes use big-endian `uint32` UTF-8 lengths and big-endian `uint64` key version/feedback epoch and cover provider, model, audit scope, policy scope, chunker version, context frame, and normalized text; assert fixed expected digests.
- [ ] **Step 3:** Write fixed-stride chunk golden vectors for 0/1/1800/1801/2000/3400/over-64 runes, Chinese/emoji/combining text, and text crossing message/tool sources. Add exact before/after append vectors asserting chunk text, start/end offsets, context frames, and identities; add insert/delete/reorder vectors proving affected downstream identities change.
- [ ] **Step 4:** Write extraction-completeness tests for every supported text protocol (OpenAI Chat/Responses/Messages/Embeddings/Images, Anthropic, Gemini, batch images), nested tool/function inputs and outputs, unknown roles, unsupported required values, per-source/nested depth/string/rune/object-key overflow, invalid UTF-8 propagation, ordered sources, stable reasons, and unsafe suffixes never reported complete.
- [ ] **Step 5:** Run `cd backend && go test ./internal/service -run 'Canonical|ModerationChunk|ExtractionCompleteness' -count=1`. Expected: FAIL on missing APIs.
- [ ] **Step 6:** Implement `CanonicalizeModerationExtraction` and `PlanModerationChunks` in `content_moderation_canonical.go`; use fixed rune offsets only. Apply the exact per-source normalization from Step 1, join canonical sources with one LF, and expose that same canonical chunk text to both provider calls and cache identity. Encode context-frame fields with `binary.PutUvarint`. Implement policy-scope serialization and the outer big-endian length/version HMAC message in `content_moderation_identity.go`.
- [ ] **Step 7:** Implement legacy policy scope over the exact spec fields: provider, validated base host/path, model, audit scope, thresholds, normalized rules, engine mode, model/group filters, failure policy, adapter/extractor/chunker versions, and feedback epoch. Exclude credentials/TTL/workers/retries. Use sorted UTF-8 JSON keys, normalized decimals, no insignificant whitespace, and deterministic normalized rule/filter arrays.
- [ ] **Step 8:** Replace silent extraction trimming with explicit `Complete=false` and stable reasons. Ensure deduplication cannot rebuild over-limit text while claiming completeness.
- [ ] **Step 9:** Run `cd backend && gofmt -w internal/service/content_moderation_canonical.go internal/service/content_moderation_canonical_test.go internal/service/content_moderation_identity.go internal/service/content_moderation_identity_test.go internal/service/content_moderation_input.go internal/service/content_moderation_input_test.go && go test ./internal/service -run 'Canonical|ModerationChunk|ModerationIdentity|Extraction|Input' -count=1 && go test -race ./internal/service -run 'Canonical|ModerationChunk|ModerationIdentity' -count=1`. Expected: PASS.
- [ ] **Step 10:** Commit with `git commit -m "feat: add stable moderation text chunks"`.

### Task 3: Provider adapters and restricted transport

**Files:**
- Create: `backend/internal/service/content_moderation_provider.go`
- Create: `backend/internal/service/content_moderation_provider_test.go`
- Create: `backend/internal/service/content_moderation_transport.go`
- Create: `backend/internal/service/content_moderation_transport_test.go`

- [ ] **Step 1:** Write OpenAI golden fixtures: Bearer auth, exact `/v1/moderations`, exactly one result, `flagged=true` always REJECT, `flagged=false` plus every returned category known/below threshold as the only PASS path, known threshold hit, empty/missing scores, unknown category/schema evolution, multiple results, auth/rate-limit/timeout errors.
- [ ] **Step 2:** Write Zhipu golden fixtures: Bearer auth, exact `/paas/v4/moderations`, model `moderation`, one string input/result, PASS/REVIEW/REJECT, case-sensitive risk-type deduplication, UTF-8 byte-order sorting, empty removal, exact 32-entry/64-rune boundaries, overflow, non-string risk types, unknown levels/schema evolution, zero/multiple results, malformed/HTTP/auth/rate-limit/timeout responses.
- [ ] **Step 3:** Write transport tests with injected resolver/dial hooks: configuration-time reject HTTP/unsafe endpoints; reject redirects, missing allowlist, loopback/private/link-local/multicast/unspecified/metadata IPv4 and IPv6, any mixed safe/unsafe DNS answer, and DNS rebind; prove no credentials cross redirects/host changes, validated IP is dialed with original TLS `ServerName`, and production/key-test constructors use the same restricted factory.
- [ ] **Step 4:** Run `cd backend && go test ./internal/service -run 'ModerationProvider|ModerationTransport' -count=1`. Expected: FAIL.
- [ ] **Step 5:** Implement `ModerationLevel`, `ProviderModerationResult`, `ModerationProvider`, adapter versions `openai-v1`/`zhipu-v1`, typed errors, and one restricted client factory shared by production and key testing.
- [ ] **Step 6:** Run `cd backend && gofmt -w internal/service/content_moderation_provider.go internal/service/content_moderation_provider_test.go internal/service/content_moderation_transport.go internal/service/content_moderation_transport_test.go && go test ./internal/service -run 'ModerationProvider|ModerationTransport|ContentModeration' -count=1`. Expected: PASS.
- [ ] **Step 7:** Commit with `git commit -m "feat: add moderation provider adapters"`.

---

## Chunk 2: PASS Cache And Service Integration

### Task 4: Redis PASS, quarantine, comparison cache, and settings-backed feedback epoch

**Files:**
- Create: `backend/internal/repository/content_moderation_pass_cache.go`
- Create: `backend/internal/repository/content_moderation_pass_cache_test.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/repository/setting_repo.go`
- Modify: `backend/internal/repository/setting_repo_integration_test.go`

- [ ] **Step 1:** Define `ContentModerationPassCache` in the service package with cache-enabled short-circuiting, all-or-nothing `LookupPASS`, best-effort `StorePASS`/`DeletePASS`, all-or-nothing/error-returning quarantine lookup plus store/delete, and bounded comparison metadata get/store/delete. Define a separate narrow `ModerationFeedbackEpochRepository` interface beside the moderation service with atomic non-negative `GetModerationFeedbackEpoch`/`IncrementModerationFeedbackEpoch`; do not widen the shared `SettingRepository` interface or its test stubs.
- [ ] **Step 2:** Write failing miniredis tests for exact PASS value schema containing only schema version and expiration metadata; default 24-hour TTL and server min/max clamping; strict pipeline reply count; command/decode/connection-interruption failures discarding every hit; best-effort partial writes; disabled cache issuing zero Redis commands; key-version rotation misses; quarantine present/missing, TTL, decode/read/connection failure returning an error rather than “not quarantined”; 30-day bounded comparison metadata; and absence of prompt/verdict/email/user identity in keys and values.
- [ ] **Step 3:** Write failing settings repository integration tests proving a missing epoch reads as zero, concurrent increments are atomic without lost updates, invalid/negative stored values fail closed, and a subsequent repository instance observes the increment before computing policy scope.
- [ ] **Step 4:** Run `cd backend && go test ./internal/repository -run ContentModerationPassCache -count=1 && go test -tags=integration ./internal/repository -run ModerationFeedbackEpoch -count=1`. Expected: FAIL because the interfaces and implementations do not exist.
- [ ] **Step 5:** Implement Redis keys `moderation:pass:v1:*`, `moderation:quarantine:v1:*`, and comparison metadata keyed by decision/request correlation. A PASS lookup returns no hits plus an error on any pipeline count, command, decode, or connection failure. A quarantine lookup has the same all-or-nothing error contract. Disabled caching returns before any Redis operation; PASS writes remain best effort after a valid complete PASS.
- [ ] **Step 6:** Add a focused `NewModerationFeedbackEpochRepository` provider backed by the existing settings table and implement atomic feedback-epoch read/increment through its SQL transaction/locking pattern, not Redis. Ensure every caller reloads the authoritative value before policy-scope/cache reuse and never accepts malformed or negative data; leave `SettingRepository` unchanged.
- [ ] **Step 7:** Add the Redis cache provider to `repository.ProviderSet`; leave the existing flagged-hash cache unchanged.
- [ ] **Step 8:** Run `cd backend && gofmt -w internal/repository/content_moderation_pass_cache.go internal/repository/content_moderation_pass_cache_test.go internal/service/content_moderation.go internal/repository/setting_repo.go internal/repository/setting_repo_integration_test.go && go test ./internal/repository -run ContentModerationPassCache -count=1 && go test -race ./internal/repository -run ContentModerationPassCache -count=1 && go test -tags=integration ./internal/repository -run ModerationFeedbackEpoch -count=1`. Expected: PASS.
- [ ] **Step 9:** Commit only the enumerated Task 4 files with `git commit -m "feat: add scoped moderation pass cache"`.

### Task 5: Bounded batch moderation

**Files:**
- Create: `backend/internal/service/content_moderation_batch.go`
- Create: `backend/internal/service/content_moderation_batch_test.go`

- [ ] **Step 1:** Write failing provider-only executor tests for PASS/REVIEW/REJECT, stable chunk-ID association, whole-batch timeout, per-call timeout, concurrency at most four, existing API-key health/rotation behavior, at most one retry per chunk, and a request-level call budget that bounds total calls even when every key fails. Assert typed stable error codes for 429, authentication exhaustion, 5xx, malformed JSON, schema mismatch/unknown, timeout, ordinary client cancellation, and missing provider result.
- [ ] **Step 2:** Add cancellation golden tests: REJECT cancels outstanding calls but preserves explicit REJECT evidence; REVIEW cancels in pre-block mode and preserves REVIEW; observe-mode REVIEW finishes outstanding calls for diagnostics; ordinary client disconnect produces cancellation and no allow result; internal cancellation caused by an explicit hit cannot replace that hit with a generic error.
- [ ] **Step 3:** Write pure aggregation tests with required chunk IDs and `REJECT > REVIEW > PASS`. Reject duplicate IDs, unexpected IDs, missing IDs, provider errors, incomplete extraction, and partial evidence; none may aggregate PASS. Keep Redis/cache fakes out of batch tests because the executor does no cache I/O or policy decision.
- [ ] **Step 4:** Run `cd backend && go test ./internal/service -run 'ModerationBatch|AggregateModerationBatch' -count=1`. Expected: FAIL because the executor and aggregator do not exist.
- [ ] **Step 5:** Implement `ModerationBatchExecutor.Execute` with one 8-second batch deadline, 3-second per-call deadlines, four-worker semaphore, existing API-key selection/health rotation, at most one retry per chunk, and a request-level maximum-call budget. Its dependencies are only the provider adapter, API-key selector, clock, and concurrency limits; it performs no Redis access, cache composition, logging, or allow/block policy.
- [ ] **Step 6:** Implement pure `AggregateModerationBatch` over extraction state, exact required IDs, cached evidence supplied by the service, fresh provider evidence, and active mode/failure inputs. Return stable typed failures for duplicate/unexpected/missing/error evidence and preserve explicit REVIEW/REJECT over internal cancellation.
- [ ] **Step 7:** Run `cd backend && gofmt -w internal/service/content_moderation_batch.go internal/service/content_moderation_batch_test.go && go test ./internal/service -run 'ModerationBatch|AggregateModerationBatch' -count=1 && go test -race ./internal/service -run 'ModerationBatch|AggregateModerationBatch' -count=1`. Expected: PASS with deterministic cancellation and bounded calls.
- [ ] **Step 8:** Commit with `git commit -m "feat: add bounded moderation batches"`.

### Task 6: Integrate incremental auditing into ContentModerationService

**Files:**
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Modify/regenerate: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1:** Write failing config tests for missing provider/cache fields defaulting to OpenAI/cache-off, Zhipu safe defaults, custom base/model preservation, invalid provider, TTL default/clamping, and enabled cache with missing HMAC falling back to fresh moderation with degraded cache health.
- [ ] **Step 2:** Write failing service tests proving local rules run despite full cache coverage; first 64,745-rune context audits every chunk; an exact repeat calls provider zero times; append-only 6,433-rune tail calls only changed/new tail chunks; and historical insertion invalidates downstream chunks. Assert one all-chunk PASS lookup pipeline, only misses sent to provider, and only fresh PASS keys written after the entire request aggregates PASS.
- [ ] **Step 3:** Add cache-failure orchestration tests: any PASS pipeline command/count/decode/connection error discards all returned hits and calls the provider for every chunk; cache-disabled performs no Redis I/O; quarantine is checked before PASS lookup; quarantine hit is explicit REVIEW; quarantine read failure follows failure policy and cannot become PASS; Redis PASS-write failure after complete PASS still allows while marking cache health degraded; non-PASS/error/incomplete requests write no PASS entries.
- [ ] **Step 4:** Add decision tests for malformed JSON/invalid UTF-8 => 400; over-64 => 413; unsupported required shape => 422; other extraction incompleteness => public 503; provider/schema/batch failures => public 503; REVIEW/REJECT block; eligible trusted fail-open only for failure/incompleteness with a valid policy snapshot; trusted exceptions never bypass explicit REVIEW/REJECT; observe mode forwards and records incomplete/provider/explicit-hit diagnostics. Verify mode-specific extraction handling occurs after local deterministic checks rather than unconditionally returning early.
- [ ] **Step 5:** Run `cd backend && go test ./internal/service -run 'ContentModeration.*(Provider|Cache|Incremental|Chunk|Incomplete|Quarantine|Decision)' -count=1`. Expected: FAIL on the new provider/cache fields and orchestration.
- [ ] **Step 6:** Add JSON config/view/update fields `provider`, `pass_cache_enabled`, `pass_cache_ttl_seconds`; include provider/validated endpoint/model/scope/thresholds/rules/filters/failure policy/versions and authoritative settings-backed feedback epoch in legacy policy scope.
- [ ] **Step 7:** Implement service orchestration in this order: extract and run complete-input local rules; apply mode-specific extraction/size/shape policy; canonicalize/chunk; load authoritative feedback epoch and policy scope; check request quarantine; perform one all-chunk PASS lookup; on any read error discard all hits; send only resulting misses to the provider-only batch executor; aggregate exact cached/fresh evidence; map typed result to existing decisions/logs; and store only fresh PASS keys after the complete request aggregates PASS. Explicit REVIEW/REJECT remains terminal in pre-block regardless of trusted failure exceptions.
- [ ] **Step 8:** Inject pass cache, settings-backed epoch, config security settings, restricted provider factory, and batch executor; regenerate Wire using `cd backend && make generate`. Do not hand-edit generated output beyond generated changes.
- [ ] **Step 9:** Run `cd backend && gofmt -w internal/service/content_moderation.go internal/service/content_moderation_test.go internal/service/wire.go internal/repository/wire.go && go test ./internal/service -run 'ContentModeration|ModerationBatch|ModerationIdentity' -count=1 && go test ./internal/repository -run ContentModerationPassCache -count=1 && go test -tags=integration ./internal/repository -run ModerationFeedbackEpoch -count=1 && go test ./internal/handler/admin -run ContentModeration -count=1 && go test ./cmd/server -run '^$' -count=1`. Expected: PASS and legacy OpenAI/cache-off behavior unchanged.
- [ ] **Step 10:** Commit Task 6 files, including generated Wire changes, with `git commit -m "feat: integrate incremental moderation"`.

---

## Chunk 3: Admin Controls, Feedback, And Verification

### Task 7: Provider-aware admin configuration and key test

**Files:**
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler_test.go`
- Modify: `frontend/src/api/admin/riskControl.ts`
- Modify: `frontend/src/views/admin/RiskControlView.vue`
- Modify: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`
- Modify: `frontend/src/i18n/locales/en/admin/channels.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/channels.ts`

- [ ] **Step 1:** Write failing backend tests for provider/cache DTO passthrough, provider-aware key testing through the production restricted client factory, unknown provider 400, and runtime status containing provider, model, cache availability/degraded reason, effective TTL, chunker version, max 1,800 runes, 200 overlap, and 64-chunk limit. Assert the response never exposes API keys, HMAC keys, key versions as secret material, or request/chunk HMACs.
- [ ] **Step 2:** Write failing frontend tests: legacy config => OpenAI/cache-off; selecting Zhipu proposes `https://open.bigmodel.cn/api` and `moderation` only for untouched defaults; saved custom values remain; cache toggle/TTL serialize; key test includes provider/base/model; bounded runtime status renders; labels use i18n.
- [ ] **Step 3:** Run `cd backend && go test ./internal/handler/admin -run 'ContentModeration.*(Provider|Cache|KeyTest|Status)' -count=1` and `pnpm --dir frontend exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts`. Expected: FAIL on missing provider/cache/status fields.
- [ ] **Step 4:** Reuse service validation/adapters in handler DTOs; add the complete secret-free runtime status; add Vue `Select` and `Toggle` controls plus advanced TTL/health UI; keep chunk/concurrency constants server-owned.
- [ ] **Step 5:** Add English and Chinese translations together.
- [ ] **Step 6:** Run `cd backend && gofmt -w internal/handler/admin/content_moderation_handler.go internal/handler/admin/content_moderation_handler_test.go && go test ./internal/handler/admin -run 'ContentModeration.*(Provider|Cache|KeyTest|Status)' -count=1` and `pnpm --dir frontend exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts && pnpm --dir frontend run typecheck`. Expected: PASS.
- [ ] **Step 7:** Commit with `git commit -m "feat: configure zhipu moderation"`.

### Task 8: Correlate upstream cyber policy and quarantine misses

**Files:**
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_cyber_test.go`
- Modify: `backend/internal/repository/content_moderation_pass_cache.go`
- Modify: `backend/internal/repository/content_moderation_pass_cache_test.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler_test.go`

- [ ] **Step 1:** Write failing eligibility tests proving the denominator includes every request that used Zhipu, had complete extraction and PASS evidence for every required chunk, and was actually forwarded to OpenAI. Correlate the numerator only when the upstream event has the same request and decision IDs within ten minutes. Explicitly exclude and count missing IDs, late events, non-Zhipu decisions, incomplete/partial PASS evidence, requests not forwarded, non-OpenAI upstreams, and `cyber_policy_session_blocked` repeats.
- [ ] **Step 2:** Write failing invalidation/review tests for immediate best-effort referenced PASS-key deletion, 24-hour quarantine write, queued request-level human review before existing notification/count side effects, false-positive quarantine removal, confirmed-violation 30-day extension, and high-severity atomic epoch increment. Assert quarantine write/delete and required epoch failures keep the event unresolved and health degraded; a failed false-positive delete does not resolve the review.
- [ ] **Step 3:** Write privacy/label tests proving comparison metadata has no prompt, email, user ID, API key, URL, or raw body; `confirmed_violation` and `false_positive` remain request-level labels and are never copied onto chunks or converted automatically into permanent rules.
- [ ] **Step 4:** Run `cd backend && go test ./internal/service -run 'ContentModeration.*(Cyber|Correlation|Quarantine|Feedback)' -count=1 && go test ./internal/repository -run ContentModerationPassCache -count=1 && go test ./internal/handler/admin -run 'ContentModeration.*Review' -count=1`. Expected: FAIL on missing comparison/correlation state.
- [ ] **Step 5:** Store bounded comparison metadata: request/decision IDs, scoped request HMAC, at most 64 chunk keys, provider/model/policy scope, aggregate level and bounded risk types, total/cached/fresh chunk counts, complete-PASS evidence, forwarded upstream kind, forwarding timestamp, and correlation deadline. Never store prompt/email/user/key/URL/raw body.
- [ ] **Step 6:** On an eligible `RecordCyberPolicyEvent`, perform best-effort referenced PASS-key deletion, write a 24-hour scoped request quarantine, then queue a request-level unresolved human-review event before existing count/notification side effects. Never create comparison samples for `cyber_policy_session_blocked`; emit bounded denominator/numerator/exclusion counters.
- [ ] **Step 7:** On `false_positive`, delete quarantine and resolve only after success. On `confirmed_violation`, extend quarantine to 30 days and atomically increment the settings-backed epoch for high severity, resolving only after required operations succeed. Any required failure leaves review unresolved and cache health degraded; labels remain request-scoped and never publish rules.
- [ ] **Step 8:** Run `cd backend && gofmt -w internal/service/content_moderation.go internal/service/content_moderation_cyber_test.go internal/repository/content_moderation_pass_cache.go internal/repository/content_moderation_pass_cache_test.go internal/handler/admin/content_moderation_handler_test.go && go test ./internal/service -run 'ContentModeration.*(Cyber|Correlation|Quarantine|Feedback)' -count=1 && go test -race ./internal/service -run 'ContentModeration.*(Correlation|Quarantine|Feedback)' -count=1 && go test ./internal/repository -run ContentModerationPassCache -count=1 && go test -race ./internal/repository -run ContentModerationPassCache -count=1 && go test -tags=integration ./internal/repository -run ModerationFeedbackEpoch -count=1 && go test ./internal/handler/admin -run 'ContentModeration.*Review' -count=1`. Expected: PASS without lost epoch updates or sensitive metadata.
- [ ] **Step 9:** Commit with `git commit -m "feat: quarantine moderation false negatives"`.

### Task 9: Bounded moderation observability and rollout gates

**Files:**
- Create: `backend/internal/service/content_moderation_metrics.go`
- Create: `backend/internal/service/content_moderation_metrics_test.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/content_moderation_test.go`
- Create: `backend/internal/handler/admin/content_moderation_metrics_handler.go`
- Create: `backend/internal/handler/admin/content_moderation_metrics_handler_test.go`
- Modify: `backend/internal/handler/admin/wire.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/routes/admin_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`
- Create: `docs/risk-control/incremental-moderation-rollout.md`

- [ ] **Step 1:** Write failing Prometheus collector tests for extracted source/rune counts, chunk count/overflow, cache hit/miss/read/write error, fresh calls/retries/provider-call latency/error class, aggregate PASS/REVIEW/REJECT/error, batch cancellation/deadline, eligible denominator, correlated numerator, exclusion reason, pending-review age, and confirmed high-severity correlated misses. Add request-level moderation latency histograms with bounded `cache_state=cold|incremental|all_hit` and `mode=pre_block|observe` labels so cold and incremental P95 are independently queryable. Assert every label is drawn from a fixed enum, with no model, user, request, URL, risk text, chunk HMAC, or other unbounded/sensitive labels.
- [ ] **Step 2:** Write failing forced-fresh sampler tests for a deterministic bounded sample rate restricted to observe/shadow mode: count sampled cached-PASS requests, perform a diagnostic fresh recheck without changing the observe-mode live decision, record cached/fresh equivalence or mismatch, and count provider failures separately from the equivalence denominator. Assert the sampler is disabled in pre-block mode, where any fresh REVIEW/REJECT would instead be terminal explicit evidence. Add tests for forwarded audit-state counters covering complete PASS, incomplete extraction, missing/error evidence, REVIEW, and REJECT so observe-mode forwarding cannot hide unsafe evidence.
- [ ] **Step 3:** Write failing runtime-status and exposition tests for provider/model, cache availability/degraded state, TTL, chunker version, effective chunk limits, exact-once lifecycle updates, Prometheus registration without duplicate collectors, and the restricted admin metrics route returning scrapeable text without keys/HMACs/identity. Test route registration and authorization using the existing admin route harness.
- [ ] **Step 4:** Run `cd backend && go test ./internal/service -run 'ContentModeration(Metrics|Status|Rollout|ForcedFresh)' -count=1 && go test ./internal/handler/admin -run ContentModerationMetrics -count=1 && go test ./internal/server/routes -run ContentModerationMetrics -count=1`. Expected: FAIL because the collector, sampler, exporter, and route do not exist.
- [ ] **Step 5:** Add the reviewed exact dependency with `cd backend && go get github.com/prometheus/client_golang@v1.23.2`; commit the resulting `go.mod` and `go.sum` changes without unrelated upgrades. Implement a registry-injected bounded-cardinality collector and wire it at extraction, chunk planning, cache, provider, aggregation, forwarding, cancellation, correlation, and review transitions. Implement diagnostic forced-fresh sampling only in observe/shadow mode with a fixed server-owned rate and strict call budget; its result updates comparison metrics and never changes the observe-mode live decision. Pre-block mode performs no diagnostic sampling.
- [ ] **Step 6:** Register the collector once through Wire and expose it through a restricted admin OpenMetrics/Prometheus handler and route compatible with the existing admin authorization middleware. Keep the JSON runtime status secret-free and separate from scrape output.
- [ ] **Step 7:** Document exact emitted metric names and PromQL formulas for every promotion gate: seven days and 10,000 complete requests; at least 1,000 successful sampled forced-fresh comparisons at 100% equivalence with provider failures excluded and graphed separately; zero forwarded incomplete/missing/REVIEW/REJECT evidence; provider/batch failure below 0.5% in 24 hours; request-level P95 incremental below 1.5 seconds and cold below 6 seconds; zero unresolved correlated misses older than 24 hours; zero confirmed high-severity misses; and correlation ID completeness at least 99.9%. Include scrape/authorization configuration, rollback/provider-disable procedure, and state that promotion is blocked when any query is unavailable.
- [ ] **Step 8:** Run `cd backend && gofmt -w internal/service/content_moderation_metrics.go internal/service/content_moderation_metrics_test.go internal/service/content_moderation.go internal/service/content_moderation_test.go internal/handler/admin/content_moderation_metrics_handler.go internal/handler/admin/content_moderation_metrics_handler_test.go internal/handler/admin/wire.go internal/server/routes/admin.go internal/server/routes/admin_test.go && go test ./internal/service -run 'ContentModeration(Metrics|Status|Rollout|ForcedFresh)' -count=1 && go test -race ./internal/service -run 'ContentModeration(Metrics|Status|Rollout|ForcedFresh)' -count=1 && go test ./internal/handler/admin -run ContentModerationMetrics -count=1 && go test ./internal/server/routes -run ContentModerationMetrics -count=1`. Expected: PASS with bounded labels, exact-once counters, distinct latency classes, and scrapeable metrics.
- [ ] **Step 9:** Commit code, module metadata, and rollout documentation with `git commit -m "feat: observe incremental moderation rollout"`.

### Task 10: Deployment wiring and final verification

**Files:**
- Modify: `deploy/.env.example`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/docker-compose.standalone.yml`

- [ ] **Step 1:** Add variable names and generation guidance only: `MODERATION_CACHE_HMAC_KEY`, version `1`, and allowed hosts. Never add generated values.
- [ ] **Step 2:** Pass variables through Compose with cache still disabled until JSON config enables it.
- [ ] **Step 3:** Run backend verification: `cd backend && go test ./internal/config ./internal/service ./internal/repository ./internal/handler/admin ./internal/server ./internal/server/routes -count=1 && go test -tags=integration ./internal/repository -run ModerationFeedbackEpoch -count=1 && go test -race ./internal/service -run 'Canonical|ModerationChunk|ModerationIdentity|ModerationBatch|ContentModeration.*(Cache|Correlation|Feedback|Metrics|ForcedFresh)' -count=1 && go test -race ./internal/repository -run ContentModerationPassCache -count=1 && go test ./cmd/server -run '^$' -count=1 && go vet ./internal/config ./internal/service ./internal/repository ./internal/handler/admin ./internal/server ./internal/server/routes ./cmd/server`. Expected: PASS.
- [ ] **Step 4:** Run frontend verification using installed worktree dependencies without rewriting the lockfile: `pnpm --dir frontend exec vitest run src/views/admin/__tests__/RiskControlView.spec.ts && pnpm --dir frontend run typecheck && pnpm --dir frontend run lint:check && pnpm --dir frontend run build`. Expected: PASS; record the pre-existing frozen-lockfile mismatch separately and do not modify `frontend/pnpm-lock.yaml`.
- [ ] **Step 5:** Run `git diff --check && git status --short && { ! rg -n --glob '!docs/superpowers/**' --glob '!frontend/node_modules/**' '1914823683@qq[.]com|166,049|166049 bytes' .; } && { ! rg -n --glob '!docs/superpowers/**' --glob '!frontend/node_modules/**' --glob '!backend/internal/config/config_test.go' '(?i)(MODERATION_CACHE_HMAC_KEY\s*[=:]\s*["'"']?[0-9a-f]{64}|Bearer\s+[A-Za-z0-9._-]{24,})' .; }`. Expected: clean diff, only intended feature files, no production identity/raw-size marker/generated secret outside excluded design/plan and validation fixtures.
- [ ] **Step 6:** Commit deployment files with `git commit -m "docs: configure moderation cache security"`.
- [ ] **Step 7:** Invoke `superpowers:requesting-code-review` with the spec, plan, complete feature commit range, rollout-query document, and recorded test evidence. Resolve every blocking finding and rerun affected exact commands.
- [ ] **Step 8:** Invoke `superpowers:verification-before-completion`, rerun Steps 3-5 from a clean feature worktree, inspect complete output, and only then report completion.
