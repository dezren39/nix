# SidePulse: what this repo adds on top of upstream

The `sidepulse` package here is built from `github:inteliwear/sidepulse` with an
ordered patch stack, listed in `pkgs/sidepulse/default.nix`. The file name says
where each patch comes from:

| File name | Source |
| --- | --- |
| `sidepulse-pr-N-*.patch` | upstream pull request inteliwear/sidepulse#N (14, 17, 26 open; 28, 30 closed unmerged). #29 (idle LEDs off) was dropped 2026-10-02: the same result is the `immediate-off` idle style in settings |
| `sidepulse-dezren39-nix-N-*.patch` | local, no upstream counterpart; documented in dezren39/nix#N |

The local patches are #328 (fixed-path LED writer helper), #329 (red error
signal, error latch, completion pulse, concurrency tempo), and #330–#333, which
this page covers along with the live configuration they work with. Until
2026-10-01 the local patches were also named `sidepulse-pr-31` … `-36`, numbers
that belong to unrelated upstream PRs. Porting history: dezren39/nix#327.

For the hardware constraints every LED change works within, see
[`sidepulse-leds.md`](./sidepulse-leds.md).

## The problem being solved

Out of the box the device answers one question — *what is the single most
urgent thing happening?* — and answers it for a few seconds at a time. Three
things follow from that which these patches address:

1. The working animation dominated. An agent is *working* almost all the time,
   so a cyan roll on a 2-LED device read as a permanent blue blink, and the
   states that actually needed attention were drowned out.
2. Signals expired on a clock. A failure or a permission prompt aged out after
   an hour whether or not anyone had seen it.
3. Only one signal could be shown. With errors and prompts no longer expiring,
   several are routinely true at once, and priority alone discards the rest.

## dezren39/nix#330 — hold the completion until it is seen

`patches/sidepulse-dezren39-nix-330-completion-hold-until-seen.patch`

Upstream treats completion as a 4-second pulse (`COMPLETION_PULSE_SECONDS`).
If you looked away for those four seconds, the run may as well not have
finished.

This holds the green signal until the user is **visibly back at a terminal**,
with a five-minute ceiling so an unattended Mac does not flash green all night.

New module `src/sidepulse/presence.py`. Two macOS APIs, no extra permissions
and no new dependencies — `pyobjc-framework-Quartz` and `-Cocoa` were already
required:

| API | Question answered |
| --- | --- |
| `CGEventSourceSecondsSinceLastEventType` | seconds since the last keystroke or mouse movement |
| `NSWorkspace.frontmostApplication` | which application owns the screen |

**Acknowledgement requires sustained presence**: 2.5 s of continuous input with
no gap over 1.5 s, while a terminal or editor is frontmost. The gap tolerance
is deliberately *below* the sustain window, and that is a correctness
constraint rather than a preference — if a silence longer than the sustain
window were tolerated, a single keystroke followed by an empty room would
satisfy "sustained", because the tally would start on that one event and
nothing afterwards would clear it. `effective_gap_seconds` enforces the
ordering even against a hand-edited settings file.

### Known limit

Only **application** granularity, never the window or tab. Window titles of
other processes require Screen Recording on macOS 15+; measured on this machine
at macOS 26.5.1, `CGWindowListCopyWindowInfo` returned 18 windows with **0**
readable titles. So "the terminal is in front" is knowable; "the tab where that
run finished is in front" is not.

### Degradation

If either reading is unavailable — headless, no window server, permission
starved — `observe()` returns `False`, never `True`. An unknown answer can
never be mistaken for a present user. Set `completion_hold.enabled: false` to
revert to the original 4-second pulse.

| Setting | Default |
| --- | --- |
| `completion_hold.enabled` | `true` |
| `completion_hold.max_seconds` | `300` |
| `completion_hold.sustained_input_seconds` | `2.5` |
| `completion_hold.extra_bundle_ids` | `[]` |

