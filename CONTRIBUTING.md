# Contributing

Thanks for helping. Bug reports, fixes and features are all welcome. For anything bigger than a small fix, open an issue first so we can agree on the approach.

[Development](docs/help/development.md) covers the layout, building, the tests and the conventions. In short:

- Run `mise run check` before pushing; CI runs the same checks plus the end-to-end tests against Radarr, Sonarr, Prowlarr and a real qBittorrent.
- Add tests with the change. Parsers of untrusted input have fuzz tests; extend them when you touch one.
- After changing the native API, run `mise run generate` and commit the updated OpenAPI document and web client.
- Never copy qBittorrent's source: it's GPL, piecemeal is MIT. The compatibility layer is written from the API documentation and observed behaviour; update [qBittorrent compatibility](docs/reference/qbittorrent-compat.md) when behaviour changes.

Pull request titles follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, ...): pull requests are squash-merged and release-please builds the changelog from the titles. Describe what changed and why, and how you tested it.

Report security problems privately; see [SECURITY.md](SECURITY.md).
