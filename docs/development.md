# Development guide

How code is written in this repository. What the system does and why is in the [architecture overview](architecture.md).

These rules are binding for every change, whether a person or a coding agent makes it. **MUST** and **MUST NOT** mean exactly that: a change that breaks a rule is not merged. **MAY** marks the only freedom a rule allows. A rule changes only through a pull request that changes this guide.

## Project layout

Every piece of code has one obvious place, so anyone can find it and nothing leaks across boundaries.

| Path | Contains |
|---|---|
| root package `murphy` | Domain types generated from `proto/`, and hand-written methods on them |
| `services/<domain>/` | One service per domain: its business logic, the interfaces it needs, and its storage under `db/`, one folder per engine |
| `internal/` | Thin clients and helpers with no domain logic: connections, provider clients, configuration loading, telemetry, serving HTTP |
| `cmd/<component>/` | One executable per component: `main.go`, its `app/` (its settings and wiring), and its entry code (HTTP routes or message handlers) |
| `openapi/` | OpenAPI specs |
| `proto/` | Protobuf definitions of the domain entities and the messages that carry them |
| `client/` | Go client generated from the OpenAPI specs |
| `tests/` | Integration, end-to-end and load tests, and the environment they run in |

1. The root package MUST NOT import anything from this repository.
2. Services MUST depend on interfaces, never on a concrete client.
3. Implementations MUST be wired into services only in `cmd/`.
4. `internal/` MUST NOT contain domain logic.

## Services

A service owns one domain. It states what it needs as interfaces, so the same logic runs on any database, broker or provider.

1. A service MUST declare the interfaces it needs next to the code that uses them.
2. Interfaces MUST describe what the service needs in domain terms (acknowledge an incident), never a generic capability (run a query).
3. A service's repository MUST live in the service's `db/<engine>/` folder and use only the service's own queries and generated code. An insert of many rows that sqlc can't express on an engine MAY be built with that engine's helper in `internal/`, with every value passed as a parameter.
4. Each service's `db/repository.go` MUST declare the `Engine` its engines implement and the `Repository` that wraps it. Logic that combines several queries MUST live there, once, and each engine MUST provide single statements.
5. Each main type of a package MUST have its own file, named after it (`service.go` for `Service`), with the interfaces it needs.
6. A component's `app` MUST build its clients, repositories and services from its settings, and its `main.go` MUST only load configuration and run it. Nothing else wires dependencies: tests use the same `app`, so they run the wiring production runs.

## Implementations

Databases, message brokers and notification providers are interchangeable. Adding one never changes the services that use it.

1. A new implementation MUST implement an existing interface.
2. Its client MUST live in `internal/`, and the implementation next to the interface's other implementations.
3. It MUST pass the interface's shared test suite unchanged, against the real service in a container or an emulator.
4. It MUST be chosen by the scheme of its URL (`postgres://`, `redis://`). Components MUST pass that URL in and MUST NOT name an implementation, so adding one changes no component.
5. Every external endpoint it uses MUST be configurable, and its setting MUST be named after its purpose, never its product (`MURPHY_CATALOG_DATABASE_URL`, not `MURPHY_POSTGRES_URL`).

## Configuration and lifecycle

Every component starts, reports its health and stops the same way, so it behaves predictably in Docker Compose, Kubernetes or anywhere else it runs.

1. Configuration MUST come only from environment variables prefixed `MURPHY_`, loaded at startup through `internal/config`. The only exceptions are the standard OpenTelemetry variables (`OTEL_*`), which configure telemetry, and the standard AWS variables (`AWS_*`), which configure credentials, region and endpoints.
2. A missing or invalid setting MUST stop the component at startup, with an error naming every variable at fault and never its value.
3. Every component MUST run through `internal/lifecycle`, which sets up telemetry first. On `SIGTERM` or `SIGINT` it stops taking new work, finishes what it has and flushes telemetry, all within `MURPHY_SHUTDOWN_TIMEOUT`.
4. Every component MUST serve `/healthz` and `/readyz` on `MURPHY_HEALTH_ADDR`. `/healthz` answers whether the process is alive; `/readyz` answers whether it can do its work, and MUST check every dependency it needs.
5. Health responses MUST NOT include error details.

## Domain and events

The domain is defined once, in Protobuf, so every component, and later every client, agrees on what an incident or a notification is. Components communicate only through events: facts about entities that anyone can react to.

