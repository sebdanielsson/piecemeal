---
description: Run piecemeal with Docker, Docker Compose or a single binary, sign in for the first time, and keep it up to date.
---

# Install

piecemeal ships as a container image for linux/amd64 and linux/arm64, and as static binaries for Linux (amd64, arm64) and macOS (arm64). There's nothing else to install: the web UI is built into the binary and the database is SQLite.

## Docker Compose

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

Make the password hash, type a password when it waits for input, and put the hash in `.env` next to `compose.yaml`:

```sh
docker run --rm -i ghcr.io/sebdanielsson/piecemeal hash-password
```

```sh
PIECEMEAL_ADMIN_PASSWORD_HASH='$argon2id$v=19$m=65536,t=3,p=4$...'
```

Use single quotes: the hash contains `$`. Then `docker compose up -d`, open `http://localhost:8080` and sign in as `admin`.

- **Port 8080** serves the web UI, the native API and the qBittorrent API. **Port 6881** (TCP and UDP) is for peers; forward it on your router for incoming connections, or see [VPN and port forwarding](guides/vpn.md).
- **`/data`** holds the database and piece completion state. Keep it on fast storage, such as an SSD. Downloads can live on slow disks.
- **`/downloads`** is the default save path. Mount it at the same path in piecemeal and in the \*arrs, so the paths piecemeal reports work for them as they are.
- **`user`** sets who owns what piecemeal writes. The image runs as an unprivileged user by default, and its `/data` and `/downloads` are writable by any user, so a new volume works whatever you choose.
- **`stop_grace_period`** gives piecemeal time to save its state when it stops.

Without a password hash, piecemeal prints a temporary admin password to the log at every start. That's fine for a first look, not for keeping.

## Docker

```sh
docker run -d --name piecemeal \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v piecemeal-data:/data -v /path/to/downloads:/downloads \
  -e PIECEMEAL_ADMIN_PASSWORD_HASH='$argon2id$...' \
  ghcr.io/sebdanielsson/piecemeal:latest
```

Images are tagged with the full version (`1.2.3`), the minor version (`1.2`), the major version (`1`, from 1.0.1) and `latest`. Pin the major version to get fixes and features without breaking changes. Each image is signed with cosign (keyless, from the release workflow) and carries an SBOM and build provenance:

```sh
cosign verify ghcr.io/sebdanielsson/piecemeal:latest \
  --certificate-identity-regexp '^https://github.com/sebdanielsson/piecemeal/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Binaries

Each [GitHub release](https://github.com/sebdanielsson/piecemeal/releases) has `piecemeal_<version>_linux_amd64`, `_linux_arm64` and `_darwin_arm64`, with a `checksums.txt`. Download one, check it, and run it:

```sh
sha256sum --check --ignore-missing checksums.txt
chmod +x piecemeal_*_linux_amd64
PIECEMEAL_DATA_DIR=/var/lib/piecemeal PIECEMEAL_DOWNLOAD_DIR=/srv/downloads ./piecemeal_*_linux_amd64
```

`gh attestation verify piecemeal_*_linux_amd64 --repo sebdanielsson/piecemeal` checks a binary's build provenance.

Outside the image, the data and download directories default to `data` and `downloads` under the working directory. Every setting is an environment variable; see [Configuration](configuration.md).

To build from source, see [Development](help/development.md).

## Health checks

- `GET /healthz` answers `ok` while the process serves requests.
- `GET /readyz` answers 503 until piecemeal is ready to take torrents. With NAT-PMP, that's once the first port mapping exists.
- `piecemeal healthcheck` calls `/healthz` on `PIECEMEAL_ADDR` and exits non-zero when it fails. The image uses it as its `HEALTHCHECK`, so distroless containers need no `curl`.

## Commands

The binary is also the tool for everything around the daemon:

| Command | What it does |
| --- | --- |
| `piecemeal` | Run the daemon, configured by `PIECEMEAL_*` variables. |
| `piecemeal hash-password` | Read a password on stdin and print its argon2id hash. |
| `piecemeal backup FILE` | Copy the database to `FILE`, while piecemeal runs. See [Backup and restore](guides/backup.md). |
| `piecemeal restore FILE` | Replace the database with a backup. piecemeal must be stopped. |
| `piecemeal import transmission\|qbittorrent DIR` | Import another client's torrents. See [Move from Transmission or qBittorrent](guides/import.md). |
| `piecemeal openapi` | Print the native API's OpenAPI document. |
| `piecemeal healthcheck` | Check `/healthz`, for container health checks. |
| `piecemeal version` | Print the version. |

In a container, run them with `docker exec piecemeal piecemeal <command>`, or for `restore` and `import`, which need piecemeal stopped, with `docker run --rm` on the same volumes.

## Upgrading

Pull the new image and recreate the container. The database migrates itself on start; take a [backup](guides/backup.md) first if you want a way back. Releases follow [semantic versioning](https://semver.org), and the [changelog](https://github.com/sebdanielsson/piecemeal/blob/main/CHANGELOG.md) marks breaking changes.
