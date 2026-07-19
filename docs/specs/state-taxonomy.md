# tui2 State Taxonomy

## Problem statement

tau's frontend has no formal taxonomy of state categories. Every new concurrent or modal
feature (plugin panels, parallel tool calls, overlays, bash mode) has invented its own
one-off mechanism instead of plugging into a shared one: an independent `atomic.Bool`, a
new nil-sentinel pointer field, a bespoke precedence check bolted onto an existing key
handler. The underlying need is for a small number of named categories - some states force
foreground, some allow background/async operation alongside a foreground state, and some are
purely owned by the application rather than the user.

This was first diagnosed against `internal/tui` (the original inline-chat engine): three to
four independent `atomic.Bool` flags racing across goroutines, a mutex-guarded field bag, a
separately-locked `panelsByID` domain, and an `OverlayStack` with its own mutex - four
independent synchronization domains, no shared taxonomy. That code is now legacy behind
`--legacy-tui`, kept only because it is being retired slowly and deliberately. **This
document does not propose changes to `internal/tui`.**

The active frontend, `internal/tui2`, is a Bubbletea (Elm-architecture) rewrite: a single
`model` struct (`internal/tui2/model.go`) mutated only inside one serialized `Update()`
loop, with no goroutines of its own - every asynchronous source is a `tea.Cmd` executed by
the Bubbletea runtime and delivered back as a `tea.Msg`. This structurally eliminated the
race conditions the old design had, but not the taxonomy problem: precedence and mutual
exclusion between roughly a dozen state-like fields (overlays, turn lifecycle, bash mode,
child-agent state, selection state) are still encoded as ad hoc, hand-written `if` chains
scattered across `dispatchKey`, `Esc` handling, `Ctrl+C` handling, and `View()`'s compositing
order - each reasoned about independently, each requiring a new overlay to touch three or
four places and remember the right manual "close my siblings" calls.

**Concrete evidence this is a live problem, not a hypothetical one**: `internal/tui2/model.go`
tracks "is a turn in flight" in two independently-maintained places - `inResponse bool` and
`agentState agentState` (an enum: `agentReady`/`agentThinking`/`agentProcessing`/
`agentRunningTool`/`agentStreaming`/`agentCancelled`/`agentError`). They are set together at
most call sites, but not all: the `sendResultMsg` error-path handler (model.go, the
`case sendResultMsg:` branch) sets `m.inResponse = false` on a failed send but never resets
`m.agentState`, so `agentState` is left stuck at `agentThinking` until the next turn
overwrites it. Two fields tracking one concept, with no mechanism forcing them to agree, is
exactly the failure mode a taxonomy is meant to prevent.

## The six categories

| # | Category | Meaning | Gating |
|---|----------|---------|--------|
| 1 | **Turn State** (foreground-exclusive) | Is a turn in flight, and in what phase | A single owned enum; gates whether a new plain-text submit runs now or queues |
| 2 | **Overlay State** (modal, exclusive or soft) | A UI affordance that wants priority over normal keys for as long as it's open | Precedence-ordered; at most one *exclusive* overlay active at a time |
| 3 | **Background-Async Ops** (declared, forced-background) | Operations explicitly declared to run *without* gating on Turn State | No gating on Category 1; each op self-guards only against concurrent copies of itself |
| 4 | **Child-Agent State** | State mirrored in from a genuinely separate OS process via the event bus | Its own status enum, split into live vs. terminal |
| 5 | **System State** (environment/application-owned) | Ambient bookkeeping reflecting the environment or the app's own internal accounting, not a user gesture | No user-facing gating |
| 6 | **Presentation State** | Pure view-model bookkeeping (cursors, selection, focus indices, collapse toggles) | No gating - always available regardless of what else is happening |

