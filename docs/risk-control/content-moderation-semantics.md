# Content Moderation Safety Semantics

This document records the safety contract for the content moderation gateway. It is intended to prevent future changes from accidentally weakening the fail-closed and local-rule behavior.

## Failure Strategy

- `fail_strategy.default` defaults to `closed`.
- `trusted_group_ids` only applies to moderation system failures, such as initialization failure, config load failure, missing external moderation API keys when an external API is required, or moderation API errors.
- `trusted_group_ids` must not bypass explicit local rule hits, hash blocklist hits, or any other deterministic block decision.
- Public groups and newly registered users should use fail-closed behavior.
- Fail-closed decisions return HTTP 503 and action `error`, with message `内容安全模块暂时不可用，请稍后重试`.

## Engine Modes

- `engine_mode=rule_only` means local rules run and external moderation API keys are not required.
- `engine_mode=api_only` means local keyword rules are skipped and an external moderation API is required.
- `engine_mode=hybrid` means local rules run first; if they do not block, the external moderation API is required.
- If `engine_mode` is explicitly configured and conflicts with legacy `keyword_blocking_mode`, `engine_mode` wins.
- If `engine_mode` is empty, it is derived from legacy `keyword_blocking_mode`.
- Config updates write both fields so older admin clients and newer API clients stay compatible.
- Legacy `keyword_blocking_mode` remains supported:
  - `keyword_only` maps to `rule_only`.
  - `api_only` maps to `api_only`.
  - `keyword_and_api` maps to `hybrid`.

## Rule Actions

- `block` blocks the request and records a 100% hit log.
- `observe` allows the request, marks it flagged, records a 100% hit log, and does not trigger automatic ban side effects.
- `warn` allows the request, marks it flagged, records a 100% hit log, and does not trigger automatic ban side effects.
- `sample_rate` must never control whether scanning runs. It only controls non-hit log sampling.

## Input Extraction

- `audit_scope` controls which client-supplied model-context text is scanned:
  - `all_context` scans all client-supplied context and is the default.
  - `user_and_tool` scans user / empty-role input plus tool and function results.
  - `user_only` scans only user / empty-role input.
- OpenAI Chat, OpenAI Responses, Anthropic Messages, and Gemini extraction scan all client-supplied model-context text, not only the latest user message.
- Client-supplied `system`, `developer`, `user`, `assistant`, `model`, `tool`, `function`, `tool_result`, `function_call_output`, and Gemini `functionResponse` content is treated as untrusted input by default.
- Unknown client-supplied message roles are scanned by default. Only strictly recognized internal scaffolds may be skipped.
- Tool/function JSON extraction scans string values and object keys, with bounded depth, string count, string length, total rune count, and object key count.
- Tool/function JSON extraction records truncation metadata such as `max_depth`, `max_strings`, `max_string_runes`, `max_total_runes`, and `max_object_keys`.
- Hit logs include non-prompt metadata for source attribution, including `engine_mode`, `keyword_blocking_mode`, `matched_source`, and truncation state when applicable. The source metadata must not store the full prompt.
- Request-local deduplication keeps the first source path for repeated normalized text and scans that text once per request body.
- Trusted internal scaffolds may be skipped only by strict match. User text mixed with `<system-reminder>` or Codex scaffold markers must still be scanned.

## Privacy

- `store_input_excerpt` defaults to true for backwards compatibility. When false, logs omit `input_excerpt` and keep only hashes and metadata.
- `search_input_excerpt` defaults to false. Admin log search does not query prompt excerpts unless this setting is explicitly enabled.
- Prompt excerpts must pass content moderation redaction before storage. Secret, token, URL, long identifier, email, and phone-like patterns are replaced with `[已脱敏]`.

## Images

- Image selection for external moderation API calls is deterministic. The first N renderable images are sent.
- Image URL and data URL strings participate in input hashing.
