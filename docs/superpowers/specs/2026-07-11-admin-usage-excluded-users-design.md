# Admin Usage Excluded Users And Analytics Refresh Design

## Goal

Make the administrator usage-record page support excluding multiple users through an email-first picker, and guarantee that every usage total and chart shown after a filter change represents the current exclusion set rather than stale pre-filter data.

## Confirmed Behavior

- The exclusion control remains multi-select.
- Administrators search by user email, select one or more users, and see each selection as an individually removable label containing the email and secondary `#ID`.
- Numeric user IDs remain the transport value. Email is display metadata only.
- Manually entered positive IDs remain supported for backward compatibility and render as `#ID` until email metadata is known.

## Root Cause

The current exclusion input has three conflicting responsibilities: search keyword, raw numeric-ID editor, and selected-value display. Selecting an email correctly stores its numeric ID, but the component immediately replaces the visible value with the comma-separated IDs. A watcher repeats that replacement whenever the filter changes.

The top-level usage page already serializes `exclude_user_ids` into a comma-separated query value for the usage list, statistics, model statistics, dashboard snapshot, chart breakdown filters, and export. The backend parser, repository filters, and cache keys already honor the exclusion on those paths. The missing frontend contract tests allow that data flow to regress unnoticed.

Two stale-state paths make correct requests appear ineffective:

- A failed latest statistics or snapshot request leaves the previous totals or chart arrays visible.
- Model, group, and endpoint distribution charts retain expanded user-breakdown data when shared filters change. An older in-flight response can also repopulate the expansion after a new exclusion is active.

## Scope

- Separate excluded-user search text from selected numeric IDs in `UsageFilters.vue`.
- Render selected excluded users as email-first removable labels.
- Update English and Chinese placeholder/action text together.
- Preserve the existing comma-separated `exclude_user_ids` request contract.
- Give usage statistics an explicit loading state and prevent failed filtered requests from retaining old totals.
- Clear failed trend/group results and make expanded chart breakdowns reload for current filters while ignoring obsolete responses.
- Add focused frontend regression coverage for the control, page request contract, statistics state, and all three distribution-chart breakdowns.

## Non-Goals

- No database, migration, billing, quota, or usage aggregation changes.
- No change to the meaning of `user_id` or `exclude_user_ids`.
- No replacement of the existing charts or search endpoint.
- No general refactor of the usage page or shared form architecture.
- No attempt to hydrate emails for externally supplied IDs that are not returned by a user search.

## Component Design

### Excluded-User Picker

`UsageFilters.vue` keeps two independent state domains:

1. `filters.exclude_user_ids` is the canonical selected-ID list used by the parent page.
2. A local search keyword and an `ID -> SimpleUser` metadata map drive display and autocomplete.

Selecting a search result adds its ID if it is not already present, records its email/deleted metadata, clears the search keyword, closes the result list, and emits one filter change. Re-selecting an existing user does not duplicate the ID or issue a redundant analytics refresh.

Selected IDs render below the search field as wrapping labels. A known user displays `email` with `#ID` as secondary text and preserves the existing deleted-user badge. An unknown or manually entered ID displays `#ID`. Each label has an icon button with a translated accessible name that removes only that ID and emits one filter change.

The search input's clear action clears only the current keyword. Page reset clears all selected IDs through the existing parent filter reset. On external filter changes, the component removes metadata for IDs that are no longer selected but never rewrites the search keyword into an ID list.

For backward compatibility, blurring or pressing Enter on input made entirely of comma/whitespace-separated positive integers adds those IDs and clears the keyword. Arbitrary text is treated only as a search keyword and is never converted into a filter without selecting a result.

### Usage Analytics Page

`UsageView.vue` retains one serializer for `exclude_user_ids`. Every top-level analytics request uses its comma-separated result:

- usage list and export;
- usage statistics and endpoint distributions;
- requested/upstream/mapping model distributions;
- dashboard trend and group snapshot;
- user-ranking and distribution-chart breakdown filters.

The statistics request owns a loading flag that is passed to `UsageStatsCards`. While a new filtered request is active, the cards show a stable loading presentation instead of presenting old values as current. Only the newest request may replace the totals. If that request fails, the stored statistics are cleared.

The snapshot request already has request sequencing. Its latest-request failure additionally clears trend and group arrays. Model-stat failures continue to clear only the affected model source. Existing loading states hide old chart content while replacement data is pending.

### Expanded Distribution Breakdowns

`ModelDistributionChart.vue`, `GroupDistributionChart.vue`, and `EndpointDistributionChart.vue` each track a monotonically increasing breakdown request sequence.

The relevant filter signature contains the date range, the serialized shared filters, and the chart's dimension source where applicable. If it changes while a row is expanded, the component clears the visible breakdown and automatically reloads that same dimension with the new filters. Collapsing the row or changing filters invalidates every older request.

A breakdown response may update the UI only when its sequence is current and the same row remains expanded. A latest-request error clears the breakdown; obsolete successes and errors are ignored.

## Data Flow

1. The administrator searches for an email and selects a user.
2. `UsageFilters` adds the numeric ID to `filters.exclude_user_ids` and emits one change.
3. `UsageView.applyFilters` increments the relevant request generations and starts list, statistics, model, and snapshot requests.
4. Each request receives the same comma-separated exclusion value.
5. Statistics cards and top-level charts show loading state until the newest responses arrive.
6. Expanded distribution breakdowns reload with the same exclusion and reject older responses.
7. Removing one selected label repeats the flow with the remaining IDs.

## Error And Edge-Case Handling

- Empty exclusion state omits `exclude_user_ids` from requests.
- Duplicate selections and duplicate manually entered IDs collapse to one ID.
- Deleted users remain selectable and visibly marked.
- Unknown externally supplied IDs remain removable through their `#ID` fallback labels.
- Invalid free text does not erase existing selections or emit a malformed filter.
- A stale request cannot replace statistics, chart arrays, or expanded breakdown data.
- A failed latest request cannot leave pre-filter values presented as the current result.

## Testing Strategy

Implementation follows test-driven development.

Focused component tests will cover:

- selecting multiple email results renders email-first labels while retaining numeric filter IDs;
- duplicate selection does not duplicate IDs or emit a redundant change;
- removing one label preserves the other selections;
- manual numeric IDs use the fallback label and external reset removes stale labels;
- the usage page sends the same serialized exclusions to list, statistics, model, snapshot, export/breakdown props, and ranking consumers;
- statistics loading and latest-request failure never present old totals as current;
- snapshot failure clears old trend/group data;
- each distribution chart reloads an expanded row when exclusions change;
- each distribution chart ignores a deferred response from the previous filter generation.

After focused Vitest checks, run frontend type checking, lint checking, and a production build. Browser verification covers the picker and refreshed analytics at desktop and mobile widths, including adding two users, removing one, and checking light and dark themes.

## Acceptance Criteria

- The exclusion picker supports multiple users and displays selected users by email with `#ID` context.
- Each selected user can be removed independently without disturbing other filters.
- All usage totals, token/cache breakdowns, costs, average latency, trend data, model/group/endpoint distributions, user ranking, and expanded breakdowns reflect the active exclusion set.
- No obsolete successful or failed response can restore pre-filter data.
- Existing raw-ID entry and deleted-user selection remain functional.
- Focused tests, type checking, lint checking, and the frontend production build pass.
