import { createRequire } from 'node:module'
import plugin from 'tailwindcss/plugin.js'

const require = createRequire(import.meta.url)
const designTokens = require('./design-tokens.json')

const rgbChannels = (hex) => {
  const normalized = hex.replace('#', '')
  const value = normalized.length === 3
    ? normalized.split('').map((character) => character.repeat(2)).join('')
    : normalized
  return [
    Number.parseInt(value.slice(0, 2), 16),
    Number.parseInt(value.slice(2, 4), 16),
    Number.parseInt(value.slice(4, 6), 16)
  ].join(' ')
}

const variableColor = (name) => `rgb(var(--color-${name}) / <alpha-value>)`

const variableScale = (name, scale) =>
  Object.fromEntries(Object.keys(scale).map((step) => [step, variableColor(`${name}-${step}`)]))

const colorVariables = (values) =>
  Object.fromEntries(
    Object.entries(values).map(([name, value]) => [`--color-${name}`, rgbChannels(value)])
  )

const rootColorVariables = {
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.brand).map(([step, value]) => [`brand-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.accent).map(([step, value]) => [`accent-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.neutral).map(([step, value]) => [`neutral-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.navy).map(([step, value]) => [`navy-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.success).map(([step, value]) => [`success-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.warning).map(([step, value]) => [`warning-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.danger).map(([step, value]) => [`danger-${step}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.platform).map(([name, value]) => [`platform-${name}`, value]))
  ),
  ...colorVariables(
    Object.fromEntries(Object.entries(designTokens.provider).map(([name, value]) => [`provider-${name}`, value]))
  ),
  ...colorVariables(designTokens.semantic.light)
}

const darkColorVariables = colorVariables(designTokens.semantic.dark)

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 兼容层：旧 primary/gray/dark 类名继续有效，值由统一令牌驱动。
        primary: variableScale('brand', designTokens.brand),
        brand: variableScale('brand', designTokens.brand),
        blue: variableScale('brand', designTokens.brand),
        indigo: variableScale('brand', designTokens.brand),
        accent: variableScale('accent', designTokens.accent),
        cyan: variableScale('accent', designTokens.accent),
        teal: variableScale('accent', designTokens.accent),
        gray: variableScale('neutral', designTokens.neutral),
        slate: variableScale('neutral', designTokens.neutral),
        dark: variableScale('navy', designTokens.navy),
        success: variableScale('success', designTokens.success),
        green: variableScale('success', designTokens.success),
        emerald: variableScale('success', designTokens.success),
        warning: variableScale('warning', designTokens.warning),
        amber: variableScale('warning', designTokens.warning),
        yellow: variableScale('warning', designTokens.warning),
        danger: variableScale('danger', designTokens.danger),
        red: variableScale('danger', designTokens.danger),
        rose: variableScale('danger', designTokens.danger),
        platform: Object.fromEntries(
          Object.keys(designTokens.platform).map((name) => [name, variableColor(`platform-${name}`)])
        ),
        provider: Object.fromEntries(
          Object.keys(designTokens.provider).map((name) => [name, variableColor(`provider-${name}`)])
        ),
        surface: {
          canvas: variableColor('surface-canvas'),
          panel: variableColor('surface-panel'),
          subtle: variableColor('surface-subtle'),
          raised: variableColor('surface-raised'),
          inverse: variableColor('surface-inverse'),
          scrim: variableColor('surface-scrim')
        },
        content: {
          primary: variableColor('content-primary'),
          secondary: variableColor('content-secondary'),
          tertiary: variableColor('content-tertiary'),
          disabled: variableColor('content-disabled'),
          inverse: variableColor('content-inverse'),
          'on-brand': variableColor('content-on-brand'),
          brand: variableColor('content-brand')
        },
        line: {
          subtle: variableColor('line-subtle'),
          default: variableColor('line-default'),
          strong: variableColor('line-strong'),
          focus: variableColor('line-focus')
        },
        status: {
          success: variableColor('status-success'),
          'success-soft': variableColor('status-success-soft'),
          'success-border': variableColor('status-success-border'),
          warning: variableColor('status-warning'),
          'warning-soft': variableColor('status-warning-soft'),
          'warning-border': variableColor('status-warning-border'),
          danger: variableColor('status-danger'),
          'danger-soft': variableColor('status-danger-soft'),
          'danger-border': variableColor('status-danger-border'),
          info: variableColor('status-info'),
          'info-soft': variableColor('status-info-soft'),
          'info-border': variableColor('status-info-border')
        }
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 12px 36px rgb(var(--color-shadow) / 0.1)',
        'glass-sm': '0 6px 20px rgb(var(--color-shadow) / 0.08)',
        glow: '0 0 0 1px rgb(var(--color-brand-500) / 0.12), 0 8px 24px rgb(var(--color-brand-500) / 0.18)',
        'glow-lg': '0 0 0 1px rgb(var(--color-brand-500) / 0.14), 0 14px 36px rgb(var(--color-brand-500) / 0.22)',
        card: '0 1px 2px rgb(var(--color-shadow) / 0.05), 0 4px 12px rgb(var(--color-shadow) / 0.04)',
        'card-hover': '0 14px 36px rgb(var(--color-shadow) / 0.11)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, rgb(var(--color-brand-500)) 0%, rgb(var(--color-brand-600)) 100%)',
        'gradient-dark': 'linear-gradient(135deg, rgb(var(--color-navy-800)) 0%, rgb(var(--color-navy-950)) 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 18% 4%, rgb(var(--color-brand-500) / 0.07) 0px, transparent 42%), radial-gradient(at 88% 2%, rgb(var(--color-accent-400) / 0.06) 0px, transparent 38%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 18px rgb(var(--color-brand-500) / 0.18)' },
          '100%': { boxShadow: '0 0 28px rgb(var(--color-brand-500) / 0.32)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: [
    plugin(({ addBase }) => {
      addBase({
        ':root': {
          ...rootColorVariables,
          colorScheme: 'light'
        },
        ':root.dark': {
          ...darkColorVariables,
          colorScheme: 'dark'
        },
        // Public and authentication pages intentionally keep their editorial light skin.
        '.landing-shell': {
          ...colorVariables(designTokens.semantic.light),
          colorScheme: 'light'
        }
      })
    })
  ]
}
