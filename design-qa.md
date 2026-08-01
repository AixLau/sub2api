# Header Card Design QA

- Source visual truth: `/Users/lushiwu/Desktop/微信图片_20260718000258_4_769.jpg`
- Browser-rendered implementation: `/Users/lushiwu/.codex/visualizations/2026/07/21/019f84db-4622-7af3-825e-b0e971c475f8/wallet-desktop-open-v3.png`
- Header recharge verification: `/Users/lushiwu/.codex/visualizations/2026/07/21/019f84db-4622-7af3-825e-b0e971c475f8/wallet-header-recharge-v5.png`
- Focused implementation region: `/Users/lushiwu/.codex/visualizations/2026/07/21/019f84db-4622-7af3-825e-b0e971c475f8/wallet-panel-focused-v5.png`
- Full-view comparison: `/Users/lushiwu/.codex/visualizations/2026/07/21/019f84db-4622-7af3-825e-b0e971c475f8/wallet-comparison-v5.png`
- Focused comparison: `/Users/lushiwu/.codex/visualizations/2026/07/21/019f84db-4622-7af3-825e-b0e971c475f8/wallet-focus-comparison.png`

## Viewport And State

- Source pixels: 962 x 952.
- Implementation pixels and CSS viewport: 1212 x 720 at device scale factor 1.
- Focused wallet panel: 304 x 260 CSS pixels.
- Density normalization: source and focused implementation were each displayed at 500 CSS pixels wide in the focused comparison.
- State: authenticated mock user, desktop header, wallet panel open, light theme.

## Fidelity Review

- Fonts and typography: system sans-serif, centered hierarchy, restrained label, strong balance value, and compact CTA weight match the reference's visual hierarchy.
- Spacing and layout rhythm: the implementation preserves the reference's image-over-content split, centered composition, generous white space, continuous rounded silhouette, and floating shadow while scaling the card down for a header popover.
- Colors and visual tokens: the generated cobalt, indigo, violet, lavender, and white-highlight artwork matches the reference palette; the content area remains neutral white with a near-black primary CTA.
- Image quality and asset fidelity: a real 1248 x 416 raster artwork is used at the top of the card. It loads at its natural dimensions and is cropped with `object-cover`; no CSS gradient or placeholder replaces the reference imagery.
- Copy and content: the card contains only `可用余额`, the formatted balance, and `立即充值`. Frozen and total balance rows remain removed; the top-level `充值` shortcut is intentionally visible beside the wallet trigger.

## Findings

- P0/P1/P2: none.
- P3: the generated fluid artwork is not pixel-identical to the reference, but it matches the subject, palette, softness, crop, and promotional art direction. This is acceptable because the source is a style reference rather than a supplied production asset.

## Interaction And Browser Checks

- Hovering the wallet or subscription control opens its panel temporarily. Clicking its trigger keeps the panel open until the user clicks outside or selects its close control.
- Close button hides the panel.
- The top-level `充值` shortcut is visible beside the wallet trigger and routes directly to `/purchase`.
- Recharge button closes the panel and calls the existing `/purchase` route; the focused Vitest assertion confirms the route value.
- The artwork loaded successfully at 1248 x 416 natural pixels.
- No wallet-component console errors occurred. The isolated preview logged expected subscription-fetch errors from `SubscriptionProgressMini` because no backend session was connected.

## Comparison History

- Earlier P1: the panel used a code-native blue-indigo gradient instead of the reference's fluid image treatment, so it did not carry the selected visual style.
- Fix: generated and integrated a dedicated blue-violet fluid raster asset, rebuilt the panel as a compact image-and-white-content card, added the image close control, removed repeated balance details, and restored a high-contrast top-level recharge shortcut after review feedback.
- Post-fix evidence: `wallet-comparison-v5.png` confirms the full component context; `wallet-focus-comparison.png` confirms the artwork, rounded silhouette, content hierarchy, and CTA treatment at readable scale.

## Support Community Card QA

