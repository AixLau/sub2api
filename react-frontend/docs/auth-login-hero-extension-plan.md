# Auth Login Hero Extension Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the `/login` auth page as a natural extension of the homepage hero, while preserving the existing registration, reset-password, and change-password routes.

**Architecture:** Keep the existing React/Vite/TypeScript app and shared `AuthPage` component. Reuse the homepage visual language from `src/styles/alwayzz.css`: white space, cyan/blue/green side glow, arc motion lines, black pill buttons, and restrained glass-like surfaces.

**Tech Stack:** React 19, Vite, TypeScript, Vitest, Testing Library, GSAP, lucide-react, custom CSS.

---

## File Structure

- Modify: `src/App.test.tsx`
  - Add/adjust tests that lock the login page information architecture, preserved links, hidden social/name fields, and homepage-style motion.
- Modify: `src/components/AuthPage.tsx`
  - Add mode-aware brand-side copy for login/register/reset/change-password.
  - Replace the login-side numbered onboarding flow with three concise capability pills.
  - Preserve existing route links, password visibility toggle, form fields, autocomplete values, and button types.
- Modify: `src/styles/alwayzz.css`
  - Refine the auth layout into a homepage hero extension.
  - Lighten the left brand area so it no longer reads as a heavy white card.
  - Restyle the right form as a lightweight, premium, white/glass panel.
  - Improve responsive behavior for desktop, tablet, and mobile.

## Chunk 1: Lock Desired Login Behavior With Tests

### Task 1: Add Login Hero Extension Assertions

**Files:**
- Modify: `src/App.test.tsx`

- [ ] **Step 1: Write the failing test**

Add assertions for `/login`:

```tsx
window.history.pushState({}, '', '/login')
const { container } = render(<App />)

expect(screen.getByRole('heading', { name: '继续接入模型 API' })).toBeInTheDocument()
expect(screen.getByText('模型 API 服务入口')).toBeInTheDocument()
expect(screen.getByText('统一管理模型服务、API Key、调用用量与接入配置。')).toBeInTheDocument()
expect(screen.getByText('稳定调用')).toBeInTheDocument()
expect(screen.getByText('用量清晰')).toBeInTheDocument()
expect(screen.getByText('模型统一接入')).toBeInTheDocument()
expect(screen.getByRole('heading', { name: '登录星链' })).toBeInTheDocument()
expect(screen.getByRole('link', { name: '创建账号' })).toHaveAttribute('href', '/register')
expect(screen.getByRole('link', { name: '找回密码' })).toHaveAttribute('href', '/reset-password')
expect(container.querySelector('.auth-feature-list')).toBeInTheDocument()
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run: `npm test -- src/App.test.tsx`

Expected: FAIL because the login side still uses the old registration-oriented heading and numbered steps.

## Chunk 2: Rebuild Shared Auth Content

### Task 2: Implement Mode-Aware Brand Content

**Files:**
- Modify: `src/components/AuthPage.tsx`

- [ ] **Step 1: Add typed side content**

Create a typed `authSideCopy` object keyed by `AuthMode`. The login entry must use:

```ts
{
  eyebrow: '模型 API 服务入口',
  title: '继续接入模型 API',
  subtitle: '统一管理模型服务、API Key、调用用量与接入配置。',
  features: ['稳定调用', '用量清晰', '模型统一接入'],
}
```

- [ ] **Step 2: Replace hard-coded side copy**

Use `sideCopy = authSideCopy[mode]` inside `AuthPage`.

- [ ] **Step 3: Replace the login numbered flow**

Render an `.auth-feature-list` of `.auth-feature-pill` elements for side features. Do not add social login buttons or name fields.

- [ ] **Step 4: Preserve existing logic**

Keep:
- `/register`, `/login`, `/reset-password`, `/change-password` hrefs.
- email/password labels and autocomplete attributes.
- password show/hide state and button labels.
- `type="button"` for auth submit buttons because there is no real auth backend yet.

- [ ] **Step 5: Run the focused test to verify it passes**

Run: `npm test -- src/App.test.tsx`

Expected: PASS for the login assertions, or only CSS-related assertions still pending.

## Chunk 3: Apply Homepage Hero Extension Styling

### Task 3: Refine Auth Layout CSS

**Files:**
- Modify: `src/styles/alwayzz.css`

- [ ] **Step 1: Update `.auth-shell`**

Keep the homepage background image variable and align the auth background with the homepage: large white base, low-contrast side glow, restrained arc/motion lines.

- [ ] **Step 2: Lighten `.auth-visual`**

Remove the heavy-card impression. The left side should feel open and brand-led, with subtle translucency only if needed.

- [ ] **Step 3: Add `.auth-feature-list` and `.auth-feature-pill`**

Use small rounded pills similar to homepage ticker pills: compact, soft white, gray text, subtle border.

- [ ] **Step 4: Restyle `.auth-card` and form controls**

Make the form panel 420-520px wide on desktop, with a lightweight glass-white surface, soft border, restrained shadow, and black pill submit button aligned with homepage CTAs.

- [ ] **Step 5: Improve responsive behavior**

Ensure:
- 1440px and 1280px desktop keep balanced two-column layout.
- 768px tablet remains readable without horizontal scroll.
- 375px mobile stacks content cleanly and keeps inputs/buttons full-width.

- [ ] **Step 6: Run tests after styling**

Run: `npm test -- src/App.test.tsx`

Expected: PASS.

## Chunk 4: Verification

### Task 4: Run Full Project Checks

**Files:**
- Read only: `package.json`

- [ ] **Step 1: Run test suite**

Run: `npm test`

Expected: PASS.

- [ ] **Step 2: Run typecheck**

Run: `npm run typecheck`

Expected: PASS.

- [ ] **Step 3: Run production build**

Run: `npm run build`

Expected: PASS.

- [ ] **Step 4: Visual browser check**

Open/check:
- `http://127.0.0.1:4173/login`
- `http://127.0.0.1:4173/register`

Confirm:
- Login feels like the homepage hero extension.
- Left side is brand content, not a heavy white card.
- Right side is a restrained product-entry form.
- Register still has email/password only.
- No social buttons or name fields appear.
- No horizontal scroll at mobile width.

