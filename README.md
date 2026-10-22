# URL Shortener with Analytics

ATAD 2026–2027, project 1: a Go service that will create short links, redirect
visitors to their original URLs, and report visit statistics for each link.

- **Student:** Dume Denisa
- **Group:** -
- **Language:** Go
- **HTTP port:** 8080

## Current status

The repository contains the initial HTTP service and project metadata. The common
routes are available: `GET /health`, `GET /version`, `GET /`, and `POST /reset`.
The reset route currently returns 204; there is no application state to clear yet.
Link creation, redirects, analytics, and persistence are not implemented yet.

The planned API is `POST /links`, `GET /links`, `GET /{code}`,
`GET /links/{code}/stats`, and `DELETE /links/{code}`. Generated codes will contain
7 characters; custom codes must contain 4–10 letters, digits, hyphens or underscores.

## Run with Docker

Build the image locally from the repository root:

```sh
docker build -t atad-url-shortener:local .
docker run --rm -p 8080:8080 atad-url-shortener:local
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
`internal/version` provides build metadata. Add application packages under
`internal/` as implementation progresses.
Local data, credentials, and build output must stay outside version control.

## Submission documentation

Student metadata is recorded in `atad.json`. The group is `-` because no group
has been assigned.

`docs/architecture.md` is due in week 7. The final report must be committed as
`docs/report.docx`. See [documentation requirements](docs/README.md).