- Source visual truth: `/var/folders/j_/wj8mmnfx46zccr8bvxj1wklh0000gn/T/codex-clipboard-51e4571d-5111-458a-a751-1ac24b0bd847.png`
- Browser-rendered implementation: `/Users/lushiwu/.codex/visualizations/2026/07/22/019f89ce-0974-79d3-b435-38555dec5d10/support-preview-enlarged-390.png`
- Source pixels: 796 x 896; the source is a tightly cropped card reference with unknown device density.
- Implementation pixels and CSS viewport: 1280 x 720 at device scale factor 1.
- Implementation dialog: 344 x 434 CSS pixels; QR frame: 296 x 240 CSS pixels.
- Responsive checks: 344 x 434 at 390 x 844 with no page overflow; 288 x 454 at 320 x 700 with no page overflow.
- State: authenticated mock user, support dialog open, QQ tab selected, light theme.
- Full-view comparison evidence: the source and implementation were opened together at original pixel dimensions. A separate focused crop was unnecessary because the complete dialog is fully legible in both images.
- Fonts and typography: the implementation retains the source hierarchy, weights, line heights, and neutral system sans-serif styling without introducing new wrapping at desktop width.
- Spacing and layout rhythm: the dialog width increases from 304 to 344 CSS pixels and the QR frame height from 208 to 240 CSS pixels while preserving the 26px outer radius, 18px QR-frame radius, centered alignment, and existing padding rhythm.
- Colors and visual tokens: white surface, neutral border, dark selected tab, subdued secondary text, backdrop, and shadow remain unchanged from the accepted card style.
- Image quality and asset fidelity: the QR image continues to use the configured bitmap with `object-contain`, so it remains sharp and uncropped as the frame grows. The preview QR is black-and-white test data; production color depends on the independently configured image.
- Copy and content: title, helper text, QQ/WeChat tabs, and scan hint are unchanged.
- Interaction and browser checks: QQ is selected by default, the dialog opens from the header support button, the enlarged card stays centered, and a clean preview load produced no console errors.
- Findings: no actionable P0/P1/P2 differences. The larger dimensions are intentional and directly address the requested scan-area increase.
- Comparison history: the earlier implementation measured 304 x 422 CSS pixels with a 208px QR frame. It was widened to 344px and the QR frame increased to 240px; post-fix desktop, 390px, and 320px measurements confirm the card remains centered and overflow-free.

final result: passed

# Layered Electric Color System QA

**Findings**

- No actionable P0, P1, or P2 findings remain.
- P3: the dark dashboard's empty-state label wraps to two lines at 390 px. It remains inside its panel, does not overlap adjacent content, and does not obscure an action, so it is recorded as non-blocking polish.

**Open Questions**

- None. The ten source images are a cross-media brand moodboard rather than one application screen. This QA therefore checks faithful transfer of palette, contrast, hierarchy, line rhythm, depth, and state behavior without claiming a pixel-for-pixel page clone.

**Source Visual Truth**

- Source directory: `/var/folders/j_/wj8mmnfx46zccr8bvxj1wklh0000gn/T/`
- Source files:
  - `codex-clipboard-f7a6aa80-12fd-430b-9e5d-abbcb948068f.png`
  - `codex-clipboard-5c918424-a7e3-4b15-b108-04b0285f7063.png`
  - `codex-clipboard-922b5d2a-7e1c-4bda-b625-e5b56e1b199b.png`
  - `codex-clipboard-7d80b95e-dbd2-4510-b2c6-74254a3d1d92.png`
  - `codex-clipboard-7e10f9ca-0b5b-4041-b975-7720bed308b6.png`
  - `codex-clipboard-3949c736-e884-4dc9-9ee3-bcafd608bc1c.png`
  - `codex-clipboard-d452b2c6-7406-4fe0-9ae9-7b5d410a29e9.png`
  - `codex-clipboard-c5b5846d-5baf-4359-bc3f-ea56326744c1.png`
  - `codex-clipboard-2ec05120-eff7-4d7f-897e-f10142a11aac.png`
  - `codex-clipboard-afb44f93-50d7-4e11-beb2-e79cae3fcf25.png`
