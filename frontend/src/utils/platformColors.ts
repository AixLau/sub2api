/**
 * Centralized platform color definitions.
 *
 * All components that need platform-specific styling should import from here
 * instead of defining their own color mappings.
 */
import { brandColors, platformIdentityColors } from '@/theme/designTokens'

export type Platform =
  | 'anthropic'
  | 'openai'
  | 'antigravity'
  | 'gemini'
  | 'grok'
  | 'composite'
  | 'meta'
  | 'mistral'

// ── Badge (bg + text + border, for inline badges with border) ───────
const BADGE: Record<Platform, string> = {
  anthropic:
    'bg-platform-anthropic/10 text-platform-anthropic-deep border-platform-anthropic/30 dark:text-platform-anthropic-soft',
  openai:
    'bg-platform-openai/10 text-platform-openai-deep border-platform-openai/30 dark:text-platform-openai-soft',
  antigravity:
    'bg-platform-antigravity/10 text-platform-antigravity-deep border-platform-antigravity/30 dark:text-platform-antigravity-soft',
  gemini:
    'bg-platform-gemini/10 text-platform-gemini-deep border-platform-gemini/30 dark:text-platform-gemini-soft',
  grok:
    'bg-platform-grok-deeper/10 text-platform-grok-deeper border-platform-grok/30 dark:bg-platform-grok/10 dark:text-platform-grok-soft dark:border-platform-grok/30',
  composite:
    'bg-platform-composite/10 text-platform-composite-deep border-platform-composite/30 dark:text-platform-composite-soft',
  meta:
    'bg-platform-meta/10 text-platform-meta-deep border-platform-meta/30 dark:text-platform-meta-soft',
  mistral:
    'bg-platform-mistral/10 text-platform-mistral-deep border-platform-mistral/30 dark:text-platform-mistral-soft',
}
const BADGE_DEFAULT = 'bg-slate-500/10 text-slate-600 border-slate-500/30 dark:text-slate-400'

// ── Light badge (softer bg, no border) ──────────────────────────────
const BADGE_LIGHT: Record<Platform, string> = {
  anthropic:
    'bg-platform-anthropic/10 text-platform-anthropic-deep dark:text-platform-anthropic-soft',
  openai: 'bg-platform-openai/10 text-platform-openai-deep dark:text-platform-openai-soft',
  antigravity:
    'bg-platform-antigravity/10 text-platform-antigravity-deep dark:text-platform-antigravity-soft',
  gemini: 'bg-platform-gemini/10 text-platform-gemini-deep dark:text-platform-gemini-soft',
  grok: 'bg-platform-grok/10 text-platform-grok-deeper dark:text-platform-grok-soft',
  composite:
    'bg-platform-composite/10 text-platform-composite-deep dark:text-platform-composite-soft',
  meta: 'bg-platform-meta/10 text-platform-meta-deep dark:text-platform-meta-soft',
  mistral:
    'bg-platform-mistral/10 text-platform-mistral-deep dark:text-platform-mistral-soft',
}

// ── Border ──────────────────────────────────────────────────────────
const BORDER: Record<Platform, string> = {
  anthropic: 'border-platform-anthropic/20',
  openai: 'border-platform-openai/20',
  antigravity: 'border-platform-antigravity/20',
  gemini: 'border-platform-gemini/20',
  grok: 'border-platform-grok/20',
  composite: 'border-platform-composite/20',
  meta: 'border-platform-meta/20',
  mistral: 'border-platform-mistral/20',
}
const BORDER_DEFAULT = 'border-gray-200 dark:border-dark-700'

// ── Border strong (higher-contrast platform tint, e.g. plaza group cards) ──
const BORDER_STRONG: Record<Platform, string> = {
  anthropic: 'border-platform-anthropic/35 dark:border-platform-anthropic/30',
  openai: 'border-platform-openai/35 dark:border-platform-openai/30',
  antigravity: 'border-platform-antigravity/35 dark:border-platform-antigravity/30',
  gemini: 'border-platform-gemini/35 dark:border-platform-gemini/30',
  grok: 'border-platform-grok/35 dark:border-platform-grok/30',
  composite: 'border-platform-composite/35 dark:border-platform-composite/30',
  meta: 'border-platform-meta/35 dark:border-platform-meta/30',
  mistral: 'border-platform-mistral/35 dark:border-platform-mistral/30',
}
const BORDER_STRONG_DEFAULT = 'border-gray-300 dark:border-dark-600'

