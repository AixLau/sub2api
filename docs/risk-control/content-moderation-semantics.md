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
- General candidate attribution and semantic evidence use only the latest direct user turn, treating empty or unknown client roles as untrusted direct user evidence and merging text parts that belong to one turn. Older user history and system, developer, assistant, tool, or ambient context cannot establish current intent. Structured inputs never fall back to flattened `Text` when the latest user turn has no matching risk; the fallback is reserved for protocols with no attributed `Sources` at all.
- The default primary model is `gpt-5.3-codex-spark` with `gpt-5.4-mini` as fallback; account selection, OAuth headers, quota refresh, and retries remain in the existing OpenAI gateway service.
- For a hybrid keyword hit, a semantic `reject` is a terminal 403 in pre-block mode only after the required ordinary API call. A semantic `allow` or `review` does not bypass the ordinary API, so `keyword_and_api` never turns a local hit into an unreviewed direct block.
- If semantic review is enabled but unavailable, hybrid candidates continue to the ordinary moderation API. If that API is also unavailable, the request is fail-opened with action `error`.
- Semantic review evaluates nine dimensions: `intent`, `target`, `authorization`, `operationality`, `executability`, `information_access`, `harm_mechanism`, `harm_evidence`, and `deception_type`. A harmful, actionable result with direct or indirect executability and a concrete harm mechanism is deterministically upgraded to `reject` under either of two authorization branches: (1) `authorization=unauthorized` against a `third_party` or `external_service` target, or (2) `authorization=not_applicable` against a target other than `self_owned|authorized_lab` whose categories hit a high-risk label (the high-risk set includes `market_manipulation`); a `deception_fraud` upgrade additionally requires explicit evidence (`harm_evidence=explicit`) with a concrete `deception_type`.
- A narrow malware consistency exception prevents a complete critical result from being downgraded solely because the reviewer emitted `authorization=unclear` or `harm_mechanism=other`: it requires `intent=harmful`, `operationality=actionable`, `executability=direct`, `harm_evidence=explicit`, severity `critical`, confidence at least 0.95, a `malware|ransomware` category, and no authorized self-owned/lab scope. Narrow operational candidates for functional ransomware, covert surveillance/private-communications interception, and protocol tampering to bypass entitlement limits may likewise turn a high-confidence general `review` into `reject` only when the semantic result independently reports explicit harmful intent and direct executability. Commercial-software binary/DLL cracking, expiry falsification, and seat/user/concurrency bypass use a non-strict, non-operational review-only candidate; it can promote `review` only after the reviewer independently confirms high-confidence explicit harm, actionable direct executability, and no self-owned or authorized-lab scope. Ordinary reverse engineering, reimplementation, startup/compatibility/UI repair, and defensive audits are not blocked by that regex alone.
- General semantic review uses `review` only for an unresolved safety-critical fact that can reasonably change the verdict between `allow` and `reject`. Low confidence, minor ambiguity, typos, slang, omissions, unfamiliar non-critical terms, and missing non-critical context are not independent reasons for review. Authorization is `not_applicable` unless protected-resource access is actually requested.
- A general semantic `review` is retained as a pending audit record and does not synchronously block traffic, regardless of local candidate category or severity. Only a confirmed general semantic `reject`, or the separately configured prompt-injection fail-closed contract above, can terminate a pre-block request.
- A general semantic result with `intent=benign|defensive` and `harm_mechanism=none` resolves to `allow` unless the reviewer explicitly reports `authorization=unauthorized`. `authorization=unclear` alone cannot make an otherwise harmless result reviewable, including ordinary operations involving private or restricted data. A model `allow` for explicitly authorized work against a `self_owned` or `authorized_lab` target (`authorization=authorized`) may additionally carry a non-`none` `harm_mechanism` and is preserved as `allow` (authorized-lab/self-owned exemption); this exemption applies only to a model `allow` verdict, not to model `review`/`reject` verdicts.
- The two allow-direction downgrades (`semantic_policy_harmless_review` and `semantic_policy_unsubstantiated_fraud`) run only when evidence is complete. Under incomplete (e.g. truncated) candidate evidence, a model `review` is kept as `review` for human triage and a model `reject` is reduced to `review`; the model's original severity is preserved in the `semantic_review_model_severity` metadata field whenever policy changes the verdict.
- Semantic evidence is incomplete whenever extraction or a source is truncated, more matching sources exist than the review source cap, or the review rune budget cannot contain every selected source header and excerpt. Completeness is carried with the constructed input rather than inferred from final string length.
- General semantic review records `harm_evidence=none|inferred|explicit` and a bounded `deception_type`. A `deception_fraud` rejection requires explicit evidence in the current user's request and a concrete deception type. Inferred deception is downgraded to allow only for a non-reject verdict with explicitly benign or defensive intent, no explicit unauthorized signal, and `public`, `provided_by_user`, or `not_applicable` information access. Missing or invalid evidence values normalize to the internal value `unknown`, which is not part of the model schema and does not trigger this downgrade; model rejects fall back to review, and ambiguous, legacy, or malformed results remain reviewable.
- Prompt injection is reviewed by a dedicated strict schema. Its required fields are `verdict`, `active_override`, `presentation`, `targets`, `confidence`, and `reason_codes`. Rollouts, logs, transcripts, tool output, skill definitions, and quoted system/developer instructions are treated as data when the outer request asks to analyze, summarize, translate, classify, debug, or review them. Only an active direct override or prompt-authoring request with confidence at least 0.70 can be upgraded to `reject`; incomplete evidence is always reduced to `review`, and `allow` is valid only with complete evidence.
- The prompt-injection baseline runs before group/model/account scope whenever global risk control is enabled. A no-hit request does not call a semantic model or write a full audit record.
- The dedicated reviewer receives the complete current source up to 12K runes. Larger sources are represented by one valid JSON evidence envelope containing head, all coverable hit windows, and tail. If every high-risk occurrence cannot fit, evidence is marked incomplete and cannot produce a final allow.

## Rule Actions

- `block` blocks the request and records a 100% hit log.
- `observe` allows the request, marks it flagged, records a 100% hit log, and does not trigger automatic ban side effects.
- `warn` allows the request, marks it flagged, records a 100% hit log, and does not trigger automatic ban side effects.
- `sample_rate` must never control whether scanning runs. It only controls non-hit log sampling.
- Observe-mode hits are audit evidence only: they must not write the blocking hash cache, increment violation counts, auto-ban an account, or send violation notifications.
- Moderation dependency errors are audit events, not non-hits, and are persisted independently of `record_non_hits`.
- Empty extraction, skipped oversized encoded payloads, missing provider keys, and upstream failures are persisted as `error` audit records without hash, violation-count, ban, or notification side effects.
- An asynchronous semantic review is counted as processed only after its audit result is stored. Persistence failures are returned to the outbox for retry.

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
