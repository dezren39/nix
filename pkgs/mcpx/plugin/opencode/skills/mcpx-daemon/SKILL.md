---
name: mcpx-daemon
description: Use when an mcpx tool result says more than one daemon matches this directory, when the user asks which mcpx daemon is being used or wants to change it, or when mcpx answers with servers that belong to another project. Covers inspecting the candidates, recommending one, and letting the tool set it.
---

# Choosing which mcpx daemon answers

mcpx runs one daemon per configuration. Two projects, two daemons; a second
checkout of the same project, two daemons; a build under test alongside a
released one, two daemons. That is correct — it is what keeps one project's
servers out of another's — and it means "which one?" is sometimes a real
question.

**Nothing is blocked while it is unanswered.** The plugin picks the best
candidate, works, and says which one it used on every result. This skill is
for improving that guess, not for unblocking anything.

## The flow

1. **`mcpx_daemon_status`** — every daemon running, best first, with an index.
2. **Reason about it, out loud.** Say which one you think fits and why.
3. **`mcpx_daemon_select`** with that index. The user is asked to confirm.

You never type a socket path. `mcpx_daemon_select` takes the index from the
status listing and sets the socket itself, so the worst you can do is
recommend the wrong daemon — which the user sees and can decline.

## Reading the listing

```
* [0] nix
      config:   /Users/x/.config/nix/.mcpx.json
      socket:   unix:///Users/x/.local/state/mcpx/daemon-8f2c1a.sock
      started:  2026-09-29T09:14:02Z (3h ago)
      version:  0.1.0
      servers:  4
      sessions: 2
      live:     3
```

`*` marks the one this session is using now.

In order of how much it tells you:

| field | what it decides |
| --- | --- |
| **config** | the only field that says which *project* this daemon is for. A config path that is an ancestor of the working directory is almost always the right answer. |
| started | "the one I started for this work" — useful when two configs are equally plausible |
| servers, live | which one is actually doing something |
| sessions | which one other agents are already attached to. Taking a daemon away from them is rude; stopping it is worse |
| version | a daemon left over from an older build. Usually the one to stop, not the one to pick |

## Recommending

Say the reasoning, not just the answer:

> Two daemons are running. `[0] nix` loads `/Users/x/.config/nix/.mcpx.json`,
> which is the config above this directory, and has 4 servers. `[1] scratch`
> is for `/tmp/scratch`, started ten minutes ago with no servers live — it
> looks like a leftover. I suggest `[0]`. Shall I select it?

Then call `mcpx_daemon_select` with `candidate: 0`.

## How long it should stick

`mcpx_daemon_select` takes `remember`:

| value | lasts | use when |
| --- | --- | --- |
| `session` (default) | this session | you are unsure, or this is a one-off |
| `until-gone` | until that daemon stops answering | it is the right daemon *while it is running* |
| `indefinite` | this directory, until changed | this project always uses this daemon |
| `permanent` | written into the mcpx config | every tool on this machine should use it |

Default to `session` unless the user says otherwise. A remembered wrong answer
is much more annoying than a wrong answer that expires.

## Stopping the others

`stopOthers: true` shuts every other daemon down after selecting. It is
frequently what the user actually wants — one stale daemon from an old build
is a common cause of "why is this server missing?".

It is also destructive: every session attached to those daemons loses its
pooled servers, and a stateful server such as a browser dies with them. Check
the `sessions` count first, and do not pass it unless the user asked for it in
those terms.

## If no daemon is running

`mcpx_daemon_status` says so. Nothing to select; the user starts one with
`mcpx daemon`, or points the plugin at a remote one with
`MCPX_DAEMON_ENDPOINT`. Do not keep calling status hoping it changes.

## Undoing

`mcpx_daemon_forget` drops the remembered choice for this directory and lets
discovery decide again. Reach for it when a remembered daemon turns out to be
the wrong one, rather than selecting a different one on top of it.
