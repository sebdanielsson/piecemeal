---
description: Every PIECEMEAL_* environment variable, every runtime setting with its default, and how to pin settings from the environment.
---

# Configuration

piecemeal has two kinds of configuration:

- **Startup configuration**: environment variables read once at start, for paths, ports, authentication and anything else that needs a restart to change.
- **Runtime settings**: speed limits, connections, seeding goals, queueing, RSS and more, changed live in **Settings** in the web UI or through the API, and stored in the database. Any of them can also be pinned by an environment variable.

## Environment variables

### Paths and network

| Variable | Default | Meaning |
| --- | --- | --- |
| `PIECEMEAL_ADDR` | `:8080` | HTTP address for the web UI and both APIs. |
| `PIECEMEAL_DATA_DIR` | `/data` in the image, `data` otherwise | Database and piece completion state. Keep it on fast storage. Only one piecemeal can use it at a time. |
| `PIECEMEAL_DOWNLOAD_DIR` | `/downloads` in the image, `downloads` otherwise | The default save path. |
| `PIECEMEAL_ALLOWED_ROOTS` | the download directory | Comma-separated directories every save path must be inside. They must not overlap the data directory or the qBittorrent web UI directory; piecemeal refuses to start if they do. |
| `PIECEMEAL_WATCH_DIR` | off | A directory polled every 5 seconds for `.torrent` and `.magnet` files (a magnet link per file). Added files are deleted; files that fail get a `.failed` suffix. |
| `PIECEMEAL_QBITTORRENT_WEBUI_DIR` | off | A qBittorrent web UI such as VueTorrent, served at `/qbittorrent/`. See [Web UIs](features/web-ui.md). |
| `PIECEMEAL_NATPMP_GATEWAY` | off | A NAT-PMP gateway, such as a VPN's, to forward the peer port through. piecemeal listens on whatever port it gets and follows changes. See [VPN and port forwarding](guides/vpn.md). |
| `PIECEMEAL_LISTEN_HOST` | all addresses | The address peer listeners bind to. |
| `PIECEMEAL_DISABLE_IPV6` | `false` | No IPv6 peers, for IPv4-only tunnels. |
| `PIECEMEAL_MMAP` | `false` | Memory-mapped file I/O. Off by default: on ZFS it amplifies writes many times over. |

### Sign-in and access

| Variable | Default | Meaning |
| --- | --- | --- |
| `PIECEMEAL_ADMIN_USERNAME` | `admin` | The local admin's username. |
| `PIECEMEAL_ADMIN_PASSWORD_HASH` | none | The local admin's argon2id hash, from `piecemeal hash-password`. Without one, piecemeal prints a temporary password at every start. Changing it ends the admin's sessions at the next start; API keys stay, so review them. |
| `PIECEMEAL_LOCAL_LOGIN` | `true` | Allow password sign-in, in the web UI and for qBittorrent clients. With it off, use SSO and API keys. |
| `PIECEMEAL_SECURE_COOKIES` | `true` | Mark cookies `Secure`. Turn it off only for plain HTTP on a host other than `localhost`. |
| `PIECEMEAL_PUBLIC_URL` | none | Where people reach piecemeal, such as `https://piecemeal.example.com`. Needed for SSO. |
| `PIECEMEAL_ALLOWED_HOSTS` | any | Comma-separated host names piecemeal answers to, besides `localhost`, loopback addresses and the public URL's host. Other names get 421, which stops DNS rebinding on a LAN. Health checks answer any name. |
| `PIECEMEAL_TRUSTED_PROXIES` | none | Comma-separated CIDRs of reverse proxies whose `X-Forwarded-For` is trusted for the client's address. |
| `PIECEMEAL_FETCH_ALLOWED_NETWORKS` | none | Comma-separated private CIDRs that adding a torrent by URL, and RSS, may fetch from. Private, loopback and link-local addresses are refused otherwise. |
| `PIECEMEAL_METRICS` | `auth` | `/metrics` access: `public`, `auth` (a session or API key) or `off`. |

### Single sign-on

See [Single sign-on](guides/sso.md) for a walkthrough.