// ── Accent (single raw color per platform; consumers derive washes/tints
//    from it via CSS color-mix, e.g. plaza paid-price zone) ──
const ACCENT: Record<Platform, string> = {
  anthropic: platformIdentityColors.anthropic,
  openai: platformIdentityColors.openai,
  antigravity: platformIdentityColors.antigravity,
  gemini: platformIdentityColors.gemini,
  grok: platformIdentityColors.grok,
  composite: platformIdentityColors.composite,
  meta: platformIdentityColors.meta,
  mistral: platformIdentityColors.mistral,
}
const ACCENT_DEFAULT = brandColors['500']

// ── Accent bar (gradient) ───────────────────────────────────────────
const ACCENT_BAR: Record<Platform, string> = {
  anthropic: 'bg-gradient-to-r from-platform-anthropic-soft to-platform-anthropic',
  openai: 'bg-gradient-to-r from-platform-openai-soft to-platform-openai',
  antigravity: 'bg-gradient-to-r from-platform-antigravity-soft to-platform-antigravity',
  gemini: 'bg-gradient-to-r from-platform-gemini-soft to-platform-gemini',
  grok: 'bg-gradient-to-r from-platform-grok to-platform-grok-deeper',
  composite: 'bg-gradient-to-r from-platform-composite-soft to-platform-composite',
  meta: 'bg-gradient-to-r from-platform-meta-soft to-platform-meta',
  mistral: 'bg-gradient-to-r from-platform-mistral-soft to-platform-mistral',
}
const ACCENT_BAR_DEFAULT = 'bg-gradient-to-r from-primary-400 to-primary-500'

// ── Text (price, icon) ─────────────────────────────────────────────
const TEXT: Record<Platform, string> = {
  anthropic: 'text-platform-anthropic-deep dark:text-platform-anthropic-soft',
  openai: 'text-platform-openai-deep dark:text-platform-openai-soft',
  antigravity: 'text-platform-antigravity-deep dark:text-platform-antigravity-soft',
  gemini: 'text-platform-gemini-deep dark:text-platform-gemini-soft',
  grok: 'text-platform-grok-deeper dark:text-platform-grok-soft',
  composite: 'text-platform-composite-deep dark:text-platform-composite-soft',
  meta: 'text-platform-meta-deep dark:text-platform-meta-soft',
  mistral: 'text-platform-mistral-deep dark:text-platform-mistral-soft',
}
const TEXT_DEFAULT = 'text-primary-600 dark:text-primary-400'

// ── Icon (check mark etc.) ──────────────────────────────────────────
const ICON: Record<Platform, string> = {
  anthropic: 'text-platform-anthropic dark:text-platform-anthropic-soft',
  openai: 'text-platform-openai dark:text-platform-openai-soft',
  antigravity: 'text-platform-antigravity dark:text-platform-antigravity-soft',
  gemini: 'text-platform-gemini dark:text-platform-gemini-soft',
  grok: 'text-platform-grok-deeper dark:text-platform-grok-soft',
  composite: 'text-platform-composite dark:text-platform-composite-soft',
  meta: 'text-platform-meta dark:text-platform-meta-soft',
  mistral: 'text-platform-mistral dark:text-platform-mistral-soft',
}
const ICON_DEFAULT = 'text-primary-500 dark:text-primary-400'

// ── Button (solid bg) ───────────────────────────────────────────────
const BUTTON: Record<Platform, string> = {
  anthropic:
    'bg-platform-anthropic-deep text-content-on-brand hover:bg-platform-anthropic-deeper active:bg-platform-anthropic-deeper',
  openai:
    'bg-platform-openai-deep text-content-on-brand hover:bg-platform-openai-deeper active:bg-platform-openai-deeper',
  antigravity:
    'bg-platform-antigravity-deep text-content-on-brand hover:bg-platform-antigravity-deeper active:bg-platform-antigravity-deeper',
  gemini:
    'bg-platform-gemini-deep text-content-on-brand hover:bg-platform-gemini-deeper active:bg-platform-gemini-deeper',
  grok:
    'bg-platform-grok-deep text-content-on-brand hover:bg-platform-grok-deeper active:bg-platform-grok-deeper',
  composite:
    'bg-platform-composite-deep text-content-on-brand hover:bg-platform-composite-deeper active:bg-platform-composite-deeper',
  meta:
    'bg-platform-meta-deep text-content-on-brand hover:bg-platform-meta-deeper active:bg-platform-meta-deeper',
  mistral:
    'bg-platform-mistral-deep text-content-on-brand hover:bg-platform-mistral-deeper active:bg-platform-mistral-deeper',
}
const BUTTON_DEFAULT =
  'bg-primary-500 text-content-on-brand hover:bg-primary-600 active:bg-primary-700'

