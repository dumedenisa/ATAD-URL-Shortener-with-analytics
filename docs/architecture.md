# Architecture — URL Shortener with Analytics

## Components and request flow

`cmd/app` handles configuration, startup on port 8080, and graceful shutdown.
`internal/httpapi` validates requests and translates storage results into HTTP.
`internal/storage` owns SQL; `internal/version` supplies build metadata. Handlers
depend on a `LinkStore` interface.

A GET redirect follows `GET /{code}` → `redirectLink` → `Store.Get` (indexed
lookup by unique code) → destination validation → `Store.RecordVisit` (atomic
increment and timestamp) → HTTP 302 with `Location`. Unknown codes return 404;
invalid stored destinations return 422; storage failures return 500 without a
redirect. Destination availability is not checked over the network. HEAD
returns redirect metadata without counting a visit. Statistics and list reads
also leave counters unchanged. Application route names are reserved.

## Data and lifetime

SQLite stores one row per link: integer `id`, case-sensitive unique `code`,
`url`, UTC `created_at`, nonnegative `visits`, and nullable `last_visit_at`.
IDs start at 1. Visit timestamps use UTC with millisecond precision. Analytics
are aggregates, not a separate event log. Listings follow descending IDs for
stable reverse insertion order. Deletion removes a row and its statistics;
transactional reset removes all rows and restarts the ID sequence.

`DATABASE_PATH` selects the file (default `data/links.db`, or `/data/links.db`
in Docker). Rows, counters, timestamps, and the ID sequence survive closing and
reopening the file. A Docker volume mounted at `/data` preserves them across
container replacement. Each store uses one database connection, a five-second
busy timeout, and full synchronous writes. The server closes the store on exit.

## Three decisions

1. **SQLite with a pure-Go driver instead of an in-memory map or JSON file.**
   SQLite provides durable atomic operations and a unique constraint across
   independent connections; manual file replacement would need additional
   concurrency and crash handling. The driver supports `CGO_ENABLED=0`. Serialized writes limit throughput.
2. **Synchronous counting before 302 instead of a background queue.** An atomic
   SQL increment avoids lost updates, and a failed write prevents the redirect.
   Matching ID, code, URL, and creation timestamp prevents a stale request from
   counting a replacement link after reset or deletion. The cost is a durable
   write on the redirect path. A crash or disconnect after commit but before
   HTTP delivery can still leave a counted, undelivered redirect; SQL commit and
   network delivery are not atomic. A queue would need recovery or accept lost visits. If redirects become slow, first measure the
   synchronous write and connection wait before adding caching or batching.
3. **Random codes with insert-and-retry instead of checking existence first.**
   `crypto/rand` generates seven characters from a 64-character alphabet. The
   unique constraint decides the winner atomically. Generated collisions retry
   up to 32 times, skipping reserved names, then return 500. A requested custom
   code conflicts immediately with 409. Tests inject a generator to force
   collisions rather than relying on their low probability.

## Validation and remaining work

Tests cover collisions, concurrency, HTTP contracts, restart persistence,
deletion, and reset, including a real HTTP server with temporary databases.
Tests and the race detector pass; clients do not follow external redirects.

All specified application routes are implemented. The CI workflow tests and
smoke-checks Docker before publishing AMD64/ARM64 images to GHCR. The local
Docker smoke test verifies non-root execution, build metadata, and persistence
through container replacement. The final report is `docs/report.docx`.
Publication status and package access must be checked in GitHub Actions/GHCR.
No latency benchmark or crash/power-loss recovery experiment has been performed.
The common course guide's additional submission and assessment rules still
need checking.
