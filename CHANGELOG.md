# Changelog

## v0.1.0 (2026-10-24)

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
