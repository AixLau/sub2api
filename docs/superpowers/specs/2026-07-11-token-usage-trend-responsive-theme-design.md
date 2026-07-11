# Token Usage Trend Theme, Positioning, And Responsive Design

## Goal

Make the shared Token usage trend chart visually consistent in light and dark themes, keep the plotted data, crosshair, and tooltip aligned, and prevent clipping or stale sizing from desktop widths down to 375px.

The change applies to all four current `TokenUsageTrend.vue` call sites: the administrator dashboard, administrator usage view, user usage view, and `UserDashboardCharts` on the user dashboard.

## Root Cause

`VariableWidthLineChart.vue` combines a G2 canvas with separately rendered HTML grid lines, axis labels, crosshair, and tooltip. These layers use fixed plot padding values, while each G2 line layer also enables its own axes. The two layout systems can therefore calculate different plot rectangles.

Tooltip placement assumes a fixed 236px width even though the rendered HTML determines its actual size. The chart does not explicitly refit when a surrounding layout transition changes its width, such as expanding the sidebar. The component CSS and the tooltip HTML also hard-code a light surface and light-theme text colors.

## Scope

- Use one plot rectangle for the G2 marks and every HTML overlay.
- Refit the G2 canvas when the chart container changes size.
- Measure the rendered tooltip before final placement and keep it within the chart body.
- Provide light and dark semantic styles for the chart surface, title, legend, grid, axes, crosshair, empty state, and tooltip.
- Keep the chart usable without horizontal overflow at 375px and wider viewports.
- Preserve the existing series, token calculations, date matching, cost display, and public component props.

## Non-Goals

- No backend, API, or usage-data changes.
- No replacement of G2 with another chart library.
- No redesign of the surrounding dashboard or usage-record pages.
- No new chart controls, gestures, filters, or metrics.
- No changes to the other Chart.js-based dashboard charts.

## Component Design

### VariableWidthLineChart

`VariableWidthLineChart.vue` remains responsible for rendering the reusable line chart and positioning its overlays.

The component will use one private, immutable layout snapshot containing the measured chart-body width and height, four plot paddings, and the derived `plotLeft`, `plotRight`, `plotTop`, `plotBottom`, `plotWidth`, and `plotHeight`. A snapshot is valid only for its measured dimensions. A body is renderable only when its width exceeds the selected left plus right padding and its height exceeds the top plus bottom padding; otherwise G2 marks and all coordinate overlays remain hidden until a renderable measurement arrives. The G2 view options, custom grid, custom x-axis labels, crosshair mapping, and tooltip snapping will all consume the current snapshot. G2 child axes will be disabled so they cannot reserve a second, different axis area.

The component also owns one x-coordinate contract. For continuous time or numeric data, both G2 and the overlays use the same minimum and maximum domain and map values linearly into `[plotLeft, plotRight]`. For categorical data, both use the unique values in first-appearance order and map their indexes into that range. An explicit `point` or `band` scale stays categorical; an explicit `time` or `linear` scale uses the continuous model when every value can be converted, otherwise it falls back to the existing categorical behavior. One unique x value is always placed at the plot center: categorical G2 uses a one-value point domain, while continuous G2 receives a symmetric non-zero domain around the value and the overlay maps the original value to 50%. `xTicks` controls displayed tick values but does not change the data domain. The existing y extent is likewise the shared G2 and overlay y-domain, while `yTicks` controls only displayed grid values.

The current timestamp-to-x conversion remains authoritative. Irregular time intervals are positioned by elapsed time rather than array index, and categorical values retain the existing index fallback. The first and last x-axis labels use edge-aware alignment so their text remains inside the container.

A `ResizeObserver` watches the chart body. Its callbacks are coalesced into one `requestAnimationFrame`. A positive-size update cancels pending tooltip placement, hides the tooltip, records a new layout snapshot, waits for Vue to apply the matching CSS variables, and then calls G2 `forceFit()`. Crossing the 480px chart-body breakpoint rebuilds the G2 options first so the new paddings are used. Zero-sized or hidden containers cancel hover state but retain the last valid snapshot until a later positive-size observation. Observer, animation-frame, and pending-fit work is cancelled during unmount.

