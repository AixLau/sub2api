# OpenAI Account Test User-Agent And Usage Record Design

## Goal

Make every OpenAI account connectivity test use the system-configured OpenAI Codex User-Agent and persist a detailed, costed entry that is visible in the administrator usage-record list.

The test record calculates model cost but does not debit any user balance, subscription allowance, API key quota, or rate-limit budget.

## Scope

This design covers the administrator account-test endpoint:

`POST /api/v1/admin/accounts/:id/test`

It applies to every OpenAI test transport currently reachable through that endpoint:

- OAuth Responses streaming
- API-key Responses streaming
- API-key Chat Completions streaming
- Compact capability probes
- OAuth and API-key image tests

Non-OpenAI account tests keep their current protocol-specific User-Agent and recording behavior. Scheduled background tests are also out of scope because they do not represent the administrator action described here.

## Decisions

### System Codex User-Agent Is Authoritative

`AccountTestService` obtains the effective User-Agent from `SettingService.GetOpenAICodexUserAgent`. That service already supplies the configured value and falls back to `DefaultOpenAICodexUserAgent` when the setting is empty or unavailable.

The OpenAI test request applies headers in this order:

1. protocol-required headers
2. account-level custom headers and header overrides
3. the system OpenAI Codex User-Agent

Writing the User-Agent last makes the system setting authoritative. An account credential `user_agent` value or a `User-Agent` entry in account header overrides cannot replace it during an account test.

This rule affects account tests only. Normal gateway forwarding keeps its existing header behavior.

### Test Usage Has No User Or API Key Actor

An administrator account test is not authenticated by an API key and has no unambiguous user group, subscription, or pricing multiplier. The record must not invent those associations.

The `usage_logs.user_id` and `usage_logs.api_key_id` columns therefore become nullable. Normal gateway records continue to require both values at the service boundary. Account-test records leave both values null and keep `account_id` required.

The service model and repository scanner represent missing actor IDs explicitly rather than assigning the record to an arbitrary user or API key. Existing records and normal writes remain backward compatible.

### Usage Source Is Separate From Transport Type

Add a `source` field to usage logs with these initial values:

- `gateway`: the default for all existing and normal API traffic
- `account_test`: an administrator OpenAI account connectivity test

The source is separate from `request_type`. A test may still be synchronous or streaming, so overloading `request_type` would discard useful transport information.

The migration defaults existing rows to `gateway`. No historical backfill job is required beyond the database default.

## Architecture

### OpenAI Test Header Provider

`AccountTestService` depends on a small interface that returns the effective system OpenAI Codex User-Agent for a context. `SettingService` implements this interface.

This boundary keeps setting lookup and caching inside `SettingService`, while allowing account-test header behavior to be tested without a database.

One shared helper finalizes headers for every OpenAI account-test request. Each OpenAI test path must call it immediately before dispatching the request through `HTTPUpstream`.

### Account Test Usage Recorder

Introduce an account-test usage recorder with one responsibility: turn a normalized successful OpenAI test result into a `usage_logs` row.

Its input contains:

- account ID
- requested and upstream model names
- input, output, and cache token counts
- image count and size metadata when applicable
- stream/request type
- duration and first-token latency
- effective User-Agent
- normalized upstream endpoint
- upstream request or response ID when available

The recorder depends on the existing model-pricing resolver and usage-log repository. It does not depend on user, API-key, subscription, quota, or balance repositories.

This makes the no-debit property structural: the account-test recording unit cannot mutate user consumption state.

### Normalized Test Result

OpenAI response processors return a normalized result after they finish emitting response content. The result carries usage and timing data without exposing transport-specific response shapes to the recorder.

Responses and Chat Completions stream parsers extract final usage objects when the upstream provides them. Image paths extract image usage and dimensions where available. Compact probes produce a valid result with zero tokens and zero model cost.

## Cost Semantics

Account-test records use the same global model-pricing metadata and token/image cost calculation rules as normal gateway records, with a rate multiplier of `1`.

The persisted fields follow these rules:

- input/output/cache cost fields contain the calculated base model costs
- `total_cost` contains their calculated total
- `actual_cost` is always `0`
- `rate_multiplier` is `1`
- user, API key, group, and subscription associations are null
- no balance, subscription, API-key quota, or rate-limit update is executed

If pricing metadata is unavailable, follow the normal gateway fallback: persist the successful test with zero calculated cost and log the pricing-resolution warning. Missing pricing must not hide the test execution.

## Recording Flow

For each OpenAI account test:

1. Resolve the account and mapped test model using existing logic.
2. Build the protocol-specific request.
3. Apply account header overrides.
4. Resolve and force the system OpenAI Codex User-Agent.
5. Send the request and process the upstream response.
6. Normalize usage, model, endpoint, request ID, and timing data.
7. Calculate model cost without a billing actor.
8. Persist an `account_test` usage row synchronously.
9. Emit the final successful SSE event.

Usage persistence is mandatory for a successful account-test operation. If upstream connectivity succeeds but the usage row cannot be persisted, the SSE stream reports that the connection succeeded but recording failed, and the overall test completes as unsuccessful. This prevents the UI from claiming the complete requested operation succeeded when no record exists.

Upstream failures remain in the existing error path and do not create usage rows because they have no completed usage or model cost to record.

## Administrator Usage List

The administrator usage query currently excludes rows whose `actual_cost` is zero. Change that predicate to include rows where `source = 'account_test'` while preserving the existing rule for gateway traffic.

The administrator usage DTO exposes `source`. The table displays an `Account test` / `账号测试` source badge for these rows. User and API-key cells render `-`; account, model, tokens, model cost, duration, first-token latency, User-Agent, endpoint, and creation time render normally.

The normal user usage endpoint continues to filter by `user_id`, so actorless account-test rows are never visible to regular users.

No new list filter is required in this change. Source filtering can be added later if operational use demonstrates a need.

## Database Migration

Add one forward-only migration that:

- drops the `NOT NULL` constraint from `usage_logs.user_id`
- drops the `NOT NULL` constraint from `usage_logs.api_key_id`
- adds `source` with a non-null `gateway` default and a constrained set of allowed values
- adds an index suitable for administrator list queries that include `account_test` rows

Update the Ent schema and regenerate Ent code. Foreign-key behavior for non-null actor IDs remains unchanged.

## Error Handling

- Empty or unavailable system UA uses `DefaultOpenAICodexUserAgent` through the existing setting service fallback.
- Account custom headers cannot override the forced system UA.
- Missing upstream usage persists a zero-token successful test record.
- Missing pricing persists a zero-cost successful test record and logs a warning.
- Usage persistence failure is surfaced distinctly from an upstream connectivity failure.
- No test-record failure may trigger user or API-key debit rollback logic because no debit is attempted.

## Testing Strategy

Implementation follows test-driven development.

Backend tests cover:

- configured system Codex UA is used by every OpenAI test transport
- default Codex UA is used when the setting is empty
- account credential UA and header overrides cannot replace the system UA
- a successful test persists one `account_test` record with account, model, timing, endpoint, and UA details
- stream usage is normalized into token fields and base model cost
- image usage is normalized into image fields and base model cost
- compact success persists a visible zero-token, zero-cost record
- test recording never calls user balance, subscription, API-key quota, or rate-limit mutation methods
- persistence failure prevents a successful final test event and reports the recording failure
- failed upstream tests do not create usage rows
- administrator list queries include zero-actual-cost `account_test` rows while continuing to exclude unrelated zero-cost gateway rows
- nullable actor associations scan and hydrate without errors
- migration and repository integration behavior on PostgreSQL

Frontend tests cover:

- account-test source badge rendering in both locales
- missing user and API key render as `-`
- model cost, account, timing, and User-Agent details remain visible

## Compatibility

The account-test request and SSE payload remain compatible except for the explicit recording-failure outcome. Existing gateway usage records retain their current source through the database default, and regular user usage behavior is unchanged.

No API key selection is added to the account-test UI.