// ── Discount badge ──────────────────────────────────────────────────
const DISCOUNT: Record<Platform, string> = {
  anthropic:
    'bg-platform-anthropic/10 text-platform-anthropic-deep dark:bg-platform-anthropic/20 dark:text-platform-anthropic-soft',
  openai:
    'bg-platform-openai/10 text-platform-openai-deep dark:bg-platform-openai/20 dark:text-platform-openai-soft',
  antigravity:
    'bg-platform-antigravity/10 text-platform-antigravity-deep dark:bg-platform-antigravity/20 dark:text-platform-antigravity-soft',
  gemini:
    'bg-platform-gemini/10 text-platform-gemini-deep dark:bg-platform-gemini/20 dark:text-platform-gemini-soft',
  grok:
    'bg-platform-grok/10 text-platform-grok-deeper dark:bg-platform-grok/20 dark:text-platform-grok-soft',
  composite:
    'bg-platform-composite/10 text-platform-composite-deep dark:bg-platform-composite/20 dark:text-platform-composite-soft',
  meta: 'bg-platform-meta/10 text-platform-meta-deep dark:bg-platform-meta/20 dark:text-platform-meta-soft',
  mistral:
    'bg-platform-mistral/10 text-platform-mistral-deep dark:bg-platform-mistral/20 dark:text-platform-mistral-soft',
}
const DISCOUNT_DEFAULT = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'

// ── Header gradient (subscription confirm) ─────────────────────────
const GRADIENT: Record<Platform, string> = {
  anthropic: 'from-platform-anthropic to-platform-anthropic-deep',
  openai: 'from-platform-openai to-platform-openai-deep',
  antigravity: 'from-platform-antigravity to-platform-antigravity-deep',
  gemini: 'from-platform-gemini to-platform-gemini-deep',
  grok: 'from-platform-grok-deep to-platform-grok-deeper',
  composite: 'from-platform-composite to-platform-composite-deep',
  meta: 'from-platform-meta to-platform-meta-deep',
  mistral: 'from-platform-mistral to-platform-mistral-deep',
}
const GRADIENT_DEFAULT = 'from-primary-500 to-primary-600'

// ── Header text (light text on gradient bg) ────────────────────────
const GRADIENT_TEXT: Record<Platform, string> = {
  anthropic: 'text-content-on-brand',
  openai: 'text-content-on-brand',
  antigravity: 'text-content-on-brand',
  gemini: 'text-content-on-brand',
  grok: 'text-content-on-brand',
  composite: 'text-content-on-brand',
  meta: 'text-content-on-brand',
  mistral: 'text-content-on-brand',
}
const GRADIENT_TEXT_DEFAULT = 'text-content-on-brand'

const GRADIENT_SUBTEXT: Record<Platform, string> = {
  anthropic: 'text-content-on-brand/80',
  openai: 'text-content-on-brand/80',
  antigravity: 'text-content-on-brand/80',
  gemini: 'text-content-on-brand/80',
  grok: 'text-content-on-brand/80',
  composite: 'text-content-on-brand/80',
  meta: 'text-content-on-brand/80',
  mistral: 'text-content-on-brand/80',
}
const GRADIENT_SUBTEXT_DEFAULT = 'text-content-on-brand/80'

// ── Picker (platform filter / radio button) ─────────────────────────
const PICKER_ACTIVE: Record<Platform, string> = {
  anthropic:
    'border-platform-anthropic bg-platform-anthropic/10 text-platform-anthropic-deep dark:border-platform-anthropic-soft dark:text-platform-anthropic-soft',
  openai:
    'border-platform-openai bg-platform-openai/10 text-platform-openai-deep dark:border-platform-openai-soft dark:text-platform-openai-soft',
  antigravity:
    'border-platform-antigravity bg-platform-antigravity/10 text-platform-antigravity-deep dark:border-platform-antigravity-soft dark:text-platform-antigravity-soft',
  gemini:
    'border-platform-gemini bg-platform-gemini/10 text-platform-gemini-deep dark:border-platform-gemini-soft dark:text-platform-gemini-soft',
  grok:
    'border-platform-grok bg-platform-grok/10 text-platform-grok-deeper dark:border-platform-grok-soft dark:text-platform-grok-soft',
  composite:
    'border-platform-composite bg-platform-composite/10 text-platform-composite-deep dark:border-platform-composite-soft dark:text-platform-composite-soft',
  meta:
    'border-platform-meta bg-platform-meta/10 text-platform-meta-deep dark:border-platform-meta-soft dark:text-platform-meta-soft',
  mistral:
    'border-platform-mistral bg-platform-mistral/10 text-platform-mistral-deep dark:border-platform-mistral-soft dark:text-platform-mistral-soft',
}