When `ResizeObserver` is unavailable, a window `resize` listener runs the same measured and coalesced update path. This fallback guarantees viewport-driven resizing only; sidebar and grid transitions require `ResizeObserver`, which is available in the project's supported modern browsers. G2 `autoFit` remains enabled as a secondary safeguard, not as the component's only resize mechanism.

### Tooltip

The tooltip remains custom HTML because it combines four token series, cache-hit rate, total usage, and optional cost in one preview.

On pointer movement, the chart first snaps the crosshair to the nearest x value. After Vue renders the tooltip content, the component reads the tooltip element's actual width and height. The anchor is the snapped x coordinate and the pointer's y coordinate, clamped to the plot bounds. Placement uses an 8px body inset and a 12px anchor gap. It prefers the right side when the full tooltip fits, otherwise the left side, otherwise the side with more space; equal available space prefers the right. The resulting x coordinate is clamped to the inset. Vertically, the tooltip is centered on the pointer and then clamped to the inset.

The tooltip uses `box-sizing: border-box`; its maximum border-box width is exactly `bodyWidth - 16px` and its maximum border-box height is `bodyHeight - 16px`. If either available dimension is non-positive, the tooltip is not shown. Long translated labels and values may wrap without increasing the page width. Content taller than the available height scrolls inside the tooltip; pointer movement over the tooltip does not update the crosshair, and the tooltip remains visible until the pointer leaves the chart body.

Every pointer movement receives a monotonically increasing placement ticket. Pointer leave, resize, data or formatter rerender, chart destruction, and component unmount also invalidate the ticket. Data, domain, or formatter changes immediately hide the existing tooltip and crosshair; the user must point again against the newly rendered domain. An asynchronous `nextTick` measurement may update position only when its ticket is still current and the tooltip element remains mounted.

### TokenUsageTrend Theme

`TokenUsageTrend.vue` will emit semantic inner markup instead of embedding a second tooltip surface with complete light/dark presentation in inline styles. The private markup contract uses `token-trend-tooltip`, `token-trend-tooltip__title`, `__rows`, `__row`, `__label`, `__marker`, `__value`, and `__summary` classes. Series marker colors remain data-driven through an inline CSS custom property whose values come only from the component's internal hexadecimal color constants, never from user or API input.

`VariableWidthLineChart.vue` owns the outer tooltip surface and styles injected inner markup through scoped `:deep(.token-trend-tooltip...)` selectors. The existing `tooltipHtml(title)` formatter signature remains unchanged and arbitrary formatter output still renders inside the generic outer surface; only the Token trend formatter relies on the private class contract.

The chart defines light defaults and dark overrides under the application's existing `.dark` root class. The chart surface uses the surrounding card surface rather than forcing white. CSS variables provide the title, text, grid, crosshair, and tooltip colors so a theme switch updates without reconstructing chart data. Colors use the project's existing Tailwind gray, blue, and dark surface values, with normal-size text combinations meeting a WCAG AA contrast ratio of at least 4.5:1.

## Responsive Behavior

- The chart and G2 canvas never exceed the component width.
- The title and legend wrap naturally without changing the plot width.
- Desktop keeps the current plot density and requested height.
- At chart-body widths of 480px and above, plot padding remains `56px 24px 42px 12px` for left, right, bottom, and top. Below 480px it becomes `48px 12px 38px 12px`.
- Automatic x ticks show the first, middle, and last values at 480px and above, and only first and last below 480px. For an even value count, the earlier/lower of the two middle values is selected. Tick indexes are deduplicated, so one- and two-value datasets render one and two labels respectively. Explicit `xTicks` remain caller-controlled.
- The first and last x-axis labels align inward; intermediate labels remain centered.
- Tooltip width is capped by the chart body and its measured position is recalculated at every display.
- Sidebar expansion, window resizing, and responsive grid transitions refit the canvas instead of leaving marks outside the card.