1. Domain entities MUST be defined in Protobuf under `proto/murphy/v1/`, in one `<domain>.proto` per domain. Types used by several domains MUST go in `shared.proto`. The Go types in the root package MUST be generated from them and used everywhere; there MUST NOT be a second, hand-written model.
2. Behaviour that needs only an entity's own data MUST live in the root package, in the hand-written `<domain>.go` next to the generated `<domain>.pb.go`. Behaviour shared by several domains goes in `shared.go`. Behaviour that also needs configuration, storage or another service belongs to the domain's service.
3. Generated types MUST be passed by pointer and MUST NOT be copied.
4. Every message MUST be an event named `<Entity><PastTenseVerb>`. The verb MUST be `Created`, `Updated`, `Deleted` or a change of state in the domain (`Acknowledged`, `Resolved`, `Delivered`, `Expired`).
5. A message MUST NOT ask a specific component to do something.
6. Each entity MUST have one event message, with the verb as an enum and two snapshots of the entity: `previous` (empty for `Created`) and `current` (empty for `Deleted`).
7. Every message MUST travel in an `Envelope` holding its ID, the time it happened, the trace context, and the event as a `oneof` with one case per entity.
8. The broker MUST carry only the encoded envelope. Nothing MUST depend on broker-specific headers or attributes.
9. A change MUST stay compatible with the previous release: fields, `oneof` cases and enum values MAY be added, and MUST NOT be renumbered or reused. `buf breaking` enforces this.
10. An incompatible change MUST go into a new package version (`v2`), and consumers MUST read both versions until the old one is drained.

## Delivery

A page must never be lost or sent twice. The broker delivers at least once and any process can crash at any moment, so every message takes effect exactly once through retries and deduplication.

1. Every published message MUST have something that retries it after a failure: the caller for `ingest` (no `202` means it sends again), redelivery by the broker for `engine` and `notifier`, and the next poll for `scheduler` (a timer stays due until its message is published).
2. `ingest` MUST answer only after the broker confirms its message.
3. Work MUST be marked done only after its messages are published: publish, wait for the broker's confirmation, then acknowledge the incoming message or mark the timer as fired.
4. Message IDs MUST be deterministic, derived from what caused them (the incoming message or the timer), so running the work again publishes the same IDs.
5. Every consumer MUST record the IDs it has handled in a table its service owns, in the same transaction as the change the message causes. When a message arrives again, the change MUST be skipped and the messages it produced MUST be published again with the same IDs.
6. Records of handled IDs MUST be removed after a retention period.
7. Components MUST retry transient failures themselves (timeouts, dropped connections, throttling, deadlocks) with exponential backoff and jitter, within the context's deadline. They MUST NOT retry permanent failures such as invalid data. When they give up, rule 1 takes over.
8. Every retry MUST be recorded in the trace.

## Database

Murphy is a modular monolith: services share one database, and each service owns its tables. Ownership keeps services independent; the rules on migrations keep upgrades safe while old and new components run side by side.

1. Each domain MUST have its own database. Its URL is configured in the component that hosts the domain (`MURPHY_<DOMAIN>_DATABASE_URL`) and passed to the domain's `db.Open`, which never reads configuration itself. Every database a component uses MUST be part of its readiness check.
2. A service's storage for an engine MUST live in `services/<domain>/db/<engine>/`: its migrations, its queries, the code generated from them and its repository.
3. Only the owning service MUST read or write its tables. Other services MUST reach that data through the owning service, never through SQL.
4. Services MUST refer to each other's data by ID. There MUST NOT be foreign keys between tables of different services.
5. A transaction MUST NOT span services. Anything that must change in one transaction belongs to one service; services coordinate through events.
6. A transaction MUST hold a connection and locks only while writing, never during computation or network calls.
7. Each service's migrations MUST be tracked separately, in its own `<service>_migrations` table, applied by `migrate`, and MUST NOT depend on another service's migrations.
8. Migrations MUST be plain SQL, numbered in sequence within the service, and MUST NOT be edited once released.
9. Migrations MUST only move forward, and each MUST work with both the current and the previous release. Removing or renaming something MUST take two releases: first stop using it, then remove it.
10. Tables MUST be plural and columns `snake_case`. Every table MUST have an `id` primary key, references MUST be named `<entity>_id`, and times MUST be `created_at` and `updated_at`, stored with a time zone in UTC. Indexes and constraints MUST be named explicitly.
11. IDs MUST be UUIDv7, generated by the application.
12. Queries MUST list their columns explicitly, and every query that runs often MUST be backed by an index.
13. The number of queries an operation runs MUST stay the same however much data it touches: related rows are read, checked and written for many IDs at once.
14. The database MUST NOT contain logic: no triggers and no stored procedures.

