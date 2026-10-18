# Implementation plan template

Name the file `<short-name>.md`, or `<UTC datetime>_<short-name>.md` when order matters.

```markdown
# <Title>

- Status: draft | in-progress | done | abandoned
- Roadmap item: <link>
- Design: <link, or "none yet">
- Issues: <links>

## Goal

One paragraph: what is true when this is done that is not true now.

## Scope

In: what this plan changes.
Out: what it leaves alone, and where that is tracked.

## Steps

Each step is small enough for one PR and says how to tell it is done.

1. **<Step name>.** What changes. Done when: <check a test or CI job can run>.
2. ...

## Tests and evidence

The tests that must exist, and what each one proves. Name the ones that fail before the change.

## Rollout and rollback

How it ships, what a user must do, and how to undo it.

## Risks

What could go wrong and what limits the damage.

## Progress

- <UTC date>: <what happened, with the PR or issue>
```
