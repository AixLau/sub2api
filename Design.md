# Dify 官网视觉风格规范（Design Spec for AI Agents）

> 本文件写给 Codex / Claude Code 等智能体。照此规范实现，即可产出与 dify.ai 官网风格高度一致的页面。
> 所有数值均从 dify.ai 线上页面实测（computed styles），可直接落地为 CSS/Tailwind token。

---

## 0. 一句话风格定位

干净、克制、偏“技术工具”气质的现代 SaaS landing 风格：大面积纯白底 + 极简黑字 + 单一高饱和蓝（电光蓝 `#0033FF`）作为唯一强调色。无渐变滥用、无重阴影，靠**细的浅蓝 1px 描边环**和**大量留白**塑造层次。字体走中性 grotesk（Söhne），标题超大、字重轻、负字距。

---

## 1. 设计基调（Design Principles）

1. **白 + 黑 + 一抹蓝**：背景几乎全白，正文近黑，强调色只用一种蓝。不要引入第二种强调色。
2. **留白即设计**：区块之间靠大 padding 拉开，不靠分割线。
3. **轻字重 + 大字号**：Hero 标题用 `font-weight: 400` 但字号拉到 60px，靠尺寸而非粗体制造冲击力。
4. **细描边代替阴影**：卡片普遍用 `box-shadow: 0 0 0 1px #E5EAFF`（1px 浅蓝色环）勾边，几乎不用投影。
5. **圆角两极分化**：交互/标签用全圆角 `999px`（pill），内容卡片用中等圆角 `12px`，小按钮偶尔 `2px`。
6. **大写 + 字距的小标题**：区块 eyebrow 用全大写 + 正字距（`letter-spacing: 1px`）。

---

## 2. 色彩系统（Color Tokens）

| Token | HEX | RGB | 用途 |
|---|---|---|---|
| `--color-bg` | `#FFFFFF` | 255,255,255 | 页面主背景（占比最高，17+ 区块为纯白） |
| `--color-bg-subtle` | `#F8F9FB` | 248,249,251 | 次级浅灰背景区块（隔开两段白底用） |
| `--color-text` | `#212121` | 33,33,33 | 正文主色 |
| `--color-text-strong` | `#000000` | 0,0,0 | 标题/eyebrow 黑色 |
| `--color-text-muted` | `#333333` | 51,51,51 | Hero 大标题、副标题文字 |
| `--color-accent` | `#0033FF` | 0,51,255 | **唯一强调色**：主按钮底、链接、高亮文字、描边 |
| `--color-accent-tint` | `#E5EAFF` | 229,234,255 | 浅蓝：卡片描边环、标签/badge 底色 |
| `--color-on-accent` | `#FFFFFF` | 255,255,255 | 蓝底上的文字 |
| `--color-border` | `#F4F4F4` | 244,244,244 | 顶栏底边等极浅分割线 |
| `--color-section-invert` | `#0033FF` | 0,51,255 | 反色区块（整段蓝底白字，用于强调段/CTA 段） |

规则：
- 强调蓝 `#0033FF` 同时承担「主按钮底色」「正文内链接色」「高亮关键词色」三重身份。
- 浅蓝 `#E5EAFF` 几乎只用于「1px 描边环」和「小标签底」，不要拿它当大面积背景。
- 反色蓝区块（`#0033FF` 整段背景）用于页面里 1~2 个最强调的段落（如 CTA、关键数据），文字全白。

---

## 3. 字体系统（Typography）

### 3.1 字族

线上使用 **Söhne**（Klim Type 商业字体）三个字重族：

| 角色 | 实测 font-family | 等价 weight |
|---|---|---|
| Regular | `Söhne Buch` | 400 |
| Medium | `Söhne Kräftig` | 500 |
| Semibold | `Söhne Halbfett` | 600 |

**落地替代方案**（无 Söhne 授权时，按相似度优先级）：
```
font-family: "Söhne", "Inter", "Suisse Int'l", "Helvetica Neue", Arial, sans-serif;
```
推荐用 **Inter**（开源、grotesk、字形最接近）作为默认实现。整站统一一个字族，仅靠 weight 区分层级。

### 3.2 字号阶梯（实测值，桌面端）

