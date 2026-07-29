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
