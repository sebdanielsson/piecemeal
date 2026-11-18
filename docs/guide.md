# User guide

How to run piecemeal and connect the things around it. Every environment variable is listed in the [README](../README.md#configuration); this guide covers the common setups.

## Install with Docker Compose

```yaml
services:
  piecemeal:
    image: ghcr.io/sebdanielsson/piecemeal:latest
    # The user that owns the downloads, so the *arrs can move and delete
    # what piecemeal downloads.
    user: "1000:1000"
    ports:
      - 8080:8080
      - 6881:6881
      - 6881:6881/udp
    environment:
      PIECEMEAL_ADMIN_PASSWORD_HASH: ${PIECEMEAL_ADMIN_PASSWORD_HASH}
    volumes:
      - piecemeal-data:/data
      - /srv/media/downloads:/downloads
    stop_grace_period: 30s
    restart: unless-stopped

volumes:
  piecemeal-data:
```

Make the password hash with `docker run --rm -i ghcr.io/sebdanielsson/piecemeal hash-password`, type the password, and put the hash in `.env` as `PIECEMEAL_ADMIN_PASSWORD_HASH='$argon2id$...'` (single quotes: the hash contains `$`). Then open `http://localhost:8080` and sign in as `admin`.

- Keep `/data` on fast storage: it holds the database and piece completion state. Downloads can live on slow disks.
- Mount downloads at the same path in piecemeal and in the *arrs, here `/downloads`. The paths piecemeal reports then work for them as they are, without remote path mappings.
- Save paths must be inside `PIECEMEAL_ALLOWED_ROOTS`, the download directory by default. The roots can't overlap `/data`.
- `stop_grace_period` gives piecemeal time to save its state on shutdown.
- Changing the password hash signs the admin out everywhere at the next start. API keys keep working; review them under API keys.

## Connect Radarr, Sonarr and Prowlarr

piecemeal speaks the qBittorrent WebAPI, so the *arrs add it as a qBittorrent download client.

1. In piecemeal, open **API keys** and create a key with the **qBittorrent API** scope, named after the app.
2. In Radarr or Sonarr, go to **Settings > Download Clients**, add **qBittorrent**, and set:
   - **Host**: piecemeal's host name as the app reaches it, such as `piecemeal` on a shared Docker network.
   - **Port**: `8080`, or `PIECEMEAL_ADDR`'s port.
   - **API key**: the key. Leave the username and password empty. With an older app that has no API key field, use the admin's username and password instead.
   - **Category**: `radarr` or `sonarr`. piecemeal creates it on the first grab.
3. Press **Test**, then **Save**.

Prowlarr is added the same way under **Settings > Download Clients**, for manual grabs.

When an app sends piecemeal a download link on a private address, such as Prowlarr's `http://prowlarr:9696/...` proxy links, piecemeal refuses to fetch it: links come from indexers, and fetching private addresses would let them reach your network. Allow the networks the *arrs and Prowlarr live on, for example the Docker network's subnet:

```yaml
    environment:
      PIECEMEAL_FETCH_ALLOWED_NETWORKS: 172.18.0.0/16
```

Without that, grabs fail with "address not allowed" in piecemeal's log, under **Log**.

## Behind a reverse proxy

Set where people reach piecemeal and which proxy to trust:

```yaml
    environment:
      PIECEMEAL_PUBLIC_URL: https://piecemeal.example.com
      PIECEMEAL_TRUSTED_PROXIES: 172.18.0.0/16
      PIECEMEAL_ALLOWED_HOSTS: piecemeal.example.com
```

- `PIECEMEAL_TRUSTED_PROXIES` lets piecemeal read the client's address from `X-Forwarded-For`, so the sign-in lockout and the audit log see real clients instead of the proxy. piecemeal logs a warning when the header arrives from a peer it doesn't trust.
- `PIECEMEAL_ALLOWED_HOSTS` answers other host names with 421. It stops DNS rebinding, where a web page a LAN user visits reaches piecemeal under the page's own name. `localhost`, loopback addresses, the public URL's host and the health checks always work.
- Cookies are marked `Secure`, so serve piecemeal over HTTPS. For plain HTTP on a host other than localhost, set `PIECEMEAL_SECURE_COOKIES=false`.

The proxy needs nothing special. Turn off response buffering if it buffers: the web UI uses server-sent events at `/api/piecemeal/v1/events`. With Caddy:

```text
piecemeal.example.com {
	reverse_proxy piecemeal:8080
}
```

## Sign in with SSO

piecemeal signs people in through any OpenID Connect provider, such as Pocket ID, Authelia, Authentik or Keycloak. Register a client at the provider with the callback `https://piecemeal.example.com/api/piecemeal/v1/auth/oidc/callback`, then:

```yaml
    environment:
      PIECEMEAL_PUBLIC_URL: https://piecemeal.example.com
      PIECEMEAL_OIDC_ISSUER: https://id.example.com
      PIECEMEAL_OIDC_CLIENT_ID: piecemeal
      PIECEMEAL_OIDC_CLIENT_SECRET: ${PIECEMEAL_OIDC_CLIENT_SECRET}
      PIECEMEAL_OIDC_NAME: Pocket ID
      PIECEMEAL_OIDC_ALLOWED_GROUPS: media
      PIECEMEAL_OIDC_ADMIN_GROUPS: admins
```

- Members of the allowed groups can sign in, and are created as users on their first sign-in. Those also in an admin group are admins; an admin group doesn't let anyone in by itself, so put admins in an allowed group too. The groups come from the ID token's `groups` claim; set `PIECEMEAL_OIDC_GROUPS_CLAIM` if your provider names it differently.
- piecemeal refuses to start with OIDC but no allowed groups, because every user can write anywhere under the allowed roots. When the provider only has people you trust, set `PIECEMEAL_OIDC_ALLOW_ANY=true` instead.
- The local admin keeps working, so you can still get in when the provider is down. Set `PIECEMEAL_LOCAL_LOGIN=false` to turn password sign-in off; the *arrs then need API keys.
- Admin rights follow the groups at each sign-in. To remove someone, take them out of the groups at the provider, then remove them under **Users**, which ends their sessions and revokes their API keys.

## Through a VPN with port forwarding

piecemeal can run in a VPN container's network namespace and forward its peer port with NAT-PMP, as ProtonVPN and some other providers offer:

```yaml
services:
  wireguard:
    image: ghcr.io/linuxserver/wireguard
    cap_add: [NET_ADMIN]
    ports:
      - 8080:8080
    volumes:
      - ./wg0.conf:/config/wg_confs/wg0.conf

  piecemeal:
    image: ghcr.io/sebdanielsson/piecemeal:latest
    network_mode: service:wireguard
    environment:
      PIECEMEAL_NATPMP_GATEWAY: 10.2.0.1
      PIECEMEAL_DISABLE_IPV6: "true"
```

- `PIECEMEAL_NATPMP_GATEWAY` is the VPN's NAT-PMP gateway, `10.2.0.1` for ProtonVPN. piecemeal asks it for a port, listens on whatever port it gets, and follows when the VPN changes it. Don't publish a peer port: it's on the VPN's side.
- `PIECEMEAL_DISABLE_IPV6` keeps peers to IPv4 when the tunnel only carries IPv4.
- `/readyz` answers 503 until the first port mapping, so an orchestrator can wait for it.
- The web UI and APIs are published on the VPN container, since piecemeal shares its network.

## Other web UIs

piecemeal's own web UI is at `/`. A qBittorrent web UI such as VueTorrent can be served at `/qbittorrent/` with `PIECEMEAL_QBITTORRENT_WEBUI_DIR`; see [qBittorrent compatibility](qbittorrent-compat.md#vuetorrent-and-other-qbittorrent-web-uis). It runs with the same rights as piecemeal's own UI, so only use one you trust.

## RSS

Feeds and download rules are under **RSS**. They work like qBittorrent's: rules match titles with wildcards or regular expressions, can filter episodes, and add matching torrents with a category, save path and tags. Rules exported from qBittorrent import as they are through the qBittorrent API's `rss/importRules`. Everyone can read feeds; only admins change feeds and rules, since rules add torrents. Others see feed URLs with the query hidden, since trackers put passkeys there.

## Disks

- Leave `PIECEMEAL_MMAP` off on ZFS: memory-mapped writes amplify there.
- Data that's being verified or moved shows as **checking** or **moving**. On slow disks, adding many torrents with data in place takes a while to verify; the load test in [Performance](performance.md) measures it.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| An *arr's test fails with 403 | The key is wrong, read-only, or was revoked, or the address is locked out after 5 failed sign-ins (15 minutes). |
| Grabs fail with "address not allowed" | The download link is on a private address; see `PIECEMEAL_FETCH_ALLOWED_NETWORKS` above. |
| The *arr can't find finished downloads | piecemeal and the *arr see the downloads under different paths. Mount them at the same path, or add a remote path mapping in the *arr. |
| 421 Misdirected Request | The host name isn't in `PIECEMEAL_ALLOWED_HOSTS`. |
| Sign-in works but you're signed out right away | Cookies are `Secure` and the site is plain HTTP. Use HTTPS, or set `PIECEMEAL_SECURE_COOKIES=false`. |
| piecemeal won't start: "the allowed root overlaps" | An allowed root contains the data directory or the qBittorrent web UI directory, or the reverse. Keep them apart. |
| piecemeal won't start: "PIECEMEAL_OIDC_ALLOWED_GROUPS: required" | See [Sign in with SSO](#sign-in-with-sso). |
| No incoming peers | The peer port isn't reachable: publish it, forward it on your router, or use NAT-PMP through the VPN. |

Admins see piecemeal's recent log under **Log**, and failed sign-ins and changes to settings, keys and users under **Audit log**.
