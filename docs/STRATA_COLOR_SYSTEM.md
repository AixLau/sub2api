# Sub2API Layered Electric Color System

> Status: v1.0, 2026-07-31
> Scope: public pages, authentication, user console, admin console, payment, monitoring, charts, dialogs and shared components.

## 1. Design Intent

The reference images share a clear visual language rather than merely a single blue:

- **Operational precision.** Repeated lines, stripes and layered planes suggest coordinated infrastructure, speed and systems working as one.
- **Binary brand contrast.** Saturated electric blue is paired with pure white and an almost-black navy. Color is used in large, decisive fields rather than as ambient decoration.
- **Measured energy.** Blue carries the visual focus; supporting surfaces stay quiet. Saturation is concentrated in actions, selected states, key metrics and brand moments.
- **Editorial confidence.** Large type, generous negative space and simple geometry create authority. In the product console this is translated into clear hierarchy and compact operational density rather than oversized marketing composition.
- **Controlled depth.** Borders and tonal surface steps define most hierarchy. Shadows are cool, low-opacity and reserved for raised or transient UI. Gradients are short tonal shifts within the blue family, never rainbow decoration.

The system is named **Layered Electric**. It transfers the reference style without copying the Strata identity or changing Sub2API's information architecture.

## 2. Color Architecture

The machine-readable source is [`frontend/design-tokens.json`](../frontend/design-tokens.json). Tailwind consumes the same file, so legacy utilities and new semantic utilities resolve from one palette.

### 2.1 Brand scale

| Token | Value | Primary use |
| --- | --- | --- |
| `brand-50` | `#F5F7FF` | Subtle selected rows and quiet information fills |
| `brand-100` | `#E5EAFF` | Info backgrounds, active navigation fills |
| `brand-300` | `#A8B7FF` | Dark-mode brand text and borders |
| `brand-400` | `#6E8CFF` | Dark-mode focus and secondary emphasis |
| `brand-500` | `#0033FF` | Primary action, switch-on, progress and brand anchor |
| `brand-600` | `#0029CC` | Hover and readable brand text on light surfaces |
| `brand-700` | `#0022B0` | Active/pressed state |
| `brand-950` | `#000B3D` | Deep branded fill, used sparingly |

Electric blue is not a general background tint. On operational screens it should occupy roughly 5-15% of the visible interface; white/neutral surfaces and content carry the rest.

### 2.2 Neutral and navy scales

- `neutral-*` is a blue-biased gray family for light-mode text, borders and surfaces.
- `navy-*` is the dark-mode structural family. `navy-950` (`#050816`) is the canvas and `navy-900` (`#090F24`) is the default panel.
- Pure white remains the strongest inverse foreground and the default light panel.
- Pure black is limited to third-party marks or media; interface text uses `content-primary`.

### 2.3 Accent and semantic colors

Ice blue (`accent-*`) is a supporting signal for data visualization and subtle highlights. It must not compete with the primary action.

Success, warning and danger remain hue-distinct for recognition:

| Meaning | Light foreground | Light soft fill | Dark foreground | Dark soft fill |
| --- | --- | --- | --- | --- |
| Success | `#067647` | `#ECFDF3` | `#6CE9A6` | `#0B2D24` |
| Warning | `#B54708` | `#FFFAEB` | `#FEC84B` | `#33250B` |
| Danger | `#B42318` | `#FEF3F2` | `#FDA29B` | `#351316` |
| Info | `#0033FF` | `#E5EAFF` | `#A8B7FF` | `#101C5C` |

Each status also has a complete `50-950` primitive scale. The semantic foregrounds anchor to `success-600`, `warning-700` and `danger-700` in light mode, and their lighter `300` steps in dark mode. Legacy Tailwind `green/emerald`, `amber/yellow` and `red/rose` utilities map to these centralized scales, so older business screens inherit the same status language without per-page color tables.

