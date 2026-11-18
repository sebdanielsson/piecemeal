---
description: Serve piecemeal behind Caddy, Traefik or nginx over HTTPS, with the real client address and a host allowlist.
---

# Reverse proxy

piecemeal serves plain HTTP. Put it behind a reverse proxy for HTTPS, and tell it where people reach it and which proxy to trust:

```yaml
    environment:
      PIECEMEAL_PUBLIC_URL: https://piecemeal.example.com
      PIECEMEAL_TRUSTED_PROXIES: 172.18.0.0/16
      PIECEMEAL_ALLOWED_HOSTS: piecemeal.example.com
```

- **`PIECEMEAL_PUBLIC_URL`** is the address people use. SSO needs it for its callback.
- **`PIECEMEAL_TRUSTED_PROXIES`** lets piecemeal read the client's address from `X-Forwarded-For` when the request comes from one of these networks, so the sign-in lockout and the audit log see real clients instead of the proxy. piecemeal logs a warning when the header arrives from a peer it doesn't trust, and ignores it.
- **`PIECEMEAL_ALLOWED_HOSTS`** answers other host names with 421. It stops DNS rebinding, where a web page a LAN user visits reaches piecemeal under the page's own name. `localhost`, loopback addresses, the public URL's host and the health checks always work.
- **Cookies are `Secure`**, so serve piecemeal over HTTPS. For plain HTTP on a host other than `localhost`, set `PIECEMEAL_SECURE_COOKIES=false`.

The proxy needs nothing special. piecemeal serves everything at the root of its host; a path prefix such as `/piecemeal/` isn't supported. The web UI keeps a server-sent events stream open at `/api/piecemeal/v1/events`, so the proxy must not buffer responses, and its timeouts should allow long-lived requests.

## Caddy

```text
piecemeal.example.com {
	reverse_proxy piecemeal:8080
}
```

Caddy flushes server-sent events as they come.

## Traefik

```yaml
services:
  piecemeal:
    labels:
      - traefik.enable=true
      - traefik.http.routers.piecemeal.rule=Host(`piecemeal.example.com`)
      - traefik.http.routers.piecemeal.tls.certresolver=letsencrypt
      - traefik.http.services.piecemeal.loadbalancer.server.port=8080
```

## nginx

```text
server {
    listen 443 ssl;
    server_name piecemeal.example.com;

    location / {
        proxy_pass http://piecemeal:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_read_timeout 1h;
        client_max_body_size 64m;
    }
}
```

`client_max_body_size` lets large `.torrent` files through: the qBittorrent API accepts bodies up to 64 MiB, and the native API's add endpoint up to 24 MiB.

## Tailscale

On a tailnet, `tailscale serve` gives piecemeal HTTPS with a `ts.net` certificate:

```sh
tailscale serve --bg --https=443 http://127.0.0.1:8080
```

Set `PIECEMEAL_PUBLIC_URL` to the `https://<machine>.<tailnet>.ts.net` address.
