# Content Moderation Upstream Account Scope Design

**Date:** 2026-07-11

**Status:** Ready for user review

## Summary

Content moderation currently scopes requests by API-key group and model before the gateway chooses an upstream account. Add a second, independent upstream-account dimension with three mutually exclusive modes:

- `all`: every selected upstream account;
- `oauth`: accounts whose type is `oauth` or `setup-token`;
- `selected`: only explicitly configured upstream account IDs.

Group, account, and model filters use intersection semantics. A request is moderated only when all enabled filters match. Existing configurations have no account fields and continue to mean `account_scope = all`.

Because the upstream account is unknown at the current moderation stage, account-aware moderation must run after a concrete account has been selected but before any request bytes are sent upstream. The implementation must preserve pre-block behavior, failover correctness, concurrency-slot cleanup, and coverage across the ordinary gateway, OpenAI HTTP, OpenAI Responses WebSocket, and image paths.

## Goals

1. Let administrators choose all upstream accounts, OAuth-credential accounts, or specific upstream accounts for content moderation.
2. Treat `oauth` and `setup-token` as OAuth-credential accounts.
3. Combine upstream-account scope with existing group and model scopes using intersection semantics.
4. Base the decision on the actual account selected for an upstream attempt, including failover attempts.
5. Keep legacy configurations behaviorally unchanged.
6. Make the selected upstream account visible in moderation audit records.
7. Preserve the guarantee that pre-block moderation completes before a matching account receives request data.

## Non-Goals

- Combining OAuth mode with an additional union of manually selected non-OAuth accounts.
- Adding platform, account-status, tag, or credential-subtype filters.
- Changing account scheduling priority or candidate eligibility.
- Changing existing audit-context scope values such as `all_context` and `user_only`.
- Retrospectively assigning upstream accounts to historical moderation logs.
- Re-running moderation for identical request content solely because failover selected another matching account.

## Product Semantics

### Account Modes

| Mode | Match rule | Configuration |
|---|---|---|
| `all` | Any non-nil selected upstream account | `account_ids` is normalized to an empty list |
| `oauth` | Selected account type is exactly `oauth` or `setup-token` | `account_ids` is normalized to an empty list |
| `selected` | Selected account ID is present in `account_ids` | At least one valid account ID is required |

The modes are mutually exclusive. Switching to `all` or `oauth` clears stale selected IDs in the persisted effective configuration. The UI may retain unsaved selection state during a local tab switch, but the submitted payload must follow the normalization above.

### Filter Composition

The effective predicate is:

```text
group_matches AND account_matches AND model_matches
```

Examples:

| Group scope | Account scope | Selected attempt | Result |
|---|---|---|---|
| Selected group A | OAuth | Group A + `oauth` | Moderate |
| Selected group A | OAuth | Group A + `apikey` | Skip |
| Selected group A | Selected account 42 | Group B + account 42 | Skip |
| All groups | Selected account 42 | Any group + account 42 | Moderate |

An absent selected account is not a match for `oauth` or `selected`. Routes that intentionally have no upstream account remain governed by their existing explicit coverage status and do not silently claim account-aware protection.

### Failover

Account scope is evaluated for every concrete upstream attempt before forwarding:

1. An out-of-scope account may be forwarded without content moderation.
2. If that attempt fails and failover selects an in-scope account, moderation runs before the in-scope attempt is forwarded.
3. Once the request has a reusable moderation outcome, another matching failover account reuses that outcome and does not call or enqueue the moderation provider again.
4. A blocking decision terminates the request; no further account is attempted.

The per-request reuse key contains the normalized moderation input hash and the effective moderation policy revision returned by the moderation service. It must not reuse an outcome from another request. Existing service-level caches retain their own stricter identities.

The gate returns one of these explicit dispositions:

| Disposition | Meaning | Reusable for the same input and policy |
|---|---|---|
| `out_of_scope` | At least one group/account/model predicate did not match | No; a later account may be in scope |
| `deterministic_allow` | Empty auditable input, rule-only allow, feature/mode bypass, or no configured audit key produced the existing allow behavior | Yes |
| `observe_enqueued` | Observe-mode work was successfully enqueued | Yes; do not enqueue duplicate work on failover |
| `observe_dropped` | Observe-mode queue did not accept the task | No |
| `allowed` | Synchronous local/external moderation completed and allowed | Yes |
| `blocked` | A local, hash, classifier, or external rule blocked | Terminal; the request stops |
| `provider_error_fail_open` | Provider/classifier error followed existing fail-open policy | No; a later matching attempt gets one more normal moderation opportunity |
| `provider_error_fail_closed` | Provider/classifier error followed existing fail-closed policy | Terminal; the request stops |

