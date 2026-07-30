# Inspira 动效组件库

从 [Inspira UI](https://inspira-ui.com) 移植/改写的动效组件集合,适配本项目技术栈。

## 公共约定

所有组件遵循以下约定(新增组件请保持一致):

- **零动效依赖**:纯 CSS keyframes / `requestAnimationFrame` / `@vueuse/core`,不引入 motion 库;keyframes 写在 SFC `<style scoped>` 内,不进 `tailwind.config.js`。
- **Tailwind v3.4**:不使用 v4 语法。
- **亮暗模式**:class 模式(`dark:` 前缀),主题色 teal(`primary-500` #14b8a6)。
- **无障碍**:全部尊重 `prefers-reduced-motion`(降级为静态/直接显示最终值);`window.matchMedia` 做判空(jsdom 兼容)。
- **jsdom 安全**:canvas 相关代码容忍 `getContext` 返回 `null`,组件在测试中挂载不抛错。
- 类名合并用 `cn`(`@/utils/cn`,clsx + tailwind-merge)。

## 组件清单

### 数值与进度

| 组件 | Props(默认值) | 说明 |
|---|---|---|
| `NumberTicker` | `value` · `duration=1500`(ms) · `decimalPlaces=0` · `prefix` · `suffix` · `formatFn?: (n) => string` | 数字滚动(rAF + ease-out)。`formatFn` 优先于 `decimalPlaces`,适合接现有格式化函数;value 变更时从旧值滚到新值 |
| `AnimatedCircularProgress` | `value`(0–100+) · `size=64` · `strokeWidth=6` · `color?`(缺省按阈值:<70 teal / 70–90 amber / ≥90 rose) · `showValue=true` · `duration=1000` | SVG 圆环进度,中心数值用 NumberTicker;>100% 环封顶画满 |
| `MultiStepLoader` | `steps: { title; description? }[]` · `current`(0-based) · `error?` | 受控竖向步骤条:已完成打勾(描边动画)、当前转圈、error 红叉 |

### 卡片与容器特效

| 组件 | Props(默认值) | 说明 |
|---|---|---|
| `BorderBeam` | `size=200` · `duration=12`(s) · `delay=0` · `borderWidth=1.5` · `colorFrom` · `colorTo` | 沿边框环游的光束。**父容器需 `relative` + 圆角**;通过 mask 裁剪到边框环,依赖 `offset-path: rect()`(不支持的浏览器自动隐藏) |
| `CardSpotlight` | `gradientSize=280` · `gradientColor?` | 鼠标跟随径向高光的包装容器(有默认插槽),hover 淡入;触屏无副作用 |
| `DirectionAwareHover` | `color?`(默认 teal 淡色) | 方向感知悬停:高光层从鼠标进入方向滑入、向离开方向滑出(hover-dir 象限判定);触屏不生效,reduced-motion 退化为淡入淡出 |
| `Lens` | `zoom=1.8` · `size=140`(镜面直径 px) | 圆形放大镜跟随鼠标(clip-path circle + 以鼠标点为 origin 的 scale);触屏与 reduced-motion 下不启用 |
| `LiquidGlass` | `radius=16` · `border=0.07` · `lightness=50` · `alpha=0.93` · `blur=11` · `scale=-180` · `frost=0.05` | SVG 置换滤镜液态玻璃;Chromium 使用折射效果,Safari/Firefox 使用不透明纯白卡片 |
| `LiquidGlassBackdrop` | 无 | 液态玻璃预览与正式联系客服弹窗共享的透明灰色点阵环境层，不覆盖页面原背景 |
| `SupportContactCardContent` | `v-model` · `qqQrCode?` · `wechatQrCode?` · `dialog?` · `dismissible?` | 原预览页与正式联系客服弹窗共享的完整卡片内容、排版、标签页和二维码状态 |
| `ScratchToReveal` | `coverColor?`(缺省随亮/暗色自动) · `coverImage?`(失败时回退颜色) · `coverText?` · `threshold=0.5` · `radius=24`;emit `complete` | 刮刮乐:canvas 覆盖层支持图片换肤并可拖动擦除,刮开面积达阈值后淡出并触发 `complete`;键盘 Enter/Space 或双击可直接揭示(可访问性);无 canvas / reduced-motion 时内容直接可见并立即 `complete` |

### 背景层(absolute 铺满父容器,父容器需 `relative`,无插槽)

| 组件 | Props(默认值) | 说明 |
|---|---|---|
| `AuroraBackground` | — | 多层柔光渐变极光,纯 CSS 漂移 |
| `FlickeringGrid` | `squareSize=4` · `gridGap=6` · `flickerChance=0.3` · `color='#14b8a6'` · `maxOpacity=0.2` | canvas 闪烁网格;ResizeObserver 跟随尺寸、IntersectionObserver 离屏暂停 |

### 列表与文字

| 组件 | Props(默认值) | 说明 |
|---|---|---|
| `AnimatedList` | `tag='div'` · `stagger=60`(ms) | TransitionGroup 包装:子项淡入上移进场、平滑位移,按索引 stagger(上限 8 项)。用法见组件顶部注释 |
| `Timeline` | `items: { time; title; description?; badge?; tone? }[]`(tone: default/success/warning/danger) | 通用竖向时间线,节点按 tone 着色,进场 stagger |
| `Marquee` | `reverse=false` · `pauseOnHover=true` · `duration=30`(s) | 无缝循环跑马灯(插槽渲染两份,第二份 aria-hidden),两侧渐变遮罩;子项需 `shrink-0 whitespace-nowrap` |
| `GlitchText` | `text` | 故障文字(青/品红错位闪动);注意在 `bg-clip-text` 渐变字上伪元素会不可见 |
| `ShimmerButton` | `as='button'` · `background`(CSS 值,默认蓝→青渐变) · `shimmerColor` · `shimmerDuration=3`(s) | 闪光扫过按钮;`as="span"` 可包进 router-link,尺寸/圆角由调用方类名决定 |

### 工具函数(非组件)

| 模块 | 导出 | 说明 |
|---|---|---|
| `confetti.ts` | `fireConfetti(options?)` · `fireCelebration()` | canvas-confetti 懒加载封装(独立 async chunk);reduced-motion 时 no-op,任何失败静默 |
| `@/utils/ripple.ts` | `installRipple(): () => void` | 全局按钮涟漪:document 捕获阶段事件委托,命中 `.btn` 即在点击点生成涟漪,无需逐按钮接入;已在 `main.ts` 安装 |

## 当前使用位置速查

- **NumberTicker**:管理端/用户端仪表盘、用量汇总卡(`UsageStatsCards`)、推广收益、账号统计弹窗、运维面板主数值、落地页统计、`StatCard`
- **BorderBeam**:登录卡(`AuthLayout`)、折扣订阅卡(`SubscriptionPlanCard`,`highlight` prop 可强制开关)、落地页终端卡
- **MultiStepLoader**:OAuth 手动授权流程(`OAuthAuthorizationFlow`)
- **AnimatedCircularProgress**:账号统计弹窗活跃天数
- **AnimatedList**:仪表盘最近调用记录
- **ScratchToReveal + fireCelebration**:兑换结果(`RedeemView`);另有支付成功、安装完成、首把 API Key 创建触发彩带
- **Timeline**:审计日志时间线视图(表格默认)
- **DirectionAwareHover**:仪表盘快捷操作(`UserDashboardQuickActions`,按各按钮 accent 配色)
- **Lens**:支付二维码弹窗悬停放大(`PaymentQRDialog`)
- 相关非 inspira 增强:`LoadingSpinner` 的 `variant="orbit"` 轨道加载变体(两个仪表盘整页加载态)、`DataTable` 横向滚动边缘渐隐提示、`PaymentMethodSelector` 选中态呼吸光晕
- **FlickeringGrid**:登录页背景;**Aurora/CardSpotlight/Marquee/ShimmerButton/GlitchText**:落地页与 404

## 测试

每个组件在 `__tests__/` 下有冒烟测试。注意:全局测试 setup(`src/__tests__/setup.ts`)把 `matchMedia` mock 为对任意 query 返回 `matches: true`,因此**测试环境默认命中 reduced-motion 降级分支**(数字直接显示最终值、confetti no-op);需要测试动画路径时,在 spec 内局部覆写 matchMedia 并在 afterEach 还原。
