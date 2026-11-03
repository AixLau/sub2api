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
 * Claude Code env for Sub2API. Model aliases are left unset so Claude Code uses
 * its own model list; current Claude models already run with a native 1M window.
 */
export function buildClaudeCodeEnv(baseUrl: string, apiKey: string): Record<string, string> {
  return {
    ANTHROPIC_BASE_URL: baseUrl,
    ANTHROPIC_AUTH_TOKEN: apiKey,
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

