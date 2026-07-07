/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        ink: '#0F0E0C',
        charcoal: '#1A1714',
        cream: '#F6EFE5',
        ivory: '#F6EFE5',
        champagne: '#E8D2A7',
        peach: '#F0B89D',
        lavender: '#BDADEB',
        sage: '#AFC8A1',
        rose: '#E8B4C6',
        pearl: '#FFF9EF',
      },
      fontFamily: {
        sans: [
          'Inter',
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'sans-serif',
        ],
        display: [
          'Inter',
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'sans-serif',
        ],
      },
      boxShadow: {
        glass: '0 34px 110px rgba(15, 14, 12, 0.22), inset 0 1px 0 rgba(255, 249, 239, 0.54)',
        soft: '0 18px 52px rgba(77, 58, 31, 0.13)',
        bloom: '0 38px 140px rgba(240, 184, 157, 0.25)',
      },
      keyframes: {
        floatSlow: {
          '0%, 100%': { transform: 'translate3d(0, 0, 0) rotate(-1deg)' },
          '50%': { transform: 'translate3d(0, -16px, 0) rotate(1deg)' },
        },
        drift: {
          '0%, 100%': { transform: 'translate3d(-2%, 0, 0) scale(1) rotate(0deg)' },
          '50%': { transform: 'translate3d(2%, -2%, 0) scale(1.04) rotate(4deg)' },
        },
        fadeUp: {
          from: { opacity: '0', transform: 'translate3d(0, 18px, 0)' },
          to: { opacity: '1', transform: 'translate3d(0, 0, 0)' },
        },
        shimmer: {
          '0%': { transform: 'translateX(-35%) rotate(16deg)', opacity: '0' },
          '30%, 62%': { opacity: '0.72' },
          '100%': { transform: 'translateX(135%) rotate(16deg)', opacity: '0' },
        },
        breathe: {
          '0%, 100%': { transform: 'scale(1)', opacity: '0.76' },
          '50%': { transform: 'scale(1.045)', opacity: '1' },
        },
      },
      animation: {
        'float-slow': 'floatSlow 9s ease-in-out infinite',
        drift: 'drift 22s ease-in-out infinite',
        'fade-up': 'fadeUp 700ms ease both',
        shimmer: 'shimmer 6.8s ease-in-out infinite',
        breathe: 'breathe 7.5s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
