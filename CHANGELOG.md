# Changelog

## [1.0.0](https://github.com/sebdanielsson/piecemeal/compare/v0.6.0...v1.0.0) (2026-11-18)


### Bug Fixes

* **deps:** update dependency @tanstack/react-query to ^5.108.0 ([#32](https://github.com/sebdanielsson/piecemeal/issues/32)) ([7e637b9](https://github.com/sebdanielsson/piecemeal/commit/7e637b906779450d59cbc95b5e7dcf4bf67b62e8))
* **deps:** update module golang.org/x/net to v0.63.0 ([#34](https://github.com/sebdanielsson/piecemeal/issues/34)) ([ffb873d](https://github.com/sebdanielsson/piecemeal/commit/ffb873dbfa0ef83d1fa68479a58d4cd2fa0795f1))
* **import:** make Transmission's first label the category ([#33](https://github.com/sebdanielsson/piecemeal/issues/33)) ([02c8925](https://github.com/sebdanielsson/piecemeal/commit/02c89254bf27fb2c5d89e9602a3d2fd4b08df0fe))


### Documentation

* prepare 1.0 ([#40](https://github.com/sebdanielsson/piecemeal/issues/40)) ([77b8691](https://github.com/sebdanielsson/piecemeal/commit/77b8691315799d28906835cb9430963ed0a23246))

## [0.6.0](https://github.com/sebdanielsson/piecemeal/compare/v0.5.0...v0.6.0) (2026-11-18)


### ⚠ BREAKING CHANGES

* with OIDC, set PIECEMEAL_OIDC_ALLOWED_GROUPS, or PIECEMEAL_OIDC_ALLOW_ANY=true to keep letting every account at the provider sign in.

### Features

* back up and restore the database ([#27](https://github.com/sebdanielsson/piecemeal/issues/27)) ([fdfd19a](https://github.com/sebdanielsson/piecemeal/commit/fdfd19aea0e2cb859c58feae1d9fb79efefee221))
* import torrents from Transmission and qBittorrent ([#28](https://github.com/sebdanielsson/piecemeal/issues/28)) ([5cdd2d3](https://github.com/sebdanielsson/piecemeal/commit/5cdd2d3b00e7306a70bf1af7d1b645efeda4994a))


### Bug Fixes

* **deps:** update dependency lucide-react to ^1.74.0 ([#25](https://github.com/sebdanielsson/piecemeal/issues/25)) ([1e1fbf4](https://github.com/sebdanielsson/piecemeal/commit/1e1fbf4981b81e72d73a29a2e5861c13b5921ada))
* **deps:** update module github.com/dlclark/regexp2 to v2 ([#23](https://github.com/sebdanielsson/piecemeal/issues/23)) ([3e10ed6](https://github.com/sebdanielsson/piecemeal/commit/3e10ed6c16f9bd3c19995ab08f0c83344322663e))
* harden authentication, fetching and RSS from a security audit ([#29](https://github.com/sebdanielsson/piecemeal/issues/29)) ([b9057c5](https://github.com/sebdanielsson/piecemeal/commit/b9057c5d29971964f990f77492ad8788f02727f7))


### Performance Improvements

* faster sync/maindata polls, measured with a 1,000-torrent load test ([#30](https://github.com/sebdanielsson/piecemeal/issues/30)) ([fc8ca1c](https://github.com/sebdanielsson/piecemeal/commit/fc8ca1c76c4605cc53a8a1e263b35cc652a97f40))

## [0.5.0](https://github.com/sebdanielsson/piecemeal/compare/v0.4.0...v0.5.0) (2026-11-18)


### Features

* RSS feeds and download rules ([#21](https://github.com/sebdanielsson/piecemeal/issues/21)) ([df2301c](https://github.com/sebdanielsson/piecemeal/commit/df2301ccafd589018637a98f0223020b8e3a8522))


### Bug Fixes

* **engine:** report a torrent complete only once its pieces are verified ([#20](https://github.com/sebdanielsson/piecemeal/issues/20)) ([6c5b949](https://github.com/sebdanielsson/piecemeal/commit/6c5b9490ea943867bc0d72f9c5f812d28876d192))

## [0.4.0](https://github.com/sebdanielsson/piecemeal/compare/v0.3.0...v0.4.0) (2026-11-18)


### Features

* full qBittorrent WebAPI and Phase 5 engine features ([#18](https://github.com/sebdanielsson/piecemeal/issues/18)) ([2cf561f](https://github.com/sebdanielsson/piecemeal/commit/2cf561fad74f2af7485e8d89e89e59e7ea0fd6d2))

## [0.3.0](https://github.com/sebdanielsson/piecemeal/compare/v0.2.0...v0.3.0) (2026-11-18)


### Features

* **auth:** OIDC sign-in ([#12](https://github.com/sebdanielsson/piecemeal/issues/12)) ([ffd6f72](https://github.com/sebdanielsson/piecemeal/commit/ffd6f7258ea6fba90bb56893ed31e4881f35dade))
* healthcheck command, and an image that runs as any user ([#13](https://github.com/sebdanielsson/piecemeal/issues/13)) ([4e08de8](https://github.com/sebdanielsson/piecemeal/commit/4e08de85739a3201c8fbbd3bb4afc689cb0cb1af))
* qBittorrent WebAPI for Radarr, Sonarr and Prowlarr ([#11](https://github.com/sebdanielsson/piecemeal/issues/11)) ([d832791](https://github.com/sebdanielsson/piecemeal/commit/d832791f21afdfa8ebc9abba97c94393127e7632))
* web UI ([#14](https://github.com/sebdanielsson/piecemeal/issues/14)) ([092d50b](https://github.com/sebdanielsson/piecemeal/commit/092d50b8f61bae35ba2b29f428cb9b18785c9325))

## [0.2.0](https://github.com/sebdanielsson/piecemeal/compare/v0.1.0...v0.2.0) (2026-11-17)


### Features

* engine, store, auth and native API ([#9](https://github.com/sebdanielsson/piecemeal/issues/9)) ([dd853fe](https://github.com/sebdanielsson/piecemeal/commit/dd853fe28ac4971e9d6d4b6d23007ee2d9a477d3))


### Bug Fixes

* **ci:** pin cosign-installer to an existing tag ([#7](https://github.com/sebdanielsson/piecemeal/issues/7)) ([2510e40](https://github.com/sebdanielsson/piecemeal/commit/2510e403d748a6e5b89d387f07265a280e1200bc))

## 0.1.0 (2026-11-16)


### Features

* project plan, skeleton binary and CI/CD ([1969b05](https://github.com/sebdanielsson/piecemeal/commit/1969b05d3cd9a29a3a778bb8279115ec1e4c7a9a))
