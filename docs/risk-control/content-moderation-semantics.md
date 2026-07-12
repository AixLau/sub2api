# Content Moderation Safety Semantics

This document records the safety contract for the content moderation gateway. It is intended to prevent future changes from accidentally weakening the fail-closed and local-rule behavior.

## Failure Strategy

- `fail_strategy.default` defaults to `closed`.
- `trusted_group_ids` only applies to moderation system failures, such as initialization failure, config load failure, missing external moderation API keys when an external API is required, or moderation API errors.
- `trusted_group_ids` must not bypass explicit local rule hits, hash blocklist hits, or any other deterministic block decision.
- Public groups and newly registered users should use fail-closed behavior.
- Fail-closed decisions return HTTP 503 and action `error`, with message `内容安全模块暂时不可用，请稍后重试`.

## Engine Modes

- `engine_mode=rule_only` means local rules are authoritative; a blocking hit terminates the request and misses are allowed without an external moderation API.
- `engine_mode=api_only` means local keyword rules are skipped and an external moderation API is required for every request.
- `engine_mode=hybrid` means local rules run first as candidate detection, but every request, including a local keyword hit, must be sent to the external moderation API for the final decision.
- If `engine_mode` is explicitly configured and conflicts with legacy `keyword_blocking_mode`, `engine_mode` wins.
- If `engine_mode` is empty, it is derived from legacy `keyword_blocking_mode`.
- Config updates write both fields so older admin clients and newer API clients stay compatible.
- Legacy `keyword_blocking_mode` remains supported:
  - `keyword_only` maps to `rule_only`.
  - `api_only` maps to `api_only`.
  - `keyword_and_api` maps to `hybrid`.

## Semantic Review

- When `semantic_review.enabled=true`, cyber/jailbreak local candidates are sent through the configured internal model router before the ordinary moderation classifier.
- The default primary model is `gpt-5.3-codex-spark` with `gpt-5-mini` as fallback; account selection, OAuth headers, quota refresh, and retries remain in the existing OpenAI gateway service.
- A semantic `reject` is a terminal 403 in pre-block mode. A semantic `allow` or `review` continues to the ordinary moderation API, so `keyword_and_api` never turns a local hit into an unreviewed direct block.
- If semantic review is enabled but unavailable, pre-block requests follow `fail_strategy`; public closed strategy returns 503 rather than silently allowing the candidate.

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

## Gateway Coverage

- `docs/risk-control/content-moderation-gateway-coverage.json` is the authoritative inventory of user-callable model request routes that can reach upstream providers.
- Every upstream route with client-supplied model context must be marked `moderation_required=true`, declare the moderation `protocol`, and have `status=covered` before release.
- Routes that look model-related but intentionally do not call upstream must use `status=intentional_no_audit` and include a concrete `review_reason`.
- `/v1/embeddings` and `/embeddings` are moderated with protocol `openai_embeddings`; embeddings input is still upstream-submitted content and must not bypass pre-block checks.
- `/v1/messages/count_tokens` is moderated with protocol `anthropic_messages` because Anthropic token counting can submit client context to upstream.
- `TestGatewayModerationCoverageManifestDefinesCriticalUpstreamEntrypoints` is the CI guard for this inventory. When adding a new gateway route or alias, update the coverage manifest in the same change.

## Privacy

- `store_input_excerpt` defaults to true for backwards compatibility. When false, logs omit `input_excerpt` and keep only hashes and metadata.
- `search_input_excerpt` defaults to false. Admin log search does not query prompt excerpts unless this setting is explicitly enabled.
- Prompt excerpts must pass content moderation redaction before storage. Secret, token, URL, long identifier, email, and phone-like patterns are replaced with `[已脱敏]`.

## Images

- Image selection for external moderation API calls is deterministic. The first N renderable images are sent.
- Image URL and data URL strings participate in input hashing.