Provider colors (Stripe, Alipay, WeChat Pay, Airwallex) and platform identity colors are scoped exceptions. They identify an external entity; they do not replace the product's primary action color. Provider UI colors are still centralized under `provider-*` tokens so identity treatments do not drift between selectors, QR flows and payment buttons.

### 2.4 Provider identity tokens

| Provider | Core token | Supporting tokens | Rule |
| --- | --- | --- | --- |
| Stripe | `provider-stripe` | `-hover`, `-dark`, `-selection`, `-secondary` | Stripe-owned payment surfaces only |
| Airwallex | `provider-airwallex` | `-hover`, `-dark`, `-selection`, `-deep` | Airwallex-owned payment surfaces only |
| Alipay | `provider-alipay` | `-hover`, `-active`, `-selection`, `-deep` | Alipay icon, handoff and QR framing |
| WeChat Pay | `provider-wechat` | `-hover`, `-active`, `-selection`, `-official`, `-deep` | WeChat Pay icon, handoff and QR framing |

The distinction between `provider-*` and product `brand-*` is intentional: provider color answers "which payment network?", while brand color answers "what is selected or actionable in Sub2API?".

Platform identities such as Anthropic, OpenAI, Gemini, Grok, Antigravity, Composite, Meta and Mistral use the separate `platform-*` family. Platform color answers "which model ecosystem?" and must not be sourced from product brand or business status aliases. UI code resolves those identities through `src/utils/platformColors.ts`; aliases such as Claude/Anthropic, Google/Gemini and xAI/Grok therefore share one treatment.

## 3. Semantic Tokens

Use semantic tokens in new code. Primitive utilities remain available for data visualization, provider identity and compatibility.

| Semantic group | Tokens | Purpose |
| --- | --- | --- |
| Surface | `surface-canvas`, `surface-panel`, `surface-subtle`, `surface-raised`, `surface-inverse`, `surface-scrim` | Page, card, muted region, popup, inverse region and modal backdrop |
| Content | `content-primary`, `content-secondary`, `content-tertiary`, `content-disabled`, `content-inverse`, `content-on-brand`, `content-brand` | Text and icons by importance or owning surface |
| Line | `line-subtle`, `line-default`, `line-strong`, `line-focus` | Dividers, controls and focus |
| Status | `status-{success|warning|danger|info}`, plus `-soft` and `-border` | Feedback states |

Examples:

```html
<section class="bg-surface-panel text-content-primary border border-line-subtle">
  <p class="text-content-secondary">Secondary information</p>
</section>

<button class="bg-primary-500 text-content-on-brand hover:bg-primary-600 active:bg-primary-700">
  Confirm
</button>
```

Dark mode is not authored component by component for these semantic tokens. The `.dark` theme swaps their values globally.

### 3.1 Inverse surfaces, brand foregrounds and scrims

These semantics must not be substituted for one another:

- `surface-inverse` and `content-inverse` are a paired surface treatment. They intentionally reverse from dark-on-light to light-on-dark when the theme changes.
- `content-on-brand` is always white because the electric-blue brand scale stays dark enough for white text in both themes.
- `surface-scrim` is always black or near-black. It never reverses to white in dark mode and is the only semantic surface for modal, drawer and full-screen backdrop dimming.

## 4. Page Hierarchy

### Light mode

1. Canvas: `surface-canvas`, a cool off-white.
2. Primary panels/cards: `surface-panel`.
3. Grouped or inactive regions: `surface-subtle`.
4. Raised menus/dialogs: `surface-raised` with a stronger shadow and `line-default`.
5. Inverse brand regions: `surface-inverse` with `content-inverse`.

### Dark mode

1. Canvas: near-black navy, not neutral gray.
2. Panels: one visible tonal step above the canvas.
3. Raised controls and popovers: another step above panels.
4. Borders carry structure; large blue-tinted glows do not.
5. Electric blue stays saturated on buttons and selected states, while text uses lighter brand tones for contrast.

### Fixed-light public skin

