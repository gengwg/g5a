# grafana-tui

[![License](https://img.shields.io/github/license/lovromazgon/grafana-tui)](https://github.com/lovromazgon/grafana-tui/blob/main/LICENSE)
[![Test](https://github.com/lovromazgon/grafana-tui/actions/workflows/test.yml/badge.svg)](https://github.com/lovromazgon/grafana-tui/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/lovromazgon/grafana-tui)](https://goreportcard.com/report/github.com/lovromazgon/grafana-tui)

Browse live Grafana dashboards in a terminal. `grafana-tui` connects to an existing Grafana instance, expands dashboard variables and repeated panels, queries through Grafana, and renders a responsive grid with stats, tables, and Braille time-series charts.

It is read-only, works over SSH, and is licensed under MIT.

## Features

- Dashboard search and direct dashboard/panel URLs.
- Grafana's 24-column grid with vertical scrolling and panel zoom.
- Horizontal and vertical repeated panels with responsive terminal wrapping.
- Datasource, custom, constant, `label_values`, and `query_result` variables, including multi-select and All.
- Grafana `/api/ds/query` requests that preserve instant/range and datasource-specific target fields.
- DataFrame joins, filtering, organization, reduction, and type conversion.
- Shared field mappings, thresholds, units, decimals, overrides, and no-value handling.
- Bounded concurrent refresh, cancellation, stale-response protection, and per-panel errors.
- Token and basic authentication, with no request or response bodies written to logs.

See [the compatibility guide](docs/compatibility.md) for the exact supported subset and current limitations.

## Installation

Install a released version with Homebrew:

```sh
brew install lovromazgon/tap/grafana-tui
```

Or download a binary from the [latest release](https://github.com/lovromazgon/grafana-tui/releases/latest).

To build this source tree, Go 1.26 or newer is required:

```sh
make build
./grafana-tui --url https://play.grafana.org
```

## Usage

```sh
grafana-tui --url URL [flags]
```

Open a dashboard or panel directly:

```sh
grafana-tui --url https://grafana.example.com/d/abc123
grafana-tui --url 'https://grafana.example.com/d/abc123?viewPanel=22'
```

### Authentication

A read-only Grafana service account is recommended. Environment variables avoid placing credentials in shell history and process listings.

```sh
export GRAFANA_URL=https://grafana.example.com
export GRAFANA_SERVICE_ACCOUNT_TOKEN=YOUR_TOKEN
grafana-tui
```

Basic auth is also available through `GRAFANA_USERNAME` and `GRAFANA_PASSWORD`, or the corresponding CLI flags. CLI flags override environment variables.

### Flags

```text
--url         Grafana instance URL (env: GRAFANA_URL)
--token       Grafana service account token (env: GRAFANA_SERVICE_ACCOUNT_TOKEN)
--username    Grafana basic auth username (env: GRAFANA_USERNAME)
--password    Grafana basic auth password (env: GRAFANA_PASSWORD)
--refresh     Auto-refresh interval (dashboard setting or 30s; 0 disables)
```

### Keybindings

Dashboard grid:

| Key | Action |
|-----|--------|
| `h` / `l` or arrows | Select the previous/next panel |
| `j` / `k` or arrows | Scroll down/up |
| `Enter` | Zoom the selected panel |
| `PgUp` / `PgDn` | Scroll one page |
| `Home` / `End` | Jump to the top/bottom |
| `t` | Change the time range |
| `v` | Change dashboard variables |
| `r` | Refresh now |
| `q` | Quit |
| `Esc` | Go back |

In the variable editor, Enter edits or applies a selection. For multi-value variables, Space toggles each value and Enter finishes editing. All is exclusive.

In a zoomed panel, `j`/`k` navigates panels, `g` opens the panel picker, and `f` filters series. `?` shows help.

## Security

The application performs read-only dashboard, datasource, and query API requests. It does not edit dashboards, alerting rules, or datasource configuration.

Request and response bodies are not logged or included in returned error strings. Logs contain only method, path, status, and byte counts. URL userinfo is removed from transport errors. On Linux, the default log is `${XDG_CACHE_HOME:-~/.cache}/grafana-tui/grafana-tui.log`; the directory is created with mode `0700` and the file with mode `0600`. Other platforms use the corresponding `os.UserCacheDir` location.

Use a least-privilege read-only service account and TLS for remote Grafana instances.

## Development

```sh
make test                    # unit tests
make test-race               # race detector
make lint                    # golangci-lint
make test-integration        # real Prometheus + Grafana containers
```

Integration tests require Docker. They create isolated temporary networks and containers for Prometheus and Grafana, then exercise variables, repeated panels, instant/range queries, joins, transformations, mappings, and renderers.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug reports should include the Grafana version, panel type, and a sanitized dashboard JSON/query-response fixture where possible. Do not attach credentials or private dashboard data.

## License

MIT. See [LICENSE](LICENSE).
