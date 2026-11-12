# URL Shortener with Analytics

ATAD 2026–2027 — Project 1

Student: Dume Denisa

Student ID: M374840 — Group: -

Email: denisa.dume@student.upt.ro

Language: Go 1.27

Repository: https://github.com/dumedenisa/ATAD-URL-Shortener-with-analytics

Container: ghcr.io/dumedenisa/atad-url-shortener-with-analytics:latest

## 1. Objective and implemented scope

The service turns an HTTP or HTTPS URL into a short code, resolves that code
with an HTTP redirect, and maintains aggregate visit statistics. All project
routes are implemented. It supports user-selected codes, generated seven-character
codes, listing, deletion, and a reset operation. SQLite preserves links and
statistics across restarts. The interface is an HTTP API rather than a graphical
application. The container listens on port 8080 and runs as a non-root user.

This report describes the implementation and its limitations. The image is
published by the repository's CI workflow after automated checks; Actions run
results and image tags provide the build's externally verifiable evidence.

---
## 2. Architecture and stored data

The entry point, cmd/app, owns process startup, database configuration, version
output, and graceful shutdown. internal/httpapi owns routing, JSON validation,
status codes, and response construction. Its LinkStore interface separates HTTP
from SQL. internal/storage owns SQLite access and atomic mutations, while
internal/version supplies build identity for the CLI and HTTP version endpoint.

A link row contains an integer ID, case-sensitive unique code, destination URL,
UTC creation timestamp, visit count, and nullable last-visit timestamp. IDs start
at 1. Creation uses Go's UTC clock; visit time is assigned during SQL execution
with millisecond precision. Analytics are aggregates rather than individual
click-event records. Listing follows descending IDs, giving stable reverse
insertion order even if the wall clock changes.

DATABASE_PATH defaults to data/links.db locally and /data/links.db in Docker.
Missing parent directories are created. Named volumes mounted at /data retain
SQLite data when containers are removed and recreated. Each store uses one
connection, a five-second busy timeout, and full synchronous writes. Delete
removes the row and its statistics; reset transactionally removes all links and
restarts the ID sequence.

## 3. HTTP contract

- GET /health: 200 with status and integer uptime_seconds.
- GET /version: 200 with app, version, commit, and built_at.
- GET /: 200 HTML identifying the application, student, group, and project.
- POST /reset: 204 with no body; clears persistent application state.
- POST /links: 201 with id, code, url, and created_at; 409 for a taken custom code.
- GET /{code}: 302 with Location; unknown codes return 404.
- GET /links: 200 JSON array, newest first; an empty list is [].
- GET /links/{code}/stats: 200 with code, visits, and last_visit_at.
- DELETE /links/{code}: 204 without a body, or 404 for an absent code.

Validation failures use 400 validation. Missing resources use 404 not_found;
conflicting custom codes use 409 conflict. An invalid destination already in
storage is 422 unprocessable. Storage or generation failures use 500 internal.
Errors contain error and message fields. URL validation accepts absolute HTTP
or HTTPS URLs with a host and no unescaped whitespace. Custom codes contain
4–10 ASCII letters, digits, hyphens, or underscores. Application route names
are reserved. Missing or null code requests automatic generation.

---
## 4. Correctness decisions and tradeoffs

### Uniqueness under concurrent requests

Codes use a database UNIQUE constraint. A pre-insert existence check is not
sufficient because another request could insert after the check. The INSERT
itself determines success. A custom-code conflict returns 409 immediately
without modifying the existing link or silently renaming the requested code.

Generated codes use crypto/rand and a 64-character alphabet. Uniform byte mapping
produces exactly seven characters. A generated collision retries with a new code;
reserved names are skipped. After 32 attempts the request fails with a JSON 500,
preventing unbounded work. Tests replace the generator to force collisions,
exhaustion, and competing requests; correctness does not depend on low collision
probability.

### Redirect path and visit persistence

A GET redirect looks up the link through the unique-code index, validates its
stored destination, atomically increments visits, sets last_visit_at, then emits
302. Unknown codes, invalid stored destinations, and failed writes do not emit
a successful redirect. Statistics reads and HEAD requests never increment visits.

The update matches ID, code, URL, and creation timestamp. If a reset or deletion
recreates a link between lookup and update, the old request cannot increment the
replacement row. Atomic SQL arithmetic avoids lost updates between concurrent
requests and between independent store connections.

