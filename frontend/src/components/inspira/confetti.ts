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

/**
 * 庆祝预设：约 1.5 秒内从左右两侧多次喷射。
 * 对外同步 void 签名，内部异步执行。
 */
export function fireCelebration(): void {
  try {
    if (isReducedMotionPreferred()) return
    void loadConfetti().then((fn) => {
      if (!fn) return

      const base: confetti.Options = {
        particleCount: 40,
        spread: 60,
        startVelocity: 55,
        ticks: 120,
        colors: THEME_COLORS,
        zIndex: 9999
      }

      // 0 ~ 1.5s 内左右两侧交替喷射
      const delays = [0, 250, 500, 750, 1000, 1250]
      for (const delay of delays) {
        setTimeout(() => {
          try {
            safeFire(fn, {
              ...base,
              angle: 60,
              origin: { x: 0, y: 0.7 }
            })
            safeFire(fn, {
              ...base,
              angle: 120,
              origin: { x: 1, y: 0.7 }
            })
          } catch {
            /* noop */
          }
        }, delay)
      }
    })
  } catch {
    /* noop */
  }
}