## HTTP API

The API is the public way to build on Murphy, so its behaviour is predictable across every endpoint and stable across releases.

1. The major version MUST be in the path (`/v1/…`), with one OpenAPI spec per major version.
2. Within a version, changes MUST only add: new endpoints, new optional parameters, new response fields. A breaking change MUST start a new version, and the previous version MUST keep working for a deprecation period.
3. List endpoints MUST paginate with cursors and MUST NOT use offsets. A request sets `limit` (with a default and a maximum) and the `cursor` from the previous response; a response returns the items and `next_cursor`, empty on the last page.
4. Lists MUST come in a stable order, newest first unless the endpoint documents otherwise.
5. Filters MUST be query parameters named after the field they filter (`status=triggered`). A repeated parameter matches any of its values, and time ranges MUST use `<field>_after` and `<field>_before`.
6. Unknown parameters MUST be rejected, never ignored.
7. Errors MUST be answered as `application/problem+json` and MUST NOT expose internal details. Unexpected failures MUST be logged and answered with `500`.
8. Every request that creates something or triggers an action MUST accept an `Idempotency-Key` header. A retry with the same key and the same request MUST return the original response without doing the work again; the same key with a different request MUST be rejected. Keys MUST be kept for 24 hours.

## Tracing, logs and metrics

Every page must be explainable afterwards: what happened, in which order, and why. Murphy traces its own work, continues the traces it receives, and measures how it performs.

1. Every method on a layer we write MUST be traced with `internal/tracer`: HTTP handlers, services, repositories and stores, and every message or timer handler. Beneath them, protocols MUST be traced by their standard OpenTelemetry instrumentation: incoming requests, database queries, and calls to Redis, a broker or another service. Functions that run for the life of the process and health checks are not traced.
2. Our span names MUST come from the function name, never typed by hand. Instrumented spans follow OpenTelemetry's conventions (`GET /v1/services/{id}`).
3. A function's error MUST be recorded on its span. Only unexpected errors mark the span as failed: invalid input, something not found and conflicts are expected outcomes, and HTTP requests fail only with a `5xx`.
4. Logs MUST be written with `internal/logger`, and every log line MUST carry the current trace and span IDs. Every HTTP request MUST be logged once, with its method, route, status and duration.
5. Fields MUST describe what is being worked on, and MUST NOT contain secrets or contact details. Murphy's prefix is added to our own fields (`service.id` becomes `murphy.service.id`); standard OpenTelemetry attributes keep their names (`http.route`).
6. Fields that apply to everything below a point, such as the incident being handled, MUST be added to the context once: with `telemetry.With` before a span starts, or with the span's `Set` once the value is known. They then appear on every log line and span beneath it, and a field added again with the same key replaces the earlier one.
7. Metrics MUST be recorded with `internal/metrics` or come from instrumentation. Their dimensions MUST have few possible values (a route, a channel, a provider), never per-item values such as an incident ID; context fields are never added to metrics.
8. Incoming requests and messages MUST continue the caller's trace, and outgoing calls and published messages MUST carry it on.
9. Every HTTP response MUST carry its trace ID (`X-Trace-Id` and `traceresponse`), and every error body MUST include it as `trace_id`.
10. Traces, metrics and logs MUST be exported over OTLP when an endpoint is configured. Without one, nothing is exported and the component runs normally.
11. Everything a component emits MUST identify it: `service.namespace` (`murphy`), `service.name`, `service.version` and a `service.instance.id` unique to the process, plus the host and container it runs on. The version comes from the build: the release tag, or a pseudo-version for untagged builds.
12. The log level MUST be set with `MURPHY_LOG_LEVEL` (`debug`, `info`, `warn` or `error`; `info` by default), and applies to every log destination.
13. Only `internal/logger`, `internal/tracer`, `internal/metrics` and `internal/telemetry` MAY import OpenTelemetry packages or `log/slog`, plus the thin clients in `internal/` that install their library's instrumentation. Everything else uses their APIs.

## Testing

Every kind of failure Murphy guards against is tested, without changing the code that runs in production.