const PICKER_INACTIVE: Record<Platform, string> = {
  anthropic:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-anthropic/50 hover:text-platform-anthropic-deep dark:hover:text-platform-anthropic-soft',
  openai:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-openai/50 hover:text-platform-openai-deep dark:hover:text-platform-openai-soft',
  antigravity:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-antigravity/50 hover:text-platform-antigravity-deep dark:hover:text-platform-antigravity-soft',
  gemini:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-gemini/50 hover:text-platform-gemini-deep dark:hover:text-platform-gemini-soft',
  grok:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-grok/50 hover:text-platform-grok-deeper dark:hover:text-platform-grok-soft',
  composite:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-composite/50 hover:text-platform-composite-deep dark:hover:text-platform-composite-soft',
  meta:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-meta/50 hover:text-platform-meta-deep dark:hover:text-platform-meta-soft',
  mistral:
    'border-line-default bg-surface-panel text-content-tertiary hover:border-platform-mistral/50 hover:text-platform-mistral-deep dark:hover:text-platform-mistral-soft',
}

const PICKER_ACTIVE_DEFAULT =
  'border-line-strong bg-surface-subtle text-content-primary'
const PICKER_INACTIVE_DEFAULT =
  'border-line-default bg-surface-panel text-content-tertiary hover:border-line-strong hover:text-content-primary'

// ── Public API ──────────────────────────────────────────────────────

function normalizePlatform(p: string): Platform | null {
  switch (p.trim().toLowerCase()) {
    case 'anthropic':
    case 'claude':
      return 'anthropic'
    case 'openai':
      return 'openai'
    case 'antigravity':
      return 'antigravity'
    case 'gemini':
    case 'google':
      return 'gemini'
    case 'grok':
    case 'xai':
      return 'grok'
    case 'composite':
      return 'composite'
    case 'meta':
      return 'meta'
    case 'mistral':
      return 'mistral'
    default:
      return null
  }
}

function platformValue<T>(p: string, values: Record<Platform, T>, fallback: T): T {
  const platform = normalizePlatform(p)
  return platform ? values[platform] : fallback
}

export function platformBadgeClass(p: string): string {
  return platformValue(p, BADGE, BADGE_DEFAULT)
}

export function platformBadgeLightClass(p: string): string {
  return platformValue(p, BADGE_LIGHT, BADGE_DEFAULT)
}

export function platformBorderClass(p: string): string {
  return platformValue(p, BORDER, BORDER_DEFAULT)
}

export function platformBorderStrongClass(p: string): string {
  return platformValue(p, BORDER_STRONG, BORDER_STRONG_DEFAULT)
}

export function platformAccentColor(p: string): string {
  return platformValue(p, ACCENT, ACCENT_DEFAULT)
}

export function platformAccentBarClass(p: string): string {
  return platformValue(p, ACCENT_BAR, ACCENT_BAR_DEFAULT)
}

export function platformTextClass(p: string): string {
  return platformValue(p, TEXT, TEXT_DEFAULT)
}

export function platformIconClass(p: string): string {
  return platformValue(p, ICON, ICON_DEFAULT)
}

export function platformButtonClass(p: string): string {
  return platformValue(p, BUTTON, BUTTON_DEFAULT)
}

export function platformDiscountClass(p: string): string {
  return platformValue(p, DISCOUNT, DISCOUNT_DEFAULT)
}

export function platformGradientClass(p: string): string {
  return platformValue(p, GRADIENT, GRADIENT_DEFAULT)
}

export function platformGradientTextClass(p: string): string {
  return platformValue(p, GRADIENT_TEXT, GRADIENT_TEXT_DEFAULT)
}

export function platformGradientSubtextClass(p: string): string {
  return platformValue(p, GRADIENT_SUBTEXT, GRADIENT_SUBTEXT_DEFAULT)
}

export function platformPickerClass(p: string, active: boolean): string {
  return active
    ? platformValue(p, PICKER_ACTIVE, PICKER_ACTIVE_DEFAULT)
    : platformValue(p, PICKER_INACTIVE, PICKER_INACTIVE_DEFAULT)
}

export function platformLabel(p: string): string {
  switch (normalizePlatform(p)) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
    case 'gemini': return 'Gemini'
    case 'grok': return 'Grok'
    case 'composite': return 'Composite'
    case 'meta': return 'Meta'
    case 'mistral': return 'Mistral'
    default: return p || 'API'
  }
}
