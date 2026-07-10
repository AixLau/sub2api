# OpenAI Account Test User-Agent And Usage Record Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Force every interactive OpenAI account test to use the system Codex User-Agent and write an admin-visible, model-costed usage record without charging a user or API key.

**Architecture:** Add an `account_test` source to `usage_logs`, allow actorless rows only for that source, and keep transport type in the existing `request_type`. A focused account-test recorder normalizes OpenAI usage, calls the existing unified pricing resolver at a fixed 1x multiplier, writes `actual_cost=0`, and persists synchronously before the SSE success event. A small User-Agent provider interface lets `AccountTestService` force the system UA after all account header overrides.

**Tech Stack:** Go 1.24, Gin, Ent, PostgreSQL migrations, Wire, Testify/sqlmock, Vue 3, TypeScript, vue-i18n, Vitest, pnpm 9.

**Specification:** `docs/superpowers/specs/2026-07-10-openai-account-test-usage-design.md`

---

## Implementation Preconditions

The current workspace contains unresolved merge conflicts, including `backend/internal/service/setting_gateway_runtime.go`, and therefore cannot compile or commit. Do not execute this plan in that state.

- Resolve the existing conflicts without discarding user changes, or create a commit/branch that contains the intended merged state.
- Then use `superpowers:using-git-worktrees` to create a dedicated implementation worktree.
- Confirm `git ls-files -u` is empty before Task 1.
- Re-read `AGENTS.md` in the implementation worktree.
- Do not alter or commit deployment secrets or local runtime data.

## File Map

New files:

- `backend/migrations/173_add_account_test_usage_source.sql`: nullable actor columns, source constraint/default, and account-test list index.
- `backend/internal/service/account_test_usage.go`: account-test source constants, normalized result, recorder interface/implementation, and no-debit model-cost calculation.
- `backend/internal/service/account_test_usage_test.go`: recorder pricing, persistence, fallback, and actor-isolation tests.
- `backend/internal/service/account_test_openai_headers.go`: system Codex UA provider interface and final header enforcement helper.
- `backend/internal/service/account_test_openai_headers_test.go`: final-header precedence tests.

Modified backend files:

- `backend/ent/schema/usage_log.go`: nullable user/API-key fields and source field.
- `backend/ent/*`: generated Ent artifacts from `go generate ./ent`.
- `backend/internal/service/usage_log.go`: usage source domain values and validation helpers.
- `backend/internal/repository/usage_log_repo_insert.go`: nullable actor insert args, source column, and validation.
- `backend/internal/repository/usage_log_repo_query.go`: source scanning, nullable actor scanning/hydration, and admin visibility predicate.
- `backend/internal/repository/usage_log_repo_unit_test.go`: insert validation and argument-order coverage.
- `backend/internal/repository/usage_log_repo_request_type_test.go`: admin zero-cost visibility query expectations.
- `backend/internal/repository/usage_log_repo_integration_test.go`: PostgreSQL actorless row create/list coverage.
- `backend/internal/service/account_test_service.go`: normalized OpenAI results, Usage extraction, synchronous record-before-success flow, and interactive/background distinction.
- `backend/internal/service/account_test_service_openai_test.go`: Responses/Chat Completions UA and usage tests.
- `backend/internal/service/account_test_service_openai_compact_test.go`: Compact UA and zero-cost record tests.
- `backend/internal/service/account_test_service_openai_image_test.go`: image UA and usage tests.
- `backend/internal/service/wire.go`: account-test recorder dependencies.
- `backend/cmd/server/wire_gen.go`: regenerated Wire output.
- `backend/internal/handler/dto/types.go`: nullable actor IDs and source in usage DTOs.
- `backend/internal/handler/dto/mappers.go`: source and nullable actor mapping.
- `backend/internal/handler/dto/mappers_usage_test.go`: admin account-test mapping coverage.

Modified frontend files:

- `frontend/src/types/index.ts`: `UsageSource` and nullable actor IDs.
- `frontend/src/components/admin/usage/UsageTable.vue`: account-test badge, base model cost display, and actor placeholders.
- `frontend/src/components/admin/usage/__tests__/UsageTable.spec.ts`: source badge, cost, and actor rendering tests.
- `frontend/src/i18n/locales/en/dashboard.ts`: English account-test labels.
- `frontend/src/i18n/locales/zh/dashboard.ts`: Chinese account-test labels.

## Chunk 1: Usage Persistence Contract

### Task 1: Add The Forward-Only Usage Source Migration

**Files:**

- Create: `backend/migrations/173_add_account_test_usage_source.sql`
- Modify: `backend/ent/schema/usage_log.go`
- Test: `backend/internal/repository/usage_log_repo_integration_test.go`
- Generate: `backend/ent/*`

- [ ] **Step 1: Write a failing PostgreSQL integration test for actorless account-test rows**

Add `TestMigration173AllowsActorlessAccountTestUsage` beside the existing migration-backed integration tests. The test must execute the migration set, insert a row with direct parameterized SQL using `NULL` for `user_id` and `api_key_id` plus `source='account_test'`, then query the row and assert both actor columns remain SQL `NULL` and the source is preserved.