“Feature/mode bypass” above preserves current behavior when moderation is disabled or `off`; early resource protection and independent cyber/image checks remain active. An implementation must not infer reusability solely from `decision.Allowed`.

## Configuration Contract

Add the following fields to the persisted configuration, admin view, and update input:

```json
{
  "account_scope": "all",
  "account_ids": []
}
```

Backend constants define `all`, `oauth`, and `selected`. Normalization applies these rules:

- missing, blank, or unknown `account_scope` becomes `all` when loading legacy stored configuration;
- duplicate and non-positive account IDs are removed;
- IDs are sorted for stable persistence and comparison;
- `all` and `oauth` persist an empty `account_ids` list;
- `selected` requires one or more IDs.

Update validation rejects an explicitly submitted unknown mode. This differs from tolerant loading: loading protects availability and backward compatibility, while admin writes reject mistakes.

For `selected`, every configured ID must resolve to an existing upstream account at save time. If an account is later deleted or becomes unavailable, it simply cannot be selected by the scheduler; the stored ID remains inert until the next configuration edit. Account status does not alter scope membership because scheduling already owns eligibility.

No database migration is needed for configuration because the setting is stored as JSON.

Selected-ID validation uses the existing `AccountRepository.GetByIDs` dependency newly injected into `ContentModerationService`. The service compares the unique requested IDs with returned accounts and rejects every missing ID. This constructor change requires updating Wire providers and every direct test constructor call. Tests provide a narrow repository stub; handlers and the frontend do not independently define account existence.

## Backend Architecture

### Account Identity

Extend the moderation check input with immutable selected-account metadata:

```go
AccountID   int64
AccountName string
AccountType string
```

The handler copies these values from the hydrated `service.Account` selected for the attempt. The moderation service does not query mutable account state on the hot path.

Add a focused configuration predicate equivalent to:

```go
func (cfg *ContentModerationConfig) includesAccount(accountID int64, accountType string) bool
```

The predicate owns normalization and matching only. Group/model matching remains in their existing predicates.

### Gate Result and Policy Revision

Add an internal account-aware gate result containing:

```go
type ContentModerationAttemptState struct {
    Disposition    string
    Decision       *ContentModerationDecision
    InputHash      string
    PolicyRevision string
    Reusable       bool
}

type ContentModerationGateResult struct {
    Disposition    string
    Decision       *ContentModerationDecision
    InputHash      string
    PolicyRevision string
    Reused         bool
    NextState      *ContentModerationAttemptState
}
```

`ContentModerationService` owns construction of this result and exposes a distinctly named method:

```go
func (s *ContentModerationService) CheckAccountAttempt(
    ctx context.Context,
    input ContentModerationCheckInput,
    prior *ContentModerationAttemptState,
) (*ContentModerationGateResult, error)
```

The handler does not derive a disposition from the public decision fields. Every production path that selects and forwards to an upstream account must migrate atomically to `CheckAccountAttempt`; there is no period where those paths rely on missing account metadata.

The existing `Check(ctx, input) (*ContentModerationDecision, error)` remains only as a deprecated, account-agnostic compatibility path for direct service tests and explicitly intentional no-account operations. It preserves the old group/model behavior by ignoring the new account predicate and always returns a non-nil allow decision for skip/bypass outcomes. It must not be called by an account-forwarding handler or batch service. An AST structural test scans production handler/service call sites and fails if an upstream-forwarding path invokes legacy `Check`.

The service computes `PolicyRevision` as SHA-256 over a versioned envelope containing both the normalized global `risk_control_enabled` value and the complete normalized persisted `ContentModerationConfig` JSON used for the check. Go's JSON encoder provides stable map-key ordering; configuration normalization provides stable ID and rule ordering. Including credentials and operational fields may conservatively invalidate reuse but cannot cause unsafe reuse. Only the hash is returned or logged, never the envelope, JSON, or credentials.

