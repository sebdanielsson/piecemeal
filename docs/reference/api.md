---
description: How to authenticate to piecemeal's native REST API and the qBittorrent WebAPI, with API keys and scopes, and how to follow live changes.
---

# APIs

piecemeal serves two APIs on the same port as the web UI:

- **The native API** at `/api/piecemeal/v1`: REST and JSON, described by an OpenAPI 3.1 document. The web UI uses it, and so can anything else. The [API reference](https://piecemeal-flax.vercel.app/reference) documents every operation; each piecemeal also serves its own interactive docs at `/api/piecemeal/v1/docs`, and the document itself at `/api/piecemeal/v1/openapi.json` and in the repository as [`api/openapi.json`](https://github.com/sebdanielsson/piecemeal/blob/main/api/openapi.json).
- **The qBittorrent WebAPI v2** at `/api/v2`, for the \*arrs, VueTorrent and other qBittorrent tools. See [qBittorrent compatibility](qbittorrent-compat.md).

## API keys

Create a key under **API keys** in the web UI, or with `POST /api/piecemeal/v1/api-keys`, and send it as a bearer token:

```sh
curl -H "Authorization: Bearer pm_..." https://piecemeal.example.com/api/piecemeal/v1/torrents
```

A key acts as the user who made it, limited by its scope:

| Scope | Native API | qBittorrent API |
| --- | --- | --- |
| `full` | everything the user can do | everything the user can do |
| `qbittorrent` | nothing | everything the user can do, except admin endpoints |
| `read-only` | reading only | reading only |

Admin endpoints, in either API, need an admin's `full` key: an admin's `qbittorrent` or `read-only` key doesn't count. Keys never expire; revoke them under **API keys**. piecemeal stores only a hash of each key, so a lost key can't be shown again: make a new one.

## Sessions

The web UI signs in with `POST /api/piecemeal/v1/auth/login` and a session cookie. Requests that change something with a session cookie must also send `X-Requested-With: piecemeal`, and an `Origin`, when the browser sends one, must match the host. That stops other sites from making requests with the cookie. API keys need neither.

Sessions last 30 days, or 7 days without use.

## Live changes

`GET /api/piecemeal/v1/events` is a server-sent events stream. It starts with a `snapshot` of every torrent, the transfer rates, categories, tags and settings, then sends `torrents`, `transfer`, `categories`, `tags` and `settings` events as they change. The web UI uses it instead of polling.

```sh
curl -N -H "Authorization: Bearer pm_..." https://piecemeal.example.com/api/piecemeal/v1/events
```

## Errors

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem documents with a `status`, a `title` and a `detail` that says what went wrong, and `errors` pointing at the bad fields when a request doesn't validate.

## Metrics

`GET /metrics` serves Prometheus metrics, when `PIECEMEAL_METRICS` is `public` or `auth`: torrents by state, peers, DHT nodes, rates and limits, session and per-torrent byte totals, free space, the listen port, and the Go runtime. With `auth`, send an API key; a `read-only` key is enough.
