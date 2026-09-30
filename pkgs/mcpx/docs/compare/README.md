# Comparisons

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  mcpx next to the five MCP revisions, opencode v1 and v2,
              lootbox and Cloudflare code mode: a granular register of
              every difference, capability matrices, and plain answers to
              the questions the comparison raises.
```

mcpx sits between things that disagree. There are five revisions of the Model
Context Protocol, two of them eras apart. There are two opencodes with
different plugin APIs and different ideas of what an MCP server is for. And
there are three other ways to run code against MCP tools. This directory
records every difference found between them, what each difference is worth to
mcpx, and where mcpx stood at one named commit.

It is the "living comparison" that #153 recommends (approach C). The earlier
records, [`../../ASSESSMENT.md`](../../ASSESSMENT.md) and
[`../../OPENCODE-V2.md`](../../OPENCODE-V2.md), are left as they were; where
this directory finds them wrong it says so
([conflicts.md §E](conflicts.md#e-mcpxs-documents-against-its-code)).

## Start here

| document | answers |
| --- | --- |
| [stateless.md](stateless.md) | Can mcpx speak the stateless 2026-07-28 protocol and every older revision at once? What "stateless" requires, where mcpx keeps state, and what stands in the way. |
| [input-required.md](input-required.md) | What `input_required` is, in plain words, with the sequence, and what mcpx does with it in each direction. |
| [content-passthrough.md](content-passthrough.md) | Did mcpx need audio, `resource_link`, `structuredContent`? What it does with each content type on each path. |
| [conflicts.md](conflicts.md) | Every communication conflict: revision against revision, prose against schema, product against product, one mcpx surface against another, mcpx's documents against its code. |
| [what-a-server-should-be.md](what-a-server-should-be.md) | The character of an MCP server according to each revision and each product. |

## Matrices

| matrix | rows |
| --- | --- |
| [matrix-revisions.md](matrix-revisions.md) | MCP revisions × every method, notification, capability field, content type, error code and HTTP header |
| [matrix-mcpx.md](matrix-mcpx.md) | mcpx × peer revision, as server and as client, at `05c78b2` |
| [matrix-opencode.md](matrix-opencode.md) | opencode v1 × v2: plugin API and every hook, the MCP client, code mode, the process model |
| [matrix-code-mode.md](matrix-code-mode.md) | lootbox × mcpx × opencode code mode (v1, v2) × Cloudflare code mode |

## The register

One row per difference: one field, one method, one error code, one header or
one behaviour. Each area file has a table (ID, difference, labels, where it
exists, **mcpx status**, value, effort, risk). Below the table is one block per
row with the detail: field names, error codes, edge cases, behaviour the
specifications imply without stating, and a source for every claim.

| area | file | rows | `mcpx missing` |
| --- | --- | --- | --- |
| lifecycle and versioning | [register/lifecycle-versioning.md](register/lifecycle-versioning.md) | 40 | 11 |
| transports | [register/transports.md](register/transports.md) | 45 | 20 |
| capabilities | [register/capabilities.md](register/capabilities.md) | 26 | 15 |
| tools | [register/tools.md](register/tools.md) | 31 | 6 |
| resources | [register/resources.md](register/resources.md) | 16 | 11 |
| prompts | [register/prompts.md](register/prompts.md) | 8 | 5 |
| completion | [register/completion.md](register/completion.md) | 11 | 4 |
| pagination and caching | [register/pagination-caching.md](register/pagination-caching.md) | 14 | 7 |
| logging | [register/logging.md](register/logging.md) | 9 | 3 |
| elicitation | [register/elicitation.md](register/elicitation.md) | 23 | 8 |
| `input_required` (MRTR), shared by elicitation, sampling and roots | [register/mrtr.md](register/mrtr.md) | 20 | 3 |
| sampling | [register/sampling.md](register/sampling.md) | 11 | 2 |
| roots | [register/roots.md](register/roots.md) | 5 | 1 |
| tasks | [register/tasks.md](register/tasks.md) | 27 | 15 |
| subscriptions and notifications | [register/notifications.md](register/notifications.md) | 18 | 10 |
| progress and cancellation | [register/progress-cancellation.md](register/progress-cancellation.md) | 14 | 6 |
| auth | [register/auth.md](register/auth.md) | 25 | 3 |
| content types | [register/content-types.md](register/content-types.md) | 19 | 2 |
| errors | [register/errors.md](register/errors.md) | 18 | 8 |
| `_meta` | [register/meta.md](register/meta.md) | 11 | 3 |
| plugin APIs | [register/plugin-apis.md](register/plugin-apis.md) | 41 | 12 |
| code mode and script execution | [register/code-mode.md](register/code-mode.md) | 54 | 11 |
| process model | [register/process-model.md](register/process-model.md) | 25 | 5 |
| **total** | | **511** | **171** |

Two areas are not in the list the register was asked for, because the 2026
revision made them matter: **pagination and caching** (`CacheableResult`
is new and required) and **`input_required`**, which is one mechanism under
three features.

### Labels

Every row carries every label that applies.

| label | meaning | rows |
| --- | --- | --- |
| `mcpx missing` | In scope for mcpx and absent or broken at the named commit. Includes conformance bugs where mcpx claims a feature and gets it wrong | 171 |
| `impl deferred` | mcpx knows and has deferred it; the row cites the document or comment that says so | 21 |
| `mcpx has, others don't` | mcpx does this and the products compared do not | 18 |
| `<rev> has` | The revision in which the thing first appears, and later revisions that change it materially | 2024-11-05: 74 · 2025-03-26: 39 · 2025-06-18: 42 · 2025-11-25: 62 · 2026-07-28: 152 |
| `<rev> removes` | The revision that removes it | 2025-06-18: 3 · 2026-07-28: 48 |
| `<rev> deprecates` | The revision that deprecates it (still defined, scheduled for removal) | 2025-03-26: 1 · 2025-11-25: 1 · 2026-07-28: 22 |
| `specs conflict` | Two revisions, or the prose and schema of one revision, disagree | 33 |
| `opencode v1 has` | opencode 1.18.31 has it | 100 |
| `opencode v2 has` | opencode 2.0.3 has it | 125 |
| `lootbox has` | lootbox has it (the deployed build unless the row says fork) | 22 |
| `has better replacement` | Superseded by something better, in a later revision or another product | 36 |
| `has no replacement` | Removed or absent with nothing taking its place | 6 |
| `does not match mcpx's goal` | A feature mcpx should not adopt; the row says why | 13 |