| Kind | Where | Runs against |
|---|---|---|
| Unit | `*_test.go` next to the code | the code under test, with generated mocks of its interfaces |
| Shared suites | next to each interface | every implementation of that interface, against the real service |
| Integration | `tests/integration/` | one component's handlers, served in-process, against real infrastructure |
| End-to-end | `tests/e2e/` | the whole system from `compose.yaml` (`--profile murphy`), as an API client |
| Load | `tests/load/` | all components, under generated traffic |

1. Tests MUST get their infrastructure from `compose.yaml`, locally and in CI, so they always run against the same versions and settings as development. `tests/testenv/` MUST provide what tests need on top of it: a throwaway database per test, stub notification providers (they record what was sent, fail on demand and send callbacks), and traffic generators that send alerts in each monitoring tool's format.
2. Tests MUST change behaviour only through configuration. Components MUST NOT contain test-only code paths.
3. Interfaces MUST be mocked with generated mocks. Tests MUST check outcomes (what was returned, what reached a dependency), not restate how the code is implemented.
4. Tests MUST use `testify`: `require` for anything the rest of the test depends on, `assert` for the checks themselves.
5. Every consumer MUST have tests for a crash between each step of its delivery handling.
6. `tests/seed/` MUST give a fresh local stack realistic data, created only through the API and safe to run again. Every domain MUST add its data there in the PR that adds the domain.
7. `go test ./...` MUST run the unit tests without Docker or network access. Tests under `tests/` MUST use the `integration` build tag, except end-to-end tests, which use `e2e`, and load tests, which use `load`.
8. Every load test MUST state its thresholds (share of successful requests, p99 latency) and fail when a run breaks them. Load tests MUST run the same scenarios in every profile (`smoke`, `load`, `stress`, `soak`, chosen with `MURPHY_LOAD_PROFILE`); only the traffic changes. Every run MUST write a JSON and a Markdown report per scenario, and a snapshot of every dashboard's data over the run, to `MURPHY_LOAD_REPORT_DIR`. CI MUST run every profile after every merge to `main`, against the full stack with its seed data, and keep the reports.

## Code

1. Code MUST use the standard library first: no web framework, no ORM, and no dependency for what a few lines of code can do. Tests are the one exception: they use `testify`, and load tests generate traffic with `vegeta`.
2. `context.Context` MUST be the first parameter of anything that does I/O.
3. Errors MUST be wrapped with what was being done.
4. Secrets MUST NOT appear in logs or errors.
5. OpenAPI specs, Protobuf definitions and SQL queries MUST change first, followed by regeneration. Generated code MUST be committed and MUST NOT be edited by hand.

## Tooling

1. The Go toolchain MUST be pinned with the `toolchain` directive, and tools (`sqlc`, `oapi-codegen`, `buf`, `protoc-gen-go`, `mockery`, `goose`, `ko`, `govulncheck`) with `tool` directives, all on their latest stable versions.
2. `gofmt`, `go vet`, `golangci-lint`, `buf lint`, `buf breaking` and `govulncheck` MUST pass.
3. `compose.yaml` MUST provide every service the components depend on (PostgreSQL, MySQL, Redis, NATS, LocalStack, and Grafana's LGTM stack for telemetry at `localhost:3000`, with an OpenTelemetry Collector that gathers container and dependency metrics), MUST create every domain's database and the broker's topics and subscriptions on every start, and MUST run every component from source in its `murphy` profile: `docker compose up` starts the services only, `docker compose --profile murphy up` starts everything, and `docker compose --profile murphy watch` also restarts the components when code changes and reruns `migrate` when a migration changes.
4. Dashboards MUST live in `tests/provision/local/grafana/dashboards/` as JSON and be provisioned by `compose.yaml`: Overview follows an alert stage by stage, Component shows any one component, and Infrastructure shows what Murphy depends on. A change that adds a metric MUST add it to the dashboard it belongs to, a new component MUST add its stage to Overview, and a new dependency MUST add its row to Infrastructure.
5. Container images MUST be built with `ko`, one per component, from `.ko.yaml`. The release workflow publishes them to `ghcr.io/murphyhq/<component>` for every `v*` tag.
6. Dependencies MUST be kept current through Dependabot, for Go modules, GitHub Actions and the images in `compose.yaml`.

## Docs and comments

1. Docs MUST describe what the code is and does now: no status tables, TODO lists or history.
2. Comments MUST explain *why*, never *what*.
3. Comments are written by people. A coding agent MUST NOT add a comment, not even a single line, without asking for permission first.