- Every source bitmap is 1080 x 1449 px. These are artwork boards, not browser viewports, so CSS viewport and device density do not apply.
- Normalized source board: `/Users/lushiwu/.codex/visualizations/2026/07/30/019fb41f-b36f-73d0-96aa-414a3c3eeb05/layered-electric-source-moodboard.png` at 1410 x 958 px. Each source was proportionally contain-fitted; no crop or hue adjustment was applied.

**Implementation Evidence**

- Preview: `http://127.0.0.1:4173`
- Capture density: 1x.
- Representative captures:

| Route or state | Screenshot | Pixels / CSS viewport |
| --- | --- | --- |
| Public home, desktop | `layered-electric-home-desktop.png` | 1440 x 900 / 1440 x 900 |
| Public home, mobile after overflow fix | `layered-electric-home-mobile-fixed.png` | 390 x 844 / 390 x 844 |
| Mobile navigation open | `layered-electric-home-menu-mobile.png` | 390 x 844 / 390 x 844 |
| Login, desktop focus after fix | `layered-electric-login-focus-fixed.png` | 1440 x 900 / 1440 x 900 |
| Login, mobile | `layered-electric-login-mobile.png` | 382 x 827 content capture / 390 x 844 browser viewport |
| Recharge, desktop selected state | `layered-electric-purchase-desktop-light.png` | 1272 x 795 content capture / 1280 x 800 browser viewport |
| Recharge, error and disabled state | `layered-electric-purchase-error-state.png` | 1272 x 795 content capture / 1280 x 800 browser viewport |
| Subscription, mobile selected state | `layered-electric-subscription-mobile.png` | 382 x 827 content capture / 390 x 844 browser viewport |
| Dialog, light | `layered-electric-dialog-light.png` | 1280 x 800 / 1280 x 800 |
| Dashboard, dark | `layered-electric-dashboard-dark.png` | 1280 x 800 / 1280 x 800 |
| Dashboard, dark mobile | `layered-electric-dashboard-dark-mobile.png` | 390 x 844 / 390 x 844 |
| Dashboard, empty mobile | `layered-electric-dashboard-empty-mobile.png` | 390 x 844 / 390 x 844 |

All screenshots are under:

`/Users/lushiwu/.codex/visualizations/2026/07/30/019fb41f-b36f-73d0-96aa-414a3c3eeb05/`

**Full-View Comparison Evidence**

- Combined same-canvas comparison: `/Users/lushiwu/.codex/visualizations/2026/07/30/019fb41f-b36f-73d0-96aa-414a3c3eeb05/layered-electric-reference-implementation-comparison.png`
- Comparison pixels: 1500 x 2753.
- The source moodboard and representative implementation board were placed on the same near-black canvas and inspected together.
- The visible transfer is consistent: saturated electric blue carries brand and action emphasis; white and blue-biased neutrals carry content; near-black navy defines dark structure; repeated fine lines and tonal layers provide rhythm; status hues stay recognizably independent.
- The implementation intentionally uses less blue surface area on operational screens than the brand boards. This preserves scanning density and state clarity while retaining the source's visual center of gravity.

**Focused Region Comparison Evidence**

- Focused same-canvas comparison: `/Users/lushiwu/.codex/visualizations/2026/07/30/019fb41f-b36f-73d0-96aa-414a3c3eeb05/layered-electric-focused-style-comparison.png`
- Comparison pixels: 1496 x 898.
- Source crops cover typography/block rhythm, the explicit blue/navy/white palette board, and repeated structural layers.
- Implementation crops cover the public hero hierarchy, keyboard focus on a quiet light surface, and dark dashboard panel/chart layering.
- These regions confirm that the transfer is structural and semantic rather than a single sampled blue applied indiscriminately.

**Required Fidelity Surfaces**

