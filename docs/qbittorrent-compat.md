# qBittorrent WebAPI compatibility

piecemeal serves the qBittorrent WebAPI v2 at `/api/v2`. It reports qBittorrent `v5.3.0` and WebAPI `2.16.2`, and follows qBittorrent 5.3.0's behaviour on the endpoints below. It is written from qBittorrent's API documentation and observed behaviour, not from qBittorrent's source.

CI checks it two ways (see [`test/e2e`](../test/e2e)): Radarr, Sonarr and Prowlarr containers run their download client test against piecemeal and Radarr grabs a release through it, and the same calls go to a real qBittorrent 5.3.0 and piecemeal and their status codes and response shapes are compared.

## Authentication

- `auth/login` answers 204 and sets the session cookie twice: as `QBT_SID_<port>` (qBittorrent 5.2 and later) and as `SID` (older clients). Wrong credentials get 401; too many failures get 403 for 15 minutes.
- Every other endpoint accepts the session cookie, piecemeal's own web session, an API key as `Authorization: Bearer pm_...`, or HTTP Basic credentials when there is no session. Basic credentials are the qBittorrent 5.2 behaviour that current Radarr, Sonarr and Prowlarr rely on.
- API keys with the `qbittorrent` or `full` scope work; `read-only` keys can only make GET requests. Create them in piecemeal, not with qBittorrent's `app/rotateAPIKey`.
- Anything unauthenticated, including an unknown key, gets 403, which the *arrs read as "log in again".
- When a request has an `Origin` or `Referer` header, it must name the host the request went to (CSRF protection).

## Endpoints

Status: **yes** is implemented, **partial** works with the differences noted, **no** answers 404 for now, and **never** won't be implemented.

### app

| Endpoint | Status | Notes |
| --- | --- | --- |
| `version` | yes | `v5.3.0` |
| `webapiVersion` | yes | `2.16.2` |
| `buildInfo` | partial | Library versions are empty strings. |
| `preferences` | partial | Download, speed, connection, BitTorrent, seeding goal and Web UI keys, mapped from piecemeal's settings. Keys for features piecemeal doesn't have are absent. |
| `defaultSavePath` | yes | |
| `setPreferences`, `shutdown`, `networkInterfaceList`, `networkInterfaceAddressList`, `sendTestEmail`, `getDirectoryContent`, `cookies`, `setCookies`, `rotateAPIKey`, `deleteAPIKey` | no | Phase 4. |

### auth

| Endpoint | Status | Notes |
| --- | --- | --- |
| `login` | yes | See above. |
| `logout` | yes | |

### torrents

| Endpoint | Status | Notes |
| --- | --- | --- |
| `info` | yes | `filter`, `category`, `tag`, `hashes`, `private`, `sort`, `reverse`, `limit` and `offset`. Not yet `includeFiles` and `includeTrackers`. `availability` is -1 and `priority` is -1 (no queueing yet). |
| `properties` | yes | 404 for unknown hashes, 200 for magnets still fetching metadata. |
| `files` | yes | `availability` is -1. |
| `trackers` | partial | Trackers and their tiers, with status "not contacted" and -1 counts: anacrolix doesn't expose announce results. DHT, PeX and LSD rows as in qBittorrent. |
| `pieceStates` | yes | |
| `add` | partial | `urls` (magnets, and http(s) links fetched with SSRF protection), `torrents`, `savepath`, `category` (created if missing), `tags`, `stopped`/`paused`, `skip_checking`/`seedMode`, `rename`, `upLimit`, `dlLimit`, `ratioLimit`, `seedingTimeLimit`, `inactiveSeedingTimeLimit`, `sequentialDownload`, `firstLastPiecePrio`, `forced`. Synchronous. Answers 200 with qBittorrent 5.2's JSON counts, 409 when every torrent was already there, 415 for invalid torrent files. `contentLayout` and `autoTMM` are ignored: content always goes under the torrent's name, as `Original` does. |
| `delete` | yes | Unknown hashes are ignored. |
| `start`, `stop` | yes | |
| `pause`, `resume` | yes | Aliases of `stop` and `start`, for WebAPI before 2.11. |
| `recheck`, `reannounce` | yes | |
| `setForceStart` | yes | |
| `setShareLimits` | partial | `ratioLimit`, `seedingTimeLimit`, `inactiveSeedingTimeLimit`; missing ones default to -2. `shareLimitAction` and `shareLimitsMode` are ignored: the global seed goal action applies, and any limit reached ends seeding. |
| `setDownloadLimit`, `setUploadLimit` | partial | Stored and reported, not enforced yet (anacrolix has only client-wide limits). |
| `toggleSequentialDownload`, `toggleFirstLastPiecePrio`, `filePrio`, `rename` | yes | |
| `topPrio`, `bottomPrio`, `increasePrio`, `decreasePrio` | partial | 409 "Torrent queueing must be enabled", as qBittorrent answers with queueing off. Queueing is Phase 5. |
| `categories`, `createCategory`, `editCategory`, `removeCategories`, `setCategory` | yes | A category's `savePath` is used for torrents added to it without a save path. Changing a category doesn't move data, as in qBittorrent without automatic torrent management. |
| `tags`, `createTags`, `deleteTags`, `addTags`, `removeTags` | yes | `addTags` creates unknown tags. |
| `setSuperSeeding`, SSL parameter endpoints | never | anacrolix has no super seeding or SSL torrents. |
| everything else | no | Phase 4. |

### Other groups

| Group | Status | Notes |
| --- | --- | --- |
| `transfer` | partial | `info` only. The rest is Phase 4. |
| `sync`, `log`, `clientdata` | no | Phase 4. |
| `torrentcreator`, `rss` | no | Phase 5. |
| `search` | never | Prowlarr covers search. |

## Torrent states

| piecemeal | qBittorrent `state` |
| --- | --- |
| Fetching metadata | `metaDL`, `forcedMetaDL` when forced |
| Downloading, payload in the last 30 s | `downloading`, `forcedDL` |
| Downloading, nothing for 30 s | `stalledDL`, `forcedDL` |
| Complete, uploading in the last 30 s | `uploading`, `forcedUP` |
| Complete, nothing for 30 s | `stalledUP`, `forcedUP` |
| Checking | `checkingDL`, `checkingUP` |
| Stopped, including at a seed goal | `stoppedDL`, `stoppedUP` |
| Error | `error` |

Seed goals stop the torrent by default (`max_ratio_act` 0), which is what Radarr and Sonarr wait for before removing a finished download. Setting piecemeal's `seedGoalAction` to `remove` or `removeWithData` reports 1 or 3, and the *arrs then warn that the client removes completed downloads.
