/**
 * canvas-confetti 封装
 *
 * - 懒加载 canvas-confetti（dynamic import），避免进首屏 bundle
 * - prefers-reduced-motion 时直接 no-op
 * - jsdom / canvas 不可用时静默失败，不影响业务流程
 */
import type confetti from 'canvas-confetti'
import { isReducedMotionPreferred } from '@/composables/usePrefersReducedMotion'

/** 项目主题色系 */
const THEME_COLORS = ['#14b8a6', '#06b6d4', '#3b82f6', '#f59e0b']
/** Must remain above onboarding overlays/popovers (99,999,998 / 99,999,999). */
const CELEBRATION_Z_INDEX = 100_000_100
const CELEBRATION_PARTICLE_COUNT = 96
const CELEBRATION_DURATION = 2800

type ConfettiFn = typeof confetti

let confettiPromise: Promise<ConfettiFn | null> | null = null

/** 懒加载 canvas-confetti，失败时返回 null */
function loadConfetti(): Promise<ConfettiFn | null> {
  if (!confettiPromise) {
    confettiPromise = import('canvas-confetti')
      .then((mod) => {
        // canvas-confetti 类型为 export=；运行时经 esModuleInterop 后函数挂在 default 上
        const m = mod as unknown as { default?: ConfettiFn }
        return m.default ?? (mod as unknown as ConfettiFn)
      })
      .catch(() => {
        // 加载失败允许下次重试
        confettiPromise = null
        return null
      })
  }
  return confettiPromise
}

function safeFire(fn: ConfettiFn, options?: confetti.Options): void {
  try {
    // canvas 不可用（jsdom getContext 返回 null）时可能抛错，吞掉
    void fn(options)
  } catch {
    /* noop */
  }
}

/**
 * 单次彩带喷射。
 * 对外同步 void 签名，内部异步执行。
 */
export function fireConfetti(options?: confetti.Options): void {
  try {
    if (isReducedMotionPreferred()) return
    void loadConfetti().then((fn) => {
      if (fn) safeFire(fn, options)
    })
  } catch {
    /* noop */
  }
}

function randomBetween(min: number, max: number): number {
  return min + Math.random() * (max - min)
}

/**
 * 使用独立 DOM 图层渲染庆祝彩带。
 *
 * canvas-confetti 的默认全屏实例在部分浏览器环境中会创建空 canvas，
 * DOM 粒子不依赖 canvas 帧缓冲，也更容易保持在高层弹窗上方可见。
 */
function fireCelebrationLayer(): void {
  if (typeof document === 'undefined' || !document.body) return

  const layer = document.createElement('div')
  layer.dataset.confettiCelebration = 'true'
  Object.assign(layer.style, {
    position: 'fixed',
    inset: '0',
    overflow: 'hidden',
    pointerEvents: 'none',
    zIndex: String(CELEBRATION_Z_INDEX)
  })

  const width = Math.max(window.innerWidth, 320)
  const height = Math.max(window.innerHeight, 480)

  for (let index = 0; index < CELEBRATION_PARTICLE_COUNT; index++) {
    const particle = document.createElement('i')
    const color = THEME_COLORS[index % THEME_COLORS.length]
    const startX = randomBetween(-24, width + 24)
    const drift = randomBetween(-180, 180)
    const startY = randomBetween(-height * 0.5, -20)
    const endY = height + randomBetween(60, 220)
    const duration = randomBetween(1900, CELEBRATION_DURATION)
    const delay = -randomBetween(0, 850)
    const size = randomBetween(7, 13)
    const isRibbon = index % 5 === 0

    Object.assign(particle.style, {
      position: 'absolute',
      left: '0',
      top: '0',
      display: 'block',
      width: `${isRibbon ? size * 0.55 : size}px`,
      height: `${isRibbon ? size * 3.2 : size * 1.45}px`,
      borderRadius: isRibbon ? '999px' : '2px',
      background: color,
      opacity: '1',
      willChange: 'transform, opacity'
    })
    layer.appendChild(particle)

    try {
      particle.animate(
        [
          {
            transform: `translate3d(${startX}px, ${startY}px, 0) rotate(0deg)`,
            opacity: 1
          },
          {
            transform: `translate3d(${startX + drift * 0.45}px, ${height * 0.45}px, 0) rotate(${randomBetween(240, 600)}deg)`,
            opacity: 1,
            offset: 0.55
          },
          {
            transform: `translate3d(${startX + drift}px, ${endY}px, 0) rotate(${randomBetween(720, 1320)}deg)`,
            opacity: 0
          }
        ],
        {
          duration,
          delay,
          easing: 'cubic-bezier(0.16, 0.72, 0.34, 1)',
          fill: 'both'
        }
      )
    } catch {
      particle.style.transform = `translate3d(${startX}px, ${height * 0.45}px, 0)`
    }
  }

  document.body.appendChild(layer)
  window.setTimeout(() => layer.remove(), CELEBRATION_DURATION + 250)
}

/**
 * 庆祝预设：全屏彩纸与飘带下落约 2.8 秒。
 */
export function fireCelebration(): void {
  try {
    if (isReducedMotionPreferred()) return
    fireCelebrationLayer()
  } catch {
    /* noop */
  }
}