Use a real account fixture because `account_id` remains required. Do not create user/API-key fixtures for this row.

Add a second assertion that an unsupported source is rejected by the check constraint, and verify `idx_usage_logs_admin_visible_id` exists through the migration schema helpers.

- [ ] **Step 2: Run the integration test and verify RED**

Run:

```bash
cd backend && go test -tags=integration ./internal/repository -run TestMigration173AllowsActorlessAccountTestUsage -count=1
```

Expected: FAIL because `usage_logs.source` does not exist and actor columns are still `NOT NULL`.

- [ ] **Step 3: Add migration 173**

Create this idempotent migration:

```sql
ALTER TABLE usage_logs
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN api_key_id DROP NOT NULL;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS source VARCHAR(32) NOT NULL DEFAULT 'gateway';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'usage_logs_source_check'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_source_check
            CHECK (source IN ('gateway', 'account_test'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_usage_logs_admin_visible_id
    ON usage_logs (id DESC)
    WHERE actual_cost > 0 OR source = 'account_test';
```

Do not edit any earlier migration.

- [ ] **Step 4: Update the Ent schema and regenerate**

Change `user_id` and `api_key_id` to `Optional().Nillable()`. Add:

```go
field.String("source").
    MaxLen(32).
    Default("gateway"),
```

Remove `.Required()` from the `user` and `api_key` edges so the Ent relationship is coherent with nullable fields. Retain both foreign keys for non-null values. Then run:

```bash
cd backend && go generate ./ent
```

Expected: generated Ent code reflects `*int64` actor fields and the new source field.

- [ ] **Step 5: Run schema and integration checks**

Run:

```bash
cd backend && go test ./ent/schema ./migrations -count=1
cd backend && go test -tags=integration ./internal/repository -run TestMigration173AllowsActorlessAccountTestUsage -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the schema slice**

```bash
git add backend/migrations/173_add_account_test_usage_source.sql backend/ent/schema/usage_log.go backend/ent backend/internal/repository/usage_log_repo_integration_test.go
git commit -m "feat(usage): add account test usage source"
```

### Task 2: Make Repository Inserts And Scans Source-Aware

**Files:**

- Modify: `backend/internal/service/usage_log.go`
- Modify: `backend/internal/repository/usage_log_repo_insert.go`
- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Test: `backend/internal/repository/usage_log_repo_unit_test.go`
- Test: `backend/internal/repository/usage_log_repo_request_type_test.go`

- [ ] **Step 1: Write failing domain and insert tests**

Add table tests proving:

```go
func TestUsageLogValidateActors(t *testing.T) {
    require.NoError(t, (&UsageLog{Source: UsageSourceGateway, UserID: 1, APIKeyID: 2, AccountID: 3}).ValidateActors())
    require.Error(t, (&UsageLog{Source: UsageSourceGateway, UserID: 0, APIKeyID: 2, AccountID: 3}).ValidateActors())
    require.Error(t, (&UsageLog{Source: UsageSourceGateway, UserID: 1, APIKeyID: 0, AccountID: 3}).ValidateActors())
    require.NoError(t, (&UsageLog{Source: UsageSourceAccountTest, AccountID: 7}).ValidateActors())
    require.Error(t, (&UsageLog{Source: UsageSourceAccountTest, UserID: 1, AccountID: 7}).ValidateActors())
    require.Error(t, (&UsageLog{Source: UsageSourceAccountTest, APIKeyID: 2, AccountID: 7}).ValidateActors())
    require.Error(t, (&UsageLog{Source: UsageSourceAccountTest}).ValidateActors())
}
```

Add repository tests that assert `prepareUsageLogInsert` produces SQL-null actor args for `account_test`, preserves integer actor args for `gateway`, and includes `source` in the exact insert argument order. Through public repository methods, assert both `Create` and `CreateBestEffort` reject invalid actor combinations before queueing or opening a transaction. Add a test proving a valid `account_test` call to `Create` selects `createSingle` before the non-empty-request-ID batching branch.

- [ ] **Step 2: Run the focused tests and verify RED**

```bash
cd backend && go test ./internal/service -run TestUsageLogValidateActors -count=1
cd backend && go test -tags=unit ./internal/repository -run 'TestPrepareUsageLogInsertAccountTestActors|TestUsageLogRepositoryRejectsInvalidActors|TestUsageLogRepositoryAccountTestUsesCreateSingle' -count=1
```

Expected: FAIL because usage source and validation do not exist.

- [ ] **Step 3: Add the domain contract**

In `usage_log.go`, add:

```go
type UsageSource string

const (
    UsageSourceGateway     UsageSource = "gateway"
    UsageSourceAccountTest UsageSource = "account_test"
)

