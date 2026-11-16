# AGENTS.md

piecemeal is a BitTorrent client: a Go daemon on anacrolix/torrent with a native REST API, a qBittorrent WebAPI v2 compatibility layer, and an embedded React SPA. The design and roadmap are in [docs/plan.md](docs/plan.md); read it before starting work.

## Commands

- `mise install`: install the pinned toolchain.
- `mise run build`: build the UI into `internal/webui/dist`, then `bin/piecemeal`.
- `mise run check`: run every CI check locally.
- `pnpm -C web <fmt|fmt:check|lint|typecheck|test|build|dev>`: UI tasks.
- `golangci-lint run`, `golangci-lint fmt`, `go test -race ./...`, `govulncheck ./...`: Go tasks.

## Conventions

- Commits and PR titles follow Conventional Commits. PRs are squash-merged and release-please reads the result.
- Go: keep anacrolix behind `internal/engine`. No CGO. Format with gofumpt and goimports (`golangci-lint fmt`).
- UI: oxfmt and oxlint must be clean. shadcn components use the `base-sera` style on Base UI.
- Never copy qBittorrent source code (GPL). The compatibility layer is written from the API documentation and observed behaviour.
- Generated files (`api/openapi.json`, the web API client) are committed, and CI checks they are current.
