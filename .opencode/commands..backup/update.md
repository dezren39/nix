---
description: Update AGENTS.md - review and refresh project instructions
agent: build
---

# Update AGENTS.md

Review and update the project's AGENTS.md file to ensure it remains accurate and context-efficient.

## Instructions

$ARGUMENTS

## Locate AGENTS.md

Search from pwd upward to filesystem root, then check `~/.config/opencode/AGENTS.md`. Use first found.

If found outside the repo and not in `~/.config/opencode/` or `~/`, **ask the user to confirm** before proceeding.

## If no specific instructions provided

Review the current AGENTS.md and verify it's up-to-date by checking:

### Core Details (always include in AGENTS.md)
- **Primary language/framework** - is this still accurate?
- **Core purpose** - one-line description still correct?
- **Essential commands** - is the primary quality check command listed (e.g., `just check`)?
- **Critical warnings** - any "never do X" rules still valid?
- **File structure** - does the tree reflect current layout?

### Context Efficiency Review
- **Inline vs reference**: Can any code examples be replaced with file links?
- **Command lists**: Is only the essential command shown, with a link to justfile/makefile for others?
- **Dependencies**: Are only core libraries listed, not exhaustive lists?
- **Redundant prose**: Can any explanations be tightened?

### Propose Changes as Questions

Do NOT make changes autonomously. Present findings using the question tool with Y/n options (custom input is always available for nuanced responses):

```
Proposed AGENTS.md updates:

1. [Structure] src/commands/ now has 3 new files - add to tree? (Y/n)
2. [Commands] `just check` renamed to `just verify` - update? (Y/n)
3. [Tighten] Error handling section is 15 lines, could be 3 - condense? (Y/n)
4. [Remove] "Store Access" code example duplicates pattern in store.rs:45 - remove? (Y/n)
5. [Add] New critical rule needed: never delete .env.example? (y/N)
```

Use `(Y/n)` for recommended changes, `(y/N)` for optional/uncertain ones.

After user responds, apply only the approved changes.

## If specific instructions provided

If the instructions modify or focus one of the review steps above (e.g., "pay extra attention to file structure" or "skip command review"), run the full review process with that adjustment applied.

Otherwise, implement the requested changes directly. **However, ask questions first if:**
- The change is major (removing sections, restructuring, changing critical rules)
- The request is ambiguous or could be interpreted multiple ways
- You need clarification on scope, intent, or priorities
- Any detail—major or minor—is unclear

After implementation:
- Verify the file remains context-efficient
- Ensure no critical rules were accidentally removed
- Confirm essential details (language, purpose, key commands) are still present

## Guiding Principles

- **Minimize tokens**: Every line should earn its place in context
- **Link over inline**: Reference files instead of duplicating content
- **One essential command**: Show the primary command, link to the rest
- **Preserve critical rules**: Never remove warnings/constraints without explicit approval
- **Historical accuracy**: If documenting architecture decisions, follow the datetime folder protocol in the existing AGENTS.md