func (s UsageSource) Normalize() UsageSource {
    if s == UsageSourceAccountTest {
        return s
    }
    return UsageSourceGateway
}
```

Add `Source UsageSource` to `UsageLog`. Add `ValidateActors()` with these rules:

- `account_id` must be positive for every row.
- `gateway` requires positive `user_id` and `api_key_id`.
- `account_test` requires both actor IDs to be zero.

Do not allow arbitrary actorless gateway records.

- [ ] **Step 4: Update every raw insert shape together**

In `usage_log_repo_insert.go`:

- append `text // source` to `usageLogInsertArgTypes` before `created_at`;
- add `source` to every CTE/INSERT column list;
- add `log.Source.Normalize()` to `prepareUsageLogInsert().args` at the matching position;
- pass `nil` for user/API-key args only when source is `account_test`;
- validate actor rules at the entry of both `Create` and `CreateBestEffort`, before transaction, empty-request-ID, batching, or fallback branches;
- in `Create`, branch on normalized `account_test` source and call `createSingle` before the request-ID batching branch;
- update hard-coded argument capacities/counts and placeholders;
- retain the existing `(request_id, api_key_id)` behavior for gateway rows.

For account-test rows, use the generated per-test UUID request ID and the explicit synchronous `createSingle` branch. Do not add a fake API-key ID.

- [ ] **Step 5: Update select/scan/hydration**

In `usage_log_repo_query.go`:

- add `source` to `usageLogSelectColumns` in the matching scan position;
- scan actor IDs into `sql.NullInt64` and map invalid values to zero in the service object;
- scan source and normalize it;
- omit zero actor IDs in `collectUsageLogIDs` so Ent never queries ID 0;
- leave account hydration required.

- [ ] **Step 6: Add repository integration coverage for nullable scan and hydration**

Add `TestUsageLogRepositoryAccountTestCreateListAndHydrate`. Create a valid account-test row through `UsageLogRepository.Create`, list it through `ListWithFilters` using the account ID, and assert:

- source is `account_test`;
- service actor IDs are zero and hydrated `User`/`APIKey` are nil;
- the required account association is hydrated;
- no Ent query includes actor ID 0;
- the row remains visible with `actual_cost=0`.

- [ ] **Step 7: Run repository tests and fix only contract regressions**

```bash
cd backend && go test ./internal/service -count=1
cd backend && go test -tags=unit ./internal/repository -count=1
cd backend && go test -tags=integration ./internal/repository -run TestUsageLogRepositoryAccountTestCreateListAndHydrate -count=1
```

Expected: PASS with no insert column/argument mismatch.

- [ ] **Step 8: Commit the repository contract**

```bash
git add backend/internal/service/usage_log.go backend/internal/repository/usage_log_repo_insert.go backend/internal/repository/usage_log_repo_query.go backend/internal/repository/usage_log_repo_unit_test.go backend/internal/repository/usage_log_repo_request_type_test.go backend/internal/repository/usage_log_repo_integration_test.go
git commit -m "feat(usage): support actorless account test records"
```

### Task 3: Expose Account-Test Records In The Admin API

**Files:**

- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Test: `backend/internal/repository/usage_log_repo_request_type_test.go`
- Test: `backend/internal/handler/dto/mappers_usage_test.go`

- [ ] **Step 1: Write failing visibility and mapper tests**

Add a query test expecting the base admin predicate:

```sql
(actual_cost > 0 OR source = 'account_test')
```

It must also assert an unrelated `gateway` row with `actual_cost=0` remains excluded.

Add a test with `ExcludeUserIDs` populated and require a null-safe exclusion clause such as:

```sql
(user_id IS NULL OR user_id <> ALL(...))
```

Add mapper coverage asserting an account-test DTO has:

```go
Source:   "account_test"
UserID:   nil
APIKeyID: nil
```

while a gateway DTO still returns non-null actor IDs.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd backend && go test -tags=unit ./internal/repository -run 'AccountTest|ListWithFiltersExcludesFailedPlaceholders|ExcludeUserIDs' -count=1
cd backend && go test ./internal/handler/dto -run AccountTest -count=1
```

Expected: FAIL on the old `actual_cost > 0` predicate and non-null DTO fields.

- [ ] **Step 3: Implement admin visibility without changing user visibility**

Replace only the administrator list base condition with:

```go
conditions = append(conditions, "(actual_cost > 0 OR source = 'account_test')")
```

The normal user list remains `WHERE user_id = $1`, so null-actor test rows cannot appear to regular users.

When `ExcludeUserIDs` is nonempty, build the null-safe exclusion locally inside `ListWithFilters` so actorless rows survive SQL three-value logic while excluded non-null user IDs remain excluded. Do not change the shared `appendUsageLogExcludeUserIDsWhereCondition` helper or the stats/trend consumers outside this endpoint.

- [ ] **Step 4: Make actor IDs nullable in DTOs and expose source**

Change the common usage DTO actor ID fields to `*int64`. Add source only to `AdminUsageLog`:

```go
Source string `json:"source"`
```

In the mapper, return pointers only for positive IDs and return `nil` for account-test actors. Map `Source` in `UsageLogFromServiceAdmin`; do not expand the regular-user API with a source field.

- [ ] **Step 5: Run focused API/repository tests**

```bash
cd backend && go test -tags=unit ./internal/repository -run 'ListWithFilters|ExcludeUserIDs' -count=1
cd backend && go test ./internal/handler/dto ./internal/handler/admin -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the admin visibility slice**

