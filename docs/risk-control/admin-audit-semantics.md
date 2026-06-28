# Admin Audit Semantics

## Scope

Admin audit events record privileged backend writes that affect users, API keys,
payment, routing, security, or global runtime behavior.

## Required Event Shape

New admin write paths should emit events through `service.EmitAdminAudit`.
Events must include:

- `audit=true`
- `admin_audit=true`
- `component=admin.audit`
- `action`
- `target_type`
- `target_id`
- `operator_id`
- `operator_role`
- `operator_ip`
- `user_agent`
- `request_id`
- `client_request_id` when available
- `result`
- `before`
- `after`

## Indexing

`OpsSystemLogSink` indexes any log event with `audit=true`, even when the event is
INFO level and the component name is not audit-specific. This prevents admin
audit records from depending on logger naming conventions.

## Redaction

`before` and `after` maps must be redacted before they enter the system-log sink.
Sensitive field names containing `secret`, `token`, `password`, `api_key`,
`apikey`, `private_key`, or `merchant_key` are stored as `***`.

Full request bodies, full response bodies, and plaintext credentials must not be
stored in admin audit logs.

## Current Coverage

The initial P0 path covers system settings updates through `settings.update`.
Further high-risk admin handlers should be migrated to `EmitAdminAudit` in small,
tested batches.

## Coverage Matrix

The authoritative admin write-surface inventory is
`docs/risk-control/admin-audit-coverage.json`.

Each entry maps one privileged write route to:

- `action`: stable audit action name.
- `target_type`: resource type affected by the operation.
- `risk_level`: `low`, `medium`, `high`, or `critical`.
- `status`: `covered`, `planned`, or `intentional_no_audit`.
- `event_phases`: which phases must eventually be recorded.
- `sensitive_fields`: fields that must not be stored as plaintext evidence.
- `implementation`: concrete code path for `covered` entries.

`covered` means the handler emits structured audit events through
`service.EmitAdminAudit` or an equivalent dedicated admin-audit path.
`planned` means the route is known and must be migrated in a later audited
batch. `intentional_no_audit` is reserved for routes that look write-like but do
not mutate privileged state; it requires a reason in the code review.

## P0 Write Domains

The P0 matrix must include these domains before a release is considered
audit-aware:

- system settings and runtime security settings
- user creation, deletion, role/status updates, group movement, quotas, and
  balance changes
- admin-managed API key group or rate-limit changes
- groups, group model/rate policy, and group ordering
- upstream accounts, credentials, quota resets, privacy, and route/channel
  configuration
- content moderation and risk-control configuration
- payment configuration, providers, plans, orders, refunds, and fulfillment
  retries
- subscription assignment, extension, revocation, quota reset, and bonus quota
- proxies, redeem codes, promo codes, and other value-bearing admin resources

## Event Phases

High-risk handlers should eventually record:

- `attempt`: an authenticated operator attempted the write.
- `success`: the write committed.
- `failure`: validation, permission, repository, or downstream failure prevented
  the write.

The current `settings.update` implementation records `success`; future batches
should add attempt/failure coverage for critical handlers first.

## Review Gate

`TestAdminAuditCoverageManifestDefinesP0AdminWriteSurface` validates that the
coverage matrix exists, is well-formed, includes the P0 actions, and marks
`settings.update` as covered. When a new admin write route is added, update the
matrix in the same change. When a planned route is migrated to structured audit,
change its status to `covered` and fill `implementation`.