| 层级 | font-size | weight | line-height | letter-spacing | color | 备注 |
|---|---|---|---|---|---|---|
| Stat 巨数字 | `90px` | 500 | `90px` (1.0) | normal | `#000` | 首页大数据（如 “1M+ / 150+”） |
| Hero H1 | `60.8px` | 400 | `60.8px` (1.0) | `-0.01em` | `#333` | 主标题，轻字重大字号 |
| Sub-headline | `28.8px` | 400 | `34.56px` (1.2) | `-0.01em` | `#333`（高亮句用 `#0033FF`） | 标题下的引导句 |
| H3 卡片标题 | `19.2px` | 500 | `23px` (1.2) | `-0.01em` | `#000` | 功能卡/区块小标题 |
| Eyebrow 区块标签 | `18px` | 600 | `25px` | `1.08px` | `#000` | **全大写** `text-transform: uppercase` |
| Body 正文 | `14px` | 400 | `24px` (~1.7) | normal | `#212121` | 段落、说明文字 |
| Nav 导航 | `12px` | 400 | `24px` | normal | `#000`/`#0033FF` | 顶栏菜单 |

要点：
- 大标题一律**负字距**（`-0.01em` 量级），行高压到等于字号（1.0），显紧凑。
- 正文行高反而宽松（1.7 左右），可读性优先。
- 强调色 `#0033FF` 常用于**句中关键词高亮**（同一行里把核心短语染蓝）。

---

## 4. 间距与布局（Spacing & Layout）

- **内容最大宽度**：`max-width: 1200px`（实测容器约 1192px），居中，两侧留白。
- **顶栏（Header）**：
  - 高度 `~69px`，`padding: 22px 24px`
  - 背景**透明**（`transparent`，非 sticky 半透明毛玻璃），底部 `border-bottom: 1px solid #F4F4F4`
  - 左 Logo，中部导航（Marketplace / Solutions / Pricing / Docs / Blog / Community），右侧 GitHub star 数 + 登录/Get Started 按钮
  - 导航项 `padding: 8px 20px` 左右，12px 字号
- **区块（Section）节奏**：上下大 padding（实测 CTA 段 `padding-top: 200px` 级别），靠垂直留白分段，而非分割线。
- **栅格**：功能区常用 2/3/4 列卡片网格，卡片间距 `gap: 16~24px`。
- 间距阶（建议落地 token）：`4 / 8 / 12 / 16 / 24 / 32 / 48 / 64 / 96 / 160 px`。

---

## 5. 圆角（Border Radius）

| Token | 值 | 用途（按出现频率） |
|---|---|---|
| `--radius-pill` | `999px` | 最常用：pill 按钮、标签、badge、头像 |
| `--radius-card` | `12px` | 内容卡片、图片容器 |
| `--radius-lg` | `20px` | 大卡 / 大图容器 |
| `--radius-sm` | `2px` | 部分实心小按钮（如表单/弹窗按钮） |
| `--radius-md` | `6px` | 输入框等 |

默认策略：**交互元素 → pill `999px`；内容容器 → `12px`**。

---

## 6. 描边与阴影（Borders & Shadows）

Dify 的标志性手法是 **1px 浅蓝描边环** 而非投影：

```css
/* 卡片默认描边（最常见，命中 19 次） */
box-shadow: 0 0 0 1px #E5EAFF;

/* 细描边变体 */
box-shadow: 0 0 0 0.75px #E5EAFF;

/* 轻投影（少量大卡用） */
box-shadow: 0 10px 20px rgba(0,0,0,0.05);

/* 强投影（仅悬浮弹层/置顶元素，全站约 1 处） */
box-shadow: 0 32px 68px rgba(0,0,0,0.3);
```

实线边框只在两处出现：
- 按钮描边：`border: 2px solid #0033FF`
- 顶栏底边：`border: 1px solid #F4F4F4`

原则：**优先用 `0 0 0 1px #E5EAFF` 描边环勾卡片**，投影克制使用。

---

## 7. 组件规范（Components）

### 7.1 按钮（Buttons）

**主按钮（Primary / 实心）**
```css
background: #0033FF;
color: #FFFFFF;
border: 2px solid #0033FF;
border-radius: 2px;      /* 弹窗/表单场景；landing CTA 可用 pill 999px */
padding: 8px 16px;
font: 500 14px/1.5 "Inter", sans-serif;
```

**次按钮（Secondary / 描边）**
```css
background: transparent;
color: #0033FF;
border: 2px solid #0033FF;
border-radius: 2px;
padding: 8px 16px;
font-weight: 500;
```

**文字链接按钮**
```css
background: transparent;
color: #0033FF;
border: none;
padding: 0;
font-weight: 400~500;
```

> landing 页 Hero 主 CTA 倾向用 pill（`border-radius: 999px`）+ 实心蓝；表单/Cookie 弹窗里的按钮用 `2px` 小圆角。两者都成立，按场景选。

### 7.2 卡片（Card）
```css
background: #FFFFFF;
border-radius: 12px;
box-shadow: 0 0 0 1px #E5EAFF;   /* 招牌描边环 */
padding: 24px;
```
卡内：H3 标题（19px/500/#000）+ Body 说明（14px/400/#212121），可选顶部小图标或图片（图片容器同样 `12px` 圆角）。

