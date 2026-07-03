# Changelog

## [0.13.0](https://github.com/samcharles93/tau/compare/v0.12.0...v0.13.0) (2026-07-03)


### Features

* default reasoning effort to auto ([7898de8](https://github.com/samcharles93/tau/commit/7898de8d6002433054ffd8121fcbdf82a9ecd998))

## [0.12.0](https://github.com/samcharles93/tau/compare/v0.11.0...v0.12.0) (2026-07-03)


### Features

* **tool-stats:** join metrics.jsonl for ground-truth error rates and durations ([a16e764](https://github.com/samcharles93/tau/commit/a16e764ee185ba2d3ea29624f71e3c0cb85d6004))

## [0.11.0](https://github.com/samcharles93/tau/compare/v0.10.1...v0.11.0) (2026-07-03)


### Features

* add tool-stats script for analysing session tool usage ([d57b93e](https://github.com/samcharles93/tau/commit/d57b93efb5f80ac538ee5201b787f313e066e527))
* **agent:** adopt tool-design lessons from Pi session analysis ([1fd6958](https://github.com/samcharles93/tau/commit/1fd69588345fec39a38b10733a01aad2bf120af3))
* **agent:** self-documenting tool limits and tolerant edit parsing ([e82d2a5](https://github.com/samcharles93/tau/commit/e82d2a5d34cf3ade502ab76ec926bbc6f394cb60))
* fix reasoning effort defaults, budget fallback, and provider-agnostic mapping ([31ab330](https://github.com/samcharles93/tau/commit/31ab3308919d95b28aaf24876854c458b4f307e4))
* **metrics:** add LLM latency, skill/extension, model/provider switch, and error metrics ([48d26fa](https://github.com/samcharles93/tau/commit/48d26fa6c1281b2e19a6b340241579076d3add3e))
* **metrics:** add metric fields to SessionSummary, extend UsageTracker, update store schema ([1e7d4ac](https://github.com/samcharles93/tau/commit/1e7d4ac6cfc79f29808f9ba4ec4b2c401e65f989))
* **metrics:** comprehensive observability system — metric events, JSONL export, TUI stats ([46cb3cd](https://github.com/samcharles93/tau/commit/46cb3cd1c008f75cf7d05d501da0bb27f4cd4d8d))
* **web:** add request cancellation support to web UI ([6b6b884](https://github.com/samcharles93/tau/commit/6b6b884222c8658e30ef2c8288a12cf6980ba419))
* **web:** add subcommand completions and cost tracker, improve TUI separators ([cc63a96](https://github.com/samcharles93/tau/commit/cc63a96851da9bfc521f1aff763a13d58f47ce7c))


### Bug Fixes

* **metrics:** address adversarial review findings ([05708b2](https://github.com/samcharles93/tau/commit/05708b2ceb635b0cef594a13805d15018e7785e3))
* **metrics:** fix headless exit summary race with shared bus client ([aa7fe46](https://github.com/samcharles93/tau/commit/aa7fe46d827343d05c5d624519aea210ac4f4736))
* **metrics:** fix headless exit summary race, rename turn.duration, and address review gaps ([dabb081](https://github.com/samcharles93/tau/commit/dabb0814c1bca59b9213069be7b69098b928a4c8))
* **tui:** submit slash commands immediately when a terminal completion is accepted ([f7779d8](https://github.com/samcharles93/tau/commit/f7779d82eeacee9b2374b5f17b2fc0207d51e189))

## [0.10.1](https://github.com/samcharles93/tau/compare/v0.10.0...v0.10.1) (2026-07-03)


### Bug Fixes

* **taui:** fix dead bracketed-paste path and add OverlayStack ([#25](https://github.com/samcharles93/tau/issues/25)) ([d06e4ec](https://github.com/samcharles93/tau/commit/d06e4ecf36824afe5234978ecefa61b0812cd5b8))

## [0.10.0](https://github.com/samcharles93/tau/compare/v0.9.1...v0.10.0) (2026-07-03)


### Features

* add plugin-rendered panels and views ([a7bdab4](https://github.com/samcharles93/tau/commit/a7bdab4bf6e47f497de2d841b17da91c0247a482))
* add plugin-rendered panels and views ([d287fbc](https://github.com/samcharles93/tau/commit/d287fbc9ea90f4a27558fa63f3390c406f89b619))

## [0.9.1](https://github.com/samcharles93/tau/compare/v0.9.0...v0.9.1) (2026-07-03)


### Bug Fixes

* respect selected index when choosing completion ([f8eab81](https://github.com/samcharles93/tau/commit/f8eab812491ba79aad8503a268ab89d8266b379b))

## [0.9.0](https://github.com/samcharles93/tau/compare/v0.8.0...v0.9.0) (2026-07-02)


### Features

* add interactive prompt support for plugins/tools ([c41a4da](https://github.com/samcharles93/tau/commit/c41a4da79d5b5d4583a4984a857720f3dae88df0))
* add setup task and remove build targets ([3729032](https://github.com/samcharles93/tau/commit/37290325354dfe9ebf5146cf04a9843ae37410f1))

## [0.8.0](https://github.com/samcharles93/tau/compare/v0.7.0...v0.8.0) (2026-07-02)


### Features

* add /skills-reload and /skills list commands ([#14](https://github.com/samcharles93/tau/issues/14), [#15](https://github.com/samcharles93/tau/issues/15)) ([4df6350](https://github.com/samcharles93/tau/commit/4df63500a2296643aad38454cef4b58d83b96a8e))
* add live token/cost tracking to status bar ([d141ce5](https://github.com/samcharles93/tau/commit/d141ce5fb933a40cd98cbc45fe3fa58db3a6e06b))
* add metrics framework, tool call sanitization, and command registry ([722ef13](https://github.com/samcharles93/tau/commit/722ef139c90220157755bd313973e5dfe5577c99))
* implement slash command handling in chat input ([9a130eb](https://github.com/samcharles93/tau/commit/9a130eb90e3ca44b3442932ac4aca3e9621a9bfd))
* implement slash commands for model, session, and extensions ([7607305](https://github.com/samcharles93/tau/commit/76073058fafb7f35ae7873b001c1457a8a87d9fb))
* **tools:** add glob tool, read-before-write safety, per-tool timeouts, and various improvements ([7cda497](https://github.com/samcharles93/tau/commit/7cda49784cb7ded6a3aed614010b7a39352bf7f4))
* **tui:** add /skills list command ([f8aa372](https://github.com/samcharles93/tau/commit/f8aa3729489d8899eef523ccf6f35d43d47dab1f))


### Bug Fixes

* address Windows compatibility issues ([2335d4e](https://github.com/samcharles93/tau/commit/2335d4ee81077cfd7c791320643d62b4f405a322))
* inject commit hash and build date in version string ([8df8128](https://github.com/samcharles93/tau/commit/8df8128819bd0fa2022f3404bb2c0180f36903e7))
* migrate release workflow from Gitea to GitHub ([f45ebb3](https://github.com/samcharles93/tau/commit/f45ebb32371504413b4b90956c06c4651a975381))
* normalize fallback output paths with filepath.ToSlash for Windows compatibility ([25832a8](https://github.com/samcharles93/tau/commit/25832a87be3114c6774b8f55b7241dcd46ca8329))
* **skill:** validate Instructions length in skill validation ([0483bc1](https://github.com/samcharles93/tau/commit/0483bc1d7e28f3ccf9f12b61e87b85e71cccd023))
* **skill:** validate Instructions length in skill validation ([044bec7](https://github.com/samcharles93/tau/commit/044bec7e9349184023fadd36479dcb2838047aa2))
* skip os.Chmod on Windows where it's a no-op ([#9](https://github.com/samcharles93/tau/issues/9)) ([c653b25](https://github.com/samcharles93/tau/commit/c653b25510ee6627bdc9da741f03b5fa78100215))
* skip Unix permission check in provider state on Windows ([ede7233](https://github.com/samcharles93/tau/commit/ede7233086b631d91f1709254e36e671820d7069))
* **tools:** add pure-Go fallback for find and grep on Windows ([#6](https://github.com/samcharles93/tau/issues/6), [#7](https://github.com/samcharles93/tau/issues/7)) ([0735ca6](https://github.com/samcharles93/tau/commit/0735ca607c530a8f209f897d2ddb0ffd04df8e05))
* **tools:** pure-Go fallbacks for find and grep on Windows ([#6](https://github.com/samcharles93/tau/issues/6), [#7](https://github.com/samcharles93/tau/issues/7)) ([19f0d66](https://github.com/samcharles93/tau/commit/19f0d66c9a85db76638d468bf77def37114ea89b))
* **tui:** remove dead duplicate skills-reload code from inline_chat.go ([9d04a91](https://github.com/samcharles93/tau/commit/9d04a915dff95e0739711785ef88d8803f693d0b))
* **tui:** resolve double-unlock and syntax errors in inline_events.go ([e7d5b52](https://github.com/samcharles93/tau/commit/e7d5b520661145d3771e2a6e6cfd7ee32347ff12))
* **tui:** resolve mutex deadlock and fatal double-unlock in handleEvent ([c51aa0d](https://github.com/samcharles93/tau/commit/c51aa0d49e0d0202e1179f37cfac683eabfe6e95))
* unify AllowedTools parser, normalize skill file paths, validate instructions length ([#12](https://github.com/samcharles93/tau/issues/12), [#13](https://github.com/samcharles93/tau/issues/13), [#16](https://github.com/samcharles93/tau/issues/16)) ([ec97f04](https://github.com/samcharles93/tau/commit/ec97f04af930767946fd0df27ff348af8d651970))
* use os.UserConfigDir() for cross-platform config directory ([#8](https://github.com/samcharles93/tau/issues/8)) ([b5e6a8b](https://github.com/samcharles93/tau/commit/b5e6a8b3c2e27d1de071842b44cf3c8864d68fa9))
* write startup diagnostics to file instead of stderr ([8482b2d](https://github.com/samcharles93/tau/commit/8482b2d8a3cf94d4efb602121896dec4217cca8b))

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
