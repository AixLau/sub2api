# AGENTS.md - 星链 Homepage Direction

> **DEPRECATED（2026-07-26，维护者决定）**：星链落地页与认证页已迁移进 `frontend/`（Vue 主站），
> 样式基座为 `frontend/src/styles/landing.css`，组件在 `frontend/src/components/landing/`，
> 认证外壳为 `frontend/src/components/layout/AuthLayout.vue`，路由默认由 Vue 应用直接提供
> （`VITE_REACT_LANDING_ROUTES` 保持未设置即可）。新的落地页改动请在 `frontend/` 进行；
> 本包仅供仍在使用双服务部署（`VITE_REACT_LANDING_ROUTES=true`）的环境过渡，删除时机由维护者决定。
> 下文的 "Do not migrate to Vue" 约定已被本次决定取代。

## Project

Build and maintain the current 星链 website prototype.

星链 is an AI large-model API service provider. The site should present a simple, stable, elegant API service layer for teams that need model capabilities without heavy technical friction.

The current product name is:
- 中文: 星链
- English fallback, only when needed: Starlink API

Do not revert the project back to the original Alwayzz creative agency business. The original Alwayzz prompt is only visual/layout inspiration.

## Current Stack

The active app is:
- React
- Vite
- TypeScript
- custom CSS in `src/styles/alwayzz.css`
- lucide-react icons

Do not migrate the active work to Vue, Tailwind, a dashboard framework, or the Aurora black-video registration prompt unless the user explicitly asks.

This package intentionally keeps only the React/Vite landing and auth-entry app. Do not add parallel Vue, Tailwind, or alternate landing implementations here unless the user explicitly asks for a new app architecture.

Active entry and core files:
- `src/main.tsx`
- `src/App.tsx`
- `src/components/Navbar.tsx`
- `src/components/HeroSection.tsx`
- `src/components/TrustedBySection.tsx`
- `src/components/Marquee.tsx`
- `src/components/AuthPage.tsx`
- `src/data/alwayzz.ts`
- `src/styles/alwayzz.css`

## Routes

Current routes are handled in `src/App.tsx`:
- `/` homepage
- `/login`
- `/register`
- `/reset-password`
- `/change-password`

Auth pages are prototype pages only. Do not add a backend, database, real authentication, API keys, payment, admin, or dashboard unless the user explicitly asks.

## Visual Direction

Use the original Alwayzz prompt as the visual foundation:
- minimal, premium, clean black-and-white surface
- warm liquid hero background
- tight, controlled typography
- fixed top navigation
- curved line animations around the hero
- marquee/ticker motion
- trusted brand row below the hero
- responsive layout that still feels polished at browser zoom and smaller desktop resolutions

Keep the homepage architecture lightweight. The user prefers not to add excessive copy or extra sections unless requested.

Avoid:
- generic blue AI gradients
- code terminal hero
- cyberpunk/neon/space/network-node visuals
- robot imagery
- dense SaaS dashboard cards
- heavy marketing paragraphs
- fake technical guarantees or fake compliance claims

## Required Background

Do not replace the warm liquid hero background unless the user explicitly asks.

Exact current hero background image:

```text
https://images.higgs.ai/?default=1&output=webp&url=https%3A%2F%2Fd8j0ntlcm91z4.cloudfront.net%2Fuser_38xzZboKViGWJOttwIXH07lWA1P%2Fhf_20260626_041422_4a459e05-abce-4150-9fb7-4ededc423cd1.png&w=1280&q=85
```

## Homepage Copy Rules

Use Chinese UI copy by default.

Current hero direction:
- headline: `让模型 API 接入，像光一样自然。`
- subtitle: `通过一套稳定的 API 服务，接入主流模型能力，减少复杂配置，让团队把注意力留给产品体验。`
- primary CTA: `开始接入`
- secondary CTA: `查看服务能力`

Keep copy short, calm, and product-related.

Do not use visible `GPT-like` wording. The user explicitly asked to remove it.

Do not add `商业可用` to the scrolling ticker unless the user explicitly asks.

Avoid unsupported claims such as:
- 官方合作
- 全球领先
- 企业级保障
- guaranteed uptime
- verified partnerships
- direct affiliation with OpenAI, Anthropic, Google, Meta, or any model provider

## Trusted Brands

Keep this label unchanged unless the user explicitly asks:

```text
Partnered with top-tier companies globally
```

Use foreign company names in the brand marquee. Current examples:
- Airbnb
- Shopify
- Notion
- Linear
- Webflow
- Figma
- Slack
- Stripe
- Vercel
- Framer

Do not imply these are verified customers or official partners. This row is a visual/trust placeholder in the prototype.

## Auth Page Direction

Login/register/reset/change-password pages should be separate routes reached by clicking navbar buttons or links.

Keep auth pages visually consistent with the homepage:
- use the same warm liquid background asset
- preserve the refined minimal style
- keep Chinese labels and concise copy
- keep the two-column registration/login composition on large screens
- keep mobile layouts clean and readable

The Aurora Sign Up prompt may be used only as layout inspiration. Do not copy its black video style, Aurora branding, or required CloudFront video into this project unless the user asks.

## Responsiveness

The homepage must adapt to:
- different desktop resolutions
- browser zoom changes
- tablet/mobile widths

Important current requirement:
- users should still be able to see or quickly reach the `Partnered with top-tier companies globally` row and brand marquee when opening the homepage.

Avoid fixed heights or typography that cause the trusted row to disappear on common laptop screens.

## Implementation Style

Follow existing React component and CSS patterns.

Prefer:
- typed arrays in `src/data/alwayzz.ts`
- small focused React components
- CSS variables and custom CSS classes
- accessible links/buttons and labels
- stable responsive dimensions with `clamp`, `svh`, `min`, `max`, and media queries where useful

Do not add large new dependencies for small visual changes.

Use lucide-react icons that exist in the installed version. If a requested icon is unavailable, choose a close available icon and keep the design consistent.

## Verification

Before claiming implementation work is complete, run the available checks:

```bash
npm test
npm run typecheck
npm run build
```

There is currently no `npm run lint` script. If linting is requested, either add a script deliberately or state that it is not available.

For visual/responsive changes, also inspect the page in the browser at the running local URL, commonly:

```text
http://127.0.0.1:4173/
```

Use the active dev/preview server already running when possible.
