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

Murphy runs as six small components: `ingest` receives webhooks, `api` serves the management API, `engine` makes Murphy's decisions, `scheduler` publishes what's due, `notifier` reaches people and systems outside Murphy, and `migrate` prepares the database. They talk through a message broker, keep their state in a database, and are released together under one version.

The [architecture overview](docs/architecture.md) explains each component and shows how an alert becomes a page.

## Running locally

Requires Docker.

| Command | Starts |
|---|---|
| `docker compose --profile murphy up` | everything: PostgreSQL, MySQL, Redis, NATS, LocalStack, Grafana, Dex, and every component built from source |
| `docker compose --profile murphy watch` | the same, restarting components when the code changes |
| `docker compose up` | the services only, to run a component yourself |

The API is at http://localhost:8080, described by [`openapi/v1.yaml`](openapi/v1.yaml), and a fresh stack is seeded with example data. Every request needs a session. The local identity provider is Dex, at http://localhost:5556/dex, with three people whose password is `murphy`: `ana@murphy.test`, the initial admin, `bo@murphy.test`, and `cy@murphy.test`, whom the stack tests sign out everywhere. Passwords work alongside Dex locally: an admin creates a set-password link with `POST /v1/users/{id}/password-links`, its code sets the password with `POST /v1/auth/password`, and `POST /v1/auth/sessions` signs in with the email and password. The API reference at http://localhost:8090 signs you in through Dex first and sends your requests with your session, and http://localhost:8090/connect.html shows a connect code as a QR code for the app, with the command that opens it in the iOS Simulator. Elsewhere, sign in through the browser at http://localhost:8080/v1/auth/sign-in, or exchange an ID token from Dex for a session token with `POST /v1/auth/sessions`. Monitoring tools send alerts to `ingest` at http://localhost:8081, described by [`openapi/ingest/v1.yaml`](openapi/ingest/v1.yaml). Pushes to the official app go through Murphy's hosted relay, whose API is described by [`openapi/relay/v1.yaml`](openapi/relay/v1.yaml); locally, the WireMock receiver stands in for it under `/relay`. Webhooks Murphy sends are described by [`openapi/webhooks/v1.yaml`](openapi/webhooks/v1.yaml); seeded people get one pointing at a local WireMock receiver at http://localhost:8092, which shows what it got at http://localhost:8092/__admin/requests. Logs, traces and metrics are in Grafana at http://localhost:3000 (Explore → Loki, Tempo or Prometheus), with linked dashboards under Dashboards → Murphy:

- **Overview** follows an alert through each stage: intake, the queue, the engine, the scheduler and the notifier.
- **api**, **ingest**, **engine**, **scheduler** and **notifier** each show what that component does (requests, messages or scheduled messages), the database behind it, its runtime and its container.
- **Infrastructure** shows the containers, PostgreSQL, MySQL, Redis and NATS.

An OpenTelemetry Collector in the stack gathers container and database server metrics, and NATS queue metrics through its exporter.

To run one component yourself against those services, with Go installed:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./cmd/api
```

Each component reports its health at `http://localhost:9000/healthz` and `/readyz`.

Integration tests, the shared suites and the component tests, run against the services from `docker compose up`:

```
go test -tags integration ./...
```

Stack tests run against the whole stack from `docker compose --profile murphy up`:

```
go test -tags stack ./tests/stack/...
```

Load tests run against the same stack. `MURPHY_LOAD_PROFILE` picks the traffic, and CI runs every profile after every merge:

| Profile | Traffic per scenario | Scenarios |
|---|---|---|
| `smoke` (the default) | 10/s for 10 seconds | one after another |
| `load` | 50/s for 2 minutes | one after another |
| `stress` | rising from 10/s for 5 minutes | one after another |
| `sustained` | rising and falling between 10/s and 50/s for 10 minutes | all at once |


```
MURPHY_LOAD_PROFILE=load go test -tags load -v ./tests/load/...
```

Each scenario writes a JSON and a Markdown report, and the run ends with every dashboard's data over the run in `dashboards/`. They go to `murphy-load/<profile>` in the system's temporary folder, or to `MURPHY_LOAD_REPORT_DIR`, and the test prints where. Watch a run live on the Overview dashboard.

## License

[AGPL-3.0](LICENSE). Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). To report a vulnerability, see [SECURITY.md](SECURITY.md).
