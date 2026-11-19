---
description: Build piecemeal from source, run its tests, end-to-end checks and load test, and contribute changes.
---

# Development

piecemeal is a Go daemon with an embedded React web UI. Contributions are welcome: open an issue to discuss anything large first.

## Layout

| Path | What's there |
| --- | --- |
| `cmd/piecemeal` | The binary: configuration, startup, HTTP routes and the CLI commands. |
| `internal/engine` | Everything that touches anacrolix/torrent: torrents, files, queueing, seeding goals, paths, import. Nothing else imports anacrolix. |
| `internal/api` | The native REST API, on [Huma](https://huma.rocks), which also produces the OpenAPI document. |
| `internal/qbit` | The qBittorrent WebAPI v2. |
| `internal/auth`, `internal/rss`, `internal/fetch`, `internal/settings`, `internal/store` | Users, sessions and keys; RSS; URL fetching with the SSRF filter; runtime settings; SQLite with goose migrations and sqlc queries. |
| `web` | The web UI: React, TanStack Router and Query, shadcn components on Base UI, Vite. |
| `website` | This site, built with [Blume](https://github.com/haydenbleasel/blume) from `docs/` and `api/openapi.json`. |
| `test/e2e`, `test/load` | End-to-end tests against real apps, and the load test. |

## Build

Tool versions are pinned in `mise.toml`:

```sh
mise install        # Go, Node, pnpm, golangci-lint, govulncheck, sqlc
mise run build      # the web UI, then bin/piecemeal with it embedded
mise run generate   # store queries, the OpenAPI document and the web API client
mise run check      # everything CI checks
pnpm -C web dev     # the web UI's dev server, proxying /api to a piecemeal on :8080
```

Go-only builds need the embed directory to exist: `mkdir -p internal/webui/dist && touch internal/webui/dist/.keep`.

piecemeal builds with `CGO_ENABLED=0`: SQLite is the pure-Go modernc driver, so the binary is static.

## Tests

- **Unit and integration tests**: `go test -race ./...` and `pnpm -C web test`. Engine tests run real torrent transfers between in-process clients.
- **Fuzzing**: the parsers of untrusted input have fuzz targets, run weekly in CI and locally with `go test -fuzz=FuzzParse ./internal/rss/`.
- **End-to-end**: `test/e2e/run.sh` starts piecemeal, a seeder, qBittorrent, Radarr, Sonarr and Prowlarr in Docker. The \*arrs test their download client against piecemeal, Radarr grabs and imports a release, and the same qBittorrent API calls go to piecemeal and a real qBittorrent 5.3 to compare status codes and response shapes.
- **Load**: `test/load/run.sh` measures piecemeal with 1,000 torrents; see [Performance](../reference/performance.md).
- **A real SOCKS5 proxy**: the proxy tests use piecemeal's own test proxy. `TestRealSOCKS5` in `internal/proxy` checks CONNECT and the UDP relay against one you run, such as Dante, when `PIECEMEAL_TEST_SOCKS5` names it and `PIECEMEAL_TEST_REACH` is an address it reaches this machine at; `PIECEMEAL_TEST_SOCKS5_USER` and `PIECEMEAL_TEST_SOCKS5_PASSWORD` add credentials.

## Conventions

- Commits and pull request titles follow [Conventional Commits](https://www.conventionalcommits.org); pull requests are squash-merged, and release-please writes the changelog and cuts releases from them.
- Go is formatted with gofumpt and goimports (`golangci-lint fmt`) and must pass `golangci-lint run`. The web UI must pass oxfmt and oxlint.
- Keep anacrolix behind `internal/engine`.
- Never copy qBittorrent's source code, which is GPL. The compatibility layer is written from the API documentation and observed behaviour.
- Generated files, `api/openapi.json` and the web API client, are committed, and CI checks they're current: run `mise run generate` after changing the API.

## This site

```sh
pnpm -C website install
pnpm -C website dev
```

Pages are the Markdown files in `docs/`, so they read the same on GitHub. The API reference is generated from `api/openapi.json` at build time.
