# URL Shortener with Analytics

ATAD 2026–2027, project 1: a Go service that will create short links, redirect
visitors to their original URLs, and report visit statistics for each link.

- **Student:** Dume Denisa
- **Group:** -
- **Language:** Go
- **HTTP port:** 8080

## Current status

The service implements the common routes (`GET /health`, `GET /version`,
`GET /`, `POST /reset`) and `POST /links` with an optional custom code. Links are saved
in SQLite; duplicate codes return 409 without changing the existing link.
Reset clears persistent links, statistics, and the ID sequence.

`GET /{code}` now redirects to a stored destination with HTTP 302.
Visit counting and `GET /links/{code}/stats` are implemented.
Listing and deletion endpoints are not implemented yet.
Omitting `code` (or supplying null) generates a code of exactly 7 characters.
Custom codes contain 4–10 ASCII letters, digits, hyphens or underscores and are
case-sensitive. The codes `health`, `version`, `reset`, and `links` are reserved
and rejected with 400 validation. URLs must be absolute HTTP or HTTPS URLs with
a host and no unescaped whitespace. Invalid JSON and invalid fields return
400 with `{"error":"validation","message":"..."}`.

## Run with Docker

Build the image locally from the repository root:

```sh
docker build -t atad-url-shortener:local .
docker run --rm -p 8080:8080 -v atad-links:/data atad-url-shortener:local
```

No published image is required for these commands. The Dockerfile currently
requires the `golang:1.27-alpine` build image specified by the starter template.
Build metadata can be supplied through the `COMMIT` and `BUILT_AT` build arguments.
The build injects these values into the binary used by `/version` and `--version`.

## Run without Docker

Install the Go version specified in `go.mod` (currently 1.27), then run:

```sh
go run ./cmd/app
```

In another terminal:

```sh
curl -i http://localhost:8080/health
curl -i http://localhost:8080/version
curl -i http://localhost:8080/
curl -i -X POST http://localhost:8080/reset
```

`/health` returns HTTP 200 and JSON such as
`{"status":"ok","uptime_seconds":3}`. `/version` returns `app`, `version`,
`commit`, and `built_at`; `/` returns an HTML identity page; `/reset` returns
HTTP 204 with no response body.

Create a custom link:

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/doc/effective_go","code":"gofmt"}' \
  http://localhost:8080/links
```

To generate a code automatically:

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"url":"https://example.org"}' http://localhost:8080/links
```

Generated codes use `crypto/rand` and the 64-character alphabet
`A-Z a-z 0-9 - _`. The database insert decides uniqueness atomically. A generated
collision triggers a new attempt; reserved route names are skipped. There are at
most 32 attempts per request. Exhaustion, a generator failure, or a database
failure returns HTTP 500 with a JSON error. Custom-code conflicts still return
409 immediately and never change the requested code. Tests inject a generator to
force repeated collisions, exhaustion, and a collision between concurrent requests.
No new dependencies are required for generation.

The response is HTTP 201 with exactly `id`, `code`, `url`, and `created_at`.
Repeating the request returns HTTP 409 with `error: "conflict"`. Two simultaneous
requests for the same code produce one 201 and one 409. A storage failure returns
HTTP 500 with a JSON error and no database details.

Visit a short link without automatically following the redirect:

```sh
curl -i http://localhost:8080/gofmt
```

The response is HTTP 302 with `Location: https://go.dev/doc/effective_go`.
Unknown codes return HTTP 404 with `error: "not_found"`. Codes are case-sensitive.
An invalid destination already present in storage returns HTTP 422 with
`error: "unprocessable"`; storage failures return HTTP 500. A broken link means
an invalid stored URL here; the service does not contact destinations to check
their availability. HEAD returns the same redirect headers with no body.

The redirect path is HTTP routing → one indexed SQLite lookup by code → stored
URL validation → atomic visit update → HTTP 302. Existing application routes take precedence over the
short-code route; GET on the POST-only `/reset` and `/links` returns 405.
GET redirects increment the visit count; HEAD only returns metadata and does not
count. Broken links, missing codes, and failed database writes do not count.

Read statistics twice after one GET redirect:

```sh
curl -i http://localhost:8080/links/gofmt/stats
curl -i http://localhost:8080/links/gofmt/stats
```

Both responses report `visits: 1` and the same `last_visit_at`. The endpoint
returns HTTP 200 and exactly `code`, `visits`, and `last_visit_at`. Before the
first visit, `visits` is 0 and `last_visit_at` is null. Timestamps use UTC with
millisecond precision. Unknown codes return JSON 404; database failures return
JSON 500. Statistics survive restart and are cleared by `POST /reset`.

Visits are persisted synchronously before sending HTTP 302, with an atomic SQL
increment to avoid lost concurrent updates. Failure to save returns an error
without a redirect. This adds a database write to redirect latency. A process
crash or client disconnect after the commit but before receipt of the response
can leave a counted visit without a delivered redirect: the database commit and
HTTP delivery are not atomic. There is no background queue to lose on shutdown.
A reset/recreation between lookup and update cannot increment the new link's
counter: the update matches the original ID, code, URL, and creation timestamp.

To inspect the version without starting the server:

```sh
go run ./cmd/app --version
docker run --rm atad-url-shortener:local --version
```

Local builds may report `unknown` for the commit and an empty build timestamp
when build metadata is unavailable.

## Development

```sh
go test ./...
go test -race ./...
```

`cmd/app` handles process startup and graceful shutdown. `internal/httpapi`
constructs the HTTP handler and tests the common routes without a network listener.
`internal/storage` manages the persistent SQLite database.
`internal/version` provides build metadata. Add application packages under
`internal/` as implementation progresses.
Local data, credentials, and build output must stay outside version control.

## Persistent storage

`internal/storage` uses SQLite through the pure-Go `modernc.org/sqlite` driver,
compatible with `CGO_ENABLED=0`. `storage.Open(path)` creates the database and
missing parent directories; the caller closes the store when finished.

The database stores ID, case-sensitive unique code, destination URL, UTC creation
time, visit count (initially zero), and last visit time (initially null). Inserts
are atomic: a duplicate returns `storage.ErrCodeTaken` without overwriting the
existing link. Missing lookups return `storage.ErrNotFound`. IDs and data survive
closing and reopening the database. Reads do not increment visits.

SQLite serializes writes, with a five-second busy timeout and full synchronous
writes. Each store uses one connection. Tests cover collisions across two stores
opening the same database file. Input validation belongs to the HTTP application layer and is enforced by the
link-creation endpoint.

The server opens `data/links.db` by default. Override it with `DATABASE_PATH`.
The database is closed when the server exits, and `--version` does not open it.
Database files under `data/` are ignored by Git. In Docker, the default path is
`/data/links.db`, writable by the unprivileged application user. The named volume
in the example preserves links when the container is removed and recreated.
`POST /reset` permanently clears these persisted links too.

## Submission documentation

Student metadata is recorded in `atad.json`. The group is `-` because no group
has been assigned.

`docs/architecture.md` is due in week 7. The final report must be committed as
`docs/report.docx`. See [documentation requirements](docs/README.md).