### 7.3 标签 / Badge
```css
background: #E5EAFF;
color: #0033FF;
border-radius: 999px;
padding: 8px 12px;
font-size: 12px;
```
用于「Forum Now Live」类提示胶囊、功能 tag。

### 7.4 Eyebrow 区块小标题
```css
text-transform: uppercase;
font: 600 18px/1.4 "Inter", sans-serif;
letter-spacing: 1.08px;
color: #000;
```
通常出现在大标题上方，如 `BUILD` / `CONNECT` / `ENTERPRISE`。

### 7.5 反色 CTA 区块（Inverted Section）
整段背景 `#0033FF`，内部所有文字 `#FFFFFF`，按钮用白底蓝字反转。用于页面收尾的「Ready to Build…」号召段。

---

## 8. 一个最小可运行的 Tailwind / CSS 主题

```css
:root {
  --color-bg:          #FFFFFF;
  --color-bg-subtle:   #F8F9FB;
  --color-text:        #212121;
  --color-text-strong: #000000;
  --color-text-muted:  #333333;
  --color-accent:      #0033FF;
  --color-accent-tint: #E5EAFF;
  --color-border:      #F4F4F4;

  --radius-pill: 999px;
  --radius-card: 12px;
  --radius-sm:   2px;

  --ring-card: 0 0 0 1px var(--color-accent-tint);

  --font-sans: "Söhne","Inter","Helvetica Neue",Arial,sans-serif;
}

body { background: var(--color-bg); color: var(--color-text);
       font: 400 14px/24px var(--font-sans); }

.h1   { font: 400 60px/1   var(--font-sans); letter-spacing: -0.01em; color: var(--color-text-muted); }
.sub  { font: 400 28px/1.2 var(--font-sans); letter-spacing: -0.01em; color: var(--color-text-muted); }
.h3   { font: 500 19px/1.2 var(--font-sans); color: var(--color-text-strong); }
.eyebrow { font: 600 18px/1.4 var(--font-sans); letter-spacing: 1px;
           text-transform: uppercase; color: var(--color-text-strong); }
.accent  { color: var(--color-accent); }

.btn-primary { background: var(--color-accent); color:#fff; border:2px solid var(--color-accent);
               border-radius: var(--radius-pill); padding:10px 20px; font-weight:500; }
.btn-outline { background: transparent; color: var(--color-accent); border:2px solid var(--color-accent);
               border-radius: var(--radius-pill); padding:10px 20px; font-weight:500; }

.card { background:#fff; border-radius: var(--radius-card); box-shadow: var(--ring-card); padding:24px; }

.header { height:69px; padding:22px 24px; background:transparent;
          border-bottom:1px solid var(--color-border); }

.container { max-width:1200px; margin-inline:auto; padding-inline:24px; }
```

对应 Tailwind 配置片段：
```js
theme: { extend: {
  colors: {
    accent: '#0033FF', 'accent-tint': '#E5EAFF',
    ink: '#212121', muted: '#333333', subtle: '#F8F9FB', hairline: '#F4F4F4',
  },
  borderRadius: { pill: '999px', card: '12px' },
  boxShadow: { ring: '0 0 0 1px #E5EAFF', card: '0 10px 20px rgba(0,0,0,0.05)' },
  fontFamily: { sans: ['Söhne','Inter','Helvetica Neue','Arial','sans-serif'] },
  maxWidth: { container: '1200px' },
}}
```

---

## 9. 复刻检查清单（Do / Don't）

**Do**
- 全站只用一种强调色 `#0033FF`；其余靠黑白灰。
- 大标题用轻字重 + 超大字号 + 负字距 + 行高 1.0。
- 卡片用 `box-shadow: 0 0 0 1px #E5EAFF` 描边环。
- 交互元素 pill 圆角、内容卡 12px 圆角。
- 区块靠留白分段；eyebrow 全大写带字距。
- 句中关键词用蓝色高亮。

**Don't**
- 不要用多彩渐变、霓虹光晕、重投影。
- 不要给正文加粗充当层级（用字号区分）。
- 不要引入第二个品牌强调色。
- 不要用厚重边框线分割区块（除顶栏极浅底边）。
- 不要把浅蓝 `#E5EAFF` 用作大面积背景（它只做描边/标签）。

---

## 10. 数据来源说明

以上 token 全部来自 `https://dify.ai` 首页线上 computed styles 实测（2026-06）。Söhne 为商业授权字体，复刻实现时用 **Inter** 作开源替代即可获得高度近似的观感。