22 terminal and editor bundle ids are recognised, plus prefix matching for
`com.jetbrains.*` and `com.todesktop.*` (Cursor's generated id).

## dezren39/nix#331 — failures and prompts stop expiring

`patches/sidepulse-dezren39-nix-331-errors-and-prompts-persist.patch`

Three separate defects, all in the same area.

### Signals addressed to a human no longer age out

`BLOCKED_ERROR` and `WAITING_FOR_INPUT` are exempt from staleness. Every other
mode describes the machine, and for those a timeout is honest — a session that
last said "working" an hour ago is not working any more. A failure and a
prompt are questions put to a person, and an hour of being ignored does not
answer either one.

The rule is applied at **both** places staleness is decided (`status_is_stale`
and `AgentMonitor.is_stale_status` are independent copies); a test asserts they
agree, so a fix to one that misses the other cannot ship.

### A failure is no longer erased by the idle that follows it

Every harness reports the error and then, within the same second, reports the
session idle — `processor.ts:633` publishes the error, `:638` sets
`{type:"idle"}`. The collector keeps only the newest record per session and the
status bar samples every 15 s, so both landed in one sample, the idle won, and
a failed run displayed as a **clean finish**. The error latch could not help:
it only ever sees the sampled row.

Going quiet is not recovering, so a stop no longer overwrites a failure.
Genuine activity — a new prompt, a tool call, a permission request — still
clears it, which is what keeps this from latching forever on its own.

### Error capture widened

Researching OpenCode's event bus found two gaps:

| Gap | Fix |
| --- | --- |
| `StructuredOutputError` never publishes `session.error` at all — it only sets the assistant message error (`prompt.ts:1310`) | subscribe to `message.updated` where `info.role === "assistant"` and `info.error` is set |
| `session.next.step.failed` / `session.next.tool.failed` — a second session runner that does not use `session.error` | both forwarded, mapped to `StopFailure` |

Two errors are deliberately **not** red, which matters now that red is
permanent:

- **`MessageAbortedError`** — you pressing escape. An instruction, not a failure.
- **`ContextOverflowError` on `session.error`** — fires for both the fatal and
  the recoverable case, and recoverable (auto-compaction) is far more common,
  so every long session would have latched red. Only the fatal variant also
  marks the assistant message, so it is ignored on `session.error` and caught
  on `message.updated`.

### Closed sessions release their signals

Since these no longer expire, they need releasing when the session goes away.
Two distinct cases:

| Case | Signal | Handling |
| --- | --- | --- |
| Session deleted in-app | `session.deleted` (`session.ts:622`) | mapped to its own `SessionDeleted` event; the row is **removed**, not transitioned, along with its subagent rows |
| Terminal closed, process killed | *nothing is emitted at all* | the plugin stamps `agent_pid` on every payload; `os.kill(pid, 0)` decides liveness |

`SessionDeleted` is deliberately distinct from `SessionEnd`: a failure is
designed to survive a completion, but nothing should survive the session it
belonged to being deleted.

Liveness is one-sided by design. **Unknown counts as alive** — hook-based
providers (Claude, Codex) run short-lived subprocesses whose pid would describe
the hook, not the agent, so they keep exactly the behaviour they had. A
recycled pid makes a dead session look alive and the row lingers, which is the
old behaviour and merely a missed improvement; the reverse error would throw
away a prompt that matters.

### Still not caught

`skill/index.ts:114` and `plugin/index.ts:140` publish `session.error` with
**no `sessionID`**. The plugin drops those (`if (!payload.session_id) return`)
and sidepulse keys everything by session, so a skill-load or plugin-load
failure will not light red. Fixing it means inventing a synthetic session id.

## dezren39/nix#332 — several signals at once

`patches/sidepulse-dezren39-nix-332-multi-signal-composition.patch`

New module `src/sidepulse/compose.py`. Composes one program carrying every live
signal instead of only the winner. Ten modes over a **ranked signal registry
with demand/ambient tiers** — seven static, two host-driven, plus `priority`
which is the historical behaviour. `beacon` is the default.

Full detail in [`sidepulse-modes.md`](./sidepulse-modes.md); the hardware
limits they work within are in [`sidepulse-leds.md`](./sidepulse-leds.md).

Three things worth recording here because they are design decisions rather than
implementation:

**Modes never name a signal.** They ask for the ranked list and the tier. "Error
and ask behave differently from done and busy" is true but is not a fact about
errors — it is a fact about whether the signal is asking the user to act.
`TIER_DEMAND` and `TIER_AMBIENT` say that directly, so a fifth signal needs no
change to any mode.

**Arrivals are a second write, not a cleverer program.** A newly-arrived signal
plays its own gesture once before joining the rotation, using the DSL's bounded
`repeat N`. The format allows exactly one `repeat` per program, so "play once
then loop the composition" is not expressible — hence a timer. Every arrival is
queued rather than collapsed to the most urgent, because several routinely
arrive in one snapshot and the quieter ones would otherwise never be seen to
arrive at all.

**A held completion must not cover a mode addressed to the user.** Once prompts
stopped expiring, a green hold could sit on top of a live permission prompt for
its whole duration — and with the ceiling removed, that duration is now
unbounded, so the exemption matters more than it did. Note this cannot be expressed with
`MODE_PRIORITY` — `WORKING` ranks *above* `COMPLETED` there, and overriding
working sessions is the entire reason the completion signal exists. The
exemption is specifically the modes waiting on a person.

### Composition changes an existing guarantee

The error latch's "red outranks everything" lives in `led_mode_for_aggregate`,
which only governs the single-signal path. In a composed mode a failure is one
voice in the rotation rather than the whole device. That is the point of
composing, but it is a real change to a previously absolute rule, and arrivals
are what restore urgency its moment.

## dezren39/nix#333 — a colour has to mean what it says

Patches #330–#332 made signals persist and made several of them visible at once.
That was the right direction and it exposed what was underneath: the rules
deciding *which* signal is true were much looser than the display built on top
of them. A signal that persists until a human retires it has to be right the
first time, because a wrong one no longer expires — it becomes the state of the
device.

Observed on a live install: a dead subagent had held the device red for
**46 hours**, over a session that had finished successfully two days earlier.
Green had effectively never appeared.

### Red was not failure

Five separate rules lit `BLOCKED_ERROR` for things that are not failures.

| Was red | Actually |
| --- | --- |
| tool `exit_code != 0` | `grep` with no match exits 1; so does a failing test, which is the point of running it |
| tool output containing `traceback` | a substring search over tool text — reading a file that mentions one matched |
| tool `interrupted: true`, `turn.aborted`, state `cancelled` | the user pressed Esc; `Interrupt` already meant `IDLE_READY` |
| `thread.state.changed` → `exited` / `closed` | the session closed; `session.exited` reported the same fact as a plain stop |
| `account.rate-limits.updated` | a wait that clears itself, with nothing for a human to fix |
| `session.next.tool.failed` | one tool call failing inside a healthy turn |

Red now means one thing: **the harness said this session cannot continue** —
`session.error`, `session.next.step.failed`, `runtime.error`, a failed terminal
state, or a denied permission. A failed *tool call* keeps its distinct event
name for the menu and the history, and no longer colours anything.

SidePulse failing to read its own monitor also painted `BLOCKED_ERROR`, which
was the same red — and latched the same way — as a dead run. There is no colour
for "the status bar is broken", and inventing one out of the agent vocabulary
is worse than saying nothing, so that path now leaves the display untouched.

### A latched signal has to stay retirable

`AgentStatus.to_dict` dropped `agent_pid`, `parent_session_id` and
`last_user_prompt_at`. `latest.json` is reloaded on every status-bar start, so
a row that survived that round-trip came back with no process to test for
liveness, no ancestor to inherit a prompt from, and no record of ever having
been prompted. Since neither `BLOCKED_ERROR` nor `WAITING_FOR_INPUT` expires on
a clock, **one restart made such a row immortal**. That is the 46-hour red: all
three of #331's escape hatches had been quietly discarded by a serialiser.

The error latch also kept subagent rows, and it was the only reader that did —
`aggregate_status`, `menu_statuses`, `CompletionWatcher` and
`active_signals_for` all drop them, and it is the one with absolute precedence
over the display. A subagent cannot be prompted, is not addressable from the
menu, and routinely orphans mid-run. `completion.py` already excludes them from
the other direction for the same reason; this makes the two agree.

### Green was an event dressed as a state

`done` was shown whenever a `COMPLETED` row was still fresh, which is for
`completed_visible_seconds` — twenty minutes. So the hold that exists to decide
when the news has been read was overruled by a timer it knows nothing about:
green survived its own acknowledgement by a quarter of an hour, then came back
on the next refresh. Green now tracks the completion signal and nothing else.

### Nothing is dismissed without an act

- The five-minute ceiling on the hold is gone. A run that finishes while you
  are away is exactly the one worth signalling; retiring the news because it
  got old guarantees you miss the completions that mattered most.
- Presence is a key press, a mouse button or a scroll. It was
  `kCGAnyInputEventType`, which counts *mouse movement* — a cat on the desk or
  a window animation under the cursor acknowledged a completion nobody had
  seen.
- The timed 4-second pulse survives only where presence genuinely cannot be
  read, and that is now decided by trying to read it rather than by a setting.

## Current configuration

Device is a SidePulse Dot (2 LEDs) at `/Volumes/PulseDot`, brightness 255, DND
off. Live settings in `~/.config/sidepulse/agent-monitor/settings.json`.

### Solo animations — one signal live

| Mode | Animation | Appearance |
| --- | --- | --- |
| `working` / `tool_running` / `long_task_progress` | custom heartbeat | dark 5 s, then one purple breathe — every ~6.6 s |
| `waiting_for_input` | `amber-pulse` | 1.6 s breathe, never fully dark |
| `blocked_error` | `error-red` | blink-blink then 700 ms pause |
| `completed` | `success-flash` | 110 ms flash every ~790 ms |
| `idle_ready` / `unknown` | `immediate-off` | dark |

The working heartbeat:

```text
off 5s none
#8800FF 1.6s pulse
repeat
```

It used to be two 60-second holds — one breath every two minutes. That is dark
**97% of the time**, and it meant the device said nothing at all during the
state it spends most of its life in: you could not tell "working" from "off",
so the absence of a signal carried no information and nothing else could be
read against it. Five seconds keeps the gesture sparse enough to sit beside
while making the state legible at a glance.

**The program text is the clock.** It is written when the device enters a
working state and restarts only when it leaves. Only `working` is set in
settings — the loader propagates it to `tool_running` and
`long_task_progress`, so all three produce byte-identical text and a tool
starting or stopping mid-job does not restart the cycle.

Alert modes are pinned to their current values rather than left implicit, so an
upstream change to `default_agent_animation_id` cannot quietly reintroduce
noise into the only states that light up.

### Composed display — two or more signals live

Default mode is **`beacon`**. Signal colours are `#FF0000` red, `#FF7A00`
amber, `#00FF66` green, `#8800FF` purple; per-LED correction is
`led_gain [0.48, 1.0]` with green at 0.876 on the front.

All of it is documented in [`sidepulse-modes.md`](./sidepulse-modes.md),
including why the amber moved, why nothing runs at full brightness, and why the
correction sits on the front LED rather than the back.

### Precedence

1. **Red** — a latched failure outranks everything.
2. **Amber** — beats green; a prompt asks you to act, a completion is news.
3. **Green** — beats all work states.
4. **Dark**.

Two things borrow the LEDs regardless: the lid open/close animations, and a
7-second battery preview on power change.

### What clears each

| Colour | Clears on |
| --- | --- |
| Green | a click, scroll or keystroke sustained 2.5 s with a terminal frontmost; being outranked |
| Amber | the session reporting anything else; session deleted; process dies |
| Red | prompting that session; "Acknowledge errors"; "Clear agents"; session deleted; process dies |

**Nothing clears on time passing.** The five-minute ceiling on green is gone,
and so is the twenty-minute window during which a finished row kept repainting
green after it had been acknowledged. Green is now the completion *signal*
rather than "a finished row is still in the monitor", which is what made it an
event you could miss in one direction and get stuck with in the other.

Presence means a key press, a mouse button or a scroll — never mouse
*movement*. The pointer drifting is not an act, and counting it retired
completions nobody had looked at.

## Verification

Every patch is checked by `nix build .#sidepulse` running the upstream suite
plus the tests each patch adds. Current totals: **665 passed, 6 skipped, 34
deselected**, with no additions to `disabledTests` — the new tests run in the
sandbox unmodified because their decision logic never touches AppKit.

| Patch | Tests added |
| --- | --- |
| 33 | `tests/test_completion_hold.py` |
| 34 | `tests/test_awaiting_human.py` |
| 35 | `tests/test_multi_signal.py` |
| 36 | additions to `tests/test_led_signals.py`, plus rewrites of the four assertions that encoded the old rules |

`checks.aarch64-darwin.sidepulse-wrapper` additionally verifies the CLI wrapper
and hook-install idempotency.