- Fonts and typography: existing product font stacks and information density are preserved. The public hero carries the reference's bold editorial contrast; console pages retain compact operational hierarchy. No copy was changed to force a visual match.
- Spacing and layout rhythm: existing page structures remain intact. The only layout correction adds shrink-safe grid tracks to the mobile home page; desktop composition and content order are unchanged.
- Colors and tokens: `frontend/design-tokens.json` is the machine-readable source for brand, accent, neutral, navy, semantic state, provider, and chart colors. Tailwind, CSS custom properties, shared components, charts, payment flows, public/auth pages, and operations views consume that source.
- Image quality and asset fidelity: existing product, provider, and QR assets are unchanged. The reference boards informed color and visual language only; no source artwork was substituted with CSS or inline vector approximations.
- Copy and content: business copy, labels, data hierarchy, routes, and task flows are unchanged.
- Icons: existing icon families are retained. Product icon color follows semantic content/status tokens; third-party marks retain scoped identity colors.
- Accessibility: the primary button is 7.20:1 against white. Light tertiary text is 4.52:1 against a white panel. Dark tertiary text is 6.69:1. Light semantic foregrounds on their soft fills range from 5.20:1 to 6.05:1; dark equivalents range from 8.10:1 to 9.78:1.

**Interaction And Runtime Checks**

- Tested mobile navigation open/close, login keyboard focus, password visibility control, recharge/subscription switching, payment-method selection, amount validation, disabled submission, dialog state, dashboard tabs, chart state, and empty state.
- At 390 px, the public home, login, recharge, subscription, dialog, and dark dashboard do not introduce horizontal document overflow.
- The Vite preview emitted no frontend compilation or runtime error. Terminal messages are limited to expected backend-proxy `ECONNREFUSED` notices because the backend was not started for the color-system preview.

**Hard-Coded Color Audit**

- No legacy teal primary values remain in current frontend source, including `#14b8a6`, `#0d9488`, `#2dd4bf`, and their RGB-channel forms.
- Raw product colors added by this change are limited to token assertions/documentation and optical white layers. Remaining source literals are scoped to third-party/provider identity, model identity, QR black/white output, data-visualization fixtures, reward artwork skins, or black/white transparency and mask treatments.
- `git diff --check` passes.

**Comparison History**

1. P2, login focus: the browser's default orange outline remained visible around the email field and conflicted with the electric-blue focus language.
   Fix: the login fields now use semantic panel, line, content, disabled, hover, and focus tokens; focus removes the browser outline and adds `line-focus` plus a 20% two-pixel ring.
   Post-fix evidence: `layered-electric-login-focus-fixed.png`.
2. P2, mobile public home: a 390 px viewport inherited an approximately 1236 px implicit grid track, clipping the headline and primary action.
   Fix: `.home-main` now uses `minmax(0, 1fr)`, direct children can shrink, and the hero is constrained with `width: 100%`, `min-width: 0`, and `max-width: 820px`.
   Post-fix evidence: `layered-electric-home-mobile-fixed.png` and `layered-electric-home-menu-mobile.png`.

**Implementation Checklist**

- [x] Shared light and dark semantic tokens are the runtime source.
- [x] Legacy utility names map through the compatibility layer.
- [x] Public, auth, user, admin, payment, monitoring, chart, dialog, and shared component surfaces are covered.
- [x] Default, hover, selected, focus, disabled, error, empty, and dark states are visibly distinct.
- [x] Desktop and mobile captures are overflow-free.
- [x] Targeted ESLint, TypeScript, focused tests, production build, and diff checks pass.

final result: passed

## Dashboard Components Preview QA

- Preview: `http://127.0.0.1:3000/dashboard-components-preview.html`
- Source dashboard reference: `/Users/lushiwu/dev/sub2api/frontend/design-qa-assets/source-dashboard.png`
- Desktop admin view: `/Users/lushiwu/dev/sub2api/frontend/design-qa-assets/dashboard-components-admin.png`
- Desktop usage view: `/Users/lushiwu/dev/sub2api/frontend/design-qa-assets/dashboard-components-usage.png`
- Mobile admin view: `/Users/lushiwu/dev/sub2api/frontend/design-qa-assets/dashboard-components-mobile.png`
- Side-by-side comparison: `/Users/lushiwu/dev/sub2api/frontend/design-qa-assets/dashboard-components-comparison.png`