`CheckAccountAttempt` performs the complete race-free sequence: load both settings, normalize configuration, compute the current revision, evaluate group/account/model scope, extract and hash input when in scope, compare an explicitly reusable prior state, and only then call or enqueue moderation if reuse does not apply. Callers never probe policy revision separately. A later failover passes the previous `NextState` back to this method; the service suppresses provider work only when current input hash and policy revision match. Tests explicitly toggle global risk control between failover attempts to prove a disabled-policy allow cannot be reused after enablement.

Every `ContentModerationGateResult`, including `out_of_scope` and other non-terminal skips, contains a non-nil allow `Decision`. `NextState` is the complete state the caller must pass to the next failover attempt, with these service-owned transitions:

- `out_of_scope` returns the incoming reusable state unchanged because an intervening unmoderated account does not invalidate the earlier decision;
- a reused outcome returns the same reusable state;
- a fresh reusable outcome replaces it with the new state;
- a fresh in-scope non-reusable outcome, including observe drop or fail-open provider error, returns nil and clears an older state;
- a terminal outcome returns nil and stops forwarding.

Callers always assign `state = result.NextState`; they do not interpret dispositions to manage state. This preserves an in-scope allow across an out-of-scope middle attempt while ensuring a later fresh failed check cannot leave a stale reusable result active.

### Pipeline Placement

Introduce an account-aware moderation gate between concrete routing and upstream forwarding:

```text
request admission -> validation/cyber/image checks -> billing eligibility
  -> select/hydrate account -> account-aware moderation gate
  -> acquire/retain forwarding resources as required -> forward -> usage
```

Routing adapters continue returning the existing `AccountSelectionResult`. Ownership is explicit:

- when `Acquired == true`, the caller owns `ReleaseFunc` immediately after routing;
- the account-aware gate runs before forwarding while that owned slot is held;
- a blocked or error stop calls and clears `ReleaseFunc` exactly once;
- an allowed, skipped, or enqueued outcome transfers the unchanged release ownership to the existing forward/finalization path;
- when `Acquired == false`, no slot is owned during moderation; the existing wait/fast-acquire path runs after the gate and owns any later release function.

This design deliberately accepts moderation latency while a scheduler-returned acquired slot is held. Splitting selection from acquisition would change scheduling race and sticky-session behavior and is out of scope. Exact-once cleanup is enforced through a small release owner/helper already compatible with `wrapReleaseOnDone`, not scattered conditional calls. Scheduling policy itself does not change.

The coverage metadata must represent the real execution order rather than relying on the current display sort order. Structural tests must fail if a forwarding route can select an account and send request bytes without passing the account-aware moderation gate.

Migration is atomic at the production-call-site level: ordinary gateway HTTP, OpenAI HTTP, Responses WebSocket, image, and batch-image forwarding paths all supply selected-account metadata to `CheckAccountAttempt` before account-scope configuration is exposed by the admin API/UI. Tests that exercise the legacy `Check` contract remain account-agnostic; new scope and pipeline tests exercise only `CheckAccountAttempt`.

### Protocol Integration

The shared gate accepts the selected account plus the existing API key, auth subject, protocol, model, and request body. Route adapters remain responsible for their existing error formats.

- Ordinary gateway HTTP paths call the gate after `GatewayRoutingStage` returns a hydrated account and before `GatewayForwardStage`.
- OpenAI HTTP paths call the gate after the OpenAI routing stage and before their forward stage, including chat completions, messages, responses, embeddings, and image generation.
- `BatchImagePublicService.Submit` applies account scope immediately after `selectProviderAndAccount` and before pricing resolution, job creation, balance hold, pending-item creation, the `uploading` transition, heartbeat start, or synchronous `provider.Submit`. A block or fail-closed error returns before any job, event, item, idempotency record, or billing hold exists and sends nothing to the provider. This preserves the existing pre-job moderation side-effect boundary. An observe enqueue or allow continues normally.
- Responses WebSocket initial setup evaluates the selected connection account before the first upstream frame is written.
- WebSocket follow-up frames reuse the connection's selected account and run the normal content extraction/moderation gate for each new auditable frame. A failover or reconnect that changes the account reevaluates account scope before sending the pending frame.

Early deterministic request validation, resource limits, cyber policy, and image permission checks remain before routing. They are not disabled by account scope.

Batch image integration uses a narrow dependency owned by `BatchImagePublicService`:

