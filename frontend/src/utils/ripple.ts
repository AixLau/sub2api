/**
 * Inspira UI 风格的全局点击涟漪(Ripple)效果。
 *
 * 零侵入:在 document 捕获阶段挂一个 click 监听,事件目标向上找最近的 .btn,
 * 在点击坐标生成 span.ripple-wave(直径 = 按钮对角线,中心在点击点),
 * 动画结束后移除节点。样式见 src/style.css 中的 .ripple-wave / @keyframes。
 */

const RIPPLE_CLASS = 'ripple-wave'
const RIPPLE_DURATION_MS = 450
/** animationend 不触发(如动画被禁用)时的兜底移除延迟,略大于动画时长 */
const FALLBACK_REMOVE_MS = RIPPLE_DURATION_MS + 150

function isDisabled(btn: Element): boolean {
  return (
    btn.hasAttribute('disabled') ||
    btn.getAttribute('aria-disabled') === 'true' ||
    (btn as HTMLButtonElement).disabled === true
  )
}

function spawnRipple(btn: HTMLElement, clientX: number, clientY: number): void {
  const rect = btn.getBoundingClientRect()
  // 直径 = 按钮对角线,scale 0→1 后恰好铺满按钮
  const size = Math.hypot(rect.width, rect.height)

  // 键盘触发的 click 没有有效坐标(clientX/Y 均为 0),退化为按钮中心
  const hasPoint = clientX !== 0 || clientY !== 0
  const x = hasPoint ? clientX - rect.left : rect.width / 2
  const y = hasPoint ? clientY - rect.top : rect.height / 2

  const wave = document.createElement('span')
  wave.className = RIPPLE_CLASS
  wave.setAttribute('aria-hidden', 'true')
  wave.style.width = `${size}px`
  wave.style.height = `${size}px`
  wave.style.left = `${x - size / 2}px`
  wave.style.top = `${y - size / 2}px`

  let fallbackTimer = 0
  const remove = (): void => {
    window.clearTimeout(fallbackTimer)
    wave.remove()
  }
  wave.addEventListener('animationend', remove, { once: true })
  // jsdom / 动画被禁用等场景下 animationend 不会触发,兜底清理避免节点堆积
  fallbackTimer = window.setTimeout(remove, FALLBACK_REMOVE_MS)

  btn.appendChild(wave)
}

/**
 * 安装全局涟漪监听。返回卸载函数(便于测试与 HMR)。
 *
 * - SSR / 无 document 环境:直接 no-op
 * - prefers-reduced-motion: reduce:不挂监听(matchMedia 缺失时视作未开启)
 * - disabled / aria-disabled 按钮不产生涟漪
 */
export function installRipple(): () => void {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    return () => {}
  }

  const reducedMotion =
    typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null
  if (reducedMotion?.matches) {
    return () => {}
  }

  const onClick = (event: MouseEvent): void => {
    const target = event.target
    if (!(target instanceof Element)) return
    const btn = target.closest<HTMLElement>('.btn')
    if (!btn || isDisabled(btn)) return
    spawnRipple(btn, event.clientX, event.clientY)
  }

  document.addEventListener('click', onClick, true)
  return () => document.removeEventListener('click', onClick, true)
}
