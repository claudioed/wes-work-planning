---
id: running-locally
title: Running locally
sidebar_label: Running locally
sidebar_position: 3
description: Run the service in-memory or against Postgres, exercise the API with curl, and connect it to the shared Kafka broker.
---

# Running locally

## Option 1 — in-memory, no infrastructure

```sh
go run ./cmd/wes
```

The server listens on `:8080` with in-memory repositories. State is lost on
restart; this is the fastest way to try the API.

## Option 2 — Postgres

```sh
docker compose up -d

# requires the golang-migrate CLI
migrate -path migrations \
  -database "postgres://wes:wes@localhost:5432/wes?sslmode=disable" up

DATABASE_URL="postgres://wes:wes@localhost:5432/wes?sslmode=disable" go run ./cmd/wes
```

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Address the HTTP server listens on |
| `DATABASE_URL` | *(unset)* | Postgres DSN; falls back to in-memory repositories if unset |
| `EVENT_PUBLISHER` | `log` | `log` or `kafka` — where domain events get published |
| `KAFKA_BROKERS` | *(unset)* | Comma-separated brokers. Required for `EVENT_PUBLISHER=kafka`, and setting it also starts the inbound integration-event consumer |

Note that `KAFKA_BROKERS` and `EVENT_PUBLISHER` are independent: setting
`KAFKA_BROKERS` alone starts *consuming* without switching the publisher away
from `log`.

## A full loop with curl

```sh
# 1 — the charge: 900 units due by 18:00, 500 more by 21:00
curl -s -X POST localhost:8080/paths/pick-a/charge \
  -H 'Content-Type: application/json' \
  -d '{"buckets":[{"cpt":"2026-08-23T18:00:00Z","quantity":900},
                  {"cpt":"2026-08-23T21:00:00Z","quantity":500}]}'

# 2 — the plan: 6 heads on 8 installed stations, 95 units/h, 8 h
curl -s -X POST localhost:8080/paths/pick-a/plan \
  -H 'Content-Type: application/json' \
  -d '{"plannedHeads":6,"installedStations":8,"rateUnitsPerHour":95,"hours":8}'

# 3 — enqueue two work units with different CPTs
curl -s -X POST localhost:8080/paths/pick-a/work-units \
  -H 'Content-Type: application/json' \
  -d '{"workUnitId":"wu-1","cpt":"2026-08-23T21:00:00Z","reference":"order-77/line-1"}'
curl -s -X POST localhost:8080/paths/pick-a/work-units \
  -H 'Content-Type: application/json' \
  -d '{"workUnitId":"wu-2","cpt":"2026-08-23T18:00:00Z","reference":"order-78/line-1"}'

# 4 — release: returns wu-2, the earlier CPT, regardless of insert order
curl -s -X POST localhost:8080/paths/pick-a/release

# 5 — telemetry, then the rebalance recommendation
curl -s localhost:8080/paths/pick-a/telemetry
curl -s localhost:8080/paths/pick-a/rebalance

# 6 — complete it
curl -s -X POST localhost:8080/work-units/wu-2/complete
```

Every endpoint, with full schemas and status codes, is in the
[REST API Reference](../api/rest-overview.md).

## Connecting to the shared Kafka broker

A single broker is shared by all five `warehouse-systems` services
(`~/warehouse-systems/docker-compose.kafka.yml`, `localhost:9092`). This
service does **not** run its own.

```sh
KAFKA_BROKERS=localhost:9092 EVENT_PUBLISHER=kafka go run ./cmd/wes
```

Smoke-test the consumer by hand — publish a `ShiftPlanCommitted`-shaped
message onto the workforce topic and read back the projection:

```sh
echo '{"event_id":"11111111-1111-4111-8111-111111111111",
       "event_type":"ShiftPlanCommitted",
       "occurred_at":"2026-08-23T09:00:00Z",
       "source":"workforce-management",
       "data":{"building_id":"BLD1","shift_id":"S1","path_id":"pick-a",
               "planned_heads":7,"planned_rate":95.5,"planned_hours":8}}' \
| kafka-console-producer.sh --bootstrap-server localhost:9092 \
    --topic warehouse.workforce.events

curl -s localhost:8080/paths/pick-a/labor-plan-view
# {"pathId":"pick-a","plannedHeads":7,"plannedRate":95.5,"plannedHours":8,"observedAt":"..."}
```

Replaying the exact same message is a no-op — see
[Idempotency](../ecosystem/integration-events.md#idempotency).

## Test suites

```sh
go build ./...
go vet ./...
go test ./...            # unit + httptest + godog acceptance
go test ./... -race
go test -tags=integration ./...   # needs DATABASE_URL and/or KAFKA_BROKERS
```

CI (`.github/workflows/ci.yml`) runs these as separate jobs — `lint`, `test`,
`bdd`, `integration`, `api-lint`, `arch-test`, `helm-lint`, with a
weekly/dispatch-only `mutation` job and a `docker-publish` job gated behind
all of them on pushes to `main`.
