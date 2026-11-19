---
description: How piecemeal authenticates people and programs, what users and admins may do, and how it guards paths, fetches and the browser.
---

# Security

piecemeal runs on your network with write access to your downloads, so it treats every request, every link from an indexer and every torrent as untrusted. This page describes what it does about that. To report a vulnerability, use [GitHub's private vulnerability reporting](https://github.com/sebdanielsson/piecemeal/security/advisories/new) rather than a public issue.

## Who can sign in

- **The local admin** signs in with a password, checked against the argon2id hash in `PIECEMEAL_ADMIN_PASSWORD_HASH`. Changing the hash ends the admin's sessions at the next start.
- **SSO users** sign in through OpenID Connect with the authorization code flow, PKCE, state and nonce. The flow's state lives in a cookie sealed with AES-GCM, so starting sign-ins costs the server nothing. Only members of `PIECEMEAL_OIDC_ALLOWED_GROUPS` get in; see [Single sign-on](../guides/sso.md).
- **Programs** use API keys, sent as bearer tokens, or for qBittorrent clients a password sign-in or HTTP Basic credentials. See [APIs](api.md#api-keys) for key scopes.

Session tokens and API keys are random, and piecemeal stores only their SHA-256 hashes. Session cookies are `HttpOnly`, `SameSite=Lax` and, by default, `Secure`. Sessions end after 30 days, or 7 days unused. Admins can end anyone's sessions, revoke anyone's keys, and remove users, which does both.

## Roles

Users can do everything with torrents, categories, tags and speed limits, read RSS, and manage their own keys and sessions. Admins can also change settings, RSS feeds and rules, cookies, and IP bans, see the log and audit log, manage users, and download backups. Admin endpoints need an admin's session or `full` key in both APIs; see [Single sign-on](../guides/sso.md#what-users-and-admins-can-do).

## Sign-in limits

After 5 failed sign-ins in 15 minutes, an address is locked out for the rest of the window. Public IPv6 addresses count per /64, since one host usually has a whole /64; private, link-local and loopback addresses count one by one. At most two password checks and four Basic checks run at once, since each argon2id check takes 64 MiB, and sign-in bodies are capped at 64 KiB. Behind a reverse proxy, set `PIECEMEAL_TRUSTED_PROXIES` so the limits see clients, not the proxy.

## Browsers

- **CSRF.** Native API requests that change something with a session cookie must send `X-Requested-With: piecemeal`, which other sites can't send without a CORS preflight piecemeal doesn't allow. An `Origin` or `Referer`, when present, must match the host, in both APIs.
- **DNS rebinding.** `PIECEMEAL_ALLOWED_HOSTS` answers other host names with 421.
- **Clickjacking.** piecemeal's own pages refuse to be framed.
- **Redirects** after sign-in only go to paths on the same site.
- **Other web UIs** served at `/qbittorrent/` share piecemeal's origin; see [Web UIs](../features/web-ui.md#vuetorrent-and-other-qbittorrent-web-uis) for what piecemeal does about that.

## Paths

Every save path, move, rename and directory listing must be inside `PIECEMEAL_ALLOWED_ROOTS`. Symlinks are resolved first, so a link can't lead out. File names inside a torrent can't climb out of its save path. The roots must not overlap the data directory or a qBittorrent web UI directory, so a torrent can't overwrite piecemeal's database or web UI.

## Fetching URLs

Adding a torrent by URL, and fetching RSS feeds, refuses loopback, private, link-local, carrier-grade NAT and other special-purpose addresses, including IPv4 addresses inside NAT64, 6to4 and Teredo IPv6 addresses. The check runs on the address actually dialed, after DNS resolution and after every redirect, so a name that resolves to an internal address is refused too. `PIECEMEAL_FETCH_ALLOWED_NETWORKS` allows the private networks you choose. Responses are capped at 16 MiB, and errors and logs show URLs without their query, where indexers and trackers put keys. Through a [proxy](../guides/proxy.md), piecemeal still looks the hostname up and checks the address, then asks the proxy for that address, so the proxy can't be used to reach a name that resolves differently there.

## Secrets in the open

- People other than admins see RSS feed URLs with the query hidden.
- Article links and torrent URLs are shown as the feed gives them, so a tracker that puts a passkey in them shares it with everyone who can read RSS.
- RSS article HTML is sanitized before it reaches qBittorrent clients.
- The proxy password is stored in the database, and so in backups, but never returned by the web UI or the APIs, and the audit log hides it. `PIECEMEAL_PROXY_PASSWORD` keeps it out of the database.

## Audit log

Admins see an audit log of failed and locked-out sign-ins, SSO sign-ins refused for their groups, API keys created and revoked, sessions ended, users created and removed, admin password changes, settings and speed limit changes, and backups downloaded.

## Building piecemeal

Releases are built in GitHub Actions. Images are signed with cosign and carry an SBOM and build provenance; binaries carry build provenance. CI runs CodeQL, `govulncheck`, dependency review, and fuzzing of the code that reads untrusted input: torrent files and magnet links, RSS feeds and rules, episode filters, IP filters, file paths, settings, and qBittorrent API parameters.

## Known limits

- An SSO user removed from an admin group stays an admin until their next sign-in. Remove them under **Users** to cut them off at once.
- API keys don't expire.
- `PIECEMEAL_BIND_INTERFACE` binds the sockets piecemeal opens for BitTorrent, public fetches and name lookups. The TCP listener isn't bound, since it only accepts connections, and neither are NAT-PMP, the connections of SSO sign-in, or lookups from a name server on loopback. A firewall in the VPN container covers everything; see [VPN and port forwarding](../guides/vpn.md#kill-switch).
- Trackers, web seeds and peers you add by hand aren't checked against the private-address filter: anyone who can add a torrent can make piecemeal connect to an address on your network.
