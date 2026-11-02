/**
 * Shared Claude Code / Codex client defaults used by the "Use API key" modal
 * and the CC Switch one-click import, so both stay in sync.
 */

/** Context window Claude Code / Codex should plan for (1M tokens). */
export const LONG_CONTEXT_WINDOW_TOKENS = 1000000

/** Auto-compaction triggers at this percentage of the context window. */
export const AUTO_COMPACT_THRESHOLD_PERCENT = 80

/** Codex `model_auto_compact_token_limit` derived from the window and threshold. */
export const CODEX_AUTO_COMPACT_TOKEN_LIMIT =
  (LONG_CONTEXT_WINDOW_TOKENS * AUTO_COMPACT_THRESHOLD_PERCENT) / 100

/**
 * Claude model pins for Anthropic groups. The `[1m]` suffix is Claude Code's
 * client-side selector for the 1M context window; Claude Code strips it before
 * sending, and the gateway also normalizes it if it leaks through.
 */
export const CLAUDE_CODE_1M_MODEL_PINS = {
  ANTHROPIC_DEFAULT_FABLE_MODEL: 'claude-fable-5-1[1m]',
  ANTHROPIC_DEFAULT_OPUS_MODEL: 'claude-opus-5-5[1m]',
  ANTHROPIC_DEFAULT_SONNET_MODEL: 'claude-sonnet-5-5[1m]'
} as const

export interface ClaudeCodeEnvOptions {
  /** Pin Claude model aliases to the current 1M-context Claude models. */
  pinClaudeModels?: boolean
}

export function buildClaudeCodeEnv(
  baseUrl: string,
  apiKey: string,
  options: ClaudeCodeEnvOptions = {}
): Record<string, string> {
  return {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
    ...(options.pinClaudeModels ? CLAUDE_CODE_1M_MODEL_PINS : {}),
    CLAUDE_CODE_AUTO_COMPACT_WINDOW: String(LONG_CONTEXT_WINDOW_TOKENS),
    CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: String(AUTO_COMPACT_THRESHOLD_PERCENT),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1'
  }
}

/** TOML lines enabling the 1M window and the compaction threshold in Codex. */
export function codexLongContextTomlLines(): string {
  return `model_context_window = ${LONG_CONTEXT_WINDOW_TOKENS}
model_auto_compact_token_limit = ${CODEX_AUTO_COMPACT_TOKEN_LIMIT}
`
}