| Variable | Default | Meaning |
| --- | --- | --- |
| `PIECEMEAL_OIDC_ISSUER` | off | The OpenID Connect issuer URL. Turns on SSO. |
| `PIECEMEAL_OIDC_CLIENT_ID`, `PIECEMEAL_OIDC_CLIENT_SECRET` | none | The client registered at the provider. |
| `PIECEMEAL_OIDC_REDIRECT_URL` | `<public URL>/api/piecemeal/v1/auth/oidc/callback` | The callback, if it must differ from the default. |
| `PIECEMEAL_OIDC_NAME` | `SSO` | The name on the sign-in button. |
| `PIECEMEAL_OIDC_ALLOWED_GROUPS` | none | Comma-separated groups allowed to sign in. Required with OIDC, unless `PIECEMEAL_OIDC_ALLOW_ANY` is set. |
| `PIECEMEAL_OIDC_ALLOW_ANY` | `false` | Let every account at the provider sign in. |
| `PIECEMEAL_OIDC_ADMIN_GROUPS` | none | Comma-separated groups whose members are admins. |
| `PIECEMEAL_OIDC_GROUPS_CLAIM` | `groups` | The ID token claim that holds the groups. |

### Logging

| Variable | Default | Meaning |
| --- | --- | --- |
| `PIECEMEAL_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `PIECEMEAL_LOG_FORMAT` | `json` | `json` or `text`, to stderr. Admins also see recent entries under **Log** in the web UI. |

piecemeal checks the whole configuration at start and reports every problem at once, then exits.

## Runtime settings

Change these in **Settings**, with `PATCH /api/piecemeal/v1/settings`, or through the qBittorrent API's `app/setPreferences`. Changing them needs an admin. Any user may change the speed limits, under **Transfer** or with `PATCH /api/piecemeal/v1/transfer`. Rates are in bytes per second, where `0` means unlimited.

| Setting | Default | Meaning |
| --- | --- | --- |
| `defaultSavePath` | the download directory | Where torrents go when neither the request nor its category names a path. |
| `incompletePathEnabled` | `false` | Keep incomplete torrents in `incompletePath`; each moves to its save path once complete. A category or a torrent can choose otherwise. |
| `incompletePath` | `temp` in the download directory | The incomplete folder. |
| `autoManagement` | `false` | New torrents use automatic management unless the request says otherwise: their save path and incomplete folder follow their category. |
| `relocateOnCategoryChange` | `true` | When an automatically managed torrent's category changes, move its data to the new category's paths. Off switches it to manual management instead. |
| `relocateOnCategoryPathChange` | `false` | When a category's paths change, move its automatically managed torrents. Off switches them to manual management instead. |
| `relocateOnDefaultPathChange` | `false` | When `defaultSavePath` or the incomplete folder changes, move the automatically managed torrents that use them. Off switches them to manual management instead. |
| `addStopped` | `false` | Add new torrents stopped. |
| `downloadLimit`, `uploadLimit` | `0` | Global limits. |
| `altDownloadLimit`, `altUploadLimit` | `0` | Alternative limits, used while `altSpeedEnabled` is on. |
| `altSpeedEnabled` | `false` | Use the alternative limits. |
| `altSpeedSchedule` | `false` | Turn the alternative limits on and off on a schedule. |
| `altSpeedScheduleFrom`, `altSpeedScheduleTo` | `08:00`, `20:00` | When they apply, `HH:MM` local time. An end before the start runs into the next day. |
| `altSpeedScheduleDays` | `everyDay` | `everyDay`, `weekdays`, `weekends`, or a day such as `monday`. |
| `peerPort` | `6881` | The port for incoming peers. NAT-PMP overrides it; `0` picks a random port. |
| `maxConnectionsPerTorrent` | `50` | Established peer connections per torrent. |
| `maxHalfOpenConnections` | `100` | Outgoing connection attempts in progress, across all torrents. |
| `utp` | `true` | Use uTP as well as TCP. |
| `dht`, `pex` | `true` | Find peers through the DHT and peer exchange. Private torrents use neither. |
| `encryption` | `prefer` | Peer connection encryption: `prefer`, `require` or `disable`. |
| `ratioLimit` | `-1` | Seeding goal: this upload ratio; `-1` is unlimited. |
| `seedingTimeLimit` | `-1` | Seeding goal: this many minutes seeding. |
| `inactiveSeedingTimeLimit` | `-1` | Seeding goal: this many minutes without uploading. |
| `seedGoalMode` | `any` | `any`: a torrent reaches its goals when one of them is met; `all`: only when every goal that is set is met. |
| `seedGoalAction` | `stop` | What happens when a torrent reaches its goals: `stop`, `remove` or `removeWithData`. |
| `queueingEnabled` | `false` | Run only a limited number of torrents at once; the rest wait in the queue. |
| `maxActiveDownloads`, `maxActiveUploads`, `maxActiveTorrents` | `3`, `3`, `5` | The queue's limits; `-1` is unlimited. |
| `dontCountSlowTorrents` | `false` | Slow torrents don't count toward the limits. |
| `slowDownloadRate`, `slowUploadRate` | `2048` | Below these rates a torrent is slow. |
| `slowTorrentTime` | `60` | Seconds a torrent must stay below them to count as slow. |
| `maxActiveCheckingTorrents` | `1` | Torrents whose data is checked at once, at least 1. The others wait, queued for checking, and neither download nor seed until their data is checked. On hard drives, checking one at a time keeps the disks from seeking between torrents. |
| `ipFilterEnabled` | `false` | Refuse peers in the IP filter's ranges. |
| `ipFilterPath` | none | An eMule `.dat`, PeerGuardian `.p2p`, or a file of IP addresses and CIDR ranges, one per line. At most 512 MiB. |
| `bannedIPs` | none | Peers to refuse, one IP address or CIDR range per line. |
| `rssEnabled` | `false` | Fetch RSS feeds on their schedule. |
| `rssRefreshInterval` | `30` | Minutes between fetches of a feed without its own interval. |
| `rssFetchDelay` | `2` | Seconds between requests to the same host. |
| `rssMaxArticles` | `50` | Articles kept per feed. |
| `rssAutoDownload` | `false` | Add torrents from new articles that rules match. |
| `rssDownloadRepacks` | `true` | With the smart episode filter, also take a REPACK or PROPER of an episode already downloaded. |
| `rssSmartEpisodeFilters` | `SxxEyy`, `NxNN` and dates | Regular expressions, one per line, that find the episode in a title. |

The seeding goals above are the defaults. A category can set its own goals, mode and action, and so can a torrent; each one follows the next level only where it's left at the default (`-2` for a goal, `default` for the mode and action): first the torrent's own, then its category's, then the global setting.

### Save paths and the incomplete folder

Every torrent has a save path, where its data ends up, and may have an incomplete folder, where its data stays until it completes. Once complete, the data moves to the save path in the background, and the torrent shows as moving until it's there; the \*arrs import it only then. A torrent whose data is already in its save path when it's added, such as one added again after it was removed, is checked and completes there.

A torrent is managed manually or automatically:

- **Manually**, its paths are the ones it was added with. A torrent added without them gets its category's save path, else `defaultSavePath`, and its category's own incomplete folder (or none, if the category has it off), else `incompletePath` while `incompletePathEnabled` is on. Moving a torrent changes its save path; data still in the incomplete folder stays there and moves to the new save path once complete. Changing the incomplete folder moves data that's still in it.
- **Automatically** (qBittorrent's Automatic Torrent Management), its paths follow its category. The save path is the category's, or a folder named after the category in `defaultSavePath` when the category has none; the incomplete folder is the category's own, or one named after the category in `incompletePath` while that is on. When the torrent's category, the category's paths or the defaults change, its data moves to the new paths, or the torrent switches to manual management and stays where it is, as `relocateOnCategoryChange`, `relocateOnCategoryPathChange` and `relocateOnDefaultPathChange` say. Turning `incompletePathEnabled` on or off always applies. Moving the torrent or changing its incomplete folder by hand switches it to manual management.

New torrents are managed automatically when `autoManagement` is on, unless they're added with a save path of their own. A category's save path and incomplete folder may be relative: the save path to `defaultSavePath`, the incomplete folder to `incompletePath`. Every path must end up inside `PIECEMEAL_ALLOWED_ROOTS`.

### Pinning settings

Any runtime setting can be pinned with an environment variable named after it in upper snake case with the `PIECEMEAL_` prefix: `downloadLimit` is `PIECEMEAL_DOWNLOAD_LIMIT`, `queueingEnabled` is `PIECEMEAL_QUEUEING_ENABLED`. A pinned setting takes the variable's value at every start and is read-only in the web UI and both APIs, which is useful when configuration lives in git:

```yaml
    environment:
      PIECEMEAL_DHT: "false"
      PIECEMEAL_ENCRYPTION: require
      PIECEMEAL_RATIO_LIMIT: "2"
```

`GET /api/piecemeal/v1/settings` lists which settings are pinned.