Counting synchronously was chosen over an in-memory background queue to avoid
losing queued visits on shutdown. It adds a durable database write to redirect
latency. A crash or disconnect after the commit but before HTTP delivery can
still leave a counted, undelivered redirect: a SQL transaction and network
receipt cannot be committed atomically. This boundary is explicitly accepted.
The server does not check whether destination websites are reachable. The first
performance investigation should measure the write and connection wait, rather
than assume that code lookup is the bottleneck.

### Storage and deployment

SQLite was chosen over a map or manually rewritten JSON for transactional
persistence and uniqueness across connections. Its pure-Go driver permits a
static build with CGO disabled. This avoids requiring a C SQLite library in the
runtime image. Serialized writes are a throughput limitation. A two-stage Docker
build keeps the compiler outside the runtime image; UID 10001 owns /data.

---
## 5. Verification and observed behavior

Unit and handler tests cover URL/code validation, malformed JSON, exact response
fields, custom-code conflicts, reserved names, generation retries, redirect
headers, broken links, storage errors, and route precedence. Storage tests check
reopening, persistent ID allocation, and uniqueness across two stores.

Concurrency tests issue simultaneous custom-code creations, force generated-code
collisions, increment visits through separate connections, and delete the same
code concurrently. One custom-code request succeeds and the other conflicts;
one delete succeeds and the other reports not_found. Concurrent visit totals
match the number of successful redirects.

The HTTP integration suite starts a real local server with temporary SQLite
files and a client that refuses to follow redirects. It exercises creation,
redirect, two statistics reads, HEAD, generated links, closing/reopening the
server and database, listing, deletion, and reset. External sites are not called.
The Docker smoke script also passed locally: it checked version metadata, UID
10001, API behavior, and persistence after container recreation with the same
named volume. GitHub Actions repeats this check before publication.

The following Go checks pass:

```sh
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build -o /tmp/atad-app ./cmd/app
```

Before the first visit, stats report visits=0 and last_visit_at=null. After one
GET redirect, repeated reads both report visits=1 with an unchanged timestamp.
Deleting a code removes its redirect and statistics. Recreating it starts with
zero visits and a new ID. Reset clears persistent rows and restarts IDs at 1.

## 6. Build, CI, and reproducibility

GitHub Actions checks formatting, module checksums, tests, race detection, vet,
and a CGO-free build. A container job then builds and smoke-tests an image before
publishing AMD64/ARM64 images to GHCR. Pull requests verify without publishing.
Main-branch pushes publish latest and an immutable-by-convention sha-<full SHA>
tag. The binary embeds commit and built_at from CI, and reports them consistently
through --version and /version. The image's CLI version defaults to 0.1.0.

```sh
docker run --rm -p 8080:8080 -v atad-links:/data \
  ghcr.io/dumedenisa/atad-url-shortener-with-analytics:latest
```

The README documents local builds, API examples, image use, and verification.
This report is regenerated with python3 scripts/build_report.py; its Markdown
source is committed beside the required docs/report.docx artifact.

---
## 7. Limitations and future work

No latency benchmark or crash/power-loss recovery experiment has been performed.
The tests establish behavior under ordinary restarts and concurrent requests,
not a guarantee about every operating-system or storage failure. Counting and
HTTP delivery have the failure boundary described above. There is no background
queue, per-visitor event log, authentication, rate limiting, expiry, pagination,
or graphical interface. The reset endpoint is intentionally available for the
course contract; this service should not be treated as a hardened public product.

A future performance iteration should measure synchronous write latency, then
consider WAL mode, connection changes, or durable asynchronous event recording.
Any queue design must state what can be lost and how recovery works. The common
course guide's extra assessment, calendar, and submission requirements must be
checked separately; only the project PDF and starter requirements were available.

## References

- ATAD 2026–2027, Project 1 — URL Shortener with Analytics (assignment PDF).
- Repository starter documentation: docs/README.md and the original Go template.
- Go net/http: https://pkg.go.dev/net/http
- SQLite driver: https://pkg.go.dev/modernc.org/sqlite
- SQLite transactions: https://www.sqlite.org/lang_transaction.html
- GitHub container publishing: https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images
