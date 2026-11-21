# Development guide

How code is written in this repository. What the system does and why is in the [architecture overview](architecture.md).

These rules are binding for every change, whether a person or a coding agent makes it. **MUST** and **MUST NOT** mean exactly that: a change that breaks a rule is not merged. **MAY** marks the only freedom a rule allows. A rule changes only through a pull request that changes this guide.

## Project layout

Every piece of code has one obvious place, so anyone can find it and nothing leaks across boundaries.

| Path | Contains |
|---|---|
| root package `murphy` | Domain types generated from `proto/`, and hand-written methods on them |
| `services/<domain>/` | One service per domain: its business logic, the interfaces it needs, and its storage under `db/<engine>/` (migrations, queries, generated code, repository) |
| `internal/` | Thin clients and helpers with no domain logic: connections, provider clients, configuration loading, telemetry |
| `cmd/<component>/` | One executable per component: `main.go`, its `config/`, and its entry code (HTTP routes or message handlers) |
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
3. A service's repository MUST live in the service's `db/<engine>/` folder and use only the service's own queries and generated code.
4. A component's `main.go` MUST build the clients from `internal/`, pass them into repositories and services, and start the component's entry code. Nothing else wires dependencies.

## Implementations

Databases, message brokers, notification providers and secret stores are interchangeable. Adding one never changes the services that use it.

1. A new implementation MUST implement an existing interface.
2. Its client MUST live in `internal/`, and the implementation next to the interface's other implementations.
3. It MUST pass the interface's shared test suite unchanged, against the real service in a container or an emulator.
4. It MUST be selectable by configuration, and every external endpoint it uses MUST be configurable.
5. It MUST accept secrets as references (`env://`, `file://`, `aws-ssm://`) and MUST NOT accept secret values.

## Domain and events

The domain is defined once, in Protobuf, so every component, and later every client, agrees on what an incident or a notification is. Components communicate only through events: facts about entities that anyone can react to.

1. Domain entities MUST be defined in Protobuf under `proto/murphy/v1/`. The Go types in the root package MUST be generated from them and used everywhere; there MUST NOT be a second, hand-written model.
2. Behaviour on an entity MUST live in hand-written files in the root package, next to the generated code.
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

1. A service's storage MUST live in `services/<domain>/db/<engine>/`: its migrations, its queries, the code generated from them and its repository.
2. Only the owning service MUST read or write its tables. Other services MUST reach that data through the owning service, never through SQL.
3. Services MUST refer to each other's data by ID. There MUST NOT be foreign keys between tables of different services.
4. A transaction MUST NOT span services. Anything that must change in one transaction belongs to one service; services coordinate through events.
5. A transaction MUST hold a connection and locks only while writing, never during computation or network calls.
6. Each service's migrations MUST be tracked separately and applied by `migrate`, and MUST NOT depend on another service's migrations.
7. Migrations MUST be plain SQL, numbered in sequence within the service, and MUST NOT be edited once merged.
8. Migrations MUST only move forward, and each MUST work with both the current and the previous release. Removing or renaming something MUST take two releases: first stop using it, then remove it.
9. Tables MUST be plural and columns `snake_case`. Every table MUST have an `id` primary key, references MUST be named `<entity>_id`, and times MUST be `created_at` and `updated_at`, stored with a time zone in UTC. Indexes and constraints MUST be named explicitly.
10. IDs MUST be UUIDv7, generated by the application.
11. Queries MUST list their columns explicitly, and every query that runs often MUST be backed by an index.
12. The database MUST NOT contain logic: no triggers and no stored procedures.

## HTTP API

The API is the public way to build on Murphy, so its behaviour is predictable across every endpoint and stable across releases.

