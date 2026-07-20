# Changelog

## [0.29.0](https://github.com/samcharles93/tau/compare/v0.28.0...v0.29.0) (2026-07-20)


### Features

* add independent command palette and model picker overlays ([9eb2027](https://github.com/samcharles93/tau/commit/9eb2027fd793282a348d25523a2ded90afde56b6))

## [0.28.0](https://github.com/samcharles93/tau/compare/v0.27.0...v0.28.0) (2026-07-20)


### Features

* cache expanded tool box body to avoid re-rendering on each frame ([d5500a8](https://github.com/samcharles93/tau/commit/d5500a8e2a3f7db3b66422d4158e42327ce686d6))

## [0.27.0](https://github.com/samcharles93/tau/compare/v0.26.0...v0.27.0) (2026-07-20)


### Features

* state taxonomy spec, plugin proto restructure, and webui child-agent bug fixes ([#70](https://github.com/samcharles93/tau/issues/70)) ([d250cc5](https://github.com/samcharles93/tau/commit/d250cc51dbaae6b0e420246484922fcd87a011ba))

## [0.26.0](https://github.com/samcharles93/tau/compare/v0.25.0...v0.26.0) (2026-07-19)


### Features

* **tui2:** formalize state taxonomy for turn/overlay/child-agent state ([b95e5d3](https://github.com/samcharles93/tau/commit/b95e5d3794320156bc9add8edfc9ee6b6233a306))


### Bug Fixes

* **tui2:** remove dead completionsOverlay adapter from overlay registry ([382c05c](https://github.com/samcharles93/tau/commit/382c05c7c09c74ea7504ec76ba87fa4b16df8837))

## [0.25.0](https://github.com/samcharles93/tau/compare/v0.24.0...v0.25.0) (2026-07-19)


### Features

* **app:** validate provider API key with a live call before persisting ([4519433](https://github.com/samcharles93/tau/commit/45194335509a7105210d8a29f1e894de09a2a9e5))
* **tui:** add copy command for messages and transcripts (CAT-170) ([ba6c559](https://github.com/samcharles93/tau/commit/ba6c55959c1099bab5671671950ea62a73919d5c))
* **tui:** add copy command for messages and transcripts (CAT-170) ([7d840cb](https://github.com/samcharles93/tau/commit/7d840cb8f0990036de8433939f99b8cf88d88830))


### Bug Fixes

* **app:** honor --insecure, trim keys, and treat 400 as rejected in validation ([55fcb89](https://github.com/samcharles93/tau/commit/55fcb897fa90c4f05f125b0ef85f87188e9e3b3f))
* **app:** honor --insecure, trim keys, and treat 400 as rejected in validation ([9ff14af](https://github.com/samcharles93/tau/commit/9ff14af0034c566b566486400890c1b5fecfa97f))
* **app:** probe OpenRouter's /auth/key, not /models, for live validation ([1b04641](https://github.com/samcharles93/tau/commit/1b046412643e49c0f1aefbd38a1786ad8766ccb1))
* **ci:** verify release archive hashes, avoid unquoted tag interpolation ([fc3c688](https://github.com/samcharles93/tau/commit/fc3c688eb219e928e2a76b534531ecbcb429f09e))
* **installer:** add prerequisite, archive, and temporary-binary validation ([b8007a8](https://github.com/samcharles93/tau/commit/b8007a82751d63c560012c9a504c1ef61a70165f))
* **installer:** bound the --version sanity check with a timeout ([a9cbeeb](https://github.com/samcharles93/tau/commit/a9cbeeb1f506f6e51c327021438160f2782cd852))
* **installer:** bound the --version sanity check with a timeout ([16ef271](https://github.com/samcharles93/tau/commit/16ef271aefc983c807971f62fc9a0d75e7d10eb6))
* **installer:** reject symlinked archive entries, use unpredictable temp path ([4b20ae9](https://github.com/samcharles93/tau/commit/4b20ae98e1bf8b8e3b4521453a0fd43806b90090))

## [0.24.0](https://github.com/samcharles93/tau/compare/v0.23.0...v0.24.0) (2026-07-17)


### Features

* **tui,webui,storage:** session tree with agent attribution and lineage (CAT-67) ([b41703f](https://github.com/samcharles93/tau/commit/b41703f7ddaf4b0bfe2c41b1040c4f1bcb5a9c58))
* **tui,webui,storage:** session tree with agent attribution and lineage (CAT-67) ([#53](https://github.com/samcharles93/tau/issues/53)) ([51be579](https://github.com/samcharles93/tau/commit/51be5790440a7c54c02584c793d8812ad0ab69fe))
* **tui:** live child drill-down + open child session (CAT-107) ([801071e](https://github.com/samcharles93/tau/commit/801071eb4f2049f25ed368c1abe33e322903eb7b))
* **tui:** live child drill-down + open child session (CAT-107) ([f0282c4](https://github.com/samcharles93/tau/commit/f0282c4f40b2ce889673c13e9b395af2ae64e26f))


### Bug Fixes

* consolidate ID helpers, fix child session ID collision, add session CRUD test coverage (CAT-169) ([eac61a2](https://github.com/samcharles93/tau/commit/eac61a2adf9846b05a4110b00175f758aeb74567))
* consolidate ID helpers, fix child session ID collision, add session CRUD test coverage (CAT-169) ([478d2b5](https://github.com/samcharles93/tau/commit/478d2b575fbf09ba84b063a88cff87efd25c5666))
* **tui2:** reconcile tool-call IDs that change again after streaming ([63c2fda](https://github.com/samcharles93/tau/commit/63c2fda959f535d452b40268324171c8c8ab5807))

## [0.23.0](https://github.com/samcharles93/tau/compare/v0.22.0...v0.23.0) (2026-07-16)


### Features

* **cli:** add provider command group with list, login, and logout subcommands ([e3e12b7](https://github.com/samcharles93/tau/commit/e3e12b7f6533f7196b30060256f7d17983af16c5))

## [0.22.0](https://github.com/samcharles93/tau/compare/v0.21.2...v0.22.0) (2026-07-16)


### Features

* **app:** add setup orchestration for provider auth and model selection ([6f7ddb9](https://github.com/samcharles93/tau/commit/6f7ddb9fbbb3ff14dc2325d808f7e2006c4d7d04))
* **cli:** add tau setup command wrapping shared setup orchestration ([45026fe](https://github.com/samcharles93/tau/commit/45026fe812d979778f2397378e9c25e486bbb9f9))
* **cli:** add terminal selector and secret input adapters for setup prompts ([9ff62f4](https://github.com/samcharles93/tau/commit/9ff62f46b1db3d2b75a8accf2bb5a3f16c422fae))
* **cli:** auto-invoke setup for first-run terminal sessions ([f4abdb2](https://github.com/samcharles93/tau/commit/f4abdb2f6ce16667ffce62105931a81f1c39ab29))
* **providers:** add managed API-key storage to auth.yaml state ([b85e586](https://github.com/samcharles93/tau/commit/b85e586d0537ff6876a8a84bb894d905e0f023cb))
* **providers:** add managed-key source to provider resolution and menus ([638193e](https://github.com/samcharles93/tau/commit/638193eafb2d8332476d5af258c4be87be8b017d))


### Bug Fixes

* **config:** make default-provider/model persistence preserve YAML comments ([8da0aa7](https://github.com/samcharles93/tau/commit/8da0aa76b06b0ece7da6bcddc039af90ea1fb689))
* **installer:** add checksum verification to install.sh and install.ps1 ([30442c0](https://github.com/samcharles93/tau/commit/30442c02200fa96a7dd52127bee59cc898034b3b))

## [0.21.2](https://github.com/samcharles93/tau/compare/v0.21.1...v0.21.2) (2026-07-14)

### Bug Fixes

- **release:** restore full platform matrix, drop darwin/amd64 exclusion
  ([35278bc](https://github.com/samcharles93/tau/commit/35278bc64625840e852a1c11c7984331086b94ea))

## [0.21.1](https://github.com/samcharles93/tau/compare/v0.21.0...v0.21.1) (2026-07-13)

### Bug Fixes

- **test:** subscribe before submitting in coordinator budget tests
  ([74418b7](https://github.com/samcharles93/tau/commit/74418b787b21da2950838b22ada2028aa646711d))

## [0.21.0](https://github.com/samcharles93/tau/compare/v0.20.0...v0.21.0) (2026-07-13)

### Features

- add Ctrl+P command palette, Ctrl+L model picker, Ctrl+O session navigator
  ([9224d56](https://github.com/samcharles93/tau/commit/9224d56087c4a6099a6b37cf45e20e43b6f86837))

### Bug Fixes

- **docs:** escape Go template mustaches breaking VitePress build
  ([86399e3](https://github.com/samcharles93/tau/commit/86399e3c3c62a6fbc05410077d80621c1c563c0d))
- **test:** raise budget-trip completion-wait timeout to reduce CI flakiness
  ([3030076](https://github.com/samcharles93/tau/commit/303007615d937bef0e46d70ba82ae54c9b096a77))

## [0.20.0](https://github.com/samcharles93/tau/compare/v0.19.0...v0.20.0) (2026-07-13)

### Features

- default to tui2, rename --new-tui to --legacy-tui
  ([ed965b3](https://github.com/samcharles93/tau/commit/ed965b3b0fa92de6e5ebd494278ee114389e50b0))

## [0.19.0](https://github.com/samcharles93/tau/compare/v0.18.1...v0.19.0) (2026-07-13)

### Features

- add event and command types for child transcript loading
  ([8239133](https://github.com/samcharles93/tau/commit/8239133f134fd9c9c0e879267a9e7b689e2586d2))
- add hierarchical tree view to session switcher
  ([9bd38a1](https://github.com/samcharles93/tau/commit/9bd38a195f3f406d4595ec4eda027f222158cb65))
- add silent listing and child transcript types
  ([4c7e8e0](https://github.com/samcharles93/tau/commit/4c7e8e027146902c8c860301cfd02ba1a635dd4e))
- add tui2 interactive chat UI
  ([c7674cf](https://github.com/samcharles93/tau/commit/c7674cfc90b780077bb00f07560dbf03a727f59c))
- **agent-observability:** add correlated metrics, logs, and redaction
  ([da52c9a](https://github.com/samcharles93/tau/commit/da52c9af53be94d6a1fd4a4ca5808443aabc4354))
- **agent-storage:** version agent spec snapshots
  ([5fa9f3e](https://github.com/samcharles93/tau/commit/5fa9f3e6e1f269c1298dce7fbe7a772f00f9a561))
- **agent-ui:** add recovery, accessibility, and fan-out bounds
  ([742b539](https://github.com/samcharles93/tau/commit/742b5392ce2c014cafcd0c0a32f936619914da4a))
- **agent:** add --tools/-t flag and wire {{.Tools}} into the system prompt
  ([89cdb43](https://github.com/samcharles93/tau/commit/89cdb43662f923eb1736c9547abf370325e4d152))
- **agent:** add budget and limit enforcement to the coordinator
  ([acd427d](https://github.com/samcharles93/tau/commit/acd427db61de49ebd00e3c56e7a324f0d9c5a7d5))
- **agent:** add provider, max-turns, timeout to spec frontmatter
  ([38638b2](https://github.com/samcharles93/tau/commit/38638b27ede467e88d4316b70dcd92042799a4ed))
- **agent:** add stdio JSONL transport for the agent wire protocol
  ([23f025c](https://github.com/samcharles93/tau/commit/23f025c07f7ac7554f3bb152bccf5f8e460f3b69))
- **agent:** add the agent tool and headless child process runtime
  ([30fbd02](https://github.com/samcharles93/tau/commit/30fbd02c295bd458749889ece1e643260e2abcf1))
- **agent:** add the shared agent instantiation function
  ([01b3875](https://github.com/samcharles93/tau/commit/01b387583f718de84b2728fc84e19f1a2b60f928))
- **agent:** enforce child concurrency and resource ceilings
  ([11a734b](https://github.com/samcharles93/tau/commit/11a734b7d931ea7f1a4e6542f4ce77a8a6e5bc52))
- **agent:** relocate root identity into a built-in tau.agent.md spec
  ([f761dbb](https://github.com/samcharles93/tau/commit/f761dbb9944ce6855e378a136c97bfca8c415ab0))
- **agent:** render live child agent state blocks in the TUI (P4.1/CAT-64)
  ([a9c0d92](https://github.com/samcharles93/tau/commit/a9c0d92e87bd065c223f96a92d08ca1ed878b17c))
- **agent:** split tool filter into an immutable ceiling and an active filter
  ([08f273b](https://github.com/samcharles93/tau/commit/08f273bdd02bfcc940ab3d48bb3cf4a3e08d9838))
- **agent:** trust-on-first-use gate for project root-spec overrides (G14)
  ([7105922](https://github.com/samcharles93/tau/commit/7105922fad7996bd006cf597998013b7eedb2932))
- **bridge:** add agent wire envelope fields and message types
  ([c13ade5](https://github.com/samcharles93/tau/commit/c13ade51c473a35ae1ef57fd66268e50a0fbbc05))
- **cli:** show relative build age in version
  ([b62084b](https://github.com/samcharles93/tau/commit/b62084bb954d2b8dd680fa91366527664b3c72d3))
- **config:** add model_modes tier map and agents defaults block
  ([82dd2c0](https://github.com/samcharles93/tau/commit/82dd2c088d70be76c2286f613f9cd0870b56da3b))
- **sessions:** CAT-67 session tree - lineage in session lists, agent attribution, resume
  ([0b3faa9](https://github.com/samcharles93/tau/commit/0b3faa91e324d0ffbf48c3b31906a1c9758e41de))
- **skills:** add spec-to-code-pipeline workflow skill
  ([e4e126a](https://github.com/samcharles93/tau/commit/e4e126a5e4a95d3f1c49ddfe4c360710c8959060))
- **store:** add agent_instances migration
  ([dc45d46](https://github.com/samcharles93/tau/commit/dc45d46b19a437af6373774310df5fb27c264c9b))
- **store:** add AgentInstance CRUD and session lineage plumbing
  ([21ea623](https://github.com/samcharles93/tau/commit/21ea6233684b18abf8f2896919d78073924b3aa3))

### Bug Fixes

- **agent-prompt:** frame delegated context as untrusted data
  ([7ab441f](https://github.com/samcharles93/tau/commit/7ab441f5641590cf0b39c846e1d58751caef7458))
- **agent-security:** require trust decision for project root-spec overrides
  ([c7925ff](https://github.com/samcharles93/tau/commit/c7925ff6ccfbf5d41612bc432cb9343def366254))
- **agent-wire:** specify JSONL protocol state machine and conformance
  ([a505e88](https://github.com/samcharles93/tau/commit/a505e887cf7268a527db904c3b68529a44b4e212))
- **agent:** authorize resume and recompute capabilities safely
  ([7eacc80](https://github.com/samcharles93/tau/commit/7eacc80ba743cbdeffcc901904eaaa863dc90392))
- **agent:** cancel entire descendant process tree via process groups
  ([99426e3](https://github.com/samcharles93/tau/commit/99426e371e8fbb12a38a013d44cb4c6afe08249a))
- **agent:** compensate orphaned instance rows on spawn failure, retry ID collisions (G3)
  ([79f12f6](https://github.com/samcharles93/tau/commit/79f12f6b5669e3e3788c181387fe32644874700d))
- **agent:** define canonical budget semantics and atomic instantiation
  ([a94e5b7](https://github.com/samcharles93/tau/commit/a94e5b76f31715b721702190f65badfa4569c140))
- **agent:** define wire authority bindings and storage constraints
  ([89336a5](https://github.com/samcharles93/tau/commit/89336a5487240e48ffb1dbeec28f557006f3c455))
- **agent:** enforce the tool ceiling at dispatch time, not just prompt-build time
  ([17b1fab](https://github.com/samcharles93/tau/commit/17b1fab5547e68ebafaf1d8960d8415ba128171f))
- **agent:** fix child/parent handshake deadlock (agent.ready vs agent.assign)
  ([2be4a56](https://github.com/samcharles93/tau/commit/2be4a5611ae19b839ecce49f2a60892b4b52d7ff))
- **agent:** give spawned children their own system prompt, not the parent's
  ([4d4cabe](https://github.com/samcharles93/tau/commit/4d4cabef49608ea571f439cb12b39888fc8417e2))
- **agent:** harden instance identity and orphan detection
  ([422e20a](https://github.com/samcharles93/tau/commit/422e20adbe3f682863ed247fb86bf677f8104fcf))
- **agent:** hash the full spec snapshot, not just the body
  ([e1bfb1e](https://github.com/samcharles93/tau/commit/e1bfb1e78f4b248b112e5baa915860bd573e08b2))
- **agent:** per-parent and process-wide concurrency ceilings for spawns (G5)
  ([7807dc6](https://github.com/samcharles93/tau/commit/7807dc636e26bb237a23993de4f0c615ff27668b))
- **agent:** process-group cancellation with three-phase escalation (G7)
  ([626f189](https://github.com/samcharles93/tau/commit/626f189311b205897d5c900abd1a195431861f16))
- **agent:** process-start identity for orphan sweep, wire it into root startup (G10)
  ([9167f69](https://github.com/samcharles93/tau/commit/9167f6924563c4d20927b02ad97bebe33fbb6716))
- **agent:** redact, rate-limit, and cap child stderr capture (G16)
  ([f55d505](https://github.com/samcharles93/tau/commit/f55d50595aad642f4c62a1d6c4b5084470b57cb6))
- **agent:** resolve skill tool attenuation contradiction
  ([c4757a0](https://github.com/samcharles93/tau/commit/c4757a0c339accbae4185b11aad9d11f830132a4))
- **agent:** resume authorization and delegated-context trust framing (G2/G11)
  ([f566236](https://github.com/samcharles93/tau/commit/f5662363cf1affe72ec29b965b055142422b74e5))
- **agent:** split budget.deadline into timeout/deadline, add spawn-time validation (G6)
  ([cc8252f](https://github.com/samcharles93/tau/commit/cc8252fb5c34280adb5de21a168764124b0af550))
- **agent:** wire spec.Provider into Instantiate's model resolution
  ([58a6919](https://github.com/samcharles93/tau/commit/58a6919aa3fee03fa26d3af5e6cb2efc3e2a73e4))
- **agent:** wire Store/Bus/ParentInstanceID/SessionID into the live agent tool
  ([af9cf2a](https://github.com/samcharles93/tau/commit/af9cf2aa0ab9d0237907f18c558360fb01735a21))
- **config:** fix two model-resolution bugs found via agent-spawn testing
  ([ae150b5](https://github.com/samcharles93/tau/commit/ae150b5a3cc8d3d6b3f6aba13caef356b7076329))
- **config:** normalize model_modes keys at unmarshal, not just merge
  ([732ca77](https://github.com/samcharles93/tau/commit/732ca772db7164db8a37c2413e191fa58e26004e))
- drain pending commands on shutdown to prevent lost messages
  ([9bbf2dd](https://github.com/samcharles93/tau/commit/9bbf2dde44277d5ce82f31c7b0bdfc51956bb7d4))
- **skills:** add enabled parsing, fix user-invocable in docs
  ([819b807](https://github.com/samcharles93/tau/commit/819b80741ed1aa4c394179697bd49ef0f8648040))
- **skills:** align frontmatter with agentskills.io spec, drop dead fields
  ([3441058](https://github.com/samcharles93/tau/commit/344105881aaf66e5c2996d590e05f0e8ea026a6f))
- **skills:** correct json tag user_invocable -&gt; user-invocable
  ([6feff9d](https://github.com/samcharles93/tau/commit/6feff9db9ce75bdbe45ab95e3ec8d96a95721de6))
- skip exit summary when session has no messages
  ([fefa7b9](https://github.com/samcharles93/tau/commit/fefa7b9fda13dd0adb76457a834220f29cd096c2))
- **store:** enforce sqlite busy_timeout via PRAGMA, not DSN query params
  ([4893818](https://github.com/samcharles93/tau/commit/4893818d0c766a5bf54654e08d9492a652d4ba69))
- strip anyOf from read tool schema for OpenAI Responses API compatibility
  ([39cc188](https://github.com/samcharles93/tau/commit/39cc188ae4df66c794544ae2e0f1fe61fd8856a1))
- **tui2:** only enter Thinking on an actual reasoning delta
  ([f411863](https://github.com/samcharles93/tau/commit/f4118637d8722cfcf5d7dcf08e919d91c3dbbe12))

## [0.18.1](https://github.com/samcharles93/tau/compare/v0.18.0...v0.18.1) (2026-07-12)

### Bug Fixes

- bundle ripgrep binary, fix grep fallback, harden CI tests
  ([7dde49d](https://github.com/samcharles93/tau/commit/7dde49dff43f6220de1164c9993deffb745b7794))
- **codex:** recover streamed tool names
  ([ecb25d0](https://github.com/samcharles93/tau/commit/ecb25d02edb8a43aec86f841c529b2e92a15f3f4))

## [0.18.0](https://github.com/samcharles93/tau/compare/v0.17.0...v0.18.0) (2026-07-11)

### Features

- add Ctrl+Home/Ctrl+End conversation jump keys
  ([08792a4](https://github.com/samcharles93/tau/commit/08792a452fd1b866ccfc28c971f1c9d2fb538593))
- add tool call summaries and status bar state machine
  ([642f1c4](https://github.com/samcharles93/tau/commit/642f1c47c9448ea59645b1210029eccc319126ce))
- add workspace codesearch indexing
  ([44dbc6e](https://github.com/samcharles93/tau/commit/44dbc6e756e0d0cff1a3de60ad309f969968c865))
- **agent:** restructure system prompt with instruction precedence and XML escaping
  ([5e3eb36](https://github.com/samcharles93/tau/commit/5e3eb368ff9062d6bdf794af78ee2cbe66a429b4))
- enhance tool-stats with structured results and richer metrics
  ([a558c87](https://github.com/samcharles93/tau/commit/a558c8774ec7cdf6906d37879d31e11135059b01))
- enrich tool metrics, add grep index, fix session contexts
  ([73afa47](https://github.com/samcharles93/tau/commit/73afa476c474590759ca7f561243e93507388758))
- enrich tool results with truncation metadata and symbol reads
  ([8b8d5b0](https://github.com/samcharles93/tau/commit/8b8d5b0bc1ad4d1bba460a42e35698562ddfbacb))
- integrate workspace codesearch indexing
  ([2938532](https://github.com/samcharles93/tau/commit/2938532511b665215461906cab693a1f9f5ca0eb))
- parse nested function call item in codex SSE
  ([3bf906a](https://github.com/samcharles93/tau/commit/3bf906a1a8113ac69474a3385d9911cd4165c48d))
- persist tool result metadata in messages
  ([9057a42](https://github.com/samcharles93/tau/commit/9057a424c3d195e820f6a0adeec1b5b386b8f4c9))
- redesign /help as a responsive click-to-expand keybinding box
  ([7681e40](https://github.com/samcharles93/tau/commit/7681e405b52b7490a4bd043489e6dd04a18e26c5))
- track and report search backend usage stats
  ([6954c0e](https://github.com/samcharles93/tau/commit/6954c0ec135ebe7552863686391b2b9b903036dd))
- **tui2:** add "View diff" overlay for edit/write tool calls
  ([c2757e8](https://github.com/samcharles93/tau/commit/c2757e8b0e7f092d521a861b8351fd34bb3f11f1))
- **tui2:** collapsible reasoning, capped input box, calm thinking indicator
  ([3f7a387](https://github.com/samcharles93/tau/commit/3f7a387532a8d5d899782cfede73d6e61bff9325))
- **tui2:** give reasoning its own visual lane, distinct from the final answer
  ([82edc9e](https://github.com/samcharles93/tau/commit/82edc9e934bcd9bc37a82fe46ed6d7ceefaa8487))
- **tui2:** introduce Tau's semantic colour palette without overriding terminal theme
  ([bf22e3e](https://github.com/samcharles93/tau/commit/bf22e3e4589bdd39185c890796d85200e9535592))

### Bug Fixes

- clip and scroll the /help overlay when it's taller than the terminal
  ([68cb34e](https://github.com/samcharles93/tau/commit/68cb34ed9080d903f72d08cb85c55c9939dadf4e))
- make Esc and Ctrl+C clear input while slash completions are open
  ([493958c](https://github.com/samcharles93/tau/commit/493958c242d75812e965501e92ce43327e03b6e4))
- prevent symlink escape in workspace file confinement
  ([349fc43](https://github.com/samcharles93/tau/commit/349fc433e1985b654e6774dc4db95f6765a6237f))
- stop echoing a runtime error to scrollback when a lone tool box already shows it
  ([a7b9675](https://github.com/samcharles93/tau/commit/a7b967592328be51336fe7dcc0eecc69e5344b9a))
- **tui2:** correct layout row drift, markdown cache key mismatches, and dead key case
  ([1d79d76](https://github.com/samcharles93/tau/commit/1d79d76eddb6e68fdb434ee43266a5080ad208f1))
- **tui2:** persist reasoning across snapshots, cap error text, reset MaxTokens on model switch
  ([a5d706e](https://github.com/samcharles93/tau/commit/a5d706e10787e0ceebd6659d85e951d2a1816bcd))
- **tui2:** remove "Thinking:" prefix from reasoning output and add intelligent word-wrapping
  ([7371cf1](https://github.com/samcharles93/tau/commit/7371cf1ac4e6e1cd4ae18c6634fec2429ee9a387))
- wire context propagation through reload, store, web UI, and plugin lifecycle
  ([f8beb51](https://github.com/samcharles93/tau/commit/f8beb5128dbab1822eff96a6b6abf30c757e657b))

## [0.17.0](https://github.com/samcharles93/tau/compare/v0.16.2...v0.17.0) (2026-07-10)

### Features

- add self update command
  ([7b1b489](https://github.com/samcharles93/tau/commit/7b1b4899c7b0d74b14af1f1ebe2ebb34c2a05524))
- **agent:** wire execution for agent-authored subagent definitions
  ([aaf9a6d](https://github.com/samcharles93/tau/commit/aaf9a6debb2b16313b98c973e974d87294f57ab4))
- **tui2:** interactive prompt system, GitHub Enterprise Copilot login
  ([df76f35](https://github.com/samcharles93/tau/commit/df76f359f16d9c52b0647bf19ac46c7e652e71d7))

### Bug Fixes

- improve TUI tool tracking, logging, and notifications
  ([64e50fe](https://github.com/samcharles93/tau/commit/64e50fe44ed5dcb0094bc4e0531e6b2aeb9be15b))

## [0.16.2](https://github.com/samcharles93/tau/compare/v0.16.1...v0.16.2) (2026-07-10)

### Bug Fixes

- **test:** remove two CI-only flaky-test races
  ([f5c0c2b](https://github.com/samcharles93/tau/commit/f5c0c2be7b7b3c57a6150e9b8b6e2b82097ae25f))

## [0.16.1](https://github.com/samcharles93/tau/compare/v0.16.0...v0.16.1) (2026-07-10)

### Bug Fixes

- **agent:** fix race in persist tests caught by task test:race
  ([7d9dff2](https://github.com/samcharles93/tau/commit/7d9dff2d645c954656e0e6353746afd76632cac7))
- **ci:** remove machine-local ai-sdk path from go.work
  ([d4406e9](https://github.com/samcharles93/tau/commit/d4406e94625efeb3488b680a2dc63460a20bcfe4))
- **cli:** explain _why_ a configured provider is unavailable
  ([fea3fe7](https://github.com/samcharles93/tau/commit/fea3fe798d326723ff75a7c9f7b6c791b9fbc6ca))

## [0.16.0](https://github.com/samcharles93/tau/compare/v0.15.1...v0.16.0) (2026-07-10)

### Features

- **agent:** discover agent-authored subagent definitions from disk
  ([e9234c7](https://github.com/samcharles93/tau/commit/e9234c793baf92525c1c35c924f51bf9fc90f2f8))
- **chat:** surface cached prompt tokens in usage and cost accounting
  ([0ee2a39](https://github.com/samcharles93/tau/commit/0ee2a395777a6fd921a626f544116bf9a7d102ec))
- **cli:** add --ephemeral flag to skip session persistence
  ([9eaf566](https://github.com/samcharles93/tau/commit/9eaf5664afe97f23021c45ffa5e220e7b53f3d33))
- **providers:** add OAuth device-code login for Copilot and Codex
  ([cb9b321](https://github.com/samcharles93/tau/commit/cb9b321134588d9af16a34ab8bd86ea50bd44aed))

### Bug Fixes

- **agent:** emit tool execution started event before running the tool
  ([6c12846](https://github.com/samcharles93/tau/commit/6c128460c2327f0edf7168b6c97b04b5eab79fd2))
- **app:** give live-discovered ollama models real reasoning effort levels
  ([ee8a1e0](https://github.com/samcharles93/tau/commit/ee8a1e026badf8f4929708574774a9ff08d9a506))
- **config:** default ui.show_reasoning to true
  ([a42bbb1](https://github.com/samcharles93/tau/commit/a42bbb15341224176471a40bf445e17166e56179))
- **providers:** forward Codex account-id header; stop auto-dismissing notifications
  ([e2b001e](https://github.com/samcharles93/tau/commit/e2b001e84b6a58b2d986394af1b0090d9ab483c0))
- **tui2:** move the steer hint out of the input box into the notification area
  ([6d50444](https://github.com/samcharles93/tau/commit/6d50444b8ec6aab1e49149eac70377cb2b6b0266))

## [0.15.1](https://github.com/samcharles93/tau/compare/v0.15.0...v0.15.1) (2026-07-08)

### Bug Fixes

- add scrolling, collapse, and config to tool-call group box
  ([6f4b93a](https://github.com/samcharles93/tau/commit/6f4b93a6594576d29e801d88f3c12277bc91ee22))
- **agent:** infinite loop in DiscoverContextFiles on Windows
  ([79997b3](https://github.com/samcharles93/tau/commit/79997b3416f9a90787f473a24557b1907520b8fe))
- preserve draft input on history recall
  ([6f4b93a](https://github.com/samcharles93/tau/commit/6f4b93a6594576d29e801d88f3c12277bc91ee22))

## [0.15.0](https://github.com/samcharles93/tau/compare/v0.14.0...v0.15.0) (2026-07-07)

### Features

- add auto-compact and mode-switcher
  ([4b411d0](https://github.com/samcharles93/tau/commit/4b411d084269d533a78802cac2c81753afa9a8d8))
- add auto-compact config for conversation history compaction
  ([db8fc83](https://github.com/samcharles93/tau/commit/db8fc83d0dbf5e0ad3e9ae81b4371fb6101f541d))
- add bash commands, config sync, TAU_LOG_LEVEL
  ([cc93350](https://github.com/samcharles93/tau/commit/cc933507bd818d205fb2fff4a516c05f21abdfed))
- add mode dividers and focus tracking
  ([b20cffb](https://github.com/samcharles93/tau/commit/b20cffbc43019360189d0c908c740f85d0b07d2e))
- add scripts/tui-parity.sh for visual parity sign-off
  ([6ccc8f8](https://github.com/samcharles93/tau/commit/6ccc8f81baaf672413201cb068a4b022b7bac379))
- allow folding of single-tool committed groups
  ([63773fe](https://github.com/samcharles93/tau/commit/63773fea0256ebe352ddf1db8c7599c40bdfab0f))
- **chat:** add stable ChatMessage.ID, shared by TUI and WebUI
  ([788438d](https://github.com/samcharles93/tau/commit/788438d0257221ef88ed6d8873d9d31b908c8125))
- implement bash commands and mode indicators
  ([99e3c48](https://github.com/samcharles93/tau/commit/99e3c48074c3328e424779723ef00babad27ce82))
- **plugin:** let plugins expose their own docs via a Documented interface
  ([641a0ef](https://github.com/samcharles93/tau/commit/641a0efd40df2830fefc12f34988c9e516bb3c0c))
- **tool-stats:** add report drilldowns
  ([9d57c91](https://github.com/samcharles93/tau/commit/9d57c91c344deefb32c3248eadf2f97a0d717cc3))
- **tui2:** add animation primitives for living working indicator
  ([1b41e49](https://github.com/samcharles93/tau/commit/1b41e493a56ad2d5d2e3bee9338f1658df7e6e65))
- **tui2:** add full keyboard shortcut reference to /help
  ([a0f671f](https://github.com/samcharles93/tau/commit/a0f671f66803f913be49fb3aa0204dc30e7a80f8))
- **tui2:** add spinner animation and animated steering indicator
  ([416b561](https://github.com/samcharles93/tau/commit/416b56158ff0c236992604c2c25345b0818f998c))
- **tui2:** alt-screen rendering, tool box overhaul, and expand/collapse interaction
  ([1740f8a](https://github.com/samcharles93/tau/commit/1740f8ab43f29fb2d31adbe3149b9cb129639dfe))
- **tui2:** enhanced /cost breakdown, /session info, !! double-bang bash, session load replay
  ([0ccc34d](https://github.com/samcharles93/tau/commit/0ccc34da6dd857a1b9fbd86b1446d10ceae8e7c2))
- **tui2:** extend the context menu to chat messages
  ([2b0b0a2](https://github.com/samcharles93/tau/commit/2b0b0a2e6ab8d2fb2e51d0981b7613d25dd64373))
- **tui2:** improve scrolling, make input visible during turn, support inline steering
  ([5fb2a3a](https://github.com/samcharles93/tau/commit/5fb2a3af078dbcd4a7e3771b3aaea6ee5cca2aad))
- **tui2:** integrate Glamour for markdown rendering on finalized messages
  ([53503ae](https://github.com/samcharles93/tau/commit/53503aea04d8859b0fe66ac5c1a0b954aeb364d9))
- **tui2:** mouse drag-to-select text, fixed-height notification area
  ([3bfcb4f](https://github.com/samcharles93/tau/commit/3bfcb4f0ddc817571785b01afa0d24f95e733a67))
- **tui2:** Phase 2 critical-gap sweep - Ctrl+C guard, plugin views, /provider, session listing
  ([bda0d60](https://github.com/samcharles93/tau/commit/bda0d607900b1e9d3136864680290dd04384e2cc))
- **tui2:** Phase 2 feature-parity sweep - commands, completions, status bar, prompts
  ([585874f](https://github.com/samcharles93/tau/commit/585874fb544d4d3b67614bb9a79e4e9d8d6da0f6))
- **tui2:** pin input area and status bar to bottom of screen in alt-screen
  ([282cd50](https://github.com/samcharles93/tau/commit/282cd5098f4b84316cac05f0bd252b418d5555a5))
- **tui2:** render the context menu as a floating overlay
  ([e20fcc0](https://github.com/samcharles93/tau/commit/e20fcc0f436152f97d299f2e83ca7aafa444161a))
- **tui2:** right-click context menu for tool calls (no rendering yet)
  ([e467634](https://github.com/samcharles93/tau/commit/e467634edf0d0768b8ae5b6cb436f3f8ed66a4f7))
- **tui2:** track per-message line ranges in the scrollback buffer
  ([fc1bc42](https://github.com/samcharles93/tau/commit/fc1bc420f255c9faa027d5965fa4d6f4b70cb9ef))
- **tui2:** wire up reasoning config, fix completion menu viewport clipping
  ([4f5209a](https://github.com/samcharles93/tau/commit/4f5209a07d5bbeab4a8caf5870102220adfa4cf5))
- **tui:** add experimental Bubbletea v2 TUI behind --new-tui flag
  ([199be58](https://github.com/samcharles93/tau/commit/199be583a32e6b9e9735e8517c81e8f71382bb30))
- **tui:** improve selection and tool history UX
  ([fc38304](https://github.com/samcharles93/tau/commit/fc383043fb7313fa2ca29bee8602881df692ce7f))

### Bug Fixes

- **agent:** merge consecutive user messages, clear stale steering queue
  ([d62ad87](https://github.com/samcharles93/tau/commit/d62ad878ff4b06c2cc65a7665a26e99182c8926d))
- **agent:** off-by-one in toolLoopSoftThreshold and combine double JSON unmarshal
  ([dfba635](https://github.com/samcharles93/tau/commit/dfba63521d6122c88c0536ed4564a4656814afb0))
- **agent:** replace function name in mergeToolCallDelta, not accumulate
  ([c230961](https://github.com/samcharles93/tau/commit/c2309614b45f77f02913121d8d5cdf942feb6406))
- **app:** defer session start/load until the TUI has subscribed
  ([e246914](https://github.com/samcharles93/tau/commit/e24691408dba42284ec0b0be0e30249f5b568e8a))
- **config:** heal missing metrics export config and its lost UnmarshalYAML field
  ([d22ea98](https://github.com/samcharles93/tau/commit/d22ea983ddf00225af2498fe6732e9b1ded2ec93))
- **docs:** embed per-plugin walkthroughs under examples/ and specs/
  ([b4d836b](https://github.com/samcharles93/tau/commit/b4d836b01e65af2a3ab27a20917214c1557f6df6))
- **plugins/mcp:** match RunCommand to the panels/views Extension interface
  ([10c40a8](https://github.com/samcharles93/tau/commit/10c40a87cc006d5bf3a96623bdb369ae73a8778f))
- **tui2:** drop context menu background fill
  ([025e386](https://github.com/samcharles93/tau/commit/025e38682da533e9780403ee2116446b80872073))
- **tui2:** fix tool-call ordering/scroll-lock, group tool calls per batch
  ([b5a72f0](https://github.com/samcharles93/tau/commit/b5a72f068fd65fcef34ebfd7142130b0346a457b))
- **tui2:** gate alias completions behind typed prefix
  ([d07cec7](https://github.com/samcharles93/tau/commit/d07cec7f36133352b8926d941d8cdcc6d165902c))
- **tui2:** match typed-out command aliases in the completions dropdown
  ([da244fb](https://github.com/samcharles93/tau/commit/da244fb9d6630225a76fb694e2975a4214a200a5))
- **tui2:** missing RequestID on chat commands, viewport padding gap
  ([46a0339](https://github.com/samcharles93/tau/commit/46a033962627864a25610ec08b8933e10558e53e))
- **tui:** clamp idle viewport scrollback
  ([b2a5e11](https://github.com/samcharles93/tau/commit/b2a5e112ddf3c27b259c14452208469796b56f6d))
- **tui:** clarify multiline messages and token status
  ([4d76c52](https://github.com/samcharles93/tau/commit/4d76c52e6834b1599b7b7f1f168a07565d346f2a))
- **tui:** improve input modes and tool display
  ([e56e01e](https://github.com/samcharles93/tau/commit/e56e01e225318779f3baf7c712b99ad65e433bdb))
- **tui:** improve input rendering and shell output
  ([dd2c4fb](https://github.com/samcharles93/tau/commit/dd2c4fb2fda226a66fd45da7664558809a9a81d1))
- **tui:** stabilize streaming viewport layout
  ([778c28b](https://github.com/samcharles93/tau/commit/778c28b9e0f0c27ea1fb05d6b81c776ad35e7281))

### Performance Improvements

- **tui2:** memoize glamour renderers per width, markdown tool output, and fix test timeout
  ([69a30b2](https://github.com/samcharles93/tau/commit/69a30b25863dcc96d3e670547aa60163645757e4))

## [0.14.0](https://github.com/samcharles93/tau/compare/v0.13.0...v0.14.0) (2026-07-04)

### Features

- add specgen tool and regenerate AsyncAPI spec, enhance TUI command experience
  ([7e6f23c](https://github.com/samcharles93/tau/commit/7e6f23ca1bf55615007c320546f8b8c0bb874196))
- **agent:** add declarative agent spec system for built-in commands
  ([e2b64d8](https://github.com/samcharles93/tau/commit/e2b64d869105b3d59692ecf37ed19f8b1f6ef13e))
- **tui:** stream tool output to a live tail log, surface runtime errors in status bar
  ([ffebdbb](https://github.com/samcharles93/tau/commit/ffebdbb3898917a3ee1779a794e87e52d1c32bc0))

## [0.13.0](https://github.com/samcharles93/tau/compare/v0.12.0...v0.13.0) (2026-07-03)

### Features

- default reasoning effort to auto
  ([7898de8](https://github.com/samcharles93/tau/commit/7898de8d6002433054ffd8121fcbdf82a9ecd998))

## [0.12.0](https://github.com/samcharles93/tau/compare/v0.11.0...v0.12.0) (2026-07-03)

### Features

- **tool-stats:** join metrics.jsonl for ground-truth error rates and durations
  ([a16e764](https://github.com/samcharles93/tau/commit/a16e764ee185ba2d3ea29624f71e3c0cb85d6004))

## [0.11.0](https://github.com/samcharles93/tau/compare/v0.10.1...v0.11.0) (2026-07-03)

### Features

- add tool-stats script for analysing session tool usage
  ([d57b93e](https://github.com/samcharles93/tau/commit/d57b93efb5f80ac538ee5201b787f313e066e527))
- **agent:** adopt tool-design lessons from Pi session analysis
  ([1fd6958](https://github.com/samcharles93/tau/commit/1fd69588345fec39a38b10733a01aad2bf120af3))
- **agent:** self-documenting tool limits and tolerant edit parsing
  ([e82d2a5](https://github.com/samcharles93/tau/commit/e82d2a5d34cf3ade502ab76ec926bbc6f394cb60))
- fix reasoning effort defaults, budget fallback, and provider-agnostic mapping
  ([31ab330](https://github.com/samcharles93/tau/commit/31ab3308919d95b28aaf24876854c458b4f307e4))
- **metrics:** add LLM latency, skill/extension, model/provider switch, and error metrics
  ([48d26fa](https://github.com/samcharles93/tau/commit/48d26fa6c1281b2e19a6b340241579076d3add3e))
- **metrics:** add metric fields to SessionSummary, extend UsageTracker, update store schema
  ([1e7d4ac](https://github.com/samcharles93/tau/commit/1e7d4ac6cfc79f29808f9ba4ec4b2c401e65f989))
- **metrics:** comprehensive observability system - metric events, JSONL export, TUI stats
  ([46cb3cd](https://github.com/samcharles93/tau/commit/46cb3cd1c008f75cf7d05d501da0bb27f4cd4d8d))
- **web:** add request cancellation support to web UI
  ([6b6b884](https://github.com/samcharles93/tau/commit/6b6b884222c8658e30ef2c8288a12cf6980ba419))
- **web:** add subcommand completions and cost tracker, improve TUI separators
  ([cc63a96](https://github.com/samcharles93/tau/commit/cc63a96851da9bfc521f1aff763a13d58f47ce7c))

### Bug Fixes

- **metrics:** address adversarial review findings
  ([05708b2](https://github.com/samcharles93/tau/commit/05708b2ceb635b0cef594a13805d15018e7785e3))
- **metrics:** fix headless exit summary race with shared bus client
  ([aa7fe46](https://github.com/samcharles93/tau/commit/aa7fe46d827343d05c5d624519aea210ac4f4736))
- **metrics:** fix headless exit summary race, rename turn.duration, and address review gaps
  ([dabb081](https://github.com/samcharles93/tau/commit/dabb0814c1bca59b9213069be7b69098b928a4c8))
- **tui:** submit slash commands immediately when a terminal completion is accepted
  ([f7779d8](https://github.com/samcharles93/tau/commit/f7779d82eeacee9b2374b5f17b2fc0207d51e189))

## [0.10.1](https://github.com/samcharles93/tau/compare/v0.10.0...v0.10.1) (2026-07-03)

### Bug Fixes

- **taui:** fix dead bracketed-paste path and add OverlayStack ([#25](https://github.com/samcharles93/tau/issues/25))
  ([d06e4ec](https://github.com/samcharles93/tau/commit/d06e4ecf36824afe5234978ecefa61b0812cd5b8))

## [0.10.0](https://github.com/samcharles93/tau/compare/v0.9.1...v0.10.0) (2026-07-03)

### Features

- add plugin-rendered panels and views
  ([a7bdab4](https://github.com/samcharles93/tau/commit/a7bdab4bf6e47f497de2d841b17da91c0247a482))
- add plugin-rendered panels and views
  ([d287fbc](https://github.com/samcharles93/tau/commit/d287fbc9ea90f4a27558fa63f3390c406f89b619))

## [0.9.1](https://github.com/samcharles93/tau/compare/v0.9.0...v0.9.1) (2026-07-03)

### Bug Fixes

- respect selected index when choosing completion
  ([f8eab81](https://github.com/samcharles93/tau/commit/f8eab812491ba79aad8503a268ab89d8266b379b))

## [0.9.0](https://github.com/samcharles93/tau/compare/v0.8.0...v0.9.0) (2026-07-02)

### Features

- add interactive prompt support for plugins/tools
  ([c41a4da](https://github.com/samcharles93/tau/commit/c41a4da79d5b5d4583a4984a857720f3dae88df0))
- add setup task and remove build targets
  ([3729032](https://github.com/samcharles93/tau/commit/37290325354dfe9ebf5146cf04a9843ae37410f1))

## [0.8.0](https://github.com/samcharles93/tau/compare/v0.7.0...v0.8.0) (2026-07-02)

### Features

- add /skills-reload and /skills list commands ([#14](https://github.com/samcharles93/tau/issues/14),
  [#15](https://github.com/samcharles93/tau/issues/15))
  ([4df6350](https://github.com/samcharles93/tau/commit/4df63500a2296643aad38454cef4b58d83b96a8e))
- add live token/cost tracking to status bar
  ([d141ce5](https://github.com/samcharles93/tau/commit/d141ce5fb933a40cd98cbc45fe3fa58db3a6e06b))
- add metrics framework, tool call sanitization, and command registry
  ([722ef13](https://github.com/samcharles93/tau/commit/722ef139c90220157755bd313973e5dfe5577c99))
- implement slash command handling in chat input
  ([9a130eb](https://github.com/samcharles93/tau/commit/9a130eb90e3ca44b3442932ac4aca3e9621a9bfd))
- implement slash commands for model, session, and extensions
  ([7607305](https://github.com/samcharles93/tau/commit/76073058fafb7f35ae7873b001c1457a8a87d9fb))
- **tools:** add glob tool, read-before-write safety, per-tool timeouts, and various improvements
  ([7cda497](https://github.com/samcharles93/tau/commit/7cda49784cb7ded6a3aed614010b7a39352bf7f4))
- **tui:** add /skills list command
  ([f8aa372](https://github.com/samcharles93/tau/commit/f8aa3729489d8899eef523ccf6f35d43d47dab1f))

### Bug Fixes

- address Windows compatibility issues
  ([2335d4e](https://github.com/samcharles93/tau/commit/2335d4ee81077cfd7c791320643d62b4f405a322))
- inject commit hash and build date in version string
  ([8df8128](https://github.com/samcharles93/tau/commit/8df8128819bd0fa2022f3404bb2c0180f36903e7))
- migrate release workflow from Gitea to GitHub
  ([f45ebb3](https://github.com/samcharles93/tau/commit/f45ebb32371504413b4b90956c06c4651a975381))
- normalize fallback output paths with filepath.ToSlash for Windows compatibility
  ([25832a8](https://github.com/samcharles93/tau/commit/25832a87be3114c6774b8f55b7241dcd46ca8329))
- **skill:** validate Instructions length in skill validation
  ([0483bc1](https://github.com/samcharles93/tau/commit/0483bc1d7e28f3ccf9f12b61e87b85e71cccd023))
- **skill:** validate Instructions length in skill validation
  ([044bec7](https://github.com/samcharles93/tau/commit/044bec7e9349184023fadd36479dcb2838047aa2))
- skip os.Chmod on Windows where it's a no-op ([#9](https://github.com/samcharles93/tau/issues/9))
  ([c653b25](https://github.com/samcharles93/tau/commit/c653b25510ee6627bdc9da741f03b5fa78100215))
- skip Unix permission check in provider state on Windows
  ([ede7233](https://github.com/samcharles93/tau/commit/ede7233086b631d91f1709254e36e671820d7069))
- **tools:** add pure-Go fallback for find and grep on Windows ([#6](https://github.com/samcharles93/tau/issues/6),
  [#7](https://github.com/samcharles93/tau/issues/7))
  ([0735ca6](https://github.com/samcharles93/tau/commit/0735ca607c530a8f209f897d2ddb0ffd04df8e05))
- **tools:** pure-Go fallbacks for find and grep on Windows ([#6](https://github.com/samcharles93/tau/issues/6),
  [#7](https://github.com/samcharles93/tau/issues/7))
  ([19f0d66](https://github.com/samcharles93/tau/commit/19f0d66c9a85db76638d468bf77def37114ea89b))
- **tui:** remove dead duplicate skills-reload code from inline_chat.go
  ([9d04a91](https://github.com/samcharles93/tau/commit/9d04a915dff95e0739711785ef88d8803f693d0b))
- **tui:** resolve double-unlock and syntax errors in inline_events.go
  ([e7d5b52](https://github.com/samcharles93/tau/commit/e7d5b520661145d3771e2a6e6cfd7ee32347ff12))
- **tui:** resolve mutex deadlock and fatal double-unlock in handleEvent
  ([c51aa0d](https://github.com/samcharles93/tau/commit/c51aa0d49e0d0202e1179f37cfac683eabfe6e95))
- unify AllowedTools parser, normalize skill file paths, validate instructions length
  ([#12](https://github.com/samcharles93/tau/issues/12), [#13](https://github.com/samcharles93/tau/issues/13),
  [#16](https://github.com/samcharles93/tau/issues/16))
  ([ec97f04](https://github.com/samcharles93/tau/commit/ec97f04af930767946fd0df27ff348af8d651970))
- use os.UserConfigDir() for cross-platform config directory ([#8](https://github.com/samcharles93/tau/issues/8))
  ([b5e6a8b](https://github.com/samcharles93/tau/commit/b5e6a8b3c2e27d1de071842b44cf3c8864d68fa9))
- write startup diagnostics to file instead of stderr
  ([8482b2d](https://github.com/samcharles93/tau/commit/8482b2d8a3cf94d4efb602121896dec4217cca8b))

## [0.7.0](https://git.catlow.cloud/sam/tau/compare/v0.6.0...v0.7.0) (2026-06-29)

### Features

- enhance release workflow with release-please job and dependencies
  ([8fc4aac](https://git.catlow.cloud/sam/tau/commit/8fc4aac344bb595bae8cf861df58e260b4ec65ab))

### Bug Fixes

- ensure correct checkout reference in release workflow
  ([84c8592](https://git.catlow.cloud/sam/tau/commit/84c859257ba704537ec496aa2aafbdc98782c4b4))
- remove tag trigger from release workflow
  ([7ec05a3](https://git.catlow.cloud/sam/tau/commit/7ec05a383ab7bdb0fe29ba144f1bbecd3217d16c))

## [0.6.0](https://git.catlow.cloud/sam/tau/compare/v0.5.6...v0.6.0) (2026-06-29)

### Features

- track per-message timestamps in chat sessions
  ([a8db286](https://git.catlow.cloud/sam/tau/commit/a8db2862f953bc84974741c74b4aff7dbd0f1f9e))

### Bug Fixes

- ignore .pnpm-store to prevent goreleaser dirty state
  ([8905613](https://git.catlow.cloud/sam/tau/commit/890561395bbdb7c9de33704fa1cc2f80ad297dbb))
- test complete pipeline after label cleanup fix
  ([1eb948d](https://git.catlow.cloud/sam/tau/commit/1eb948d7302391f53233de44c99d30a3089293b5))

## 0.5.6 (2026-06-29)

First automated release. Established the release pipeline (release-please + GoReleaser on Gitea). No functional changes
to tau across 0.5.2–0.5.6 - these versions were CI bring-up.
