# Grafana compatibility

`g5a` aims for operational fidelity: values, mapped states, colors, table rows, and layout should remain meaningful in a terminal. It does not attempt pixel-perfect browser rendering or complete Grafana plugin compatibility.

Unsupported transformations and panel types are shown explicitly rather than silently presented as healthy data.

## Dashboard model and APIs

- Classic dashboard JSON from `GET /api/dashboards/uid/:uid`.
- Dashboard resources from `/apis/dashboard.grafana.app/{v2,v2beta1,v1beta1,v1}/namespaces/:namespace/dashboards/:uid`, with GridLayout, RowsLayout, AutoGrid, and Tabs converted into the existing dashboard model. Namespace selection uses `default` for org 1, `org-<id>` for other OSS orgs, and `stacks-<id>` for Grafana Cloud. Tabs are stacked with text headings and can repeat with their panels; AutoGrid uses the configured columns and row heights. Library panels are expanded through `GET /api/library-elements/:uid`; unsupported variable kinds return explicit errors.
- Dashboard search through `GET /api/search`.
- Panel and variable queries through Grafana's backend APIs, primarily `POST /api/ds/query`.
- Current alert rules through `/api/prometheus/:uid/api/v1/rules`, with legacy `/api/alerts` fallback.
- Grafana versions that retain these legacy read APIs. Grafana 13 deprecates some `/api` routes but keeps them operational.

## Datasources

Prometheus is covered by unit and real Grafana integration tests. Other backend datasources are forwarded with their raw datasource-specific target fields preserved, but do not yet have dedicated integration coverage.

Legacy timeseries targets without explicit `instant` or `range` fields default to range queries. Explicit `instant`, `range`, `format`, `intervalMs`, and `maxDataPoints` values are preserved. Relative Grafana time expressions and `/s` through `/y` rounding are supported; unsupported expressions produce a visible error instead of silently using the current time.

## Variables

| Feature | Status |
|---------|--------|
| Datasource variables | Supported, including type/regex filtering |
| Custom and constant variables | Supported |
| Prometheus `label_values` | Supported |
| `query_result` and raw instant queries | Supported |
| Datasource-native `queryType` variables (for example Incident `labels`/`severities`) | Supported |
| Dependent variables | Resolved in dashboard order |
| Regex extraction | Supported, including the first capture group |
| Multi-value and All | Supported, including repeat expansion |
| Raw, pipe, regex, CSV, JSON, query-param formatting | Supported |
| Saved multi-value selections | Supported |
| Dashboard schema v2 | GridLayout and RowsLayout supported; datasource, query, custom, constant, textbox, interval, and switch variables |

## Repeated panels and layout

- Classic `gridPos` layout on a 24-column grid.
- Horizontal and vertical panel repeats with scoped variable values.
- Row repeats with scoped variable values, including repeated panels within repeated rows.
- `repeatDirection` and `maxPerRow` for panel repeats.
- Schema v2 GridLayout, RowsLayout, AutoGrid, stacked/repeated Tabs, and library panels are supported.
- Nested RowsLayout levels that both define repeats return an explicit error; one repeating level is supported.
- Narrow terminals reduce repeated-panel columns and scroll vertically.

## Panel types

First-class support for the current dashboard viewer:

| Panel | Behavior |
|-------|----------|
| `stat` | Reductions, mappings, thresholds, units, labels, foreground/background colors |
| `table` | Typed cells, time-aligned multi-series columns, sorting, mappings, thresholds, units, per-column overrides, truncation, sum/count footers and row counts |
| `timeseries` / legacy `graph` | Braille lines, duplicate-name handling, units, Last/Max and other basic legend calculations |
| `alertlist` | Unified and legacy read APIs, Grafana and supported datasource-managed rules, list/stat views, custom label grouping, filters, sorting, max items, and instance labels |
| `geomap` | Latitude/longitude and geohash points on an ASCII coordinate grid, aliases, names/labels, values, threshold colors, and exact point rows |

Existing best-effort renderers also cover `gauge`, `bargauge`, `barchart`, `heatmap`, `histogram`, `piechart`, `state-timeline`, `status-history`, `candlestick`, `trend`, and `logs`. Their Grafana option coverage is more limited.

Text panels remain visible in navigation. Alert-list acknowledgement and other alerting writes are intentionally unsupported.

## Transformations

- `joinByField` with outer joins across frames and refIDs, including joins on Prometheus labels.
- `organize` preserves label fields in their original relative order before numeric values. Other transformations expand labels only when needed by `joinByField`, `groupingToMatrix`, or exact-name/literal-alternative field filters; unrelated labels remain attached to their fields instead of becoming sparse table columns.
- `groupingToMatrix` with `rowField`, `columnField`, `valueField`, and `emptyValue`.
- `sortBy` with ordered field rules and numeric-aware stable sorting.
- `merge` with field-name alignment and null padding for missing fields.
- `filterFieldsByName`.
- `organize`, including exclusions, renaming, and `indexByName` ordering.
- `filterByValue`.
- `convertFieldType` for string-to-number conversion.
- `calculateField` with binary arithmetic (including all-number-fields), unary operations, row reduction, cumulative sum/mean, trailing/centered moving windows, and row indices, including legacy/current operands, constants, aliases, field replacement, and time-aligned cross-query operands.
- `reduce` with common reducers.

Unknown transformation IDs produce a visible warning in the affected panel. Other transformation types are not yet implemented.

## Field presentation

- DataFrame `field.Config`, panel defaults, and ordered `byName` / `byRegexp` overrides.
- Display names, compact datasource `displayNameFromDS` names (metric prefix or first label value), width-aware URL legends, and `${__field.labels.name}` interpolation.
- Units, including locale-formatted numbers, literal, prefix, suffix, and scaled count custom units, decimals, no-value text, min/max, and absolute/percentage thresholds.
- DataFrame time fields rendered as local timestamps.
- Value, range, regex, null, and NaN mappings.
- Mapping colors, threshold colors, and color-background cells.
- Table alignment and width hints.

Grafana's complete field-override matcher set, color schemes, data links, and every visualization-specific option are not yet covered.

## Refresh and failures

- Dashboard refresh interval, or 30 seconds when unspecified; string durations and `refresh: false` are accepted.
- Explicit CLI interval overrides and `--refresh=0` to disable polling.
- Bounded panel-query concurrency and cancellation on new refreshes.
- Stale responses cannot overwrite newer variable/time selections.
- One failed panel does not clear other panels.
- Unsupported features are shown as warnings or placeholders, never silently treated as healthy values.
