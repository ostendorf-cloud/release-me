# Changelog

## [v0.5.1](https://github.com/ostendorf-cloud/release-me/compare/v0.5.0...v0.5.1) (2026-11-13)

### Bug Fixes

- **scripts:** give publish-release-assets' mktemp -d a template ([#151](https://github.com/ostendorf-cloud/release-me/pull/151))
- **action:** give mktemp -d an explicit template ([#150](https://github.com/ostendorf-cloud/release-me/pull/150))
- **renovate:** pinDigests is a config key, not a preset ([#147](https://github.com/ostendorf-cloud/release-me/pull/147))
- **releasepr:** tolerate a CRLF pull request body ([#144](https://github.com/ostendorf-cloud/release-me/pull/144))

## [v0.5.0](https://github.com/ostendorf-cloud/release-me/compare/v0.4.0...v0.5.0) (2026-11-12)

### Breaking Changes

- **pr:** create the release commit through the forge so it is signed ([#78](https://github.com/ostendorf-cloud/release-me/pull/78))

### Features

- **labels:** create a missing label instead of failing ([#139](https://github.com/ostendorf-cloud/release-me/pull/139))
- **ci:** enforce the Go license header with addlicense ([#136](https://github.com/ostendorf-cloud/release-me/pull/136))
- **ci:** enforce the DCO sign-off of every commit in a pull request ([#132](https://github.com/ostendorf-cloud/release-me/pull/132))

### Bug Fixes

- **release:** reject or correct the dangerous release config combinations ([#122](https://github.com/ostendorf-cloud/release-me/pull/122))
- **history:** keep empty commits out of a component older than them ([#123](https://github.com/ostendorf-cloud/release-me/pull/123))

## [v0.4.0](https://github.com/ostendorf-cloud/release-me/compare/v0.3.0...v0.4.0) (2026-11-10)

### Breaking Changes

- **release:** delete the release branch once its pull request is released ([#79](https://github.com/ostendorf-cloud/release-me/pull/79))

### Features

- **updater:** bump the version files of the terraform-module, expo and krm-blueprint release types ([#117](https://github.com/ostendorf-cloud/release-me/pull/117))
- **version:** add release-please's versioning strategies ([#116](https://github.com/ostendorf-cloud/release-me/pull/116))
- **release:** create the GitHub release as a draft under draft ([#88](https://github.com/ostendorf-cloud/release-me/pull/88))
- **pr:** a header and a footer for every release pull request body ([#93](https://github.com/ostendorf-cloud/release-me/pull/93))
- **changelog:** link commits and closed issues in changelog entries ([#101](https://github.com/ostendorf-cloud/release-me/pull/101))
- **release:** add include-v-in-release-name for the release name ([#99](https://github.com/ostendorf-cloud/release-me/pull/99))
- **pr:** open release pull requests as drafts with release-pull-request-draft ([#96](https://github.com/ostendorf-cloud/release-me/pull/96))
- **release:** add force-tag-creation to leave the tag to the release publish ([#95](https://github.com/ostendorf-cloud/release-me/pull/95))
- **updater:** add updaters for release-please's release type version files ([#98](https://github.com/ostendorf-cloud/release-me/pull/98))
- **action:** add the release fields release-please-action outputs to each component ([#108](https://github.com/ostendorf-cloud/release-me/pull/108))
- **changelog:** rename, reorder and add sections through changelog-sections ([#86](https://github.com/ostendorf-cloud/release-me/pull/86))
- **config:** add the release-as key ([#97](https://github.com/ostendorf-cloud/release-me/pull/97))
- **pr:** leave the changelog file alone with skip-changelog ([#112](https://github.com/ostendorf-cloud/release-me/pull/112))
- **changelog:** configure the heading date with changelog-date-format ([#110](https://github.com/ostendorf-cloud/release-me/pull/110))
- **components:** leave subpaths out of a component with exclude-paths ([#82](https://github.com/ostendorf-cloud/release-me/pull/82))
- **workspace:** release maven workspace dependents with their dependency ([#111](https://github.com/ostendorf-cloud/release-me/pull/111))
- **config:** add free pre-release types with their own labels ([#103](https://github.com/ostendorf-cloud/release-me/pull/103))
- **release:** tag without a GitHub release under skip-github-release ([#83](https://github.com/ostendorf-cloud/release-me/pull/83))
- **pr:** sign the release commit off under signoff ([#107](https://github.com/ostendorf-cloud/release-me/pull/107))
- **workspace:** carry release-please's always-link-local over ([#106](https://github.com/ostendorf-cloud/release-me/pull/106))
- **changelog:** point changelog links at changelog-host ([#105](https://github.com/ostendorf-cloud/release-me/pull/105))
- **changelog:** credit entry authors with changelog-authors ([#94](https://github.com/ostendorf-cloud/release-me/pull/94))
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
