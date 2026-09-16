---
name: lootbox
description: Use before calling any lootbox tool — mcp_fff, mcp_fff_worktree, mcp_fff_nix, mcp_codedb, mcp_codebase_memory, mcp_context7 or mcp_chrome_devtools. Carries the fff constraint and query syntax, which index covers which directory, how to index a new repository, and the per-server usage rules that lootbox does not forward. Load it when a lootbox query returns nothing, or when choosing between the overlapping search tools.
---

# lootbox

lootbox discards the instructions each MCP server publishes at initialize, so
nothing below reaches the model unless this skill is loaded. Everything here was
measured against the running servers.

## Which index covers what

| namespace          | root                                                  |
| ------------------ | ----------------------------------------------------- |
| `mcp_fff`          | `~/git` — 26 repositories                             |
| `mcp_fff_worktree` | `~/.local/share/opencode/worktree` — agent worktrees  |
| `mcp_fff_nix`      | `~/.config/nix` — the nix-darwin system configuration |

An fff process fixes its root when the lootbox daemon starts and cannot be
retargeted. Outside all three roots it returns `0 matches`, never an error. Use
the built-in `grep`, which follows the real session directory.

codedb and codebase-memory index per project and currently cover the same three
trees. They also answer with silence for anything unindexed.

## fff

`grep` searches contents and is the default. `find_files` searches names. Use
`multi_grep` when you want several identifiers in one call rather than repeated
greps.

Search **bare identifiers**. One per query, no code syntax, no keywords:

    good  InProgressQuote            definition and every usage
    bad   struct ActorAuth           keywords narrow it, misses type aliases
    bad   ctx.data::<ActorAuth>      code syntax, 0 results

**Never use regex** unless you need alternation. `.*`, `\d+`, `\s+` almost always
return nothing, because matching is per line. For OR logic:

    multi_grep(['ActorAuth', 'PopulatedActorAuth', 'actor_auth'])

Constraints filter a search. For `grep` they are prepended inline; for
`multi_grep` they go in the separate `constraints` parameter. A constraint must
be shaped like one of:

    *.rs   *.{ts,tsx}        extension
    src/   quotes/           directory — the trailing slash is required
    schema.rs                filename
    !test/   !*.spec.ts      exclude

A bare word is **not** a constraint — it becomes search text. `quote TODO` looks
for the literal string "quote TODO"; `quotes/ TODO` looks for TODO under
`quotes/`. Prefer broad constraints; `quotes/storage/db/ query` misses results.

The parameter is `query`, not `pattern`. Passing `pattern` silently returns
`0 matches.`

Stop after two greps and read the code. Results already expand definition bodies,
so a follow-up read is often unnecessary. `|` marks definition context, `[def]`
marks definition files.

## codedb

Reach for the structural tools first: `codedb_symbol` for a definition,
`codedb_callers` for usages, `codedb_deps` for blast radius, `codedb_outline`
before `codedb_read`. `codedb_search` is a substring fallback for when the symbol
name is unknown — not the default. Edit with native tools; `codedb_edit` exists
only for clients that have none.

Files larger than 2 MB are skipped and reachable only through `codedb_read`.

## codebase-memory

A call graph rather than an index, so prefer it when the question is about
relationships: `trace_path` between two symbols, `detect_changes` for blast
radius, `query_graph` for structural queries, `get_architecture` to orient.

`search_graph` is BM25 — literal terms with camelCase splitting, no semantic
matching. Its `semantic_query` returns noise (cosine 0.04–0.06 against unrelated
functions) and is not worth calling.

Call `check_index_coverage` before asserting something does not exist; its own
docs say coverage is best-effort, never proof of completeness. Paginate when
`has_more` is set.

## context7

Use for any library, framework, SDK, CLI or cloud service question — even one
that seems familiar, because training data lags releases. Prefer it over web
search for library documentation. Resolve the library id first, then query.

Not for refactoring, business logic, code review, or general programming
concepts. It has itself indexed; it does not have lootbox.

## Indexing

Both graph tools index on demand, and re-indexing an existing project updates it
in place rather than creating a duplicate.

    codedb <path> index
    codebase-memory-mcp cli index_repository --repo-path <path> --mode fast

Either accepts a parent directory holding many repositories, and both honour
each nested repository's `.gitignore`. Use `--mode fast`: `moderate` and `full`
additionally build the semantic edges that do not work.

`codedb` and `codebase_memory` refuse to index a home directory or `/tmp`.
