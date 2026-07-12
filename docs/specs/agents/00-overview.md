# Agent Processes: Overview and Decision Record

Status: agreed design, pre-implementation
Date: 2026-07-12
Decided by: Sam Catlow, via structured design interview
Pages: this directory is the authoritative spec for tau's agent-process architecture. If code and these pages disagree during implementation, raise it, don't silently drift.

## Vision

Tau's agents are all just agents. The user talks to one; it calls others. There is no privileged "main agent" code path and no separate "sub-agent" runtime: every tau process starts by instantiating an agent spec, and that spec is the identity of the process for its whole life. Sessions are created, forked and destroyed underneath that identity; the identity persists across all of them.

Agents run as separate OS processes. Delegation spawns a child tau process with its own spec, its own session, its own model, and a strictly attenuated toolset. State lives in the shared SQLite store (already WAL-mode); the wire between processes carries only control messages and streamed events. This is deliberate: the protocol is the asset, transports are pluggable, and the store is the data plane.

## Principles

1. **Everything is an agent.** The interactive entry point instantiates `tau.agent.md`, a real embedded spec resolved through the same path as every other spec. No agent is expressible only in code.
2. **Specs are identities, sessions are work.** A process resolves its spec once at startup and snapshots it. Sessions come and go under that identity.
3. **Processes are ephemeral executors.** Continuity lives in the store, not in a resident process. A follow-up to a finished child is a new process resuming the child's session.
4. **Capability only shrinks down the tree.** A child's effective toolset is the intersection of its spec and its parent's effective set. No spawn can widen capability. Widening means going back to the human.
5. **One coordinator, many surfaces.** Mode invocation (in-session) and process invocation (spawned) share the same coordinator turn machinery. TUI and WebUI render from the same bridge events. Drift between surfaces is a bug.
6. **The protocol is the asset.** The envelope is peer-shaped (from/to instance addresses) from day one. v1 carries it over stdio pipes; Unix sockets, TCP and discovery are transport swaps, not redesigns.
7. **Local first.** Everything stays on the user's machines and network. No cloud dependency is introduced by any part of this design.

## Decision record

Every decision below was put to Sam explicitly and agreed on 2026-07-12.

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Root process spec | Embedded built-in `tau.agent.md`, resolved via the normal Parse/Resolve path; `tau --agent <name>` swaps it; user/project files can override by name | Full symmetry; root agent is configurable like any other |
| 2 | Agent instance identity | `spec-name#instance-id` minted at process start | Parallel instances of one spec must be distinguishable on the wire and in the DB |
| 3 | Spec persistence | Snapshot resolved spec into `agent_instances` table at startup; sessions reference the instance row | Auditability (what exactly ran this session), immunity to mid-flight spec edits, doubles as the agent registry |
| 4 | Modes vs processes | Coexist; one spec format, invocation-time choice; both paths share the coordinator | compact/summarise stay cheap in-session; delegation gets isolation; no drift by construction |
| 5 | Model field | Enforced; optional `provider` added; unset inherits invoker's resolved pair, else global default | Per-agent models are the point of process agents; inheritance keeps cheap parents cheap |
| 6 | Model modes (tiers) | Single `model:` field accepts a tier name or a concrete model; tiers resolve via config `model_modes` at instantiation, tiers tried first | Specs stay portable across provider churn and inherit Sam's experience-tuned mapping |
| 7 | Tool capability flow | Strict attenuation: child effective = child spec ∩ parent effective; spawn may narrow, never widen | No privilege escalation down the tree, ever |
| 8 | Spawn gating | Spawning is a normal registry tool (`agent`), gated by `tools` lists and attenuation; depth capped by global default (2), spec may lower, raise only up to a config ceiling; parent stamps the child's depth | Reuses the existing permission mechanism; belt-and-braces recursion guard |
| 9 | Spawn targeting | All resolvable specs spawnable by default; `disable-model-invocation: true` (now enforced) opts a spec out of being an agent-tool target | Reuses the reserved field with its skills-mirroring semantics; user-invocable and spawnable stay independent axes |
| 10 | Invocation-style field | None added; `user-invocable`, `mode-switcher`, `disable-model-invocation` and unconditional CLI access cover the matrix | Lean format; the human at the CLI outranks the spec |
| 11 | Resource limits | Split: spec carries structural traits (`max-turns`, optional default timeout); spawn call carries token/cost budget and deadline | The spec knows the agent kind; the invoker knows the task size |
| 12 | Child context | Spawn parameter `context: fresh \| fork`, default fresh; fork uses CloneChatSessionState; `parent_session_id` always set | Clean delegation by default, full-context deep dives on request |
| 13 | Child lifetime | Exit after task; follow-ups resume the child session in a new process | Continuity in the store; no reaping, no idle daemons; Go startup is cheap |
| 14 | Transport v1 | stdio pipes, one JSON envelope per line | Zero discovery/cleanup/permissions surface; envelope unchanged when transports upgrade |
| 15 | Completion contract | Child's final text plus structured envelope: status, child session id, instance id, usage totals, partial output and reason on abnormal end | Parent model reasons over prose; harness keeps refs for resume, cost and UI linking; failures are data |
| 16 | Concurrency | Spawn blocks the calling turn; fan-out via the coordinator's existing parallel tool execution; background mode is a later additive flag | No task registry or notification plumbing in v1; N calls in one turn already run concurrently |
| 17 | Per-spawn model override | Allowed; precedence spawn param > spec > inherit | Same rationale as budgets; tiers keep overrides portable |
| 18 | Child visibility | Live compact state block per child in TUI/WebUI, full event stream forwarded through the bridge in agent-scoped envelopes, drill-down expands the transcript | State-not-log; nothing hidden; both UIs render identical data |

