# piecemeal plan

piecemeal is a self-hosted BitTorrent client: a single Go binary built on [anacrolix/torrent](https://github.com/anacrolix/torrent) that serves its own REST API, a qBittorrent-compatible WebAPI, and an embedded web UI. The qBittorrent API is what lets Radarr, Sonarr, Prowlarr and other qBittorrent tools use it as a drop-in download client.

It grows out of the test daemon in [`sebdanielsson/infra` `docker/transmission-wireguard/anacrolix`](https://github.com/sebdanielsson/infra/tree/main/docker/transmission-wireguard/anacrolix), which proved the library on the target host: NAT-PMP port forwarding through a WireGuard VPN, a responsive API under heavy disk load, and 9.7 MiB/s onto a ZFS RAID-Z1 pool once mmap storage was turned off.

Status: all six phases are done, and 1.0 runs on hogsmeade in place of Transmission. Two items from Phase 5 stay out of 1.0: per-torrent speed limits are stored and reported but not enforced, because anacrolix/torrent only limits rates client-wide, and there is no proxy support. Release history is in the [changelog](../CHANGELOG.md).

## Goals

- A torrent client whose API and UI stay responsive whatever the disks are doing. Disk I/O never runs on a thread that serves requests.
- Full qBittorrent WebAPI v2 compatibility, except search, so existing integrations work unchanged.
- One binary, one container, one port, one auth system, no external database.
- A first-class native API, described by OpenAPI, that the web UI uses itself.
- Release engineering from day one: signed multi-arch images, SBOMs, provenance, automated releases and dependency updates.

### Non-goals

- qBittorrent's search plugin API (`/api/v2/search/*`). Search plugins are Python scripts run by qBittorrent; Prowlarr already covers search.
- Multi-tenant use. Everyone who can sign in can see and manage every torrent.
- Features anacrolix/torrent does not have: super seeding, I2P and SSL torrents. Their qBittorrent endpoints exist but answer with a documented "not supported".

## Decisions

| Topic | Decision | Why |
| --- | --- | --- |
| Engine | anacrolix/torrent, CGO off | The most complete BitTorrent implementation in Go or Rust: encryption, v2/hybrid, web seeds, holepunching, uTP, WebTorrent. Pure Go builds for every target. |
| Web UI | React SPA embedded in the Go binary: Vite, TanStack Router, Query and Table, shadcn (`base-sera`, Base UI), Tailwind | One process and one auth system. Server rendering adds nothing to a signed-in live dashboard, and a Node BFF would need its own auth on top of the daemon's, which Radarr and Sonarr call directly. TanStack Router keeps a later move to TanStack Start open. |
| Auth | OIDC (Pocket ID) for people, a local admin as fallback, named API keys for tools, plus qBittorrent's cookie login | Passkeys and 2FA come from Pocket ID. Radarr, Sonarr and Prowlarr send `Authorization: Bearer <key>` to qBittorrent when a key is set, so one key system serves both APIs. |
| qBittorrent API | WebAPI v2 as in qBittorrent 5.3.0 (WebAPI 2.16.2), 121 endpoints, everything except search | Radarr, Sonarr and Prowlarr use 15 of them; third-party UIs such as VueTorrent and tools such as qbit_manage use most of the rest. |
| Native API | Huma v2, code-first, OpenAPI 3.1, errors as RFC 9457 problem details | The spec is generated from the Go handlers, committed, and turned into the UI's TypeScript client, with a CI check that both are in sync. |
| App data | SQLite through `modernc.org/sqlite`, goose migrations, sqlc queries | Embedded, CGO-free and type-safe. Piece completion stays in anacrolix's bolt store. |
| Storage | anacrolix file storage with mmap off by default, and without part files | On the target RAID-Z1, mmap page writes caused about 13x write amplification (about 530 KiB/s). With pwrite, ZFS merges chunks per transaction group. Incomplete files keep their final names, as in qBittorrent by default; anacrolix v1.63.0 also has a data race between part-file promotion and the piece hasher in its non-mmap I/O. |
| Tooling | Go from `go.mod`; Node 26 and pnpm 12 pinned in `mise.toml`; oxfmt and oxlint; vitest; golangci-lint | Same conventions as the author's other repositories. |
| Releases | release-please on conventional commits; images on GHCR | Squash-merged PR titles drive versions and the changelog. |
| License | MIT | anacrolix/torrent is MPL-2.0, which is file-level copyleft and fine to depend on unmodified. No qBittorrent code (GPL) is copied: the compatibility layer is written from the API documentation and observed behaviour only. |

## Architecture

```mermaid
flowchart LR
  subgraph clients[Clients]
    ui[Web UI SPA]
    arr[Radarr / Sonarr / Prowlarr]
    qbui[qBittorrent tools<br/>VueTorrent, qbit_manage]
    prom[Prometheus]
  end
  subgraph pm[piecemeal - one process, one port]
    http[HTTP router]
    auth[auth: sessions, API keys, OIDC]
    native[native API<br/>/api/piecemeal/v1]
    qbit[qBittorrent API<br/>/api/v2]
    sse[events, SSE]
    webui[embedded SPA]
    metrics[/metrics]
    engine[engine: anacrolix client,<br/>queueing, limits, seed goals]
    natpmp[NAT-PMP]
    store[(SQLite)]
    bolt[(bolt piece completion)]
  end
  disk[(downloads)]
  ui --> http
  arr --> http
  qbui --> http
  prom --> metrics
  http --> auth --> native & qbit
  http --> webui
  native & qbit & sse --> engine
  native & qbit --> store
  engine --> bolt
  engine --> disk
  engine --- natpmp
```

- **engine** owns the anacrolix client and everything torrent-shaped: adding and removing, start and stop, queueing (max active downloads and uploads), global, alternative and per-torrent speed limits, seed goals (ratio, seeding time, inactive time), categories to save paths, file priorities, sequential download, first-and-last piece priority, recheck, and moving storage. It publishes state changes on an internal event bus.
- **natpmp** keeps TCP and UDP mappings on a configured gateway and hands the forwarded port to the engine. A changed port rebinds the listener without restarting the process, which the test daemon could not do.
- **store** keeps users, sessions, API keys, categories, tags, per-torrent settings and statistics that anacrolix does not persist, such as upload totals, added time, completion time and the stopped state.
- **auth** is shared middleware: a session cookie for the UI, `SID` for qBittorrent clients, Bearer API keys for both APIs, and OIDC sign-in.
- **native** is the REST API the web UI uses, with Server-Sent Events at `/api/piecemeal/v1/events` for live updates.
- **qbit** translates qBittorrent WebAPI calls (form-encoded bodies, `|`-separated hashes, qBittorrent's state names and field names) onto the engine and store.
- **webui** serves the embedded SPA with an `index.html` fallback and immutable caching for hashed assets.

Everything is served on one port (default 8080), so the client can live in a VPN container's network namespace and still be reachable through a single proxy.

## Repository layout

```text
cmd/piecemeal/          main: flags, config, wiring
internal/config/        startup configuration from env vars
internal/settings/      runtime settings in SQLite; an env var pins a setting and makes it read-only
internal/engine/        anacrolix lifecycle, storage, queueing, limits, seed goals, events
internal/natpmp/        port mapping
internal/store/         SQLite, migrations (goose), queries (sqlc)
internal/auth/          users, sessions, API keys, OIDC, CSRF, rate limiting
internal/api/           native API (Huma), SSE
internal/qbit/          qBittorrent WebAPI v2
internal/webui/         embedded SPA handler; `pnpm -C web build` writes its dist/
internal/metrics/       Prometheus collectors
internal/watch/         watch folder
internal/fetch/         SSRF-safe download of .torrent URLs
internal/logbuf/        recent log entries for the log endpoints
web/                    the SPA (Vite + React)
api/openapi.json        generated native API spec, committed
docs/                   this plan, architecture notes, the compatibility matrix
```

## Native API

Resources under `/api/piecemeal/v1`: `torrents` (with `files`, `peers`, `trackers`, `webseeds` and `pieces` subresources and actions such as start, stop, recheck, reannounce and move), `categories`, `tags`, `transfer` (rates and limits), `settings`, `users`, `api-keys`, `sessions`, `log`, and `events` (SSE).

- JSON in and out; errors as `application/problem+json`.
- Cursor pagination and server-side filtering and sorting on `torrents`, so the UI stays fast with thousands of torrents.
- Every mutation emits an event, and the UI applies events to its TanStack Query cache instead of polling.
- `api/openapi.json` is generated from the handlers and committed. The web client is generated from it with `openapi-typescript` and `openapi-fetch`. CI fails if either is stale.

## qBittorrent compatibility

Target: qBittorrent 5.3.0's WebAPI 2.16.2. piecemeal reports exactly that from Phase 2 and follows 5.3.0's behaviour on every endpoint it implements. Clients choose code paths by version, and the *arrs behave the same for any WebAPI from 2.11.2 up: they gate categories, paths, share limits and `stopped` on the version, but not API keys or tags. Reporting 5.3.0 early means tools such as VueTorrent may call Phase 4 endpoints before they exist; building 5.0 behaviour first would mean changing it again later. The pre-2.11 names (`pause`, `resume`, `paused`) are accepted as aliases. [docs/reference/qbittorrent-compat.md](reference/qbittorrent-compat.md) tracks every endpoint.

| Group | Endpoints | Phase | Notes |
| --- | --- | --- | --- |
| auth | 2 | 2 | `login` (204 with `QBT_SID_<port>` and `SID` cookies) and `logout`. Bearer API keys and Basic credentials accepted on every endpoint; unauthenticated requests get 403, which the *arrs rely on. |
| app | 17 | 2 (core), 4 (rest) | `version`, `webapiVersion`, `buildInfo`, `preferences` and `setPreferences` mapped onto piecemeal settings. `rotateAPIKey` and `deleteAPIKey` manage the caller's key. `shutdown` and `sendTestEmail` answer "not supported". |
| torrents | 62 | 2 (Radarr/Sonarr/Prowlarr subset), 4 (rest) | The *arrs call `add`, `info`, `properties`, `files`, `delete`, `categories`, `createCategory`, `setCategory`, `setForceStart`, `setShareLimits`, `topPrio` and `addTags`, plus `auth/login`, `app/webapiVersion` and `app/preferences`. Phase 2 also covers the cheap neighbours: start, stop, tags, category editing, toggles, limits, rename. `setSuperSeeding` and the SSL parameter endpoints answer "not supported". |
| transfer | 13 | 4 | Global and alternative limits, speed-limit mode, `banPeers`, session start and stop. |
| sync | 2 | 4 | `maindata` and `torrentPeers` with `rid` deltas, which VueTorrent and qBittorrent's own WebUI depend on. |
| log | 2 | 4 | From piecemeal's structured log. |
| clientdata | 2 | 4 | Key-value storage for WebUI clients. |
| torrentcreator | 4 | 5 | Built on anacrolix's metainfo builder. |
| rss | 17 | 5 | Feeds, folders, articles and auto-download rules. |
| search | 11 | never | Out of scope. |

Torrent state is mapped onto qBittorrent's names (`downloading`, `stalledDL`, `metaDL`, `queuedDL`, `forcedDL`, `checkingDL`, `uploading`, `stalledUP`, `queuedUP`, `forcedUP`, `stoppedDL`, `stoppedUP`, `error`, `missingFiles`, `moving`) from engine state, peer activity and queue position.

`docs/qbittorrent-compat.md` will track every endpoint and field as implemented, partial, or not supported, generated from the test suite.

## Auth and security

- **People:** OIDC authorization code flow with PKCE, state and nonce (`coreos/go-oidc`, `golang.org/x/oauth2`), against Pocket ID by default. An optional allowed-groups claim limits who can sign in. Users are created on first sign-in.
- **Fallback:** one local admin whose password is an argon2id hash from an environment variable, so it can live in sops. Local sign-in can be turned off once OIDC works.
- **Sessions:** random 256-bit tokens, stored hashed in SQLite with absolute and idle timeouts, in an `HttpOnly`, `Secure`, `SameSite=Lax` cookie. Unsafe UI requests also need a custom header and a matching `Origin`.
- **qBittorrent clients:** `auth/login` issues a `SID` cookie backed by the same session table. Like qBittorrent, piecemeal checks `Origin` and `Referer` when they are present.
- **API keys:** named, shown once, stored as SHA-256 hashes (they are high-entropy), scoped as `full`, `qbittorrent` or `read-only`, with last-used tracking.
- **Abuse limits:** login rate limiting and temporary lockout, and an audit log of sign-ins, key changes and setting changes.
- **Adding by URL** (both APIs) is the one place piecemeal fetches arbitrary URLs. It gets SSRF protection: private and link-local ranges are blocked unless allowed, plus size and redirect limits.
- **Filesystem:** every path from a client (save path, rename, move) is resolved and confined to configured roots.

## Web UI

Phase 3 builds the MVP; later phases fill in the rest.

- **Sign-in:** OIDC button and local form.
- **Torrent list:** virtualized table with sorting, column choice, filters by state, category, tag and tracker, multi-select actions, and live rates.
- **Torrent detail:** general, files (a tree with priorities and rename), peers, trackers, web seeds and pieces.
- **Add torrent:** magnet, file or URL; category, tags, save path, start state, sequential and first-and-last options.
- **Management:** categories and tags.
- **Transfer:** speed graph, global and alternative limits, alternative-speed schedule.
- **Settings:** connection, BitTorrent, queueing, seeding goals, downloads, port forwarding status, appearance. Env-set values show as read-only.
- **Admin:** users, API keys, sessions, audit log, log viewer.
- **General:** dark mode, keyboard shortcuts, works at phone width.

## Observability

- Prometheus metrics at `/metrics`: engine totals, per-torrent rates and progress, peers, disk queue, plus the Go runtime and process. They can be turned off or put behind auth.
- Structured JSON logs (`log/slog`).
- `/healthz` (process) and `/readyz` (engine started, port mapped).
- `pprof` behind admin auth.

## Testing

- **Unit:** table-driven tests for qBittorrent field and state mapping, form parsing, auth and config.
- **Swarm integration:** an in-process anacrolix seeder and tracker create a torrent and download it through piecemeal, so tests are deterministic and need no internet.
- **qBittorrent contract tests:** CI runs a real qBittorrent container and piecemeal through the same scripted calls and compares response shapes (fields, types, status codes, headers). That catches drift without copying any qBittorrent code.
- **Radarr, Sonarr, Prowlarr end-to-end:** containers in CI are configured with piecemeal as a qBittorrent download client. CI runs their "test" action, pushes a release through Radarr's and Sonarr's `release/push` API against the in-CI swarm, and checks grab, completion, import, and removal after the seed goal.
- **Web:** vitest with Testing Library for components; Playwright against a real binary for sign-in, add, progress and delete.
- **Security and robustness:** CodeQL, govulncheck, dependency review, and nightly Go fuzzing of qBittorrent form parsing and metainfo input.

## CI/CD

| Workflow | Trigger | Does |
| --- | --- | --- |
| `ci.yaml` | push to `main`, PRs | Web: format, lint, typecheck, tests, build. Go: golangci-lint, `go vet`, `go test -race`, govulncheck, build. Docker: multi-arch build without pushing. Later: OpenAPI drift check, swarm and *arr end-to-end jobs. An aggregate `ci` job is the single required check. |
| `release-please.yaml` | push to `main` | Maintains the release PR. When a release is cut, in the same run (tags pushed with `GITHUB_TOKEN` don't trigger workflows): multi-arch image to `ghcr.io/sebdanielsson/piecemeal`, cosign keyless signature, SBOM and SLSA provenance, plus attested `linux/amd64`, `linux/arm64` and `darwin/arm64` binaries and checksums on the GitHub release. |
| `codeql.yaml` | push, PRs, weekly | Go, JavaScript/TypeScript and Actions. |
| `dependency-review.yaml` | PRs | Blocks newly added vulnerable or disallowed-license dependencies. |
| `lint-pr.yaml` | PRs | Conventional PR titles, because the squash title is what release-please reads. |

Dependencies are updated by Renovate through the shared `github>sebdanielsson/renovate-config` preset: Go modules (with `go mod tidy`), npm, GitHub Actions, Dockerfile base images and `mise.toml` tools.

The `main` ruleset requires a PR, the `ci` check and CodeQL results, linear history, signed commits, and blocks force-pushes and deletion. Repository admins can bypass it.

### Deployment

piecemeal's own CD ends at a signed image on GHCR. Deploying is GitOps in `sebdanielsson/infra`:

1. The `transmission-wireguard` stack runs `ghcr.io/sebdanielsson/piecemeal:<version>` in place of today's build-at-start test service, in the WireGuard network namespace, with `/chungus/media/downloads` mounted at `/downloads` so the paths it reports match Radarr's and Sonarr's.
2. Secrets (OIDC client secret, admin password hash, session key) go in the stack's `.env.sops` and are decrypted by its existing pre-deploy hook.
3. Renovate in the infra repo opens a PR for each release; merging it makes Arcane redeploy within five minutes.
4. A Pocket ID OIDC client for `https://piecemeal.risk-bee.ts.net/api/piecemeal/v1/auth/oidc/callback` has to be created by hand.

## Phases

| Phase | Scope | Done when |
| --- | --- | --- |
| 0. Foundation | Repository, this plan, CI/CD, a skeleton binary and image | CI is green, the release PR flow works, and a signed `v0.1.0` image and binaries are published |
| 1. Engine and native API | Port the test daemon (NAT-PMP with live rebinding, pwrite storage, watch folder); config; SQLite store; torrents, categories and tags; native API with OpenAPI; SSE; metrics; local admin, sessions and API keys | piecemeal replaces the test daemon on hogsmeade from a GHCR image, with at least its features |
| 2. qBittorrent API for *arr | auth, the core of app, the 13 torrents endpoints above, state mapping, seed-goal enforcement; contract and *arr end-to-end tests in CI | Radarr and Sonarr on hogsmeade use piecemeal for real grabs, with completed-download handling and removal after seeding |
| 3. Web UI MVP and OIDC | SPA scaffold, OIDC sign-in, torrent list with live updates, add dialog, detail tabs, categories and tags, basic settings, API keys | Day-to-day use needs neither Transmission's nor qBittorrent's UI |
| 4. Full qBittorrent API | The rest of torrents, transfer, sync with `rid` deltas, log, the rest of app, clientdata | VueTorrent works against piecemeal for its core flows; the compatibility matrix shows every non-search endpoint as implemented or deliberately not supported |
| 5. Advanced features | Queueing, alternative-speed schedule, per-torrent limits, move storage, IP filter, proxy, RSS feeds and rules, torrent creator | Feature parity with Transmission as configured today, plus RSS |
| 6. 1.0 hardening | Security review (auth, CSRF, SSRF, path handling), fuzzing, a 1,000-torrent load test, backup and restore, import from Transmission and qBittorrent, user docs | `v1.0.0` |

## Risks and open questions

- **anacrolix API churn.** The test daemon already had to hold `anacrolix/stm` at v0.5.0 because v0.6.0 broke `anacrolix/dht`. The engine package isolates the library, and Renovate PRs for the anacrolix family are reviewed rather than automerged.
- **Which version to report.** Claiming 5.3.0 makes clients call endpoints that may not exist yet. Report the highest version whose endpoints are fully covered, and raise it as phases land.
- **State the library doesn't persist.** Upload totals, seeding time and stopped state have to be checkpointed to SQLite, or a restart resets ratios and seed goals.
- **ZFS and old disks.** The target pool is three ageing 2 TB WD Green drives in RAID-Z1 at 80% full. Performance tests must run there, not only on SSDs in CI.
- **Running in a VPN network namespace.** NAT-PMP, the bound port and the single HTTP port all have to work inside the WireGuard container's namespace, as the test daemon does today.
- **Pocket ID availability.** If the IdP is down, the local admin still gets in.
- **openapi-typescript and TypeScript 7.** openapi-typescript 7.13 needs TypeScript 5's compiler API ([openapi-ts/openapi-typescript#2841](https://github.com/openapi-ts/openapi-typescript/issues/2841)). It runs from `web/tools/openapi-gen`, a workspace package with its own TypeScript 5, until a release supports 7.