### Scope

- The preview intentionally excludes the proposed model consumption ranking and user spending ranking.
- Admin preview includes resource health rings, token composition, and an attention summary.
- Usage preview includes adaptive group composition, efficiency metrics, cost drivers, and period comparison.
- This is an isolated frontend preview; existing `/admin/dashboard`, `/usage`, routes, components, and API integrations remain unchanged.

### Responsive And Interaction Checks

- Desktop admin and usage states render without console warnings or errors.
- Admin/usage switching and Today/Total token switching work.
- Group composition supports multiple groups, one group, and empty states.
- At 390 x 844, both preview modes collapse to a single-column layout without horizontal overflow.
- The mobile usage view reports a 390px document width for a 390px viewport.
- Targeted ESLint and frontend TypeScript checks pass.

### Findings

- P0/P1/P2: none.
- The circular charts are reserved for bounded composition and health metrics. Ranking-oriented model and user data is intentionally absent.

final result: passed

# Liquid Glass Design QA

**Source visual truth**

- URL: https://inspira-ui.com/docs/cn/components/visualization/liquid-glass
- Capture: in-app browser screenshot of the official Chromium demo.
- Pixels: 1280 × 720.

**Implementation evidence**

- URL: http://127.0.0.1:3015/support-dialog-preview
- Capture: in-app browser screenshot of the support-dialog preview using the production component parameters and displacement algorithm.
- Pixels: 1280 × 720.
- CSS viewport: 1280 × 720; device density normalized at 1×.
- State: desktop, light product background, QQ selected, support dialog open.

**Full-view comparison evidence**

- The official and local captures were combined side by side at equal pixel dimensions.
- Both surfaces show a translucent glass layer, displaced background detail at the perimeter, RGB channel separation, a thin luminous rim, and content that remains unaffected by the backdrop displacement.
- The local surface is intentionally larger and sits on a lighter background because it is a support dialog rather than the official narrow demo bar. The visual mechanism and tuning values match the reference.

**Focused region comparison evidence**

- A separate crop was not needed: at 1280 × 720 the official bar and the complete 360 px support dialog were both readable in the equal-size side-by-side comparison. The glass rim, displaced dot field, typography, and controls were visible without scaling ambiguity.

**Required fidelity surfaces**

- Fonts and typography: existing product font stack and support-dialog hierarchy are preserved; labels remain legible over the refracted surface.
- Spacing and layout rhythm: the existing 360 px dialog, 28 px radius, tab spacing, QR frame, and modal centering are preserved.
- Colors and visual tokens: the component uses the reference `lightness: 50`, `blend: difference`, `alpha: 0.93`, `blur: 11`, `scale: -180`, and `frost: 0.05` values. No white wash or nested blur remains on the supported liquid surface.
- Image quality and asset fidelity: the production QR image path and rendering behavior are unchanged. The preview uses mock QR content only to validate layout and glass contrast.
- Copy and content: support title, introduction, tabs, and scan hint are unchanged.

**Findings**

- No actionable P0, P1, or P2 differences remain for the requested liquid-glass behavior.
- P3: refraction is naturally more noticeable over high-contrast content than over a nearly uniform page background. The local dot field provides enough detail to reveal it without making the modal decorative or noisy.

**Comparison history**

1. Earlier implementation used `scale: -70`, near-white `lightness: 94`, only 1–2 px channel offsets, a 35% white wash, and a blurred overlay. Evidence: it read mainly as gray frosted glass and hid the displacement.
2. Fixes applied: restored the official -180/-170/-160 channel scales and source-map values, removed overlay pre-blur, removed the white wash and nested blur layers, strengthened the restrained dot field, and used the official multi-layer inset shadow treatment.
3. Post-fix evidence: equal-size side-by-side browser comparison shows displaced dots and the same edge/color-split behavior as the reference while preserving support-dialog readability.

**Implementation checklist**

- [x] Official displacement parameters applied.
- [x] Chromium displacement retained; unsupported browsers use a solid white card with no backdrop filter.
- [x] Support-dialog copy and QR behavior preserved.
- [x] QQ/WeChat switching verified.
- [x] Focused tests, ESLint, and TypeScript checks passed.

