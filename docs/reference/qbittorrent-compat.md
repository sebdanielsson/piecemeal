---
description: Every qBittorrent WebAPI v2 endpoint and how piecemeal implements it, for the *arrs, VueTorrent and other qBittorrent tools.
---

# qBittorrent WebAPI compatibility

piecemeal serves the qBittorrent WebAPI v2 at `/api/v2`. It reports qBittorrent `v5.3.0` and WebAPI `2.16.2`, and follows qBittorrent 5.3.0's behaviour on the endpoints below. It is written from qBittorrent's API documentation and observed behaviour, not from qBittorrent's source.

CI checks it two ways (see [`test/e2e`](https://github.com/sebdanielsson/piecemeal/tree/main/test/e2e)): Radarr, Sonarr and Prowlarr containers run their download client test against piecemeal and Radarr grabs a release through it, and the same calls go to a real qBittorrent 5.3.0 and piecemeal and their status codes and response shapes are compared.

## Authentication

- `auth/login` answers 204 and sets the session cookie twice: as `QBT_SID_<port>` (qBittorrent 5.2 and later) and as `SID` (older clients). Wrong credentials get 401; too many failures get 403 for 15 minutes.
- Every other endpoint accepts the session cookie, piecemeal's own web session, an API key as `Authorization: Bearer pm_...`, or HTTP Basic credentials when there is no session. Basic credentials are the qBittorrent 5.2 behaviour that current Radarr, Sonarr and Prowlarr rely on.
- API keys with the `qbittorrent` or `full` scope work; `read-only` keys can only call endpoints that read. `app/rotateAPIKey` creates a `qbittorrent`-scoped key for the signed-in user (see below); keys can also be created in piecemeal.
- Anything unauthenticated, including an unknown key, gets 403, which the \*arrs read as "log in again".
- When a request has an `Origin` or `Referer` header, it must name the host the request went to (CSRF protection).

## Requests

- Endpoints that read accept GET and POST, since some clients send everything as POST. Endpoints that change something accept only POST and answer 405 to GET.
- Parameters come from the query string and from urlencoded or multipart bodies. Bodies are read only after authentication and are capped at 64 MiB.
- Endpoints for features piecemeal doesn't have answer 409 with a short reason, qBittorrent's status for a valid request it can't carry out.
- Actions answer 200 with an empty body, or 204 where qBittorrent 5.3.0 does (`setSpeedLimits`, `addTrackers`, `editTracker`, `removeTrackers`, `setTags`, `deleteAPIKey`, `clientdata/store`).
- Instance-wide changes and private data need an admin, as in piecemeal's own API: other users' `setPreferences` applies the speed limits only, and their `banPeers`, `cookies`, `setCookies` and `log/main` get 403.
- Endpoints that take a single `hash` answer 404 for an unknown one; endpoints that take `hashes` skip unknown ones.

## Endpoints

Status: **yes** is implemented, **partial** works with the differences noted, **no** answers 404 `Endpoint does not exist` for now, and **never** answers 409.

### app

| Endpoint | Status | Notes |
| --- | --- | --- |
| `version` | yes | `v5.3.0` |
| `webapiVersion` | yes | `2.16.2` |
| `buildInfo` | yes | `libtorrent` names the anacrolix/torrent version; `qt`, `boost`, `openssl` and `zlib` are empty, since piecemeal uses none of them. |
| `processInfo` | yes | |
| `preferences` | yes | All 241 keys of qBittorrent 5.3.0 with their types. Keys piecemeal maps come from its settings (download, speed, connection, queueing, `max_active_checking_torrents`, seeding goals, IP filter, schedule, Web UI); the rest report qBittorrent's defaults and are fixed. `web_ui_api_key` is the prefix of the caller's key from `rotateAPIKey`, or empty: piecemeal stores only key hashes. `web_ui_host_header_validation_enabled` and `web_ui_domain_list` report `PIECEMEAL_ALLOWED_HOSTS`. |
| `setPreferences` | partial | Applies the keys piecemeal maps and ignores the rest, so writing back the whole object works. Keys pinned by environment variables are ignored. A mapped key with a bad value fails the whole request with 400; a `json` that isn't an object is ignored, as in qBittorrent. Rate limits are rounded down to whole KiB/s, as qBittorrent does. `max_active_checking_torrents` below 1 is stored as 1. `web_ui_password` is ignored: manage users in piecemeal. |
| `defaultSavePath` | yes | |
| `networkInterfaceList`, `networkInterfaceAddressList` | yes | Listed for the settings page; piecemeal binds with `PIECEMEAL_LISTEN_HOST`, so choosing one has no effect. |
| `getDirectoryContent` | yes | Only inside the allowed roots; 404 elsewhere. With `withMetadata`, objects with the name, type, size and dates; Linux reports no creation time, so `creation_date` is the inode change time there. |
| `getFreeSpaceAtPath` | yes | Only inside the allowed roots; 404 elsewhere. |
| `cookies`, `setCookies` | yes | Sent with torrent files fetched by URL, matched by domain, path and expiry. Kept across restarts. Admins only: they're usually tracker logins. |
| `rotateAPIKey`, `deleteAPIKey` | yes | Manage the caller's own `qbittorrent`-scoped key, named "qBittorrent API key". Need a session or a `full`-scope key. |
| `shutdown`, `sendTestEmail` | never | piecemeal runs as a service and sends no mail. |

### auth

| Endpoint | Status | Notes |
| --- | --- | --- |
| `login` | yes | See above. |
| `logout` | yes | |

### sync

| Endpoint | Status | Notes |
| --- | --- | --- |
| `maindata` | yes | Full and partial updates with the `rid` protocol, per session or API key. `server_state` has the 5.3.0 fields; counters piecemeal doesn't keep are 0. |
| `torrentPeers` | yes | Full and partial updates. `client`, `country` and `relevance` are empty or 0. |

### transfer

| Endpoint | Status | Notes |
| --- | --- | --- |
| `info` | yes | |
| `speedLimitsMode`, `setSpeedLimitsMode`, `toggleSpeedLimitsMode` | yes | Any `mode` but 0 turns the alternative limits on, as in qBittorrent. |
| `downloadLimit`, `uploadLimit`, `setDownloadLimit`, `setUploadLimit` | yes | The normal limits, whatever the mode, rounded down to whole KiB/s. |
| `getSpeedLimits`, `setSpeedLimits` | yes | |
| `banPeers` | yes | Adds the addresses to `banned_IPs`. |
| `pauseSession`, `resumeSession` | never | anacrolix has no session pause. |

### log

| Endpoint | Status | Notes |
| --- | --- | --- |
| `main` | yes | piecemeal's own log, info and above, from a bounded buffer. Admins only. |
| `peers` | yes | Addresses the IP filter refused (`blocked`, reason "IP filter" and the range's description) and banned ones (reason "banned"), each at most once a minute, the last 1,000. |

### torrents

| Endpoint | Status | Notes |
| --- | --- | --- |
| `info` | yes | `filter`, `category`, `tag`, `hashes`, `private`, `sort`, `reverse`, `limit` and `offset`; `includeFiles` and `includeTrackers` add the `files` and `trackers` lists (the trackers without the DHT, PeX and LSD rows). `availability` is the distributed copies among connected peers: -1 for complete torrents, as qBittorrent reports seeds, and 0 for torrents not running. `popularity` is the ratio per month of activity. `priority` is the queue position, 0 outside the queue, and -1 with queueing off. |
| `properties` | yes | 404 for unknown hashes, 200 for magnets still fetching metadata. `availability` and `popularity` as in `info`. |
| `files` | yes | `availability` is the share of a file's pieces some connected peer has, -1 for complete torrents. |
| `trackers` | partial | Trackers and their tiers, with status "not contacted" and -1 counts: anacrolix doesn't expose announce results ([#44](https://github.com/sebdanielsson/piecemeal/issues/44)). DHT, PeX and LSD rows as in qBittorrent. |
| `pieceStates`, `pieceHashes` | yes | |
| `add` | partial | `urls` (magnets, and http(s) links fetched with SSRF protection and the cookie jar), `torrents`, `savepath`, `category` (created if missing), `tags`, `stopped`/`paused`, `skip_checking`/`seedMode`, `rename`, `contentLayout`, `ratioLimit`, `seedingTimeLimit`, `inactiveSeedingTimeLimit`, `sequentialDownload`, `firstLastPiecePrio`, `forced`. Synchronous. Answers 200 with qBittorrent 5.2's JSON counts, 409 when every torrent was already there, 415 for invalid torrent files. `autoTMM`, `upLimit` and `dlLimit` are ignored. |
| `delete` | yes | Unknown hashes are ignored. |
| `start`, `stop` | yes | |
| `pause`, `resume` | yes | Aliases of `stop` and `start`, for WebAPI before 2.11. |
| `recheck`, `reannounce` | yes | Rechecks wait their turn, as in qBittorrent: see `max_active_checking_torrents`. Stopped torrents are checked too and stay stopped; stopping a torrent cancels its recheck. `reannounce` announces to every tracker; it doesn't take `trackers`/`urls`. |
| `setForceStart` | yes | |
| `setShareLimits` | partial | `ratioLimit`, `seedingTimeLimit`, `inactiveSeedingTimeLimit`; missing ones default to -2. `shareLimitAction` and `shareLimitsMode` are ignored: the global seed goal action applies, and any limit reached ends seeding. qBittorrent 5.3.0 requires those two; piecemeal doesn't, for older clients. |
| `downloadLimit`, `uploadLimit` | yes | 0 for every torrent: piecemeal has no per-torrent speed limits, so none applies. `dl_limit` and `up_limit` in `info` and `properties` are -1. |
| `setDownloadLimit`, `setUploadLimit` | never | anacrolix/torrent only limits the whole client; tracked in [#43](https://github.com/sebdanielsson/piecemeal/issues/43). The global limits work. |
| `toggleSequentialDownload`, `toggleFirstLastPiecePrio`, `filePrio`, `rename` | yes | |
| `topPrio`, `bottomPrio`, `increasePrio`, `decreasePrio` | yes | 409 "Torrent queueing must be enabled" with queueing off, as in qBittorrent. |
| `categories`, `createCategory`, `editCategory`, `removeCategories`, `setCategory` | yes | A category's `savePath` is used for torrents added to it without a save path. Changing a category doesn't move data, as in qBittorrent without automatic torrent management. |
| `tags`, `createTags`, `deleteTags`, `addTags`, `removeTags`, `setTags` | yes | `addTags` and `setTags` create unknown tags. |
| `addTrackers`, `editTracker`, `removeTrackers` | yes | `hash` may name several torrents, `all`, or `*` for `removeTrackers`. Percent-encoded URLs, as VueTorrent sends them, are decoded. `editTracker` takes `url` or `origUrl`; `tier` is ignored. |
| `webseeds`, `addWebSeeds`, `editWebSeed`, `removeWebSeeds` | yes | |
| `addPeers` | yes | Answers with each torrent's count of added and invalid peers. |
| `setLocation`, `setSavePath` | yes | Data moves in the background (state `moving`), inside the allowed roots. |
| `renameFile`, `renameFolder` | yes | Renames on disk and remembers the mapping. |
| `export` | yes | With the current trackers and web seeds, named after the torrent. |
| `downloadFile` | yes | Complete files only, by index or path; supports ranges. |
| `count` | yes | |
| `SSLParameters` | partial | Empty strings: piecemeal has no SSL torrents. |
| `setAutoManagement`, `setDownloadPath` | partial | Accepted and ignored: piecemeal has no automatic torrent management or incomplete folder. |
| `setSuperSeeding`, `setSSLParameters` | never | anacrolix has no super seeding or SSL torrents ([#46](https://github.com/sebdanielsson/piecemeal/issues/46), [#48](https://github.com/sebdanielsson/piecemeal/issues/48)). |
| `setComment` | yes | 204. Replaces the comment `info`, `properties` and `sync/maindata` show, kept across restarts; the torrent file and `export` keep the original. |
| `pieceAvailability` | yes | How many connected peers have each piece; `[]` for complete torrents, ones not running, and magnets without metadata. |
| `fetchMetadata` | yes | POST only. A magnet or bare info hash answers 202 with the hash while its metadata comes from peers, a torrent URL 202 with `{}` while it downloads (through the SSRF filter and cookie jar), then 200 with the metadata object; 400 for a source that isn't one. Nothing is added: the magnet joins the client with no storage and transfers disallowed until its metadata arrives. Up to 16 magnets and 64 URLs are fetched at once, each kept for an hour. `add` then uses what was fetched. |
| `saveMetadata` | yes | The fetched torrent file, by its source or info hash; 404 for an unknown URL, 409 while it isn't fetched. |
| `parseMetadata` | yes | POST only. Metadata objects for the uploaded torrent files, in field-name order; 415 for a file that isn't a torrent. |

### Other groups

| Group | Status | Notes |
| --- | --- | --- |
| `clientdata` | yes | `load` and `store`, kept across restarts, up to 1 MiB in all; a `store` past that gets 409. |
| `torrentcreator` | yes | `addTask`, `status`, `torrentFile`, `deleteTask`. Sources must be inside the allowed roots. |
| `search` | never | Prowlarr covers search. |

### rss

Feeds and download rules work as in qBittorrent 5.3.0: paths join names with `\`, feed `uid`s are stable, and actions answer 200, or 204 for `setFeedRefreshInterval`, `importRules` and `markAsRead` on an unknown path. Reading needs any user; changing feeds and rules needs an admin, since rules add torrents.

| Endpoint | Status | Notes |
| --- | --- | --- |
| `items` | yes | With `withData`, feeds carry `title`, `lastBuildDate`, `isLoading`, `hasError` and their articles, newest first. Article descriptions are sanitized HTML, and `link` and `torrentURL` are left out unless they're http(s) URLs (or magnet links for `torrentURL`). Callers other than admins see feed URLs with the query replaced by `…`, since trackers put passkeys there; `rules` and `exportRules` mask `affectedFeeds` the same way. |
| `addFolder`, `addFeed`, `removeItem`, `moveItem`, `setFeedURL`, `setFeedRefreshInterval` | yes | A feed added with an empty `path` is named after its title once fetched. Feed URLs are fetched with the same SSRF protection and cookies as torrent URLs. |
| `markAsRead`, `refreshItem` | yes | |
| `setRule`, `renameRule`, `removeRule`, `cloneRule`, `rules`, `exportRules`, `importRules` | yes | Rules take `torrentParams` or the legacy `addPaused`, `assignedCategory`, `savePath` and `torrentContentLayout`; a bad definition makes a rule with the defaults, as in qBittorrent. Options piecemeal doesn't use, such as `download_path` and per-torrent limits, are kept and returned. |
| `matchingArticles` | yes | Keyed by feed name, as in qBittorrent. |

Matching follows qBittorrent: wildcard terms (`*`, `?`, `[…]`) that must all match, `|` between alternatives, or one regular expression each with `useRegex`; the episode filter (`1x2;8-15;20-;`); the smart episode filter with REPACK and PROPER handling; `ignoreDays` from `lastMatch`; and rules tried in `priority` order, the first taking the article. Regular expressions are PCRE-like (lookahead and inline flags work), with a time limit per match. Only unread articles are downloaded, newest first, and an article is marked read once its torrent is added. Unlike qBittorrent, new articles are taken even when a feed's `lastBuildDate` didn't change. The seven `rss_*` preferences map to piecemeal's RSS settings.

## VueTorrent and other qBittorrent web UIs

piecemeal can serve a qBittorrent web UI such as VueTorrent at `/qbittorrent/`, with the API under it at `/qbittorrent/api/v2`, and then reports `alternative_webui_enabled`. See [Web UIs](../features/web-ui.md#vuetorrent-and-other-qbittorrent-web-uis).

## Torrent states

| piecemeal | qBittorrent `state` |
| --- | --- |
| Fetching metadata | `metaDL`, `forcedMetaDL` when forced |
| Downloading, payload in the last 30 s | `downloading`, `forcedDL` |
| Downloading, nothing for 30 s | `stalledDL`, `forcedDL` |
| Complete, uploading in the last 30 s | `uploading`, `forcedUP` |
| Complete, nothing for 30 s | `stalledUP`, `forcedUP` |
| Checking its data, or queued for checking | `checkingDL`, or `checkingUP` when it had finished downloading, as qBittorrent 5.3.0 reports both |
| Stopped, including at a seed goal | `stoppedDL`, `stoppedUP` |
| Waiting in the queue | `queuedDL`, `queuedUP` |
| Moving data | `moving` |
| Error | `error` |

Seed goals stop the torrent by default (`max_ratio_act` 0), which is what Radarr and Sonarr wait for before removing a finished download. Setting piecemeal's `seedGoalAction` to `remove` or `removeWithData` reports 1 or 3, and the \*arrs then warn that the client removes completed downloads.
