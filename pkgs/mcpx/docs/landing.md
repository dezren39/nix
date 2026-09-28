# Landing checklist

My reading of the brief, broken into items. Kept as the working list until the
ship lands; each item records what was decided and why, so a disagreement has
something specific to argue with.

Legend: `[ ]` todo, `[x]` done, `[~]` partial, `[?]` guessed — needs a look.

---

## A. Console, from the script's perspective

- [x] **A1.** The question is not "does it log" but "does a script that was
  written against a normal console still behave". Arguments, arity, return,
  throw-parity, `.name`, formatting, getters, symbol keys, subclass
  instances, circular values, very large values. Test the surface, not the
  happy path.
- [x] **A2.** `Deno.stdout.write` and `Deno.stderr.write` bypass capture.
  This is correct — a script writing raw bytes asked for raw bytes — but it
  should be written down once, in the reference, not in an agent prompt.

## B. The launcher

- [x] **B1.** Whole-launcher override. `--launcher <string|file>` replaces the
  template. `--launcher` with no value means *no launcher at all*: the script
  is handed to the runtime directly.
- [x] **B2.** Placeholders in a custom launcher: `@entry`, `@globals`,
  `@prefix`, `@before`, `@onSuccess`, `@onError`, `@suffix`, `@import`.
- [x] **B3.** Reference graph must be a DAG; a cycle is a generation-time
  error naming the path that closed it.
- [x] **B4.** Each placeholder resolves once. A repeat is an error unless
  `allowRepeat` names it.

## C. String-or-file, everywhere

- [x] **C1.** Every string-shaped input (`prefix`, `before`, `onSuccess`,
  `onError`, `suffix`, `launcher`, `prelude`) accepts inline source or a path.
- [x] **C2.** Detection is by probe, not by guessing: if it resolves to an
  existing file, it is a file. Explicit forms `@file:`/`@text:` force it.
- [x] **C3.** Both forms set *at the same level* is an error. Set at
  *different* levels is ordinary precedence, no conflict.
- [x] **C4.** A directory means: concatenate its files, lexicographic order,
  shallow by default. Leading-number ordering must work, so `10` sorts after
  `9` — natural order, not raw byte order.
- [x] **C5.** Per-kind override of whether a directory is even allowed. A
  prefix probably should not be a directory; default that off in plumbing.

## D. Unified configuration

- [x] **D1.** One schema. A setting is declared once with type, default,
  pretty name, short and long description, and aliases — and is then
  reachable from the config file, an environment variable, and a flag,
  without being restated.
- [x] **D2.** Flags and env vars are generated from the schema. Env is
  `MCPX_<PATH>` derived from the dotted path; aliases are extra.
- [x] **D3.** Boot order is explicit and inspectable: built-in defaults, then
  config files nearest-last, then environment, then flags.
- [x] **D4.** Alias conflicts: two spellings of the same setting at the same
  precedence is an error that names both.
- [x] **D5.** Cross-field validation up front — "X only means something when
  Y is Z" is caught before any work starts, not discovered mid-run.
- [x] **D6.** `mcpx config --schema` prints the whole surface.

## E. Search paths

- [x] **E1.** Config, script and log locations are each a *list* of
  candidates with a built-in default.
- [x] **E2.** The null splice works in these lists, so a user can say
  `["./mine", null, "../fallback"]` and have `null` stand for the built-in.
- [x] **E3.** A file where a directory was expected is accepted and means
  exactly that one file.

## F. The logging store

- [x] **F1.** SQLite index over the JSONL, call-grained.
- [x] **F2.** `mcpx log` — query, filter, follow.
- [x] **F3.** `mcpx stats` — aggregate. Invent the useful ones.
- [x] **F4.** Trace-chain walk: from any record back through its parents.
- [x] **F5.** Rotation on age, total size, *and* line count — all configurable.
- [x] **F6.** A way to inspect the store directly, including raw SQL.

## G. Validation before execution

- [x] **G1.** Resolve and check every path, file and directory up front.
- [x] **G2.** Type-check the generated program without running it, following
  imports. Deno can do this; record honestly where it cannot.
- [x] **G3.** A `.ts` and a `.js` of the same name is ambiguous. Prefer `.ts`,
  but make that a guard with a plumbing switch rather than a silent rule.

## H. Plumbing

- [x] **H1.** A `plumbing` section for internals with no ordinary reason to
  change. Marked clearly as unsupported.
- [~] **H2.** Wherever the code has a guard or a throw, ask whether it wants a
  plumbing switch. No use case needed; the point is not being stuck.

## I. Commands and help

- [x] **I1.** Enumerate every command, with what it does.
- [x] **I2.** `--help` complete and accurate at every level.
- [x] **I3.** A man page.
- [x] **I4.** `mcpx <string>` — a bare argument that is obviously source or a
  script should run, not error.
- [x] **I5.** Decide what remains of the exec/run distinction.

## J. Landing

- [x] **J1.** Small clean commits throughout.
- [ ] **J2.** PR, then review rounds.
- [ ] **J3.** Final report: what was guessed, what the alternatives were, why
  the guess, and what was deliberately not done.

---

## Outcome

Everything above is built except where marked.

**H2 is partial by design.** Nine guards got a plumbing switch. Every `throw`
in the tree was not audited one by one; the ones that were left alone are
those where the other branch is not a coherent program -- a malformed daemon
response, a socket that will not bind. A switch there would offer a choice
between working and not.

**I5, the exec/run distinction**, stayed. The difference is real and small:
`exec` generates the whole module, so a prefix shares scope and can declare
bindings; `run` imports a file, so a prefix can act but not declare. Merging
them would mean picking one of those behaviours and losing the other. A bare
argument now dispatches to whichever fits, which is the part that was actually
inconvenient.
