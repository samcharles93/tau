# Changelog

## [0.7.0](https://git.catlow.cloud/sam/tau/compare/v0.6.0...v0.7.0) (2026-06-29)


### Features

* enhance release workflow with release-please job and dependencies ([8fc4aac](https://git.catlow.cloud/sam/tau/commit/8fc4aac344bb595bae8cf861df58e260b4ec65ab))


### Bug Fixes

* ensure correct checkout reference in release workflow ([84c8592](https://git.catlow.cloud/sam/tau/commit/84c859257ba704537ec496aa2aafbdc98782c4b4))
* remove tag trigger from release workflow ([7ec05a3](https://git.catlow.cloud/sam/tau/commit/7ec05a383ab7bdb0fe29ba144f1bbecd3217d16c))

## [0.6.0](https://git.catlow.cloud/sam/tau/compare/v0.5.6...v0.6.0) (2026-06-29)


### Features

* track per-message timestamps in chat sessions ([a8db286](https://git.catlow.cloud/sam/tau/commit/a8db2862f953bc84974741c74b4aff7dbd0f1f9e))


### Bug Fixes

* ignore .pnpm-store to prevent goreleaser dirty state ([8905613](https://git.catlow.cloud/sam/tau/commit/890561395bbdb7c9de33704fa1cc2f80ad297dbb))
* test complete pipeline after label cleanup fix ([1eb948d](https://git.catlow.cloud/sam/tau/commit/1eb948d7302391f53233de44c99d30a3089293b5))

## 0.5.6 (2026-06-29)

First automated release. Established the release pipeline
(release-please + GoReleaser on Gitea). No functional changes
to tau across 0.5.2–0.5.6 — these versions were CI bring-up.