```go
type BatchImageModerationGate interface {
    CheckAccountAttempt(
        ctx context.Context,
        input ContentModerationCheckInput,
        prior *ContentModerationAttemptState,
    ) (*ContentModerationGateResult, error)
}
```

After request validation, the service serializes the normalized `BatchImageSubmitRequest` with `encoding/json` and uses those exact bytes with protocol `batch_images`; `collectBatchImagesInput` therefore continues extracting `items[].prompt` and `items[].reference_images` without a second schema. `BatchImageOwner` is extended with optional request-time `UserEmail`, `APIKeyName`, and `GroupName` snapshots populated by the authenticated handler, while its existing user, API key, and group IDs remain authoritative. The selected account supplies account ID/name/type, the selected provider supplies `Provider`, and the normalized request supplies `Model`.

The adapter calls `CheckAccountAttempt` with nil prior state and converts a terminal gate result to the existing content-moderation HTTP status, error code, and message through a typed `ContentModerationGateError` understood by the batch handler's existing service-error response path. Non-terminal dispositions return no error. Unit tests use a fake `BatchImageModerationGate`; `ContentModerationService` directly satisfies the production interface without method overloading. The old handler-level `ModerateBatchImageSubmit` call is removed only after this service-level gate is covered, preventing duplicate moderation.

### Per-Request Decision State

HTTP handlers keep request-local moderation state rather than global mutable state. The state records:

- the explicit gate disposition;
- the effective decision and terminal error/block result;
- the normalized input hash and service-returned policy revision;
- whether the outcome is reusable under the disposition table above.

An out-of-scope attempt does not mark moderation complete. A matching failover therefore still triggers moderation. Only dispositions marked reusable in the table can suppress a later check, and only when input hash and policy revision match.

Responses WebSocket state is connection-local and guarded by the connection's single frame-processing serialization point. It stores a bounded entry only for the current pending frame: frame input hash, policy revision, disposition, and account ID/type used for scope evaluation. The entry is cleared after that frame is forwarded, blocked, or abandoned. Follow-up frames never reuse a prior frame's result, even when text is byte-identical. A reconnect creates new connection state. If failover changes the account while the same frame is pending, scope is reevaluated; an existing reusable in-scope result may be reused only under the same input hash and policy revision. No mutable moderation state is shared across connections or goroutines.

### Logging and Observability

Structured skip and decision logs add:

- `account_id`;
- `account_name` where safe and available;
- `account_type`;
- `account_scope`;
- `configured_account_ids` for configuration diagnostics;
- `in_account_scope`.

Use a distinct `content_moderation.skip_account_out_of_scope` event. Do not log credentials or account credential metadata.

Effective-protection status adds account coverage with normalized values such as `all_accounts`, `oauth_accounts`, and `selected_accounts`. Selected account scope is reported as partial coverage, analogous to selected group scope.

## Audit Persistence

Add nullable upstream-account identity to new moderation log rows:

- `account_id`;
- `account_name` as a request-time snapshot;
- `account_type` as a request-time snapshot.

Use a forward-only SQL migration. `content_moderation_logs` is a raw-SQL repository, not an Ent entity, so update the service model plus every insert, select, scan, and row-mapping path in `internal/repository/content_moderation_repo.go` and its repository tests; do not add an Ent schema or generated Ent code. Historical rows remain null. Persistence must use the account metadata carried in the check input, not a later repository lookup.

The admin log API exposes the fields, and the initial UI displays account identity in log details/table context. Account-based audit-log filtering and a dedicated `account_id` database index are explicitly out of scope for this implementation.

## Admin UI

The existing scope settings tab keeps group and model controls and adds an unframed account-scope section between them.

Use a three-option segmented control:

- All accounts
- OAuth credential accounts
- Selected accounts

OAuth helper text states that both `oauth` and `setup-token` are included. Selected mode shows a searchable, paginated multi-select backed by the existing admin account-list API. Search results display account name, platform, and type so duplicate names are distinguishable. Already selected accounts remain visible when the search result page changes.

The account picker must handle stale configured IDs. It displays an ID fallback when details cannot be loaded and allows the administrator to remove it. Saving selected mode with no IDs is blocked client-side and independently rejected by the backend.

The overview scope summary displays both dimensions, for example `All groups / OAuth accounts` or `3 groups / 5 accounts`. All user-visible text is added to both English and Chinese locale files.

## Error Handling