**Follow-up polish**

- If the production dashboard behind the dialog is unusually uniform, slightly increasing the dot-field opacity can make refraction more obvious without changing the glass component.

final result: passed

# Layered Electric Final QA Addendum

- Source and implementation comparison: `/Users/lushiwu/.codex/visualizations/2026/07/30/019fb41f-b36f-73d0-96aa-414a3c3eeb05/layered-electric-final-reference-implementation-comparison.png` (1500 x 2840), containing all ten reference images and the rendered home, login, recharge, dark dashboard, and mobile error states on one canvas.
- Native implementation evidence: `layered-electric-final-production-home.png` and `layered-electric-final-login-focus.png` at 1280 x 800; `layered-electric-final-home-menu-mobile.png` at 390 x 844; `layered-electric-final-purchase-light.png` at 1272 x 795; `layered-electric-final-purchase-mobile-error-fixed.png` at 382 x 827. Captures use 1x density; the 390 px mobile document width matches its CSS viewport.
- Final P2 correction: invalid custom recharge amounts no longer combine warning yellow with a blue selected check. The amount tile, message, border, fill, text, and `!` marker now use danger semantics, while the selected payment method remains electric blue.
- Required surfaces checked: typography/copy are preserved; layout rhythm remains stable; light/dark design tokens cover surfaces, content, lines, focus, states, providers, and charts; existing product imagery and icons remain intact; desktop/mobile interaction states are readable and distinct.
- Verification: recharge selector tests 4/4 passed; targeted ESLint, full `lint:check`, full `typecheck`, production build, and `git diff --check` passed. Build output has only existing import/chunk warnings.
- Runtime: the in-app browser shows the production preview at 1280 x 800 without document overflow. Console output is limited to expected backend `ECONNREFUSED` proxy messages because the backend is not running; no frontend color-system runtime error was observed.
- Findings: no actionable P0, P1, or P2 visual issue remains; legacy teal primary values are absent from current frontend source.

final result: passed

# Sidebar Width Reduction QA

- Source visual truth: `/var/folders/j_/wj8mmnfx46zccr8bvxj1wklh0000gn/T/codex-clipboard-46f9de7c-8ade-4055-97cc-1d4839bb6cf3.png` at 454 x 1522 pixels, treated as a 2x capture of an approximately 227 x 761 CSS-pixel sidebar.
- Implementation evidence: `/tmp/sub2api-sidebar-208.png` at 1778 x 1111 pixels and focused crop `/tmp/sub2api-sidebar-208-focus.png` at 231 x 1000 pixels.
- Browser viewport and state: 1600 x 1000 CSS pixels, light theme, authenticated user dashboard, expanded sidebar. The source shows an admin menu, so comparison was limited to the requested width, spacing, typography fit, and layout boundary.
- Full-view comparison: the sidebar measures 207.99 CSS pixels and the main content begins at 207.99 CSS pixels, with no overlap or empty gap.
- Focused comparison: source and implementation were opened together. The 208px implementation is visibly narrower than the previous 224px layout while retaining aligned icons, labels, active background, and active indicator. No visible label overflows.
- Fonts and typography: unchanged; tested labels remain single-line and untruncated.
- Spacing and layout rhythm: expanded width and main offset both changed from 224px to 208px. Collapsed width and offset remain 72px.
- Colors and visual tokens: unchanged.
- Image quality and assets: no image or icon assets changed. The local logo image was unavailable because the public-settings backend was not running.
- Copy and content: unchanged.
- Interaction checks: collapse moved both sidebar and main offset to 72px; re-expand restored both to 208px.
- Runtime checks: local API requests logged expected errors because no backend was running; no sidebar rendering or interaction errors occurred.
- Verification: focused sidebar Vitest 11/11 passed, TypeScript typecheck passed, and `git diff --check` passed.
- Findings: no actionable P0, P1, or P2 issue remains. No post-comparison visual fix was required.

final result: passed
