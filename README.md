# Murphy

[![CI](https://github.com/murphyhq/server/actions/workflows/ci.yml/badge.svg)](https://github.com/murphyhq/server/actions/workflows/ci.yml) [![License: AGPL-3.0](https://img.shields.io/github/license/murphyhq/server)](LICENSE) [![Self-hosted](https://img.shields.io/badge/self--hosted-yes-2ea44f)](docs/architecture.md)

[![Go version](https://img.shields.io/github/go-mod/go-version/murphyhq/server)](go.mod) [![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://go.dev) [![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-000000?logo=opentelemetry&logoColor=white)](https://opentelemetry.io) [![OpenAPI](https://img.shields.io/badge/OpenAPI-6BA539?logo=openapiinitiative&logoColor=white)](https://www.openapis.org) [![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-FE5196?logo=conventionalcommits&logoColor=white)](https://www.conventionalcommits.org)

Anything that can go wrong will go wrong, and Murphy makes sure someone hears about it.

Murphy is an open-source, self-hostable on-call paging server: schedules, rotations, overrides and escalation policies. It takes alerts from monitoring tools and keeps paging people until someone acknowledges.

## Principles

- **Focused.** Incident response, starting with paging and on-call, alongside the monitoring you already have.
- **Free means free.** The server costs nothing; only services we host, like mobile push, can.
- **Never drop a page.** Every accepted alert is durably stored, and failures are loud, never silent.
- **Lightweight to run.** A small team can self-host it.
- **Observable.** Murphy exports its own telemetry and joins the traces it receives, via OpenTelemetry.
- **Automatable.** Everything is configurable through the API.

## How it works

Murphy runs as six small components: `ingest` receives webhooks, `api` serves the management API, `engine` decides who to page, `scheduler` handles escalation timers, `notifier` sends notifications, and `migrate` prepares the database. They talk through a message broker, keep their state in a database, and are released together under one version.

The [architecture overview](docs/architecture.md) explains each component and shows how an alert becomes a page.

## Running locally

Requires Docker.

| Command | Starts |
|---|---|
| `docker compose --profile murphy up` | everything: PostgreSQL, Redis, NATS, LocalStack, Grafana, and every component built from source |
| `docker compose --profile murphy watch` | the same, restarting components when the code changes |
| `docker compose up` | the services only, to run a component yourself |

The API is at http://localhost:8080, described by [`openapi/v1.yaml`](openapi/v1.yaml), and a fresh stack is seeded with example data. Monitoring tools send alerts to `ingest` at http://localhost:8081, described by [`openapi/ingest/v1.yaml`](openapi/ingest/v1.yaml). Logs, traces and metrics are in Grafana at http://localhost:3000 (Explore → Loki, Tempo or Prometheus).

To run one component yourself against those services, with Go installed:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./cmd/api
```

Each component reports its health at `http://localhost:9000/healthz` and `/readyz`.

Integration tests run against the services from `docker compose up`:

```
go test -tags integration ./...
```

## License

[AGPL-3.0](LICENSE). Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). To report a vulnerability, see [SECURITY.md](SECURITY.md).
