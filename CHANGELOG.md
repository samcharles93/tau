# Changelog

## [0.5.6](https://git.catlow.cloud/sam/tau/compare/v0.5.5...v0.5.6) (2026-06-29)


### Bug Fixes

* re-trigger with label pre-creation fix ([96c753b](https://git.catlow.cloud/sam/tau/commit/96c753bc31906c2870027fdeab68ada9fe8e9003))
* test goreleaser dirty state fix ([d837027](https://git.catlow.cloud/sam/tau/commit/d837027de97b254f7a9c31e5520c6d91ab84045e))

## [0.5.5](https://git.catlow.cloud/sam/tau/compare/v0.5.4...v0.5.5) (2026-06-29)


### Bug Fixes

* re-trigger release pipeline with merge detection fix ([4c0dbc1](https://git.catlow.cloud/sam/tau/commit/4c0dbc179f1121a96fdf77e3665a671d87b1aec3))

## [0.5.4](https://git.catlow.cloud/sam/tau/compare/v0.5.3...v0.5.4) (2026-06-29)


### Bug Fixes

* test automated release pipeline after v0.5.3 baseline ([884dfbf](https://git.catlow.cloud/sam/tau/commit/884dfbf27af3df74c7caa805b15cd15af8a9286d))

## [0.5.3](https://git.catlow.cloud/sam/tau/compare/v0.5.2...v0.5.3) (2026-06-29)


### Bug Fixes

* **ci:** add GITHUB_API_URL env for octokit REST base URL ([ee01404](https://git.catlow.cloud/sam/tau/commit/ee014042e01c9409b13675f3df0dce6bea2fe235))
* **ci:** add repo-url and github-api-url for gitea backend ([b9ddd0e](https://git.catlow.cloud/sam/tau/commit/b9ddd0e31270236a22869156f3b4d56263ffc998))
* **ci:** bump release-please fork to v5.1.0, add actions badge to README ([18d3909](https://git.catlow.cloud/sam/tau/commit/18d390988d550d57b94bb64d6da3b6201a0554d8))
* **ci:** configure git auth for release-please internal clone ([4777e76](https://git.catlow.cloud/sam/tau/commit/4777e764bf3aafd2a803059517822299003d288f))
* **ci:** re-trigger release-please with v5 fork ([72dce72](https://git.catlow.cloud/sam/tau/commit/72dce72f5843bc7e0f20ca440a835af2dc665d4c))
* **ci:** re-trigger with fork push fix ([44c820e](https://git.catlow.cloud/sam/tau/commit/44c820e03930cb94f00cd2c82480236f59e8ac5e))
* **ci:** retrigger with updated v5 fork including REST API fix ([b150683](https://git.catlow.cloud/sam/tau/commit/b150683d57cf5a4f6c45fa78ca6c734e503ac2d2))
* **ci:** retry with v5 tag on release-please-action fork ([d4aacce](https://git.catlow.cloud/sam/tau/commit/d4aacceb25cc66f190b04d21a7493c6e8d25021f))
* **ci:** use .netrc for universal git basic auth ([7467199](https://git.catlow.cloud/sam/tau/commit/7467199dcdd7d9c94d51aefb326df2c40ebb81d9))
* **ci:** use canonical GITEA_TOKEN secret in both workflows ([35c3a72](https://git.catlow.cloud/sam/tau/commit/35c3a726da947e29d3abbff6226240f7313457fa))
* **ci:** use extraheader git auth for release-please ([1c2e367](https://git.catlow.cloud/sam/tau/commit/1c2e367410934777c0bf0fae69925ee75d620d66))
* **ci:** use git credential store for clone auth ([388eb66](https://git.catlow.cloud/sam/tau/commit/388eb667d1f9c6e92715c8f64ff870048efe4c04))
* **ci:** use GITHUB_TOKEN secret name, pass both to goreleaser ([6c63f15](https://git.catlow.cloud/sam/tau/commit/6c63f15954cfe8d091e8c9cd2abeb4a769500846))
* **ci:** use RELEASE_TOKEN secret for release workflows ([de0d631](https://git.catlow.cloud/sam/tau/commit/de0d631429ce16d5934b34bb5880f0c1a06222a0))
* **ci:** use sam username for git basic auth insteadOf ([70860dd](https://git.catlow.cloud/sam/tau/commit/70860dd82ef56d775dad846d5c3245f289e5cc5a))
* **ci:** wire in custom gitea release-please fork at v5 ([7b6cafe](https://git.catlow.cloud/sam/tau/commit/7b6cafe18744e99093315052b5cbf3a3bed86674))
* force goreleaser to use GITEA_TOKEN for gitea releases ([e735390](https://git.catlow.cloud/sam/tau/commit/e735390ca9f78e049adeeade90088112e0b85836))

## [0.5.2](https://github.com/samcharles93/tau/compare/v0.5.1...v0.5.2) (2026-06-29)


### Bug Fixes

* **ci:** use native release-please action for GitHub ([bf4ccf6](https://github.com/samcharles93/tau/commit/bf4ccf65156b5908dbc3e9cd3e4c77970f403ca4))
* **ci:** use release-please manifest config and enable PR creation ([8be85be](https://github.com/samcharles93/tau/commit/8be85bea06002d243e3cd2226541a5705c21c76e))
* **ci:** use simple strategy with version-file to bump VERSION ([86a898e](https://github.com/samcharles93/tau/commit/86a898e9deb8450cdbdcdb8b3fdfe9f7b81ffc8e))
