export const dashboardStatThemes = [
  'pink',
  'blue',
  'green',
  'purple',
  'amber',
  'indigo',
  'violet',
  'rose',
] as const

export type DashboardStatTheme = (typeof dashboardStatThemes)[number]

export type DashboardStatIcon =
  | 'dollar'
  | 'key'
  | 'chart'
  | 'cube'
  | 'database'
  | 'bolt'
  | 'clock'

export type DashboardStatAccent = boolean | 'value' | 'description' | 'both'
export type DashboardStatDecoration = boolean | 'wave' | 'bars' | 'none'