These definitions are deliberately written in frontend-agnostic terms. See
["Webui adoptability"](#webui-adoptability) below for why.

## Full field mapping

Every stateful field in `internal/tui2/model.go`'s `model` struct, one category each.

### Category 1 - Turn State
- `agentState` - the sole enum driving turn phase, and (since [Category 1 merge](#category-1-merge-phase-2-done)) the sole source of truth for turn-in-flight state.
- `inResponse` - `func (m *model) inResponse() bool`, derived from `agentState` rather than a second independently-maintained field (see the drift bug above, now fixed).
- `streaming`, `reasoning` - in-progress text/reasoning deltas for the active turn.
- `streamStartedAt` - when the streaming phase of the current turn began (tokens/sec estimate).
- `tools`, `committedGroups` - live and flushed tool-call data for the current/most recent turn.
- `committedReasoning` - completed reasoning blocks, still collapsible.
- `turnQueue` - prompts queued behind a running turn.
- `lastSubmit` - 300ms submit-debounce guard.
- `spinnerFrame` - working-indicator animation frame, driven while a turn is in flight.
- `steering` - **a modifier bit on Category 1, not its own category.** It only has meaning while `agentState` is `agentThinking`/`agentStreaming` - it overlays onto whichever turn phase is active rather than representing an independent state.

### Category 2 - Overlay State
- `activePrompt` + `promptQueue` - open interactive prompt (agent question / local UI flow) and its queue.
- `contextMenu` - open right-click menu.
- `diffViewer` - open "View diff" overlay.
- `childTranscriptViewer` - open child-agent transcript drill-down.
- `sessionTreeOverlay` - open Ctrl+O session navigator.
- `helpOverlay` - open `/help` overlay.
- The completions dropdown (`compToken`/`compSelected`/`compDismissed`/`compDismissedToken`) - the one *soft* overlay: it consumes only the keys it recognizes and falls through otherwise, unlike the six above which are exclusive.

All seven are nil-sentinel (or dropdown-visibility-sentinel) fields; dispatch precedence and mutual exclusion between them are now unified by [Category 2 mechanism](#category-2-mechanism-phase-3-done) below (`internal/tui2/overlay.go`) - the fields themselves are unchanged, only how precedence/exclusion is declared and enforced.

### Category 3 - Background-Async Ops
No dedicated fields of their own beyond their self-guard bool; see the [naming table](#category-3-naming-phase-4-done) below. `bashRunning`/`bashCallID` are the canonical, already-real example: a `!bash` submit gates only on `bashRunning`, never on `agentState`/`inResponse()` - a bash command and an in-flight chat turn are allowed to run concurrently, deliberately.

### Category 4 - Child-Agent State
- `childAgents` - per-tool-call-ID terminal-summary-or-latest-live-snapshot.
- `childMessages` - live per-call-ID transcript buffers for running children.
- `childAgentOrder` - insertion-order index for Tab-cycling (the map has no iteration order).
- `focusedChild` - index into `childAgentOrder` for keyboard nav (`-1` = none).

Deliberately its own category rather than folded into Category 3: unlike bash/refresh/provider-login (tui2-native `tea.Cmd`s), children are state *mirrored in* from a separate OS process with its own lifecycle spec (`docs/specs/agents/02-spawning-and-lifecycle.md`, `docs/specs/agents/05-ui.md`) and its own status vocabulary, formalized as `ChildAgentStatus` (see below).

### Category 5 - System State
- `sessionsFetchInFlight` - guards against more than one silent session-list prefetch in flight. Set via `startSessionsFetch`, cleared via `finishSessionsFetch` at each of its three call sites in `events.go` (`SessionsListedEvent`, `ChatRuntimeErrorEvent`, `ChatNotificationEvent`) - see the [Category 5 fix](#category-5-fix-phase-4-done) below.
- `focused` - terminal window focus, tracked via `tea.FocusMsg`/`tea.BlurMsg`. Reflects the *environment*, not a user gesture, which is why it belongs here rather than in Category 6 alongside `input`/`autoFollow`.
- `panels` - plugin-pushed panel views. Flagged as a **known gap, not addressed here**: `activePanel()` only ever returns the first key found in the map, so multiple concurrently-open plugin panels are not actually modeled today despite the map's shape suggesting otherwise. Candidate for a separate ticket.

### Category 6 - Presentation State
- `tools`-adjacent view toggles: `toolGroupCollapsed`, `toolCallsDefaultCollapsed`, `focusedTool` (`-1` sentinel, must be explicitly initialized - not the Go zero value), `expandedID`.
- Reasoning-adjacent view bookkeeping: `lastReasoningKey`, `reasoningKeySeq`.
- `input`, `inputCursor`, `history`, `historyIdx` (`-1` sentinel), `draftInput`, `autoFollow`.
- `pendingQuit` - the Ctrl+C double-tap quit-confirm timer. A standalone gesture timer, not folded into Category 1 - it only matters once Category 1 and Category 3 (bash) are both idle.
- Selection/drag: `viewportSel`, `inputSel`, `statusSel`, `toolsSel` (four independent `selectionState` instances, each with `-1` anchor/cursor sentinels), `dragRegion`.

**Known adjacent gap, not addressed here**: `focusedTool` (Category 6) and `focusedChild` (Category 4) are two different categories that already share an ad hoc mutual-exclusion contract today (`focusNextTool`/`focusNextChild` in `tools.go` manually clear each other). Unifying them into one `focusTarget` concept is a plausible future stretch item, flagged but not planned in this document.

## Category 2 mechanism (Phase 3, done)

Replace the current three to four scattered precedence ladders - `dispatchKey`'s overlay
chain, `Esc`'s own nested ladder, `Ctrl+C`'s own nested ladder, and `View()`'s separate
compositing list - with one declared order, expressed once:

```go
type overlayID int

const (
	overlayPrompt overlayID = iota
	overlayHelp
	overlayDiff
	overlayChildTranscript
	overlaySessionTree
	overlayContextMenu
	overlayCompletions // the one soft (non-exclusive) slot
)

// overlay is the uniform surface every Category 2 modal implements.
// Rendering is deliberately NOT part of this interface - see the note below.
type overlay interface {
	active() bool
	handleKey(m *model, msg tea.KeyPressMsg) (cmd tea.Cmd, consumed bool)
	close(m *model)
}

type overlaySlot struct {
	id        overlayID
	ov        overlay
	exclusive bool
}

// overlayPrecedence is the single declared dispatch order (internal/tui2/overlay.go).
func (m *model) overlayPrecedence() []overlaySlot { /* ... */ }
```

`dispatchKey` now calls `dispatchExclusiveOverlayKey`, which loops over `overlayPrecedence()`
and returns the first active exclusive slot's result outright. The soft completions slot keeps
its own explicit call site in `dispatchKey` (unchanged from before this mechanism), since it
must run strictly between "no exclusive overlay is open" and "normal keybindings," which a
single combined loop can't express - it stays registered in `overlayPrecedence()` purely for
`closeOtherExclusiveOverlays` and as living documentation of full precedence order. Mutual
exclusion becomes one method, `closeOtherExclusiveOverlays(keep overlayID)`, called by every
"open" function instead of each remembering which specific siblings to nil out.

**Rendering is not unified**, and deliberately so: `activePrompt` renders inline as flow
content (`computeLayout`/`renderPrompt`), while the other five render as floating compositor
layers composited directly in `View()`. `View()`'s existing compositing sequence is left as-is
rather than driven by `overlayPrecedence()` - it encodes paint/Z-order, a different concern
from dispatch precedence, and since `closeOtherExclusiveOverlays` keeps at most one of the five
compositor-based overlays active at a time, their relative draw order never mattered at
runtime anyway. Forcing `view()` into the shared interface would have meant either a rendering
model change for `activePrompt` or a leaky no-op method - neither justified by the actual
problem (scattered precedence + manual nil-outs), so it was dropped from the interface during
implementation.

The concrete fields (`activePrompt`, `contextMenu`, ...) stay exactly where they are - the
adapters are thin wrappers over existing fields, not a data migration. This is deliberately
a small, additive mechanism, not a rewrite.

**Note**: the `Esc` and `Ctrl+C` ladders are *not* part of this mechanism. They are
single-key fallback chains that span Categories 1/3/6 ("what does Esc mean when nothing
modal is open"), not modal precedence - forcing them into the overlay stack would
misrepresent them. They stay as hand-written ladders.

This mechanism must satisfy every existing precedence/exclusion test unchanged:
`TestActivePromptTakesPriorityOverOpenContextMenu`, `TestEnqueuePromptClosesOpenContextMenu`,
`TestCompletionsDoNotConsumeKeysWhileContextMenuOpen`,
`TestChildTranscriptLoadedEventPopulatesOverlay` /
`TestChildTranscriptLoadedEventStaleResponseIgnored`,
`TestChatRuntimeErrorEventClosesStuckOverlay` (all in `internal/tui2/model_*_test.go`).

## Category 1 merge (Phase 2, done)

`agentState` becomes the single source of truth. `inResponse` becomes a derived method:

```go
func (m *model) inResponse() bool {
	switch m.agentState {
	case agentThinking, agentProcessing, agentRunningTool, agentStreaming:
		return true
	default: // agentReady, agentCancelled, agentError
		return false
	}
}
```

The `inResponse bool` field is deleted. Every current read/write site (`input.go`,
`keybindings.go`, `events.go`, `completions.go`, `tools.go`, `model.go`) is migrated to call
the method instead of reading the field, and every site that *sets* `inResponse` is deleted
in favor of the adjacent `agentState` assignment already present at that call site - except
the `sendResultMsg` error path, which today sets `inResponse = false` with **no**
corresponding `agentState` reset; that path gets `m.agentState = agentReady` added, fixing
the drift bug cited above.

## `ChildAgentStatus` enum

Replaces the bare `string` status (`childAgentResult.status` in `internal/tui2/tools.go`,
`ChildAgentStateEvent.Status` in `internal/chat/types.go`) and the literal-string
`isChildTerminal` switch with a real closed type, matching the lifecycle already specified in
`docs/specs/agents/02-spawning-and-lifecycle.md` (`spawned → ready → working → exited`) and
the doc-comment on `ChildAgentStateEvent.Status` - today that comment is the *only* place the
six-value set is written down; nothing enforces it.

```go
// ChildAgentStatus is the closed set of states a spawned child agent can
// report, per docs/specs/agents/05-ui.md. "working" is the only live value;
// the rest are terminal.
type ChildAgentStatus string

const (
	ChildAgentWorking         ChildAgentStatus = "working"
	ChildAgentCompleted       ChildAgentStatus = "completed"
	ChildAgentFailed          ChildAgentStatus = "failed"
	ChildAgentCancelled       ChildAgentStatus = "cancelled"
	ChildAgentBudgetExhausted ChildAgentStatus = "budget_exhausted"
	ChildAgentTimedOut        ChildAgentStatus = "timed_out"
)

func (s ChildAgentStatus) IsTerminal() bool {
	switch s {
	case ChildAgentCompleted, ChildAgentFailed, ChildAgentCancelled, ChildAgentBudgetExhausted, ChildAgentTimedOut:
		return true
	default:
		return false
	}
}
```

A future status value added without updating `IsTerminal` fails to compile rather than
silently falling into "not terminal" through the old bare-string `default` case - closing the
"unrecognized future string" gap the string-based design had.

## Category 3 naming (Phase 4, done)

Recommendation: documentation plus a lightweight named registry, not a runtime enforcement
mechanism - a full scheduler would be over-engineering for the handful of known cases below.

| Op | Trigger | Self-guard | Deliberately NOT gated on |
|---|---|---|---|
| Bash command | `!cmd` submit | `bashRunning` | `agentState` / `inResponse()` |
| `/refresh` model discovery | command | none today (fire-and-forget) | `agentState`, `bashRunning` |
| `/provider login` OAuth polling | command | none today | `agentState`, `bashRunning` |
| Session-list prefetch | completions/session-tree open | `sessionsFetchInFlight` | `agentState`, `bashRunning` |
| Notification auto-clear timer | any `setNotification*` call | `notificationGen` counter | everything |

The fix here is *naming* the category so a future contributor can see, in one place, "these
operations are declared to run concurrently with an in-flight turn" - not mechanizing it,
which would reintroduce a "flag nobody remembers to check" problem in reverse.

## Category 5 fix (Phase 4, done)

Fold `sessionsFetchInFlight`'s three independent clear-sites into one setter/clearer pair:

```go
func (m *model) startSessionsFetch()  { m.sessionsFetchInFlight = true }
func (m *model) finishSessionsFetch() { m.sessionsFetchInFlight = false }
```

A new failure path added later still has to remember to call `finishSessionsFetch()` - the
same cognitive load as today - but there is now one grep-able name instead of a bare bool
assignment scattered across three files.

## Webui adoptability

`internal/webui/src/stores/session.ts` already independently reimplements overlapping
concepts in TypeScript/Pinia - its own `status = ref('idle')` (Category 1) and its own
`activePrompt = ref<InteractivePrompt | null>(null)` (Category 2). Per this project's
architecture principle that the TUI and web UI must not structurally drift from each other,
the six category *definitions* above are written in frontend-agnostic prose specifically so
`session.ts` can eventually be audited against the same vocabulary without a second design
pass - only the "mechanism" sections (`overlayPrecedence`, the `agentState` merge, the
`sessionsFetchInFlight` fix) are Go-specific implementation detail. The one concrete artifact
worth mirroring in TypeScript now, since it is the shared wire type both frontends already
consume identically: a `ChildAgentStatus` union type plus an `isTerminal()` helper matching
the Go enum above. No other webui changes are proposed by this document.

## Out of scope

- `internal/tui` (legacy, behind `--legacy-tui`) - not touched by any part of this taxonomy.
- A deep `internal/webui` refactor - not part of this initiative.
- The `panels`/`activePanel()` multi-panel gap (Category 5) - flagged as a candidate ticket, not fixed here.
- `focusedTool`/`focusedChild` ring unification (Categories 4/6) - flagged as a future stretch item, not planned here.

## Phased implementation plan

| Phase | What | Depends on | Status |
|---|---|---|---|
| 0 | This document | - | Done |
| 1 | `ChildAgentStatus` typed enum (`internal/chat/types.go`, `internal/tui2/tools.go`, `internal/tui2/events.go`, `internal/agent/tools/agent.go`, `internal/app/child.go`) | Phase 0 | Done |
| 2 | `agentState`/`inResponse` merge, including the `sendResultMsg` bug fix | Phase 0 | Done |
| 3 | Category 2 overlay-precedence mechanism (new `internal/tui2/overlay.go`) | Phase 0 (sequenced after Phase 2 to avoid churn on the same files, not a hard dependency) | Done |
| 4 | Category 3 naming table (doc-only) + Category 5 `sessionsFetchInFlight` setter/clearer | Phase 0 | Done |

Phase 2 note: `inResponse` is now `func (m *model) inResponse() bool` (`internal/tui2/statusbar.go`),
derived from `agentState`. The `sendResultMsg` error-path bug is fixed (`internal/tui2/model.go`'s
`case sendResultMsg:` now resets `agentState`, not a separate field) and covered by
`TestUpdateSendResultMsgError` in `internal/tui2/model_test.go`.

Phase 3 note: implemented as designed except for the rendering-unification refinement described
above (View() left as-is, `view()` dropped from the interface). Wiring every "open" function
through `closeOtherExclusiveOverlays` also tightened mutual exclusion beyond what existed before:
previously each open site only cleared whichever sibling had caused a problem in the past (usually
just `contextMenu`), so e.g. a prompt arriving while a diff viewer was open left the diff viewer
state technically still set, merely shadowed by `activePrompt`'s higher dispatch precedence rather
than actually closed. This is now closed consistently everywhere, covered by
`TestEnqueuePromptClosesOtherExclusiveOverlays` in `internal/tui2/model_overlays_test.go`.

Each phase is independently shippable and revertable. Phases 2-4 are fully specified above so
they can be picked up as separate future sessions without re-deriving the design.
