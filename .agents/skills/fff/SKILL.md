---
name: fff
description: Use when searching code with the fff tools exposed through lootbox (mcp_fff grep, find_files, multi_grep) — especially when a query returns 0 results, when you need the constraint syntax to filter by extension or directory, or to check whether fff's index even covers the directory you are working in. Do not use for opencode's own built-in grep/glob tools.
---

# fff

Frecency-ranked search: frequent and recent files rank first, git-dirty files are
boosted. Exposed through lootbox as `mcp_fff`.

lootbox does not forward MCP server instructions, so none of the below reaches
the model unless this skill is loaded.

## Scope — check this first

The lootbox daemon starts fff once with a fixed root of `~/git`. It indexes that
tree and nothing else.

- Working under `~/git`: `mcp_fff` is correct.
- Anywhere else (`~/.config/nix`, opencode worktrees under
  `~/.local/share/opencode/worktree`, `/tmp`): **fff returns 0 matches, not an
  error.** Use opencode's built-in `grep`/`glob`, which follow the real session
  directory.

An empty fff result outside `~/git` means "not indexed", never "not present".

## Which tool

- `grep` — default. Searches file contents. Use when you have a specific name.
- `find_files` — searches file NAMES. Use when looking for a file or exploring
  what exists for a topic.
- `multi_grep` — OR across several patterns in one call. Use for case variants.

## Rules

**Search bare identifiers.** One identifier per query, no code syntax, no
keywords.

    good: InProgressQuote          finds definition and all usages
    good: ActorAuth                finds enum, struct, call sites
    bad:  struct ActorAuth         keywords narrow it, misses type aliases
    bad:  ctx.data::<ActorAuth>    code syntax, 0 results

**Never use regex unless you need alternation.** Patterns like `.*`, `\d+`,
`\s+` almost always return 0 results, because matching is per line. For OR
logic use `multi_grep` with literal patterns.

**Use one `multi_grep`, not three greps.**

    good: multi_grep(['ActorAuth', 'PopulatedActorAuth', 'actor_auth'])
    bad:  grep ActorAuth -> grep PopulatedActorAuth -> grep actor_auth

**Stop after 2 greps and read the code.** More greps do not improve
understanding. Results auto-expand definition bodies, so a follow-up read is
often unnecessary. Lines marked `|` are definition context; `[def]` marks
definition files.

## Constraint syntax

For `grep`, constraints are prepended inline to the query. For `multi_grep` they
go in the separate `constraints` parameter. A constraint must look like one of:

    *.rs  *.{ts,tsx}      extension
    src/  quotes/         directory, trailing slash required
    schema.rs             filename
    !test/  !*.spec.ts    exclude

Bare words are not constraints — they become search text.

    good: '*.rs query'         'quotes/ TODO'      'schema.rs TODO'
    bad:  'quote TODO'         searches for the literal string "quote TODO"

Prefer broad constraints; `quotes/storage/db/ query` is usually too narrow.

## Parameter name

The argument is `query`, not `pattern`. Passing `pattern` silently returns
`0 matches.`
