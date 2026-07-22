export const calculateCacheHitRate = (
  inputTokens: number,
  cacheCreationTokens: number,
  cacheReadTokens: number
): number => {
  const totalPromptTokens = inputTokens + cacheCreationTokens + cacheReadTokens
  return totalPromptTokens > 0 ? (cacheReadTokens / totalPromptTokens) * 100 : 0
}