```bash
git add backend/internal/repository/usage_log_repo_query.go backend/internal/repository/usage_log_repo_request_type_test.go backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go backend/internal/handler/dto/mappers_usage_test.go
git commit -m "feat(admin): expose account test usage records"
```

## Chunk 2: Forced System Codex User-Agent

### Task 4: Add A Final OpenAI Test Header Boundary

**Files:**

- Create: `backend/internal/service/account_test_openai_headers.go`
- Create: `backend/internal/service/account_test_openai_headers_test.go`
- Modify: `backend/internal/service/account_test_service.go`
- Modify: `backend/internal/service/account_test_service_openai_test.go`
- Modify: `backend/internal/service/account_test_service_openai_compact_test.go`
- Modify: `backend/internal/service/account_test_service_openai_image_test.go`
- Modify: `backend/internal/service/wire.go`
- Generate: `backend/cmd/server/wire_gen.go`
- Modify: `backend/internal/handler/admin/account_handler_available_models_test.go`

- [ ] **Step 1: Write failing header precedence tests**

Define a test provider:

```go
type staticCodexUAProvider string

func (p staticCodexUAProvider) GetOpenAICodexUserAgent(context.Context) string {
    return string(p)
}
```

Name the seven transport cases `TestAccountTestService_InteractiveSystemCodexUserAgent_<Transport>`. For Responses OAuth, Responses API key, Chat Completions, Compact OAuth, Compact API key, OAuth image, and API-key image paths, assert the dispatched request has exactly `system-codex-ua` even when account credentials and `header_overrides` contain different User-Agent values.

Add focused fallback cases under `TestFinalizeOpenAIAccountTestHeaders_DefaultUserAgent`, proving both an empty provider value and a nil provider produce `DefaultOpenAICodexUserAgent`.

Add `TestAccountTestService_BackgroundDoesNotForceSystemCodexUserAgent`, proving `RunTestBackground` retains the pre-change path-specific UA behavior and does not force the interactive system UA.

- [ ] **Step 2: Run the transport, fallback, and background-isolation tests and verify RED**

```bash
cd backend && go test -tags=unit ./internal/service -run 'InteractiveSystemCodexUserAgent|FinalizeOpenAIAccountTestHeaders_DefaultUserAgent|BackgroundDoesNotForceSystemCodexUserAgent' -count=1
```

Expected: FAIL because existing paths use account/default UA or no UA.

- [ ] **Step 3: Add the provider interface and finalizer**

Create:

```go
type OpenAICodexUserAgentProvider interface {
    GetOpenAICodexUserAgent(context.Context) string
}

func finalizeOpenAIAccountTestHeaders(
    ctx context.Context,
    req *http.Request,
    account *Account,
    provider OpenAICodexUserAgentProvider,
) {
    account.ApplyHeaderOverrides(req.Header)
    ua := DefaultOpenAICodexUserAgent
    if provider != nil {
        if configured := strings.TrimSpace(provider.GetOpenAICodexUserAgent(ctx)); configured != "" {
            ua = configured
        }
    }
    req.Header.Set("User-Agent", ua)
}
```

Keep this helper OpenAI-test-specific. Do not change normal gateway forwarding.

- [ ] **Step 4: Apply the helper immediately before every OpenAI dispatch**

Add `codexUAProvider OpenAICodexUserAgentProvider` to `AccountTestService` and its constructor. Introduce an explicit internal `accountTestExecutionOptions{Interactive bool}` boundary: public `TestAccountConnection` passes `Interactive=true`, while `RunTestBackground` passes `Interactive=false`. Thread this option through OpenAI routing and call the finalizer only for interactive tests. Replace path-specific custom-UA logic and direct `ApplyHeaderOverrides` calls with the finalizer immediately before `Do`/`DoWithTLS` on the interactive path; keep existing path-specific header behavior on the background path.

Do not apply the Codex UA to Gemini, Claude, Antigravity, or Grok tests.

- [ ] **Step 5: Update Wire**

Pass `*SettingService` as the provider to `NewAccountTestService`, then run the repository's Wire generator from the command documented in `backend/Makefile`. If no narrower target exists, run:

```bash
cd backend && make generate
```

Inspect `backend/cmd/server/wire_gen.go` and confirm the existing `settingService` instance is passed to `NewAccountTestService`.

Run `rg -n 'NewAccountTestService\(' backend` and update every direct constructor caller in the same slice. Do not add a second constructor that silently omits the provider.

- [ ] **Step 6: Verify all OpenAI header paths**

```bash
cd backend && go test -tags=unit ./internal/service -run 'InteractiveSystemCodexUserAgent|FinalizeOpenAIAccountTestHeaders_DefaultUserAgent|BackgroundDoesNotForceSystemCodexUserAgent|AccountTestService.*(OpenAI|Compact|Image)' -count=1
cd backend && go test ./cmd/server ./internal/handler/admin -run '^$'
```

Expected: PASS.

- [ ] **Step 7: Commit the UA slice**