## v1 scope

In scope:

- `tau.agent.md` built-in and process identity at startup
- Spec format additions and newly enforced fields
- `model_modes` tier resolution in config
- `agent_instances` table and `sessions.agent_instance_id`
- The `agent` tool, spawn executor, attenuation, depth and budget enforcement
- Child process entry over stdio JSONL, the envelope message catalogue, AsyncAPI regeneration
- Completion contract, cancellation, orphan handling, cost accumulation
- TUI and WebUI child state blocks with drill-down
- Documentation and integration tests for all of the above

Deferred (tracked as backlog stories, not designed in detail here):

- Unix socket per instance and direct attach to a running child
- Background (non-blocking) spawns and resident children
- Lateral child-to-child messaging
- Cross-machine transport (TCP/WebSocket, mDNS discovery; lift patterns from p2pchat and nell-engine)
- Per-spec spawn allowlists (`agents:` field), if attenuation + depth prove insufficient
- Interactive approval escalation for toolset widening

## Glossary

- **Spec**: an `.agent.md` file (YAML frontmatter + Go text/template body) describing an agent kind.
- **Instance**: one running (or historical) agent process, addressed as `spec-name#id`, recorded in `agent_instances`.
- **Mode**: a spec entered in-session (slash command or Shift-Tab), running under the current process's identity. Modes do not create instances.
- **Child**: an instance spawned by another instance via the `agent` tool.
- **Envelope**: the discriminated JSON wrapper every wire message travels in, extending the existing bridge envelope with `from`/`to`.
- **Tier**: a named model mode (`fast`, `smart`, `deep`, user-definable) mapping to a provider/model pair in config.

## Page map

- `00-overview.md`: this page
- `01-agent-spec-format.md`: the spec file format, field reference, tiers, built-in changes
- `02-spawning-and-lifecycle.md`: identity, instantiation, the agent tool, attenuation, budgets, lifecycle and failure
- `03-wire-protocol.md`: envelope, message catalogue, stdio transport rules, versioning
- `04-storage-and-sessions.md`: schema changes, store API, data-plane rules
- `05-ui.md`: TUI/WebUI rendering of child agents
- `06-implementation-plan.md`: phased work breakdown mirroring the Linear stories