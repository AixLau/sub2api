# Content Moderation Safety Semantics

This document records the safety contract for the content moderation gateway. It is intended to prevent future changes from accidentally weakening deterministic rule blocking or changing the availability behavior of the moderation pipeline.

## Failure Strategy

- `fail_strategy` remains accepted for compatibility with existing configuration and status responses.
- Ordinary moderation system failures remain fail-open so a broken general-purpose audit dependency does not interrupt user traffic. This includes initialization, config or risk-switch reads, missing required audit keys, and ordinary moderation API errors.
- A fail-open system failure returns `Allowed=true` with action `error`, records logs/metrics, and continues forwarding. It is not treated as a normal moderation pass.
- Prompt-injection candidates use a separate opt-in contract. When `mode=pre_block`, `semantic_review.prompt_injection_reviewer_enabled=true`, and `semantic_review.prompt_injection_fail_closed=true`, reviewer `review`, unavailable/invalid output, or incomplete evidence returns HTTP 503 and never reaches upstream. These outcomes use `semantic_review_deferred`, `semantic_review_unavailable`, or `semantic_review_incomplete`; they do not increment violation count, auto-ban, or send a violation email.
- Fail-open behavior must never bypass a successful deterministic decision. In `rule_only`/`keyword_only`, a local blocking rule or hash hit still blocks immediately.
- A successful ordinary moderation or semantic review rejection still blocks according to the active pre-block mode.

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

- When `semantic_review.enabled=true`, local high-risk candidates are sent through the configured internal model router. `trigger=all` also evaluates in-scope pre-block text without requiring a configured keyword. For a hybrid keyword hit, the ordinary moderation API is always called first; the semantic result is applied only after that API allows the request.
- Prompt Filter regexes are evaluated per extracted input source, never across a flattened request. In `hybrid`, every Prompt Filter match is a review candidate rather than a regex-only terminal block. Only an explicit `rule_only` configuration may locally block a strict operational match from a direct user source; system, developer, tool, and known Codex wrapper context are non-terminal.
- The default primary model is `gpt-5.3-codex-spark` with `gpt-5-mini` as fallback; account selection, OAuth headers, quota refresh, and retries remain in the existing OpenAI gateway service.
- For a hybrid keyword hit, a semantic `reject` is a terminal 403 in pre-block mode only after the required ordinary API call. A semantic `allow` or `review` does not bypass the ordinary API, so `keyword_and_api` never turns a local hit into an unreviewed direct block.
- If semantic review is enabled but unavailable, hybrid candidates continue to the ordinary moderation API. If that API is also unavailable, the request is fail-opened with action `error`.
- Semantic review evaluates `intent`, `target`, `authorization`, `operationality`, and `executability`. An explicit harmful, actionable, directly executable, unauthorized tuple is deterministically upgraded to `reject`; unclear authorization remains `review` for manual handling.
- Prompt injection is reviewed by a dedicated strict schema. Its required fields are `verdict`, `active_override`, `presentation`, `targets`, `confidence`, and `reason_codes`. A direct active hierarchy override with confidence at least 0.80 is upgraded to `reject`; `allow` is valid only with complete evidence.
- The prompt-injection baseline runs before group/model/account scope whenever global risk control is enabled. A no-hit request does not call a semantic model or write a full audit record.
- The dedicated reviewer receives the complete current source up to 12K runes. Larger sources are represented by one valid JSON evidence envelope containing head, all coverable hit windows, and tail. If every high-risk occurrence cannot fit, evidence is marked incomplete and cannot produce a final allow.

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
- Successful forwarding additionally requires a request-local moderation receipt with `LocalScanDone=true` and `ForwardAllowed=true`. An entrypoint marker alone is not sufficient.
- WebSocket handshake only records entrypoint entry. The initial request frame and each later `response.create` frame require independent receipts; a prior frame's receipt cannot authorize a later frame.
- Selected-account checks first issue `deferred_selected_account`, then replace it with a completed receipt after `CheckAccountAttempt`; the deferred state cannot reach a forward adapter.

## Privacy

- `store_input_excerpt` defaults to true for backwards compatibility. When false, logs omit `input_excerpt` and keep only hashes and metadata.
- `search_input_excerpt` defaults to false. Admin log search does not query prompt excerpts unless this setting is explicitly enabled.
- Prompt excerpts must pass content moderation redaction before storage. Secret, token, URL, long identifier, email, and phone-like patterns are replaced with `[已脱敏]`.

## Images

- Image selection for external moderation API calls is deterministic. The first N renderable images are sent.
- Image URL and data URL strings participate in input hashing.
