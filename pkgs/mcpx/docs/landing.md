# Landing checklist

My reading of the brief, broken into items. Kept as the working list until the
ship lands; each item records what was decided and why, so a disagreement has
something specific to argue with.

Legend: `[ ]` todo, `[x]` done, `[~]` partial, `[?]` guessed — needs a look.

---

## A. Console, from the script's perspective

- [ ] **A1.** The question is not "does it log" but "does a script that was
  written against a normal console still behave". Arguments, arity, return,
  throw-parity, `.name`, formatting, getters, symbol keys, subclass
  instances, circular values, very large values. Test the surface, not the
  happy path.
- [ ] **A2.** `Deno.stdout.write` and `Deno.stderr.write` bypass capture.
  This is correct — a script writing raw bytes asked for raw bytes — but it
  should be written down once, in the reference, not in an agent prompt.

## B. The launcher

- [ ] **B1.** Whole-launcher override. `--launcher <string|file>` replaces the
  template. `--launcher` with no value means *no launcher at all*: the script
  is handed to the runtime directly.
- [ ] **B2.** Placeholders in a custom launcher: `@entry`, `@globals`,
  `@prefix`, `@before`, `@onSuccess`, `@onError`, `@suffix`, `@import`.
- [ ] **B3.** Reference graph must be a DAG; a cycle is a generation-time
  error naming the path that closed it.
- [ ] **B4.** Each placeholder resolves once. A repeat is an error unless
  `allowRepeat` names it.

## C. String-or-file, everywhere

- [ ] **C1.** Every string-shaped input (`prefix`, `before`, `onSuccess`,
  `onError`, `suffix`, `launcher`, `prelude`) accepts inline source or a path.
- [ ] **C2.** Detection is by probe, not by guessing: if it resolves to an
  existing file, it is a file. Explicit forms `@file:`/`@text:` force it.
- [ ] **C3.** Both forms set *at the same level* is an error. Set at
  *different* levels is ordinary precedence, no conflict.
- [ ] **C4.** A directory means: concatenate its files, lexicographic order,
  shallow by default. Leading-number ordering must work, so `10` sorts after
  `9` — natural order, not raw byte order.
- [ ] **C5.** Per-kind override of whether a directory is even allowed. A
  prefix probably should not be a directory; default that off in plumbing.

## D. Unified configuration

- [ ] **D1.** One schema. A setting is declared once with type, default,
  pretty name, short and long description, and aliases — and is then
  reachable from the config file, an environment variable, and a flag,
  without being restated.
- [ ] **D2.** Flags and env vars are generated from the schema. Env is
  `MCPX_<PATH>` derived from the dotted path; aliases are extra.
- [ ] **D3.** Boot order is explicit and inspectable: built-in defaults, then
  config files nearest-last, then environment, then flags.
- [ ] **D4.** Alias conflicts: two spellings of the same setting at the same
  precedence is an error that names both.
- [ ] **D5.** Cross-field validation up front — "X only means something when
  Y is Z" is caught before any work starts, not discovered mid-run.
- [ ] **D6.** `mcpx config --schema` prints the whole surface.

## E. Search paths

- [ ] **E1.** Config, script and log locations are each a *list* of
  candidates with a built-in default.
- [ ] **E2.** The null splice works in these lists, so a user can say
  `["./mine", null, "../fallback"]` and have `null` stand for the built-in.
- [ ] **E3.** A file where a directory was expected is accepted and means
  exactly that one file.

## F. The logging store

- [ ] **F1.** SQLite index over the JSONL, call-grained.
- [ ] **F2.** `mcpx log` — query, filter, follow.
- [ ] **F3.** `mcpx stats` — aggregate. Invent the useful ones.
- [ ] **F4.** Trace-chain walk: from any record back through its parents.
- [ ] **F5.** Rotation on age, total size, *and* line count — all configurable.
- [ ] **F6.** A way to inspect the store directly, including raw SQL.

## G. Validation before execution

- [ ] **G1.** Resolve and check every path, file and directory up front.
- [ ] **G2.** Type-check the generated program without running it, following
  imports. Deno can do this; record honestly where it cannot.
- [ ] **G3.** A `.ts` and a `.js` of the same name is ambiguous. Prefer `.ts`,
  but make that a guard with a plumbing switch rather than a silent rule.

## H. Plumbing

- [ ] **H1.** A `plumbing` section for internals with no ordinary reason to
  change. Marked clearly as unsupported.
- [ ] **H2.** Wherever the code has a guard or a throw, ask whether it wants a
  plumbing switch. No use case needed; the point is not being stuck.

## I. Commands and help

- [ ] **I1.** Enumerate every command, with what it does.
- [ ] **I2.** `--help` complete and accurate at every level.
- [ ] **I3.** A man page.
- [ ] **I4.** `mcpx <string>` — a bare argument that is obviously source or a
  script should run, not error.
- [ ] **I5.** Decide what remains of the exec/run distinction.

## J. Landing

- [ ] **J1.** Small clean commits throughout.
- [ ] **J2.** PR, then review rounds.
- [ ] **J3.** Final report: what was guessed, what the alternatives were, why
  the guess, and what was deliberately not done.
