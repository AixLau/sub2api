# Wallet Header Design QA

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

final result: passed
