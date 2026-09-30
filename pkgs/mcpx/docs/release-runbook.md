# Release runbook

How `docs/story.md` stays true. `README.md` is a symlink to it, so this is the
first thing anyone reads about mcpx.

The file has two halves and they have different rules.

## The two halves

**Above `<!-- table-of-contents-marker -->` is the story.** One agent, one
prompt, one script, with the exact bytes it saw. It is the whole interface in
one read. It is *closed by default*: fix it, do not grow it.

**Below the marker is the feature list.** It is *open by default*: any commit
that lands on `main` may edit an entry or add one. It grows forever and that is
fine, because nobody reads past the part that answers their question.

## Rules for the story

Change it only to:

- correct something that is now false — a flag renamed, output changed, a
  measured number that has moved;
- replace a quoted block whose source changed. Every fenced block in the story
  is real output or a real file. Re-run the command, paste the result.

Do **not** add a new capability to the story. A new capability gets an entry
below the marker. The story stays one prompt long.

Extending the story needs explicit sign-off from the repository owner. If a
change would make the walk longer or add a second scenario, open it as a
question first.

When a quoted block changes, verify it rather than editing by hand:

```bash
mcpx ls
mcpx search screenshot click
mcpx types chrome_devtools | head -20
mcpx scripts
mcpx run shot-flow
```

The instruction file quoted in full is `AGENTS.md`. If that file changes, the
quote and the line-by-line breakdown after it both change.

## Rules for the feature list

Ordering is meaning. Roughly:

1. **Core** — what mcpx is for. Zero-context discovery, pools, the client.
   These change rarely and are read first.
2. **Standard** — shipped, supporting. Scripts, config, ops, packaging.
3. **Proposed** — wanted, not built. Everything under the `## Proposed` rule.

New shipped work goes after the last `standard` entry unless it is genuinely
core. New wants go under `## Proposed`. Within a release, append in merge order.

### Entry format

A small addition can be a heading and two links:

```markdown
### [feat(a11y): support for Teletypewriter](../docs/tty.md) #123

2026-10-02T11:30:00-05:00
```

Anything larger opens with a key block:

```markdown
## Session-isolated process pools

​```
created:      2026-09-25T18:30:00-05:00
last-updated: 2026-09-27T05:05:00-05:00
increment:    4
status:       core
tags:         area:pools, area:concurrency
description:  one sentence, present tense, what it does
​```

Prose, examples, the reasoning that is not obvious from the code.

2026-09-27T05:05:00-05:00
```

Required keys: `created`, `last-updated`, `increment`, `status`, `description`.

Optional: `owner`, `creator`, `tags`, `dek`, `note`, `editors-note`, `links`.

`increment` is a counter of commits on `main` that touched the section. It does
not have to be exact; it has to be non-decreasing. Bump it with every edit.

`tags` are Datadog-style: bare tags (`area:pools`) and key-value labels
(`platform:nix`, `cost:context`). Reuse existing keys before inventing one —
current keys are `area`, `platform`, `cost`.

`status` is one word:

| status | meaning |
| --- | --- |
| `core` | mcpx is not mcpx without it |
| `standard` | shipped and supported |
| `candidate` | shipped, may still change shape |
| `alpha` / `beta` | shipped behind a flag or unstable |
| `proposed` | wanted, not built. Say so in the prose too |
| `partial` | some of it works; the entry says which part |
| `deprecated` | works, will be removed. Link the replacement |
| `broken` | known broken, not yet removed |
| `retired` | removed. Struck through, or moved to `docs/archive.md` |

**Every top-level entry ends with its `last-updated` timestamp on its own
line**, RFC 3339 with offset. It duplicates the key block on purpose: the key
block is for machines, the trailing line is for whoever is scrolling.

### Removing things

Never delete an entry. Either strike the heading through:

```markdown
## ~~Old thing~~
```

and set `status: retired` with a note pointing at the replacement, or move the
whole entry to `docs/archive.md` and leave a one-line stub linking to it.

Heavy refactors move the old text to the archive too. The archive is append-only
and has no ordering rules.

## Per-commit

Any commit that lands on `main` and changes behaviour:

1. Edit or add the entry below the marker.
2. Bump `increment`, set `last-updated`, update the trailing timestamp.
3. If it changed something the story quotes, re-run the command and paste the
   real output.
4. If it changed `AGENTS.md`, update the quote and the breakdown.

A commit that only changes behaviour covered by an existing entry does not need
a new one; it needs that entry updated.

## Per-release

1. Confirm every entry touched since the last release has a current
   `last-updated` and a bumped `increment`.
2. Re-run the story's commands and diff against what is quoted. This is the
   real check — the story is the only part that can silently rot.
3. Move anything now `retired` into `docs/archive.md`.
4. Promote `proposed` entries that shipped: change `status`, move them up into
   the shipped run, set `created` to when the work landed rather than when it
   was first wanted.
5. Re-read the first screen. If the first thing a newcomer sees is no longer
   the most important thing, reorder.

## Checks

```bash
cd pkgs/mcpx
gofmt -l .
go vet ./...
go test ./... -count=1
MCPX=./bin/mcpx scripts/stress.sh      # concurrency, leaks, orphans
MCPX=./bin/mcpx scripts/bench.sh       # latency, against lootbox
(cd ../.. && nix build .#mcpx)         # unit suites in the sandbox
```

`README.md` must stay a symlink to `docs/story.md`:

```bash
test -L README.md && readlink README.md   # -> docs/story.md
```
