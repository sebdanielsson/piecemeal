---
description: Fixes for the problems people run into most with piecemeal, from the *arrs and reverse proxies to SSO, paths and peers.
---

# Troubleshooting

Admins see piecemeal's recent log under **Log**, and failed sign-ins and changes to settings, keys and users under **Audit log**. For more detail, set `PIECEMEAL_LOG_LEVEL=debug`.

| Symptom | Cause and fix |
| --- | --- |
| An \*arr's download client test fails with 403 | The API key is wrong, read-only or revoked, or the address is locked out after 5 failed sign-ins; wait 15 minutes. With a username and password, check them, and that `PIECEMEAL_LOCAL_LOGIN` is on. |
| Grabs fail with "address not allowed" | The download link is on a private address, such as Prowlarr's proxy. Set `PIECEMEAL_FETCH_ALLOWED_NETWORKS`; see [Radarr, Sonarr and Prowlarr](../guides/arr.md#download-links-on-private-networks). |
| The \*arr can't find finished downloads | piecemeal and the \*arr see the downloads under different paths. Mount them at the same path, or add a remote path mapping in the \*arr. |
| "path is outside the allowed roots" when adding or moving | The save path is outside `PIECEMEAL_ALLOWED_ROOTS`, or a category's save path is. |
| 421 Misdirected Request | The host name isn't in `PIECEMEAL_ALLOWED_HOSTS`, or the public URL's host. |
| You're signed out right after signing in | Cookies are `Secure` and the site is plain HTTP. Use HTTPS, or set `PIECEMEAL_SECURE_COOKIES=false`. |
| The web UI doesn't update live | A reverse proxy buffers the event stream at `/api/piecemeal/v1/events`. Turn buffering off; see [Reverse proxy](../guides/reverse-proxy.md). |
| Everyone is locked out of signing in at once | piecemeal sees every client as the reverse proxy. Set `PIECEMEAL_TRUSTED_PROXIES`, and look for the warning about an untrusted `X-Forwarded-For` in the log. |
| piecemeal won't start: "the allowed root overlaps" | An allowed root contains the data directory or the qBittorrent web UI directory, or the reverse. Keep them apart. |
| piecemeal won't start: "PIECEMEAL_OIDC_ALLOWED_GROUPS: required" | Set the groups that may sign in, or `PIECEMEAL_OIDC_ALLOW_ANY=true`; see [Single sign-on](../guides/sso.md). |
| "another piecemeal is using the data directory" | Another piecemeal, or a `restore` or `import`, is using it. Stop it first. |
| SSO sign-in says you're not in an allowed group | The ID token has no matching group. Check the scopes the client asks for, and `PIECEMEAL_OIDC_GROUPS_CLAIM`. |
| No incoming peers; the status bar says firewalled | The peer port isn't reachable: forward it on your router, or use NAT-PMP through a VPN; see [VPN and port forwarding](../guides/vpn.md). |
| Torrents get few new peers while one downloads at the global download limit | anacrolix/torrent makes no new peer connections, for any torrent, while the global download limit is used up ([anacrolix/torrent#1241](https://github.com/anacrolix/torrent/issues/1241)). piecemeal retries dialling every second, so outgoing connections get through when the limit has room, while incoming ones are refused whenever it is, also for torrents that only seed. Raise the download limit or turn it off, or turn the alternative limits on only when you need them. |
| Torrents stay in "checking" or "queued for checking" for a long time | piecemeal is verifying data on disk, after an import, a restore, a recheck or a lost piece completion database, `maxActiveCheckingTorrents` torrents at a time (one by default) while the rest wait. It's limited by the disk; see [Performance](../reference/performance.md). On SSDs, checking more at once can be faster. |
| Writes are slow on ZFS | Leave `PIECEMEAL_MMAP` off. Memory-mapped writes land as small page writes in large records and amplify many times over. |

If something else is wrong, [open an issue](https://github.com/sebdanielsson/piecemeal/issues) with the version (`piecemeal version`), what you did, and the log around it.
