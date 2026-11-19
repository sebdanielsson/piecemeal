# Changelog

## [1.1.1](https://github.com/sebdanielsson/piecemeal/compare/v1.1.0...v1.1.1) (2026-11-19)


### Bug Fixes

* **engine:** open a magnet's storage when its metadata arrives during the add ([#63](https://github.com/sebdanielsson/piecemeal/issues/63)) ([0946dee](https://github.com/sebdanielsson/piecemeal/commit/0946dee40bd96000fa0bf9b3d12db2fc3255c8c4))

## [1.1.0](https://github.com/sebdanielsson/piecemeal/compare/v1.0.0...v1.1.0) (2026-11-19)


### Features

* check one torrent's data at a time ([#54](https://github.com/sebdanielsson/piecemeal/issues/54)) ([783ab3a](https://github.com/sebdanielsson/piecemeal/commit/783ab3a4fb9f465b6c80e54ec409e2e2bad7137b)), closes [#41](https://github.com/sebdanielsson/piecemeal/issues/41)
* fetch, parse and save metadata before adding a torrent ([#55](https://github.com/sebdanielsson/piecemeal/issues/55)) ([fba9f3d](https://github.com/sebdanielsson/piecemeal/commit/fba9f3d0b1da271cb6ceed33d642ded98a97b04c))
* fill the remaining gaps in the qBittorrent API ([#52](https://github.com/sebdanielsson/piecemeal/issues/52)) ([0148fd7](https://github.com/sebdanielsson/piecemeal/commit/0148fd7369ab116aee6d1349ae5d159bba9d6893))
* incomplete folder and automatic torrent management ([#59](https://github.com/sebdanielsson/piecemeal/issues/59)) ([c78bba1](https://github.com/sebdanielsson/piecemeal/commit/c78bba131d0b404059f3cf2c8c58465156ce7b90))
* share limit actions and modes per torrent and category ([#56](https://github.com/sebdanielsson/piecemeal/issues/56)) ([e8308ae](https://github.com/sebdanielsson/piecemeal/commit/e8308aeedde276729bb0b45f4707a6999cd12ac5))


### Bug Fixes

* **deps:** update module github.com/danielgtaylor/huma/v2 to v2.41.0 ([#60](https://github.com/sebdanielsson/piecemeal/issues/60)) ([aa7b60d](https://github.com/sebdanielsson/piecemeal/commit/aa7b60de60c4bbabab39692ced08bf607619d277))
* drop per-torrent speed limits, which were never applied ([#49](https://github.com/sebdanielsson/piecemeal/issues/49)) ([6412135](https://github.com/sebdanielsson/piecemeal/commit/64121351adf7fdd6eabf4afa4582234558a1e462))
* **engine:** dial peers added while the download limit is used up ([#51](https://github.com/sebdanielsson/piecemeal/issues/51)) ([f81f70c](https://github.com/sebdanielsson/piecemeal/commit/f81f70c7d964aded406922393eb161ccdc734f02)), closes [#37](https://github.com/sebdanielsson/piecemeal/issues/37)
* refuse magnet URIs without an info hash instead of panicking ([#53](https://github.com/sebdanielsson/piecemeal/issues/53)) ([684e9ba](https://github.com/sebdanielsson/piecemeal/commit/684e9ba783d8ff661d32103898ae26c304407140))

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