The public landing and authentication shell intentionally keeps the reference's editorial light composition even when the console preference is dark. `.landing-shell` rebinds semantic variables to the light theme rather than maintaining a second hard-coded palette. Its black/white alpha layers are optical treatments local to that skin; product controls within the shell still use the shared brand and status tokens.

## 5. Component Rules

### Navigation

- Default items use secondary content.
- Hover adds a subtle neutral surface.
- Active items use a soft brand fill, brand text and a narrow electric-blue indicator.
- Headers and sidebars are separated with subtle lines; no heavy shadow is used for permanent chrome.

### Buttons

| Variant | Default | Hover | Active | Disabled |
| --- | --- | --- | --- | --- |
| Primary | `brand-500`, `content-on-brand` | `brand-600` | `brand-700` | 50% opacity, no lift/shadow |
| Secondary | Panel + default line | Subtle surface + strong line | Subtle surface | Disabled content and surface |
| Ghost | Transparent | Subtle surface | Stronger neutral fill | Disabled content |
| Danger | Solid danger | One tonal step darker | Strong danger | Same disabled rule |

Primary buttons use a restrained blue-to-blue gradient only where the existing component already uses a gradient. Do not mix blue with purple.

### Inputs, selects and text areas

- Default: panel surface, default line and primary content.
- Hover: strong line.
- Focus: brand border plus a 2px low-opacity focus ring.
- Error: danger border/ring and danger helper text.
- Disabled: subtle surface, disabled content and no hover response.
- Placeholder: tertiary content; it must remain distinguishable from entered text.

### Cards and panels

- Default cards are defined by a subtle border and a small cool shadow.
- Hover elevation is for actionable cards only.
- Nested cards are avoided; use surface bands, dividers or grouped rows inside a card.
- Brand fills are reserved for a highlighted metric, onboarding moment or selected plan, not every card header.

### Tables

- Header: subtle surface and tertiary/secondary content.
- Body: panel surface and primary content.
- Row hover: subtle surface.
- Selected row: `brand-50`/dark brand soft fill plus a brand selection control.
- Grid lines are quiet; status must not depend on row color alone.

### Badges and status

- Use a soft fill, readable semantic foreground and optional semantic border.
- Info uses the brand family.
- Success, warning and danger keep their own hues.
- Neutral metadata uses neutral tokens; provider badges may keep provider identity colors.

### Dialogs, dropdowns and toasts

- Overlay: `surface-scrim` at 48-64% opacity with restrained blur. Media previews may increase opacity to 75-80%.
- Panel: raised surface, default line and cool shadow.
- Default focus lands on the first meaningful control.
- Toasts combine an icon, text and semantic edge/progress color; color is never the only signal.

### Loading and empty states

- Skeletons alternate between subtle and default neutral fills.
- Spinners use brand color with a transparent track segment.
- Loading states preserve layout and do not introduce a new accent hue.
- Empty-state icons use disabled content; the primary recovery action uses the standard primary button.

## 6. State Matrix

| State | Color treatment |
| --- | --- |
| Default | Neutral surface, content and line tokens |
| Hover | One surface/line step stronger; no hue change for neutral controls |
| Selected | Soft brand fill + brand foreground + visible selection mark |
| Focus visible | `line-focus` and a 2px brand ring; never remove the outline without replacement |
| Pressed | `brand-700` for primary actions; neutral controls use stronger neutral fill |
| Disabled | Disabled content, subtle surface, 50-60% opacity where necessary |
| Loading | Preserve label width where possible; brand spinner/progress |
| Success | Success icon/text/fill plus explicit copy |
| Warning | Amber icon/text/fill plus explicit copy |
| Error | Danger border/icon/text plus recovery guidance |

## 7. Legacy Mapping

The compatibility layer intentionally remaps existing utilities so the whole product moves together.

