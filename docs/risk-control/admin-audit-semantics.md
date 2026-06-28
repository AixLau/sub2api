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
