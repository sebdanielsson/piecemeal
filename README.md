# piecemeal

A self-hosted BitTorrent client: one Go binary built on [anacrolix/torrent](https://github.com/anacrolix/torrent), with its own REST API, a qBittorrent-compatible WebAPI for Radarr, Sonarr, Prowlarr and other qBittorrent tools, and an embedded web UI.

**Status:** early. The engine, the native API and its auth work; the qBittorrent API and the web UI are next. See [docs/plan.md](docs/plan.md) for the design and roadmap.

## Running

```sh
docker run -d --name piecemeal \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v piecemeal-data:/data -v /path/to/downloads:/downloads \
  -e PIECEMEAL_ADMIN_PASSWORD_HASH='$argon2id$...' \
  ghcr.io/sebdanielsson/piecemeal:latest
```

Make the password hash with `docker run --rm -i ghcr.io/sebdanielsson/piecemeal hash-password` and type the password. Without a hash, piecemeal logs a temporary admin password at every start.

The native API is at `/api/piecemeal/v1`, with interactive docs at `/api/piecemeal/v1/docs` and the OpenAPI document in [api/openapi.json](api/openapi.json). Sign in with `POST /api/piecemeal/v1/auth/login`, or create an API key (`POST /api/piecemeal/v1/api-keys`) and send it as `Authorization: Bearer pm_...`.

## Configuration

Startup configuration comes from environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `PIECEMEAL_ADDR` | `:8080` | HTTP address for the APIs and web UI. |
| `PIECEMEAL_DATA_DIR` | `/data` in the image | Database and piece completion state. Keep it on fast storage. |
| `PIECEMEAL_DOWNLOAD_DIR` | `/downloads` in the image | Default save path. |
| `PIECEMEAL_ALLOWED_ROOTS` | the download dir | Comma-separated directories every save path must be inside. |
| `PIECEMEAL_WATCH_DIR` | off | Directory polled for `.torrent` and `.magnet` files. |
| `PIECEMEAL_NATPMP_GATEWAY` | off | NAT-PMP gateway (such as a VPN's) to forward the peer port through. piecemeal listens on whatever port it gets and follows changes. |
| `PIECEMEAL_LISTEN_HOST` | all | Address to bind peer listeners to. |
| `PIECEMEAL_DISABLE_IPV6` | `false` | No IPv6 peers, for IPv4-only tunnels. |
| `PIECEMEAL_MMAP` | `false` | Memory-mapped file I/O. Off by default: on ZFS it amplifies writes. |
| `PIECEMEAL_ADMIN_USERNAME` | `admin` | The local admin. |
| `PIECEMEAL_ADMIN_PASSWORD_HASH` | none | argon2id hash from `piecemeal hash-password`. |
| `PIECEMEAL_LOCAL_LOGIN` | `true` | Allow password sign-in. |
| `PIECEMEAL_SECURE_COOKIES` | `true` | Mark cookies Secure. Turn off only for plain HTTP on a host other than localhost. |
| `PIECEMEAL_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-For` is trusted for client addresses. |
| `PIECEMEAL_FETCH_ALLOWED_NETWORKS` | none | Private CIDRs that adding by URL may fetch from. Private, loopback and link-local addresses are refused otherwise. |
| `PIECEMEAL_METRICS` | `auth` | `/metrics` access: `public`, `auth` (session or API key) or `off`. |
| `PIECEMEAL_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `PIECEMEAL_LOG_FORMAT` | `json` | `json` or `text`. |

Runtime settings (speed limits, connections, DHT, encryption, seeding goals and more) are changed through `PATCH /api/piecemeal/v1/settings`. Any of them can be pinned with an environment variable named after it, such as `PIECEMEAL_DOWNLOAD_LIMIT` for `downloadLimit`; pinned settings are read-only in the API.

## Development

Tool versions are pinned in [`mise.toml`](mise.toml).

```sh
mise install        # Go, Node, pnpm, golangci-lint, govulncheck, sqlc
mise run build      # web UI, then bin/piecemeal with it embedded
mise run generate   # store queries, OpenAPI document, web API client
mise run check      # everything CI checks
pnpm -C web dev     # UI dev server, proxying /api to a piecemeal on :8080
```

Go-only builds need the embed directory to exist: `mkdir -p internal/webui/dist && touch internal/webui/dist/.keep`.

## Releases

Releases are cut by [release-please](https://github.com/googleapis/release-please) from conventional commits. Each release publishes a signed multi-arch image to `ghcr.io/sebdanielsson/piecemeal`, with SBOM and provenance attestations, plus binaries for linux/amd64, linux/arm64 and darwin/arm64.

## License

[MIT](LICENSE)
