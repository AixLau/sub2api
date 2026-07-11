# OpenAI API Key Account Test User-Agent Design

## Goal

Make OpenAI API Key account test requests resolve `User-Agent` through the same helper and priority rules as OpenAI OAuth account tests. This prevents Go's HTTP/2 transport from supplying `Go-http-client/2.0` when no account header override is configured.

## Behavior

All OpenAI API Key account test request variants set `User-Agent` with `resolveOpenAICodexUpstreamUserAgent` before applying account header overrides:

1. Account-level `user_agent` credential.
2. Global OpenAI Codex User-Agent setting.
3. Built-in OpenAI Codex User-Agent default.
4. Enabled account header override, applied last, remains authoritative.

The change covers the Responses API, Chat Completions API, and image generation test paths. OAuth behavior remains unchanged.

## Implementation

Update the three API Key request builders in `backend/internal/service/account_test_service.go` to set the resolved User-Agent before the existing `ApplyHeaderOverrides` call. Reuse the existing resolver rather than adding another helper or configuration key.

## Tests

Add focused account test service coverage that captures the outgoing request and verifies:

- An API Key account test sends its configured `user_agent`.
- The Chat Completions test path sends the resolved User-Agent.
- The image test path sends the resolved User-Agent.
- An enabled explicit `User-Agent` header override still wins.

Run the focused service tests followed by the relevant backend package test suite.