Cloudflare code mode has no label of its own. Rows about it name it in *where
it exists* and use the other labels for what it means for mcpx.

### The other columns

- **mcpx status** (header `mcpx @ 05c78b2`): `✓` works as defined · `partial`
  (the cell says how) · `✗` missing or broken · `n/a` · `acc.` accepted
  beyond what the revision defines (accept liberally), then a few words.
- **Value**: `+` what mcpx gains by doing it, `−` what it costs or why it is
  moot; high, med or low.
- **Effort**: S (hours), M (days), L (a week or more), XL (a redesign).
- **Risk**: what breaks, or who is surprised, if it is done or if it is not.

## Refreshing the status column

The status column is a snapshot, and the protocol work was changing mcpx's
protocol behaviour while this was written (a modern-first probe and
conformance fixes). To refresh:

1. Pick the new commit. Replace `05c78b2` in the header of each table and in
   the `mcpx @` field of each block you re-check.
2. For each row, re-read the mcpx `path:line` citations in its block. The
   status cell and the `mcpx @` field are the only parts that describe mcpx.
   Everything else describes a specification or another product, and stays
   true until that changes.
3. Where the fix landed, set the cell to `✓` and drop `mcpx missing` from the
   labels. Keep the row: the difference between the revisions is still real.
4. Update the counts above.

[matrix-mcpx.md](matrix-mcpx.md) has the same property: each row cites the code
it describes.

## Citations

Every claim has a source, in one of these forms:

| form | means |
| --- | --- |
| `schema/<rev>/schema.ts:N` | the schema of that revision, in `modelcontextprotocol/modelcontextprotocol` at `046fa30` |
| `<rev>/<path>.mdx:N` | specification prose, `docs/specification/<rev>/<path>.mdx` in the same repository; public at `https://modelcontextprotocol.io/specification/<rev>/<path>` |
| `seps/<file>.md:N`, `docs/extensions/…:N`, `docs/docs/…:N` | SEPs, extension pages and guides in the same repository |
| `internal/…:N`, `docs/…:N`, `plugin/…:N` | mcpx, relative to `pkgs/mcpx` at `05c78b2` |
| `v1:packages/…:N` | opencode 1.18.31, `/nix/store/xczwr0rfqkd99yjg4aqv28yfxhim4pg1-source` |
| `v2:packages/…:N` | opencode 2.0.3, `nix eval --raw .#opencode2.src` (`/nix/store/y08jwgxhq18hm706fisnqx68n39bid9i-source`) |
| `.lootbox/…:N`, `nix:…:N` | files in this repository at `05c78b2` (lootbox's fork checkout is `.lootbox/`; the deployed lootbox is upstream `587a5a1` plus the patches in `flake.nix`) |
| `@modelcontextprotocol/sdk@1.22.0/…:N` | the MCP SDK lootbox resolves, from the Deno npm cache |
| `https://unpkg.com/…` (L*n*) | an npm package opencode pins but neither tree vendors |
| a URL and a section heading | Cloudflare documentation or blog posts, fetched 2026-09-30 |
| `wire W20`, `M1`, `S2`, … | a recorded session against a binary built from `05c78b2`, with isolated state, driving the repository's test servers; see [stateless.md](stateless.md#evidence) |

Quoted text after a citation is verbatim from that line. Four quotes of
specification prose have relative links expanded to absolute ones.

## Related documents

- [`../protocol.md`](../protocol.md): mcpx's own protocol design (accept
  liberally, send conservatively, one `downgrade()` at the edge). This
  directory records differences and conflicts; it does not restate that
  design.
- `docs/spec/server-obligations.md`: the exhaustive list of what a server
  MUST and SHOULD do per revision. The protocol work is writing it, and it is
  not on `main` at `05c78b2`. [what-a-server-should-be.md](what-a-server-should-be.md)
  summarises the character of each revision and product, not the obligations.
- [`../opencode-plugin.md`](../opencode-plugin.md): the plugin's design. Claims in
  it that the pinned sources contradict are in [conflicts.md §E](conflicts.md#e-mcpxs-documents-against-its-code).