| Previous system | New mapping |
| --- | --- |
| Teal `primary-*` | Electric `brand-*`; existing `primary-*` class names remain valid |
| Legacy `blue-*` / `indigo-*` utilities | Electric `brand-*` scale |
| Legacy `cyan-*` / `teal-*` utilities | Supporting ice-blue `accent-*` scale |
| Legacy `green-*` / `emerald-*` utilities | Central `success-*` scale |
| Legacy `amber-*` / `yellow-*` utilities | Central `warning-*` scale |
| Legacy `red-*` / `rose-*` utilities | Central `danger-*` scale |
| Slate `accent-*` | Ice-blue `accent-*` |
| Tailwind gray/slate defaults | Blue-biased `neutral-*`; existing `gray-*` and `slate-*` class names remain valid |
| Slate `dark-*` | Near-black `navy-*`; existing `dark-*` class names remain valid |
| `bg-gray-50` page canvas | Prefer `bg-surface-canvas` in new/shared code |
| `bg-white dark:bg-dark-800` panels | `bg-surface-panel` or `bg-surface-raised` |
| `text-gray-900 dark:text-white` | `text-content-primary` |
| `text-gray-500 dark:text-dark-400` | `text-content-tertiary` |
| `border-gray-200 dark:border-dark-700` | `border-line-default` or `border-line-subtle` |
| Raw `#0033FF` / `#E5EAFF` | `primary-500` / `primary-100` or semantic info tokens |
| Raw teal focus/glow values | Brand CSS variables and focus tokens |
| Repeated payment-provider literals | `provider-*` Tailwind colors or `providerColors` in TypeScript |

## 8. Consistency Principles

1. Choose color by meaning, not by the closest visual primitive.
2. A page may have one dominant call to action. Secondary actions stay neutral.
3. Use blue for brand, selection, focus and informational emphasis; do not use it to mean success.
4. Keep permanent structure border-led and transient UI shadow-led.
5. Do not introduce a new hard-coded product color in Vue/CSS. Add or reuse a token.
6. External provider colors, destructive states and categorical chart series are explicit exceptions, but each must come from its centralized token source.
7. Verify light and dark themes together, including hover, focus, disabled, empty and error states.
8. Maintain WCAG AA contrast: 4.5:1 for normal text, 3:1 for large text and meaningful UI boundaries.
9. Color cannot be the only carrier of state; pair it with text, iconography, shape or position.
10. Decorative gradients must remain tonal, low-opacity and subordinate to content.

## 9. Adding New Pages and Components

1. Start with semantic surface/content/line tokens.
2. Reuse `.btn`, `.input`, `.card`, `.table`, `.badge`, `.modal-*` and shared Vue components before adding local styles.
3. Use `primary-*` for brand actions and selections; use `status-*` for feedback.
4. Import chart colors from `src/theme/designTokens.ts`; do not create a local categorical array.
5. Keep third-party and platform identity colors inside the smallest identity-specific component. Payment charts and badges use `paymentMethodColorClass`; model/platform UI uses the helpers in `platformColors.ts`. Both resolve to the centralized `provider-*` and `platform-*` tokens.
6. Test both themes and keyboard focus at desktop and mobile widths.
7. If a new semantic need cannot be expressed, extend `design-tokens.json`, document it here, and expose it through Tailwind before using it.
8. Use `surface-scrim` for modal and drawer backdrops; do not use `surface-inverse`, because inverse surfaces intentionally become white in dark mode.

## 10. Review Checklist

- No raw former teal brand values remain in product UI.
- Page canvas, panels, raised surfaces and inverse regions are visually distinct.
- Primary action, selected state and focus state are related but not identical.
- Dark surfaces do not collapse into one undifferentiated navy block.
- Text and icons remain readable on every surface.
- Status hues retain consistent meanings.
- Charts begin with brand blue but keep categorical separation.
- Provider branding remains recognizable without leaking into product controls.
- Payment selectors, QR screens, popup handoffs and buttons share the same provider identity tokens.
- Public, auth, user, admin, payment and modal surfaces feel like one system.
- Modal, drawer and full-screen backdrops remain dark in both themes.
