# piecemeal

A self-hosted BitTorrent client: one Go binary built on [anacrolix/torrent](https://github.com/anacrolix/torrent), with its own REST API, a qBittorrent-compatible WebAPI for Radarr, Sonarr, Prowlarr and other qBittorrent tools, and an embedded web UI.

**Status:** planning. Nothing works yet beyond a skeleton binary; see [docs/plan.md](docs/plan.md) for the design and roadmap.

## Development

Tool versions are pinned in [`mise.toml`](mise.toml).

```sh
mise install        # Go, Node, pnpm, golangci-lint, govulncheck
mise run build      # web UI, then bin/piecemeal with it embedded
mise run check      # everything CI checks
pnpm -C web dev     # UI dev server, proxying /api to a piecemeal on :8080
```

Go-only builds need the embed directory to exist: `mkdir -p internal/webui/dist && touch internal/webui/dist/.keep`.

## Releases

Releases are cut by [release-please](https://github.com/googleapis/release-please) from conventional commits. Each release publishes a signed multi-arch image to `ghcr.io/sebdanielsson/piecemeal`, with SBOM and provenance attestations, plus binaries for linux/amd64, linux/arm64 and darwin/arm64.

## License

[MIT](LICENSE)