```bash
git add backend/internal/service/account_test_openai_headers.go backend/internal/service/account_test_openai_headers_test.go backend/internal/service/account_test_service.go backend/internal/service/account_test_service_openai_test.go backend/internal/service/account_test_service_openai_compact_test.go backend/internal/service/account_test_service_openai_image_test.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go backend/internal/handler/admin/account_handler_available_models_test.go
git commit -m "feat(accounts): force system Codex UA for OpenAI tests"
```

## Chunk 3: Usage Extraction, Pricing, And Mandatory Recording

### Task 5: Normalize Successful OpenAI Test Results

**Files:**

- Create: `backend/internal/service/account_test_usage.go`
- Test: `backend/internal/service/account_test_usage_test.go`
- Modify: `backend/internal/service/account_test_service.go`
- Test: `backend/internal/service/account_test_service_openai_test.go`
- Test: `backend/internal/service/account_test_service_openai_compact_test.go`
- Test: `backend/internal/service/account_test_service_openai_image_test.go`

- [ ] **Step 1: Write failing parser tests for Responses and Chat Completions**

Use real SSE payloads with final usage:

```json
{"type":"response.completed","response":{"id":"resp_test_1","model":"gpt-5.4","usage":{"input_tokens":11,"output_tokens":7,"input_tokens_details":{"cached_tokens":3}}}}
```

and:

```json
{"id":"chatcmpl_test_1","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":13,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":4}}}
```

Assert normalized request ID, upstream model, input/output/cache tokens, stream flag, endpoint, duration, and first-token timing.

Add non-Compact terminal Responses and Chat Completions cases with no `usage` object. Assert normalization still succeeds with zero Usage so the later recorder persists a zero-token, zero-cost row rather than treating the response as a failure.

Before implementation, add failing normalization tests for API-key image, OAuth image, Compact OAuth, and Compact API key. Image assertions cover usage tokens, image-output tokens, image count, normalized size, endpoint, and stream flag. Compact assertions cover its distinct endpoint, `Kind=compact`, `Stream=false`, and zero Usage.

- [ ] **Step 2: Verify RED**

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestConsumeOpenAIAccountTest.*(Usage|Image|Compact)' -count=1
```

Expected: FAIL because stream consumers return only `error`.

- [ ] **Step 3: Add the normalized result type**

Create a focused type:

```go
type OpenAIAccountTestResult struct {
    Kind            OpenAIAccountTestKind
    RequestID       string
    RequestedModel  string
    UpstreamModel   string
    Usage           OpenAIUsage
    ImageCount      int
    ImageSize       string
    Stream          bool
    DurationMs      int
    FirstTokenMs    *int
    UpstreamEndpoint string
    UserAgent       string
}
```

Define explicit `responses`, `chat_completions`, `image`, and `compact` kinds. The recorder uses the kind to select pricing semantics; it must not infer Compact from a zero token count.

Use `extractOpenAIUsageFromJSONBytes` rather than adding a second ad hoc token parser. Use `gjson` or typed existing response structs for request ID and model extraction.

- [ ] **Step 4: Refactor stream consumers to return results before success emission**

The consumers may continue emitting content/status events, but they must not emit `test_complete` themselves. Return the normalized result when the terminal event is reached. Preserve current errors for early EOF, malformed streams, `response.failed`, and upstream error events.

Add `stream_options: {"include_usage": true}` to Chat Completions test payloads so compatible upstreams return usage.

- [ ] **Step 5: Normalize image and Compact results**

- API-key image: extract Usage from the raw JSON body, set image count from returned data, and retain the normalized size tier.
- OAuth image: extract Usage from SSE events and image count from completed image events/tool usage.
- Compact: return zero Usage, zero image count, `Stream=false`, and endpoint `/backend-api/codex/responses/compact` or the normalized API-key compact path.

- [ ] **Step 6: Keep non-OpenAI callers behaviorally stable**

`testGrokAccountConnection` currently reuses the OpenAI Responses stream consumer. After the refactor, let it consume the result and emit its existing successful completion without invoking the account-test recorder.

Scheduled tests must not persist interactive account-test usage. Reuse the `accountTestExecutionOptions.Interactive` boundary introduced in Task 4: interactive administrator tests record usage; `RunTestBackground` neither forces the interactive UA nor records usage. Do not add a second `recordUsage` boolean that could drift from the interaction mode.

- [ ] **Step 7: Run parser and existing account-test tests**

```bash
cd backend && go test -tags=unit ./internal/service -run 'AccountTestService|ConsumeOpenAIAccountTest' -count=1
```

Expected: PASS with existing SSE content/error behavior preserved.

- [ ] **Step 8: Commit normalized result handling**

```bash
git add backend/internal/service/account_test_usage.go backend/internal/service/account_test_usage_test.go backend/internal/service/account_test_service.go backend/internal/service/account_test_service_openai_test.go backend/internal/service/account_test_service_openai_compact_test.go backend/internal/service/account_test_service_openai_image_test.go
git commit -m "refactor(accounts): normalize OpenAI test usage"
```

### Task 6: Calculate Model Cost Without A Billing Actor

**Files:**

- Modify: `backend/internal/service/account_test_usage.go`
- Test: `backend/internal/service/account_test_usage_test.go`
- Modify: `backend/internal/service/account_test_service.go`
- Modify: `backend/internal/service/wire.go`
- Generate: `backend/cmd/server/wire_gen.go`
- Modify: `backend/internal/handler/admin/account_handler_available_models_test.go`

- [ ] **Step 1: Write a failing recorder test using real pricing code**

Construct `BillingService` with test pricing and a real `ModelPricingResolver` test fixture. Pass a normalized result with input/output/cache tokens and assert the captured `UsageLog` contains:

```go
Source:         UsageSourceAccountTest
UserID:         0
APIKeyID:       0
AccountID:      account.ID
RateMultiplier: 1
ActualCost:     0
```

Also assert `InputCost`, `OutputCost`, `CacheReadCost`, and `TotalCost` match `CalculateCostUnified` at 1x.

Add table cases that map and price:

- input, output, cache-creation, cache-read, and image-output tokens;
- image count plus normalized size through unified image pricing;
- a Compact result whose input fields are all zero and whose persisted component costs, image-output cost, and total cost are all exactly zero.

- [ ] **Step 2: Verify RED**

```bash
cd backend && go test -tags=unit ./internal/service -run TestAccountTestUsageRecorder -count=1
```

Expected: FAIL because the recorder does not exist.

- [ ] **Step 3: Implement the recorder around pure pricing dependencies**

Define a narrow repository interface satisfied by the existing usage repository:

```go
type AccountTestUsageRepository interface {
    Create(context.Context, *UsageLog) (bool, error)
}

