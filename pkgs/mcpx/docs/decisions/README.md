# Decisions

A decision record exists when two or more issues propose different answers to
one question and a choice made later would be a breaking change. Each record
says what the code already does, what each issue proposed, which option was
taken and why, and exactly what every affected issue has to change as a result.
A record that lists options without choosing one does not belong here.

They exist because #181 found eighteen collisions between themes, each between
issues written by people who could not see each other's work, and none with an
owner.

| # | title | status | date | issues |
| --- | --- | --- | --- | --- |
| [0001](0001-hook-vocabulary.md) | One hook vocabulary, and where a hook's output goes in the exec stream | accepted | 2026-09-30 | #81 #82 #112 #111 · #181 rows 2, 7, 13 |
| [0002](0002-autonomy-dial.md) | One autonomy dial, and a ceiling the daemon sets | accepted | 2026-09-30 | #111 #112 #113 #114 #110 · #181 rows 3, 5 |
| [0003](0003-declared-vs-enforced-capabilities.md) | What mcpx declares, and what it enforces | accepted | 2026-09-30 | #82 #177 · #181 row 10 |

## Status

- **proposed** — in a pull request, open for argument.
- **accepted** — merged. A record reaches `main` only by being accepted, so a
  record on `main` that says `proposed` is a bug.
- **superseded by NNNN** — a later record changed the decision. The old one
  stays, with a line at the top naming its replacement; the history of why a
  thing was decided is worth more than a tidy directory.

A change that contradicts an accepted record changes the record in the same
pull request, or it does not merge.

## Numbering

Four digits, in the order they were written. A number is never reused, even for
a record that was withdrawn before merging.

## Template

````markdown
# NNNN — the decision, as a statement rather than a question

```
number:   NNNN
status:   proposed
date:     YYYY-MM-DD
issues:   #the ones whose text changes because of this · #181 rows, if any
code:     the commit the file:line references were checked against
```

> Two to four sentences: what collided, and what this record settles.

---

## Context

What ships today, with file:line, checked against `code:` above. What each
issue proposes, quoted briefly. Anything found while looking that the issues
did not know — a delivery hole, a stale count, a line that has moved — goes
here, because the decision may depend on it.

## Options

### A — …

What it is and what it costs. Real alternatives only; an option nobody would
take is padding.

### B — …

### C — …

## Decision

The option, stated so it can be implemented without asking. Names, values,
defaults, the API if there is one, and the tests that prove it.

## Consequences

Per affected issue: what changes in it. Then what is now forbidden, and what
is deferred and to which issue.

## Mapping

| term | from | becomes |
| --- | --- | --- |
| every competing term | the issue or file it came from | the chosen one |
````