Geometry tests use chart-body widths of 343px, 480px, and 960px, plus undersized bodies at and below the 16px total inset. Browser verification uses a 375px viewport and records the actual body width rather than assuming it equals the viewport. Desktop verification uses a 1440px viewport with both expanded and collapsed sidebar layouts. The component root and chart canvas receive `min-width: 0` and `max-width: 100%`; no consumer-page change is expected unless browser QA proves an ancestor is the source of overflow, in which case only the directly responsible grid wrapper may receive `min-width: 0`.

## Data And Event Flow

1. `TokenUsageTrend.vue` converts each `TrendDataPoint` into the four existing token series.
2. `VariableWidthLineChart.vue` normalizes the series and creates the time scale.
3. A positive body measurement creates the authoritative layout snapshot and x/y coordinate models.
4. Vue applies the snapshot as CSS variables, then G2 renders marks with the same dimensions, paddings, and domains.
5. HTML overlays derive their bounds and tick positions from that snapshot.
6. Pointer movement maps the pointer into the snapshot's plot rectangle and selects the nearest timestamp.
7. Vue renders tooltip markup for that timestamp.
8. The chart measures and clamps the tooltip, then exposes the final position if its placement ticket remains current.
9. A container resize invalidates hover state, creates a new snapshot, and refits or rebuilds G2 in that order.

## Edge Cases

- Empty data continues to show the translated empty state.
- A single timestamp snaps to the only available x position.
- A single timestamp renders at the plot center in G2 and every overlay.
- Duplicate timestamps collapse to the first unique x value. Continuous values are compared in sorted numeric/time order; equal-distance ties choose the earlier/lower value even when input is unsorted.
- Mixed valid and invalid dates use the existing categorical fallback in first-appearance order.
- A chart narrower than the preferred tooltip uses the available width minus both 8px safety insets.
- Tooltip placement remains inside the body at the first and last timestamp and near the top and bottom edges.
- Tooltip content taller than the available body height uses its bounded internal scroll area.
- Repeated rapid pointer events cannot apply an obsolete asynchronous tooltip measurement.
- Pointer leave, resize, data rerender, chart destruction, and unmount cannot apply obsolete tooltip measurements.
- Theme styles remain readable if the page switches theme while the chart is mounted.

## Testing Strategy

Implementation follows test-driven development.

Focused component tests will cover:

- G2 line layers do not enable a second axis layout.
- A pure layout helper produces measured bounds and the exact desktop and narrow paddings.
- A pure x-coordinate helper drives G2 domain options, crosshair snapping, and axis overlay positions.
- Irregular and unsorted timestamps snap to the nearest elapsed-time position with deterministic duplicate and tie behavior.
- Actual tooltip dimensions determine left/right placement and boundary clamping.
- Narrow and undersized containers cap tooltip dimensions and keep first/last labels inside the chart.
- `ResizeObserver` callbacks are coalesced, trigger the ordered snapshot/fit flow, handle zero-size and non-renderable observations, and disconnect on unmount; the window fallback uses the same flow for viewport changes.
- Every invalidating lifecycle event cancels pending tooltip placement.
- Tooltip markup follows the private semantic class contract and the generic outer surface supports both theme palettes.
- Existing totals, cache-hit rate, optional cost, date parsing, and HTML escaping remain unchanged.

After focused Vitest checks, run frontend type checking, lint checking, and a production build. Browser QA will verify all four current consumers: the administrator dashboard, administrator usage view, user usage view, and user dashboard. Each is checked in light and dark themes at 375px and 1440px; administrator pages are additionally checked with both sidebar states. Hover checks cover the first, middle, and last data points, and document-level horizontal overflow is measured rather than judged only from a screenshot.

## Acceptance Criteria

- The chart surface, labels, grid, crosshair, and tooltip are readable and visually integrated in both themes.
- Lines, x-axis labels, snapped crosshair, and tooltip timestamp refer to the same data position.
- The tooltip never crosses the chart body's left, right, top, or bottom safety inset.
- No chart canvas, line, label, legend item, or tooltip creates horizontal page overflow at 375px.
- In supported browsers with `ResizeObserver`, resizing the window or toggling sidebar width leaves the plot inside its card. The fallback path guarantees window resizing when that API is unavailable.
- Existing Token trend calculations and optional cost output remain correct.
- Focused tests, type checking, lint checking, and the frontend production build pass.