type AccountTestCostCalculator interface {
    CalculateCostUnified(CostInput) (*CostBreakdown, error)
}
```

The recorder holds only:

```go
type AccountTestUsageRecorder struct {
    usageRepo      AccountTestUsageRepository
    costCalculator AccountTestCostCalculator
    resolver       *ModelPricingResolver
}
```

Choose the billing model deterministically: use trimmed `UpstreamModel` when present, otherwise trimmed `RequestedModel`. Persist `RequestedModel` as the displayed model and persist `UpstreamModel` separately only when it differs.

For non-Compact results, calculate with:

```go
cost, err := r.costCalculator.CalculateCostUnified(CostInput{
    Ctx:            ctx,
    Model:          billingModel,
    GroupID:        nil,
    Tokens:         usageTokens,
    RequestCount:   requestCountForAccountTest(result),
    SizeTier:       result.ImageSize,
    RateMultiplier: 1,
    Resolver:       r.resolver,
})
```

`requestCountForAccountTest` returns the positive image count only for `Kind=image` and returns `1` for text transports. For `Kind=compact`, bypass pricing entirely and construct a zero `CostBreakdown`; never pass Compact through per-request/image resolution.

Map every component exposed by `OpenAIUsage` into `UsageTokens`, including cache creation/read and image-output tokens. Leave 5m/1h cache fields zero because OpenAI account-test responses do not expose them; do not extend the shared protocol type in this feature. Copy calculated input/output/cache/image-output and total costs to the log, then force `ActualCost=0`. Persist `BillingMode` from the breakdown, image count/size metadata, `Stream`, `RequestType` derived from `Stream`, duration, first-token latency, final UA, and upstream endpoint. Generate a UUID request ID only when upstream supplies neither request nor response ID.

`*BillingService` satisfies the cost-calculator interface in production. Use the real service for component/image pricing tests and a stub only for error classification tests. The recorder must not receive `UserRepository`, `APIKeyRepository`, `BillingCache`, subscription repositories, or quota services.

- [ ] **Step 4: Add missing-pricing and zero-usage tests**

Assert `errors.Is(err, ErrModelPricingUnavailable)` logs a warning and persists zero cost, matching normal gateway fallback. Assert Compact and non-Compact successful responses with missing Usage each persist one zero-token, zero-cost row. Assert repository errors and unexpected pricing errors propagate and do not masquerade as missing-pricing fallback.

- [ ] **Step 5: Wire the recorder**

Add a provider constructor and inject the recorder into `AccountTestService`. Regenerate Wire:

```bash
cd backend && make generate
```

Run `rg -n 'NewAccountTestService\(' backend` again and update every caller, including `backend/internal/handler/admin/account_handler_available_models_test.go`.

- [ ] **Step 6: Run recorder and Wire compile checks**

```bash
cd backend && go test ./internal/service -run TestAccountTestUsageRecorder -count=1
cd backend && go test ./cmd/server ./internal/service ./internal/handler/admin -run '^$'
```

Expected: PASS.

- [ ] **Step 7: Commit the recorder**

```bash
git add backend/internal/service/account_test_usage.go backend/internal/service/account_test_usage_test.go backend/internal/service/account_test_service.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go backend/internal/handler/admin/account_handler_available_models_test.go
git commit -m "feat(accounts): price account test usage without charging"
```

### Task 7: Persist Before The Interactive SSE Success Event

**Files:**

- Modify: `backend/internal/service/account_test_service.go`
- Test: `backend/internal/service/account_test_service_openai_test.go`
- Test: `backend/internal/service/account_test_service_openai_compact_test.go`
- Test: `backend/internal/service/account_test_service_openai_image_test.go`

- [ ] **Step 1: Write failing ordering and failure tests**

Use a recorder stub that records calls. Assert:

- every successful interactive OpenAI transport records exactly once;
- record input contains the forced system UA;
- the repository call occurs before `test_complete.success=true` is written;
- recorder failure emits an error mentioning that upstream connection succeeded but usage recording failed;
- recorder failure does not emit `success=true`;
- upstream failures never invoke the recorder;
- `RunTestBackground` never invokes the recorder.

- [ ] **Step 2: Verify RED**

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestAccountTestService_.*(RecordsUsage|RecordFailure|Background)' -count=1
```

