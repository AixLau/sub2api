import tokens from '../../design-tokens.json'

export const brandColors = tokens.brand
export const accentColors = tokens.accent
export const neutralColors = tokens.neutral
export const navyColors = tokens.navy
export const successColors = tokens.success
export const warningColors = tokens.warning
export const dangerColors = tokens.danger
export const semanticColors = tokens.semantic
export const platformIdentityColors = tokens.platform
export const providerColors = tokens.provider
export const categoricalPalette = tokens.dataViz.categorical
export const tokenUsageColors = tokens.dataViz.tokenUsage

export const chartSeriesColors = {
  primary: categoricalPalette[0],
  success: categoricalPalette[1],
  warning: categoricalPalette[2],
  danger: categoricalPalette[3],
  contrast: categoricalPalette[4],
  accent: categoricalPalette[5],
  pink: categoricalPalette[6],
  orange: categoricalPalette[7],
  secondaryBrand: categoricalPalette[8],
  positive: categoricalPalette[9],
  sky: categoricalPalette[10],
  purple: categoricalPalette[11],
} as const

export function getChartTheme(isDark: boolean) {
  const theme = semanticColors[isDark ? 'dark' : 'light']

  return {
    text: theme['content-tertiary'],
    grid: theme['line-subtle'],
    tooltipSurface: theme['surface-raised'],
    tooltipTitle: theme['content-primary'],
    tooltipBody: theme['content-secondary'],
    tooltipBorder: theme['line-default'],
  }
}

export function colorWithAlpha(hex: string, alpha: number): string {
  const normalized = hex.replace('#', '')
  const value = normalized.length === 3
    ? normalized.split('').map((character) => character.repeat(2)).join('')
    : normalized
  const red = Number.parseInt(value.slice(0, 2), 16)
  const green = Number.parseInt(value.slice(2, 4), 16)
  const blue = Number.parseInt(value.slice(4, 6), 16)
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`
}
