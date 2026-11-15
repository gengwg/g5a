# Contributing

Keep the application read-only and dashboard-driven. Do not special-case a company, dashboard UID, cluster, metric name, or PromQL expression. Add generic behavior through the dashboard compiler, DataFrame pipeline, shared field presentation, or renderer registry.

## Before submitting a change

1. Add focused tests for the behavior, including a failure case where useful.
2. Run `go test ./...` and `go test -race ./...`.
3. Run `make lint`.
4. For query, variable, transformation, or renderer changes, run `make test-integration` with Docker available.
5. Update `docs/compatibility.md` and the README when user-visible support changes.

Prefer small, upstreamable changes and existing dependencies. New dependencies need a clear benefit. Follow the existing Go style and keep exported APIs stable unless a breaking change is intentional.

## Fixtures and privacy

Use sanitized dashboard JSON and DataFrame responses that reproduce the relevant schema. Never include tokens, passwords, internal URLs, Slack IDs, customer names, or private metric labels. Real Grafana integration coverage should use the container fixtures in `grafana/integration_test.go`.

## Commit and release hygiene

Use imperative commit subjects and do not commit generated binaries. Releases are built with GoReleaser. Keep the MIT license and upstream copyright notices intact.