Expected: FAIL because success is currently emitted inside transport processors.

- [ ] **Step 3: Add one completion boundary**

Implement a helper that:

1. fills account ID, mapped/requested model, final UA, endpoint, and metrics;
2. synchronously calls the recorder when `accountTestExecutionOptions.Interactive` is true;
3. returns a distinct error on persistence failure;
4. emits `metrics.completeEvent(true, "")` only after persistence succeeds.

All OpenAI paths must use this helper. Do not duplicate record construction per transport.

- [ ] **Step 4: Run all account test unit tests**

```bash
cd backend && go test -tags=unit ./internal/service -run 'AccountTestService' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run the service package suite**

```bash
cd backend && go test -tags=unit ./internal/service -count=1
```

Expected: PASS with no warnings introduced by recorder stubs.

- [ ] **Step 6: Commit mandatory recording**

```bash
git add backend/internal/service/account_test_service.go backend/internal/service/account_test_service_openai_test.go backend/internal/service/account_test_service_openai_compact_test.go backend/internal/service/account_test_service_openai_image_test.go
git commit -m "feat(accounts): record successful OpenAI account tests"
```

## Chunk 4: Administrator Display And End-To-End Verification

### Task 8: Render Account-Test Records Correctly

**Files:**

- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/admin/usage/UsageTable.vue`
- Test: `frontend/src/components/admin/usage/__tests__/UsageTable.spec.ts`
- Modify: `frontend/src/i18n/locales/en/dashboard.ts`
- Modify: `frontend/src/i18n/locales/zh/dashboard.ts`

- [ ] **Step 1: Write a failing component test**

Extend the DataTable stub to render `cell-user`, `cell-api_key`, `cell-model`, `cell-cost`, and `cell-user_agent`. Mount an account-test row with null actors, `total_cost=0.012345`, `actual_cost=0`, and the forced Codex UA.

Assert rendered text contains:

- `Account test` in English test messages;
- `$0.012345` as the primary displayed model cost, not `$0.000000`;
- the configured Codex UA.

Wrap the stubbed user and API-key slots in separate `data-testid="account-test-user"` and `data-testid="account-test-api-key"` elements. Assert each element's trimmed text equals exactly `-`, and assert neither contains `#`, `#null`, or a stray identifier.

Hover the cost trigger and assert the tooltip labels `total_cost` as `Model cost`; assert `actual_cost=0` is not presented as the primary billed cost. Repeat the render with Chinese test messages and assert `账号测试` and `模型成本`.

- [ ] **Step 2: Run the component test and verify RED**

```bash
pnpm --dir frontend exec vitest run src/components/admin/usage/__tests__/UsageTable.spec.ts
```

Expected: FAIL because `source` is unknown and the primary cost uses `actual_cost`.

- [ ] **Step 3: Add source and nullable actor types**

In `frontend/src/types/index.ts`:

```ts
export type UsageSource = 'gateway' | 'account_test'
```

Change usage `user_id` and `api_key_id` to `number | null`. Add `source?: UsageSource` to the base `UsageLog` because the regular-user API does not expose it, and add required `source: UsageSource` to `AdminUsageLog`.

- [ ] **Step 4: Add the account-test badge without a new table column**

Render a compact source badge inside the existing model cell. Use `usage.accountTest` for its label. Do not add a new column or change saved column preferences.

- [ ] **Step 5: Display model cost for account tests**

Add:

```ts
const primaryDisplayedCost = (row: AdminUsageLog): number =>
  row.source === 'account_test' ? (row.total_cost ?? 0) : (row.actual_cost ?? 0)
```

Use it only for the primary cost value. Keep the tooltip breakdown, and label account-test `total_cost` as model cost rather than user billed cost. Do not make `actual_cost` nonzero. The component test must hover the tooltip and verify these labels in both English and Chinese message sets.

- [ ] **Step 6: Add English and Chinese translations**

Add at least:

```ts
accountTest: 'Account test'
modelCost: 'Model cost'
```

and:

```ts
accountTest: '账号测试'
modelCost: '模型成本'
```

to the root `usage` locale objects in both dashboard locale files.

- [ ] **Step 7: Run component, type, and locale checks**

```bash
pnpm --dir frontend exec vitest run src/components/admin/usage/__tests__/UsageTable.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
```

Expected: PASS.

- [ ] **Step 8: Commit the UI slice**