1. The major version MUST be in the path (`/v1/…`), with one OpenAPI spec per major version.
2. Within a version, changes MUST only add: new endpoints, new optional parameters, new response fields. A breaking change MUST start a new version, and the previous version MUST keep working for a deprecation period.
3. List endpoints MUST paginate with cursors and MUST NOT use offsets. A request sets `limit` (with a default and a maximum) and the `cursor` from the previous response; a response returns the items and `next_cursor`, empty on the last page.
4. Lists MUST come in a stable order, newest first unless the endpoint documents otherwise.
5. Filters MUST be query parameters named after the field they filter (`status=triggered`). A repeated parameter matches any of its values, and time ranges MUST use `<field>_after` and `<field>_before`.
6. Unknown parameters MUST be rejected, never ignored.
7. Every request that creates something or triggers an action MUST accept an `Idempotency-Key` header. A retry with the same key and the same request MUST return the original response without doing the work again; the same key with a different request MUST be rejected. Keys MUST be kept for 24 hours.

## Tracing and logs

Every page must be explainable afterwards: what happened, in which order, and why. Murphy traces its own work and continues the traces it receives.

1. Every function that takes a `context.Context` MUST be traced, through the shared helper in `internal/telemetry`.
2. Span names MUST come from the function name, never typed by hand.
3. A function's error MUST be recorded on its span, and the span marked as failed.
4. Attributes MUST describe what the function worked on, with Murphy's prefix (`murphy.incident.id`). They MUST NOT contain secrets or contact details.
5. Incoming requests and messages MUST continue the caller's trace, and outgoing calls and published messages MUST carry it on.
6. Every log line MUST carry the current trace and span IDs.
7. Traces, metrics and logs MUST be exported over OTLP.

## Testing

Every kind of failure Murphy guards against is tested, without changing the code that runs in production.

| Kind | Where | Runs against |
|---|---|---|
| Unit | `*_test.go` next to the code | the code under test, with in-memory fakes of its interfaces |
| Shared suites | next to each interface | every implementation of that interface, against the real service |
| Integration | `tests/integration/` | one component with real infrastructure in containers |
| End-to-end | `tests/e2e/` | all components together |
| Load | `tests/load/` | all components, under generated traffic |

1. `tests/testenv/` MUST provide the test environment: infrastructure in containers, components started from configuration, stub notification providers (they record what was sent, fail on demand and send callbacks), and traffic generators that send alerts in each monitoring tool's format.
2. Tests MUST change behaviour only through configuration. Components MUST NOT contain test-only code paths.
3. Fakes MUST be hand-written, and tests MUST check outcomes (what was sent, what was stored), not which methods were called.
4. Every consumer MUST have tests for a crash between each step of its delivery handling.
5. `go test ./...` MUST run the unit tests without Docker or network access. Tests under `tests/` MUST use the `integration` build tag.

## Code

1. Code MUST use the standard library first: no web framework, no ORM, and no dependency for what a few lines of code can do.
2. `context.Context` MUST be the first parameter of anything that does I/O.
3. Errors MUST be wrapped with what was being done.
4. Secrets MUST NOT appear in logs or errors.
5. OpenAPI specs and Protobuf definitions MUST change first, followed by regeneration. Generated code MUST be committed and MUST NOT be edited by hand.

## Tooling

1. The Go toolchain MUST be pinned with the `toolchain` directive, and tools (`sqlc`, `oapi-codegen`, `buf`, `goose`, `ko`) with `tool` directives in `go.mod`, all on their latest stable versions.
2. `gofmt`, `go vet`, `golangci-lint` and `buf lint` MUST pass.
3. `compose.yaml` MUST provide every service the components depend on (PostgreSQL, NATS and LocalStack) and MUST run every component from source in its `murphy` profile: `docker compose up` starts the services only, `docker compose --profile murphy up` starts everything, and `docker compose --profile murphy watch` also restarts the components when code changes.
4. Container images MUST be built with `ko`, one per component, from `.ko.yaml`. The release workflow publishes them to `ghcr.io/murphyhq/<component>` for every `v*` tag.
5. Dependencies MUST be kept current through Dependabot, for Go modules, GitHub Actions and the images in `compose.yaml`.

## Docs and comments

1. Docs MUST describe what the code is and does now: no status tables, TODO lists or history.
2. Comments MUST explain *why*, never *what*.
3. Comments are written by people. A coding agent MUST NOT add a comment, not even a single line, without asking for permission first.
