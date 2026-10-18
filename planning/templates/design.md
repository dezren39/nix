# Design record template

Name the file `<UTC datetime>_<kind>_<name>.md`, for example `2026-10-18T14-30-00Z_design_blob_streaming.md`.

```markdown
# <Title>

- Status: draft | accepted | superseded by <link>
- Date: <UTC datetime>
- Roadmap item: <link to the row, or "none">
- Issues: <links>

## Intent

What was asked for or noticed, in plain words. Who is affected and what they see today.

## Design

What the system does after this change. Name the modules, formats and invariants that change.

## Decisions

Each decision with the reason. Say who decided it when it was a product call.

## Alternatives considered

What else was possible and why it lost.

## Consequences

What gets easier, what gets harder, what must be migrated or kept compatible.

## Open questions

What is not decided. Each question names who or what settles it.
```

Append later updates as new sections that start with `## <UTC datetime> <Update|Clarification|Deviation|Discovery|Decision>:` and a one-line summary.