```bash
git add frontend/src/types/index.ts frontend/src/components/admin/usage/UsageTable.vue frontend/src/components/admin/usage/__tests__/UsageTable.spec.ts frontend/src/i18n/locales/en/dashboard.ts frontend/src/i18n/locales/zh/dashboard.ts
git commit -m "feat(admin): show account test usage details"
```

### Task 9: Verify Migration, Backend, Frontend, And Browser Behavior

**Files:**

- Verify only; fix only regressions caused by this feature.

- [ ] **Step 1: Run formatting and generated-file checks**

```bash
cd backend
gofmt -w \
  ent/schema/usage_log.go \
  internal/service/account_test_usage.go \
  internal/service/account_test_usage_test.go \
  internal/service/account_test_openai_headers.go \
  internal/service/account_test_openai_headers_test.go \
  internal/service/account_test_service.go \
  internal/service/account_test_service_openai_test.go \
  internal/service/account_test_service_openai_compact_test.go \
  internal/service/account_test_service_openai_image_test.go \
  internal/service/usage_log.go \
  internal/repository/usage_log_repo_insert.go \
  internal/repository/usage_log_repo_query.go \
  internal/repository/usage_log_repo_unit_test.go \
  internal/repository/usage_log_repo_request_type_test.go \
  internal/repository/usage_log_repo_integration_test.go \
  internal/handler/dto/types.go \
  internal/handler/dto/mappers.go \
  internal/handler/dto/mappers_usage_test.go \
  internal/handler/admin/account_handler_available_models_test.go \
  internal/service/wire.go \
  cmd/server/wire_gen.go
go generate ./ent
make generate
cd ..
git diff --check
git diff --exit-code
```

Expected: generators complete successfully, `git diff --check` has no output, and `git diff --exit-code` confirms formatting/regeneration did not change committed files. If it reports a diff, inspect it, rerun affected tests, and commit the freshness fix before proceeding.

- [ ] **Step 2: Run focused backend suites**

```bash
cd backend && go test -tags=unit ./internal/service ./internal/repository ./internal/handler/dto ./internal/handler/admin -count=1
```

Expected: PASS.

- [ ] **Step 3: Run backend unit tests**

```bash
cd backend && go test -tags=unit ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Run migration integration coverage**

```bash
cd backend && go test -tags=integration ./internal/repository -run 'UsageLog|Migration' -count=1
```

Expected: PASS against PostgreSQL. If PostgreSQL is unavailable, report this as unverified; do not claim it passed.

- [ ] **Step 5: Run frontend verification**

```bash
pnpm --dir frontend exec vitest run src/components/admin/usage/__tests__/UsageTable.spec.ts src/views/admin/__tests__/UsageView.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
pnpm --dir frontend run build
```

Expected: PASS.

- [ ] **Step 6: Start the development server and verify in a browser**

Prerequisites:

- PostgreSQL and Redis configured for the development backend;
- an authenticated administrator account;
- one safe OpenAI OAuth test account and one safe OpenAI API-key test account;
- optional controlled OpenAI-compatible capture upstream that logs inbound request headers without exposing production credentials.

Start the backend in terminal 1:

```bash
cd backend && SERVER_HOST=127.0.0.1 SERVER_PORT=8080 go run ./cmd/server
```

Start the frontend in terminal 2:

```bash
VITE_DEV_PROXY_TARGET=http://127.0.0.1:8080 VITE_DEV_PORT=3000 pnpm --dir frontend run dev -- --host 127.0.0.1
```

Open `http://127.0.0.1:3000/admin/accounts` and `http://127.0.0.1:3000/admin/usage` using `browser:control-in-app-browser`.

For outbound-header observation, point only the safe API-key test account at the controlled capture upstream and inspect that upstream's request log. If no controlled capture endpoint or safe test accounts are available, mark those external checks unverified; do not send real credentials to an untrusted endpoint. The unit transport tests remain the required automated UA evidence.

In the browser:

1. Set a distinctive OpenAI Codex UA in admin settings.
2. Test an OpenAI OAuth account and an OpenAI API-key account.
3. Confirm captured upstream requests use the system UA despite account UA overrides.
4. Open the administrator usage list.
5. Confirm each successful interactive test creates one `账号测试` row.
6. Confirm user/API-key cells show `-`, account/model/UA/timing are visible, and primary cost shows model cost.
7. Confirm no user balance, subscription usage, or API-key quota changes.
8. At `1440x900`, capture a screenshot and verify the badge, separate actor placeholders, model cost tooltip, full UA title, and table alignment.
9. At `390x844`, capture a screenshot and verify the same data remains reachable without badge/text overlap or incoherent column collision.
10. Record any unavailable authentication/account/capture prerequisites as unverified rather than claiming success.

- [ ] **Step 7: Inspect final diff and repository state**

```bash
git status --short
git diff --stat HEAD~8..HEAD
git log --oneline -8
```

Expected: only planned files and generated Ent/Wire artifacts are present; no secrets or deployment data are staged.

- [ ] **Step 8: Run `superpowers:verification-before-completion`**

Use the verification skill, report exact commands and results, and do not claim completion for tests that were not run.
