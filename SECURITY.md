# Security policy

## Supported versions

Security fixes go into the latest release. Upgrade to it before reporting, in case the problem is already fixed.

## Reporting a vulnerability

Report vulnerabilities privately through [GitHub's private vulnerability reporting](https://github.com/sebdanielsson/piecemeal/security/advisories/new), not in a public issue. Include the version, how piecemeal is configured (with secrets removed), and the steps that show the problem.

You'll get an answer within a week. Once a fix is released, the advisory is published with credit to you unless you'd rather stay anonymous.

[Security](docs/reference/security.md) in the docs describes piecemeal's security model: users and admins, API key scopes, sign-in limits, confined paths and the SSRF filter.

## Scope

In scope: the piecemeal daemon, its native and qBittorrent APIs, its web UI, and the container image. That includes authentication and sessions, API key scopes, the separation between admins and users, path handling under the allowed roots, and fetching URLs (the SSRF filter).

Out of scope: third-party web UIs served with `PIECEMEAL_QBITTORRENT_WEBUI_DIR`, which run with the same rights as piecemeal's own; peers, trackers and web seeds, which BitTorrent connects to by design; and denial of service by someone who already holds an admin account.