- Unknown account-scope values on update return `INVALID_CONTENT_MODERATION_ACCOUNT_SCOPE`.
- Empty selected-account lists return `CONTENT_MODERATION_ACCOUNT_IDS_REQUIRED`.
- Unknown selected account IDs return an error naming the invalid ID without exposing credentials.
- Account-list search failure leaves current selections intact and shows the existing administrative error notification.
- A missing selected account during forwarding follows existing routing/failover behavior; moderation does not invent a replacement account.
- Moderation provider failure follows the existing fail strategy after all three scope predicates match.
- Any terminal error or block after `AccountSelectionResult.Acquired == true` releases and clears its `ReleaseFunc` exactly once; non-terminal outcomes transfer ownership to the existing forward/finalization path.

## Compatibility and Rollout

1. Deploy backend tolerant readers and new response fields first; old configurations normalize to `all`.
2. Deploy the updated frontend, which always submits normalized account fields.
3. Existing API clients that omit the new fields preserve their current effective values on partial updates. A full legacy configuration without the fields loads as `all`.
4. No feature flag is required because default behavior is unchanged.

Rollback to an older binary is safe for the setting JSON because Go JSON decoding ignores unknown fields. New moderation-log columns are additive.

## Testing

### Service Tests

- legacy default and tolerant-load normalization;
- explicit update rejection for unknown mode;
- ID normalization and selected-mode validation;
- `all`, `oauth`, and `selected` predicate tables;
- `setup-token` matches OAuth mode while `apikey`, `upstream`, `bedrock`, and `service_account` do not;
- group/account/model intersection;
- effective-protection account coverage and unsafe reasons;
- audit-log account snapshot construction and persistence.

### Pipeline and Handler Tests

- routing chooses an account before the account gate and forwarding happens only after it;
- out-of-scope attempt skips moderation;
- matching first attempt moderates before forward;
- out-of-scope first attempt followed by matching failover moderates before the retry;
- matching first attempt followed by another matching failover reuses only a disposition explicitly marked reusable;
- matching reusable first attempt followed by an out-of-scope attempt and then another matching attempt preserves and reuses the first state;
- fail-open provider error is retried on a later matching failover, while observe enqueue is not duplicated;
- blocking and provider-error exits release acquired slots once;
- OpenAI HTTP, ordinary gateway, image, and Responses WebSocket paths carry account identity;
- WebSocket follow-up content is moderated against the connection account without cross-frame state reuse;
- route-coverage tests detect forwarding without the account-aware gate.
- an AST structural test rejects legacy `ContentModerationService.Check` calls from account-forwarding production paths;
- legacy direct `Check` tests preserve group/model semantics, ignore account scope by contract, and return a non-nil allow decision for skips.

### Batch Image Tests

- matching account moderation runs after account selection and before pricing, job creation, or `provider.Submit`;
- block and fail-closed outcomes occur before jobs, events, pending items, idempotency persistence, and balance holds;
- observe enqueue and allow outcomes proceed to provider submission;
- normalized request serialization preserves prompts and reference images consumed by `collectBatchImagesInput`;
- the typed gate error preserves the existing batch content-moderation HTTP status, code, and message.

### Frontend Tests

- legacy API response initializes `all`;
- each segmented option loads and saves the correct payload;
- OAuth copy explicitly includes both credential types;
- selected mode requires IDs;
- account search, pagination, selection retention, stale-ID fallback, and removal;
- group and account selections are preserved independently;
- overview summary and bilingual locale keys.

### Verification

Run focused backend service/handler/repository packages first, then broader backend unit tests because routing and moderation are shared traffic paths. Run focused Vitest specs plus frontend typecheck and lint checks. Verify the forward-only migration and raw-SQL repository coverage when audit columns are added; no Ent generation is required.

## Acceptance Criteria

1. An administrator can save exactly one of the three account-scope modes.
2. OAuth mode moderates `oauth` and `setup-token` accounts and no other account types.
3. Selected mode moderates only configured account IDs.
4. Group, account, and model predicates are intersected.
5. The predicate uses the actual account for each upstream attempt.
6. No matching account receives request bytes before pre-block moderation allows them.
7. Failover into scope triggers moderation; repeated matching attempts do not duplicate an unchanged decision.
8. Blocking and error paths do not leak concurrency slots.
9. Legacy configurations retain all-account behavior.
10. New audit rows identify the selected upstream account without exposing credentials.
