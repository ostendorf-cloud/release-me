# Changelog

## [v0.4.0](https://github.com/ostendorf-cloud/release-me/compare/v0.3.0...v0.4.0) (2026-11-07)

### Features

- **changelog:** render GitHub's generated release notes with changelog-type github ([#113](https://github.com/ostendorf-cloud/release-me/pull/113))
- **workspace:** release cargo workspace dependents with their dependency ([#109](https://github.com/ostendorf-cloud/release-me/pull/109))
- **updater:** add a plain updater for version.txt ([#92](https://github.com/ostendorf-cloud/release-me/pull/92))
- **pr:** configurable release pull request title patterns ([#90](https://github.com/ostendorf-cloud/release-me/pull/90))
- **changelog:** sentence case entries with changelog-sentence-case ([#91](https://github.com/ostendorf-cloud/release-me/pull/91))
- **components:** allow a root component beside other components ([#89](https://github.com/ostendorf-cloud/release-me/pull/89))
- **updater:** add an xml updater for pom.xml and .csproj ([#87](https://github.com/ostendorf-cloud/release-me/pull/87))
- **updater:** add npm-lock, cargo-lock and gemfile-lock updaters ([#84](https://github.com/ostendorf-cloud/release-me/pull/84))
- **config:** several labels per role and extra labels ([#104](https://github.com/ostendorf-cloud/release-me/pull/104))
- **updater:** add a generic updater for release-please's version markers ([#102](https://github.com/ostendorf-cloud/release-me/pull/102))
- **version:** add bump-patch-for-minor-pre-major ([#81](https://github.com/ostendorf-cloud/release-me/pull/81))
- **releasepr:** turn the release pull request guide off ([#80](https://github.com/ostendorf-cloud/release-me/pull/80))
- **releasepr:** a configurable layout for a release pull request with several components ([#77](https://github.com/ostendorf-cloud/release-me/pull/77))

## [v0.3.0](https://github.com/ostendorf-cloud/release-me/compare/v0.2.1...v0.3.0) (2026-10-28)

### Features

- **action:** per-component outputs for released, version, tag and the release pull request ([#67](https://github.com/ostendorf-cloud/release-me/pull/67))
- **pr:** one shared release pull request for linked and independent ([#66](https://github.com/ostendorf-cloud/release-me/pull/66))
- **changelog:** link the version heading to the compare view ([#65](https://github.com/ostendorf-cloud/release-me/pull/65))
- **history:** an empty commit or a Release-Component footer reaches a component ([#64](https://github.com/ostendorf-cloud/release-me/pull/64))
- **pr:** a Release-As commit footer forces the version ([#63](https://github.com/ostendorf-cloud/release-me/pull/63))
- **config:** turn a pre-release type off with false ([#62](https://github.com/ostendorf-cloud/release-me/pull/62))

## v0.2.1 (2026-10-27)

### Bug Fixes

- **ci:** git worktree remove does not take --quiet ([#60](https://github.com/ostendorf-cloud/release-me/pull/60))

## v0.2.0 (2026-10-27)

### Features

- **pr:** a comment command forces the version or sets release notes ([#56](https://github.com/ostendorf-cloud/release-me/pull/56))
- **skills:** add the migrate-release-please-to-release-me skill ([#36](https://github.com/ostendorf-cloud/release-me/pull/36))
- **skills:** add the setup-release-me skill ([#35](https://github.com/ostendorf-cloud/release-me/pull/35))
- **cli:** add a migrate command for release-please ([#34](https://github.com/ostendorf-cloud/release-me/pull/34))
- **config:** turn a pre-release type off with an empty label ([#33](https://github.com/ostendorf-cloud/release-me/pull/33))
- **config:** add a changelog path per component ([#32](https://github.com/ostendorf-cloud/release-me/pull/32))
- **config:** add a tag-prefix per component ([#30](https://github.com/ostendorf-cloud/release-me/pull/30))
- **ci:** rerun the action when a release pull request is labeled ([#27](https://github.com/ostendorf-cloud/release-me/pull/27))
- **version:** versioning modes for components, independent, locked or linked ([#25](https://github.com/ostendorf-cloud/release-me/pull/25))
- **changelog:** configurable source, first-parent, squash, all or nested ([#24](https://github.com/ostendorf-cloud/release-me/pull/24))
- **releasepr:** explain how the release works in the pull request body ([#23](https://github.com/ostendorf-cloud/release-me/pull/23))
- **action:** attribute the release commit to the app bot account ([#18](https://github.com/ostendorf-cloud/release-me/pull/18))
- composite action, image, binaries and self release ([#14](https://github.com/ostendorf-cloud/release-me/pull/14))
- typed updaters and the moving major tag ([#13](https://github.com/ostendorf-cloud/release-me/pull/13))
- pre-releases through labels on the release pull request ([#12](https://github.com/ostendorf-cloud/release-me/pull/12))
- components with their own tags and release pull requests ([#11](https://github.com/ostendorf-cloud/release-me/pull/11))
- release pull request, tag and release for one component ([#9](https://github.com/ostendorf-cloud/release-me/pull/9))
- next and changelog from the first parent history ([#8](https://github.com/ostendorf-cloud/release-me/pull/8))
- **cli:** command skeleton with usage and version

### Bug Fixes

- **comment:** a release-as command updates the release pull request immediately ([#58](https://github.com/ostendorf-cloud/release-me/pull/58))
- **release:** create every release as a draft until assets attach ([#51](https://github.com/ostendorf-cloud/release-me/pull/51))
- **action:** resolve version from VERSION file for a digest pin ([#55](https://github.com/ostendorf-cloud/release-me/pull/55))
- **forge:** stop reporting a permanently failed release as a success ([#50](https://github.com/ostendorf-cloud/release-me/pull/50))
- **run:** recognize a merged, not yet tagged release pull request ([#46](https://github.com/ostendorf-cloud/release-me/pull/46))
- **release:** preserve a pre-release tag's flag through goreleaser ([#42](https://github.com/ostendorf-cloud/release-me/pull/42))
- **forge:** a merged release pull request is recognised again ([#40](https://github.com/ostendorf-cloud/release-me/pull/40))
- **pr:** a pre-release label change pushes the release branch ([#31](https://github.com/ostendorf-cloud/release-me/pull/31))
- **pr:** commit the release branch with an explicit git identity ([#16](https://github.com/ostendorf-cloud/release-me/pull/16))

## v0.1.1 (2026-10-23)

### Bug Fixes

- **run:** recognize a merged, not yet tagged release pull request ([#46](https://github.com/ostendorf-cloud/release-me/pull/46))

## v0.1.0 (2026-10-23)

### Features

- **skills:** add the migrate-release-please-to-release-me skill ([#36](https://github.com/ostendorf-cloud/release-me/pull/36))
- **skills:** add the setup-release-me skill ([#35](https://github.com/ostendorf-cloud/release-me/pull/35))
- **cli:** add a migrate command for release-please ([#34](https://github.com/ostendorf-cloud/release-me/pull/34))
- **config:** turn a pre-release type off with an empty label ([#33](https://github.com/ostendorf-cloud/release-me/pull/33))
- **config:** add a changelog path per component ([#32](https://github.com/ostendorf-cloud/release-me/pull/32))
- **config:** add a tag-prefix per component ([#30](https://github.com/ostendorf-cloud/release-me/pull/30))
- **ci:** rerun the action when a release pull request is labeled ([#27](https://github.com/ostendorf-cloud/release-me/pull/27))
- **version:** versioning modes for components, independent, locked or linked ([#25](https://github.com/ostendorf-cloud/release-me/pull/25))
- **changelog:** configurable source, first-parent, squash, all or nested ([#24](https://github.com/ostendorf-cloud/release-me/pull/24))
- **releasepr:** explain how the release works in the pull request body ([#23](https://github.com/ostendorf-cloud/release-me/pull/23))
- **action:** attribute the release commit to the app bot account ([#18](https://github.com/ostendorf-cloud/release-me/pull/18))
- composite action, image, binaries and self release ([#14](https://github.com/ostendorf-cloud/release-me/pull/14))
- typed updaters and the moving major tag ([#13](https://github.com/ostendorf-cloud/release-me/pull/13))
- pre-releases through labels on the release pull request ([#12](https://github.com/ostendorf-cloud/release-me/pull/12))
- components with their own tags and release pull requests ([#11](https://github.com/ostendorf-cloud/release-me/pull/11))
- release pull request, tag and release for one component ([#9](https://github.com/ostendorf-cloud/release-me/pull/9))
- next and changelog from the first parent history ([#8](https://github.com/ostendorf-cloud/release-me/pull/8))
- **cli:** command skeleton with usage and version

### Bug Fixes

- **release:** preserve a pre-release tag's flag through goreleaser ([#42](https://github.com/ostendorf-cloud/release-me/pull/42))
- **forge:** a merged release pull request is recognised again ([#40](https://github.com/ostendorf-cloud/release-me/pull/40))
- **pr:** a pre-release label change pushes the release branch ([#31](https://github.com/ostendorf-cloud/release-me/pull/31))
- **pr:** commit the release branch with an explicit git identity ([#16](https://github.com/ostendorf-cloud/release-me/pull/16))

## v0.1.0-rc.1 (2026-10-23)

### Features

- **config:** add a tag-prefix per component ([#30](https://github.com/ostendorf-cloud/release-me/pull/30))
- **ci:** rerun the action when a release pull request is labeled ([#27](https://github.com/ostendorf-cloud/release-me/pull/27))
- **version:** versioning modes for components, independent, locked or linked ([#25](https://github.com/ostendorf-cloud/release-me/pull/25))
- **changelog:** configurable source, first-parent, squash, all or nested ([#24](https://github.com/ostendorf-cloud/release-me/pull/24))
- **releasepr:** explain how the release works in the pull request body ([#23](https://github.com/ostendorf-cloud/release-me/pull/23))
- **action:** attribute the release commit to the app bot account ([#18](https://github.com/ostendorf-cloud/release-me/pull/18))
- composite action, image, binaries and self release ([#14](https://github.com/ostendorf-cloud/release-me/pull/14))
- typed updaters and the moving major tag ([#13](https://github.com/ostendorf-cloud/release-me/pull/13))
- pre-releases through labels on the release pull request ([#12](https://github.com/ostendorf-cloud/release-me/pull/12))
- components with their own tags and release pull requests ([#11](https://github.com/ostendorf-cloud/release-me/pull/11))
- release pull request, tag and release for one component ([#9](https://github.com/ostendorf-cloud/release-me/pull/9))
- next and changelog from the first parent history ([#8](https://github.com/ostendorf-cloud/release-me/pull/8))
- **cli:** command skeleton with usage and version

### Bug Fixes

- **pr:** a pre-release label change pushes the release branch ([#31](https://github.com/ostendorf-cloud/release-me/pull/31))
- **pr:** commit the release branch with an explicit git identity ([#16](https://github.com/ostendorf-cloud/release-me/pull/16))
