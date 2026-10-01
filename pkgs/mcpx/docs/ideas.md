# Ideas

Unvetted. Everything from a brainstorm lands here first, numbered, so nothing
is lost between thinking of it and deciding about it. Items graduate to
[`proposals.md`](./proposals.md) once they have a shape, and from there to the
feature list in [`story.md`](./story.md) once built.

Nothing here is committed to. Some of it is probably wrong.

## Format

Each item keeps **the thinking, not a summary of it**. A one-line version of
"maybe support a list of dirs to add to the priority list, and allow either
front or back, and maybe aliasing to dodge conflicts" loses the two open
questions inside it, which are the part worth keeping.

So: a heading, the original thought in enough detail to reconstruct the intent,
and where it went. `→ proposal` means it has a written design. `→ shipped`
means it is built. `→ answered` means it was a question, and **the answer is
recorded here**, because an answer that only exists in a conversation is lost.

## Should this be issues instead?

Probably, eventually. A file is right while there is one author and no
repository to file against: it diffs, it reviews, and it cannot rot in a
tracker nobody opens. The moment a second person is involved, or an item needs
discussion threaded against it, these become issues and this file becomes an
index of links. The graduation path is the same either way.

---

## Intake 2026-09-27

Extracted from one message, grouped, not prioritised.

---

### Paths and identity

**1. Git version floor.** Does `--path-format=absolute` need a new git?
→ **answered.** It landed in git 2.31, March 2021. The stated policy is that
anything released before 2026-01-01 is fair game, so it is now used directly,
with a hand-absolutising fallback kept for older git. Cost of the fallback is
one extra `git rev-parse` only when the first attempt fails.

**2. Is `--git-common-dir` relative to cwd or to the repo root?**
→ **answered.** To **cwd**. Verified: from `pkgs/mcpx/internal` it returns
`../../../.git`. This matters because joining with cwd is therefore correct,
and joining with the repo root would have been wrong. It is also why
`--show-toplevel` needs no treatment: it is already absolute.

**3. Is there a realpath that does not involve cwd?**
→ **answered.** `realpath(1)` exists as a shell command, and Go's
`filepath.EvalSymlinks` is the in-process equivalent. Neither needs cwd for an
*absolute* path; both resolve a *relative* one against cwd. So the order is
forced: absolutise first, resolve second. Doing it the other way silently
resolves against the wrong directory.

**4. Would `--absolute-git-dir` work instead?**
→ **answered: no, for either scope.** It returns the *git directory*, and for a
linked worktree that is the worktree's private `.git/worktrees/<name>`, not the
shared common dir. Using it for `repo` scope would make every worktree look
like a separate repository — the exact bug the scope exists to avoid. And for
`worktree` scope the wanted value is the working tree root, not a git dir at
all, so `--show-toplevel` is already the right call.

**5. Prefer realpath everywhere for comparison and search.**
→ **shipped.** Every path that becomes a key goes through one `canonical()`
helper: absolutise, then `EvalSymlinks`, falling back to `Clean` when the path
cannot be resolved. `/tmp` and `/private/tmp` now key identically, as do
symlinked checkouts.

**6. Are there cases where the relative form should win?** Display, plausibly —
`repo:../..` is unreadable but `repo:/Users/.../nix` is long. Never identity.
Open.

---

### Catalogue, types and search

**7. Implement round-robin budgeting.** → **shipped** as `mcpx catalog`.

**8. Keep whole-namespace `types` too, or force the budget on everyone?**
→ **answered: keep both.** They answer different questions. `catalog` is "what
exists, within a budget"; `types` is "I have chosen this namespace, give me all
of it". Collapsing them would make the common case need a flag, and would
ration an agent that already knows what it wants.

**9. How are signatures ranked — is there similarity indexing? An agent? A
library?** → **answered: none of those.** Catalogue ranking is by *cost*,
`len(line)/4`, ties broken by path. No embeddings, no model, no dependency.
That is deliberately different from `mcpx search`, which is *relevance*-ranked
by substring scoring. Two rankings because they answer two questions.

**10. Could a query bias what fits?** → **shipped** as `--bias`, which promotes
matching signatures ahead of the pure cost order. This is the merge of the two
ranking systems and it seems to work: at a budget too small for
`take_screenshot`, `--bias screenshot` promotes it anyway.

**11. `ls` does not search — is that a separate thing?** → **answered: yes.**
`ls` enumerates namespaces and counts, never tools. `search` ranks tools across
namespaces. `catalog` sits between them: every namespace, some tools.

**12. Better search: typo tolerance, concept similarity, tags.** → proposal.
Worth noting the ordering: tags are cheap and exact, typo tolerance is cheap
and fuzzy, similarity is expensive and needs a model or an index.

**13. Types improvements** beyond single-tool output. → proposal.

---

### Protocols beyond MCP

**14. Import an OpenAPI service as a namespace.** → proposal. Confirmed
feasible: the codegen already consumes JSON Schema, which is what an OpenAPI
operation carries.

**15. Emit OpenAPI *per namespace*.** The inverse, so non-agent consumers can
use the same servers without speaking MCP. → proposal.

**16. Expose mcpx itself as an MCP server and/or an OpenAPI endpoint**,
optionally secured, with or without the MCP type wrappers. → proposal. This is
distinct from 15: it makes mcpx a *server*, not just a client.

**17. A common adapter interface** so MCP, OpenAPI and anything later plug in
the same way. → proposal.

**18. Steal from MCP alternatives and mcp++ efforts.** → proposal.

---

### MCP server mechanics

**19. How are Chrome debugging ports handled?** → **answered: they are not,
and do not need to be.** `chrome-devtools-mcp` has no listen-port flag. With
`--isolated` it launches its own Chrome with a temporary profile and an
ephemeral debugging port of Chrome's own choosing. This is why mcpx has no port
machinery, and why the predecessor's auto-port code was solving a problem this
server does not have.

**20. Can you attach to different Chrome processes?** → **answered: yes**, via
`--browserUrl http://127.0.0.1:9222` or `--wsEndpoint ws://...`. That is the
case where per-instance templating becomes necessary, because each instance
needs a *different* URL.

**21. Auto-increment or random ports as a general feature.** → proposal, as
argument templating plus an allocator. Generalises beyond Chrome to any server
that does take a port.

**22. MCP servers have their own config — how is that handled?**
→ **answered: argv and environment, and that is all there is.** MCP defines no
configuration channel, so a server's options are its command-line flags and its
environment. mcpx passes `command`, `args`, `env` and `cwd` per server. The one
thing MCP *does* define that mcpx was ignoring is `instructions` from
`initialize` — now surfaced.

**23. `--pageIdRouting` defaults to true** and is documented as "useful for
concurrent agent sessions". Worth understanding before assuming pooling is the
only answer to concurrency. Open.

**24. What other common MCP settings are we missing?** → proposal. Known gaps:
sampling, roots, progress notifications, cancellation, elicitation, OAuth.

---

### Scopes and sessions

**25. Config policy for scopes** — allow-list, default, and whether a flag or a
script may override. → proposal.

**26. Named sessions, and naming one that started anonymous.** → proposal. The
subtle part: an anonymous key is torn down when its caller exits, so naming it
must flip that bit.

**27. Per-scope keepalive defaults.** → proposal.

**28. Change keepalive at runtime, from CLI or script.** → proposal.

**29. Reap on an HTTP status code.** → proposal, but MCP's own `ping` is the
better first version: no configuration and works for every server.

**30. Reap on a webhook.** → proposal. Splits into outbound (easy, it is events
with an HTTP sink) and inbound (needs authentication; `mcpx session stop`
already covers anything local).

**31. Tie an instance to a pid.** → **shipped** as `scope: pid`, reaped on the
reaper tick when the process is gone rather than waiting out an idle timer.

---

### Config and script discovery

**32. Extra directories on the priority list**, with a choice of front or back:
`--override-paths` and `--fallback-paths` taking lists of directories or files.
→ proposal. The open question inside it is whether "front or back" is two flags
or one flag plus an ordering rule.

**33. Aliasing a file to a different name to dodge conflicts.** → proposal, and
flagged there as probably the wrong shape — shadowing already resolves
conflicts by precedence, and an alias adds a second mechanism for the same job.

**34. Do config and scripts share a directory?** → **answered: yes.**
`.config/mcpx/config.json` and `.config/mcpx/scripts/*.ts`, with `.mcpx/` as
the lower-precedence alternative spelling. Both are searched upward from cwd,
and a directory carrying both spellings warns once.

---

### Logging and output

**35-43.** Every command takes a display format; everything expressible as JSON
is available as JSON; a logging helper for scripts accepting either a plain
string or structured values; plain strings wrapped into the same record shape;
automatic enrichment with ambient context; formats json / json-pretty / logfmt
/ text / compact / bare; message templates preserved next to the interpolated
message; a default closer to a classic log line.

→ consolidated into one proposal. **Answer to "does the existing logger
interpolate": there is no logger yet.** The daemon prints unstructured lines
and scripts have no helper at all.

---

### Logging, round two

**69. How does a script return several times?** → **answered: `emit()`**, a
separate export, not a log level. Logs and results share one wire channel with
a `kind` discriminator so they stay ordered, but they are different functions
with different destinations: logs go to stderr rendering, results to stdout or
the envelope.

**70. Should the discriminator be `_kind`?** → **answered: it is not a user-facing
attribute at all.** `kind` lives at the top of the wire record; user attributes
are nested under `attrs`. A script writing `log.info("x", {kind: "y"})` sets
an attribute, not the discriminator. The real collision was elsewhere -- see
"Reserved attribute names".

**71. Should there be `log.output()` / `log.return()` helpers?** Deliberately
not. Logging and returning are different acts, and putting them on one object
invites `log.return` being read as "log that we returned". `emit` is the verb.

**72. Can JS attributes merge onto Go context?** → proposal, "Log context and
enrichment layering". slog does have the seam (`Handler.Handle` takes a
context) and mcpx does not use it yet.

**73. Does a streamed result also produce a log line?** → **answered: no.**
Verified: `emit({a:1})` produces one stdout line and no record.

**74. How does variadic translate between JS and Go?** → **answered: it does
not, and should not.** JS has real variadics, so `log.info(msg, ...rest)` with
runtime type inspection is idiomatic. Go's slog uses alternating key/value
pairs (`slog.Info("msg", "k", v)`) or typed `slog.Attr`. Forcing either into
the other's shape would make one side unidiomatic. They meet at the wire
format, which is a message plus a map, and each side reaches that in its own
way.

**75. How expensive is the Error-based source capture?** → **answered, measured:**
`new Error().stack` costs **4.89 us** against a 0.10 us baseline for the JSON
serialisation alone -- roughly fifty times the cost of the record it decorates.
Parsing the frame adds only 0.3 us on top. So capture is opt-in, and the
expensive part is building the stack, not reading it.

**76. Does JS run for every log message even when filtered?** → **answered: yes,
partly, and that is worth fixing.** The script always serialises and writes;
the level threshold is applied in Go. A debug call in a hot loop therefore
costs a write even when nothing will print. → proposal.

**77. Should source capture be configurable globally and per server?**
→ **shipped for global** (`logging.source` in config, plus the flag and the
environment variable), and per-server `logLevel` now resolves. Per-server
*source* is not meaningful, because source capture describes the script, not
the server it is talking to.

### Harness integration and distribution

**44-53.** Plugins for opencode 1 and 2, Claude, Codex; the split between an
instructions file, plugin-injected instructions, and a tool surface; a plugin
suppressing a redundant instructions file by detecting a marker; an install
command per harness; hash-matching so an update never clobbers a user's edits;
daemon-side config updates; a curl-able install document; a curl-able **setup
document written for an agent** rather than a human; `mcpx setup-prompt` /
`install-prompt`; platform variants for nixos, flakes, darwin, darwin+homebrew.

→ consolidated into one proposal. The genuinely novel piece is the
agent-targeted setup document: the artefact is a prompt, not a script.

---

### Packaging and service

**54-59.** NixOS module and service; Homebrew formula; a non-flake nix
installer; the binary installing its own service; autostart at boot versus on
first use; whether sudo is ever appropriate.

→ consolidated into one proposal, with a firm view on the last one: **never
sudo.** The daemon is per-user and owns per-user MCP servers; a system unit
would run them as the wrong user and share state across users.

---

### Transport, scale and language

**60. What does the daemon actually speak?** → **answered.** HTTP over two
listeners: a unix socket for the CLI, and `127.0.0.1` on an OS-assigned
ephemeral port for script clients, because `fetch()` over a unix socket is not
portable across deno, bun and node. Not stdio, not named pipes, not files.

**61. Is the port fixed or random? What if it is taken?** → **answered.**
Ephemeral, chosen by the OS, so there is nothing to collide with. It is
published in a daemon record file that the CLI reads.

**62. Can multiple daemons run?** → **answered: yes, already.** One per config
fingerprint, each with its own socket. `mcpx daemons` lists them.

**63. Accept a URI with or without host and port** (`example.com`,
`example.com:8008`, `:42`). → proposal. Parsing is trivial; authentication is
the actual blocker, because loopback currently provides trust for free.

**64. Load testing.** → proposal. Untested beyond 12 concurrent runs.

**65. Go or Rust?** → proposal, with a recommendation to stay: the workload is
process supervision and JSON, where the runtime is idle most of the time.

---

### Naming and comparison

**66. `mcpx`, `xmcp`, or something else?** → proposal. Short version: `mcpx` is
fine, renaming gets more expensive weekly, and the only real argument is that
`mcpsh` states the actual pitch.

**67. What does opencode v2 have that this does not?** → **answered**, and
tracked: OAuth and elicitation for remote servers, in-sandbox `search()` so
discovery never enters the transcript, a persistent PTY daemon, a shared
background service, and OpenAPI import. mcpx is ahead on shell access, pooling
and scoping, real-runtime TypeScript, and per-config isolation.

**68. Track parity deliberately.** → proposal.

---

## Intake 2026-09-30

From one message about adapters, plus two language/transport requests. Filed
straight to issues rather than proposals, per the graduation path above: these
needed a design argument written down, not a line kept.

---

### Adapters without hand-writing JSON

**78. Can a binary become a declaration automatically, without a model?**
The AI-assisted version is already sketched (#114 has the `--help` parse plan,
#83 has it as step 5), but both park it behind a large design and neither owns
it. The deterministic half is shippable on its own. → **#295.**

The thing worth keeping from the thinking: `--help` is the *worst* regular
source, not the best, and leading with it is why this looks harder than it is.
Shell completion scripts are generated data structures — fish's
`complete -c tool -l flag -d 'desc'` is nearly a declaration already, and zsh
`_arguments` specs carry flag, type and description in a fixed grammar. Cobra,
clap and argparse all emit them. Ladder: completions → a published spec (which
is `paths.apis`, not an adapter at all) → man → `--help` → `--bare`.

And completions need **two ways in, not one**. Extracting from the binary
(`tool completion fish`, `__complete`) is the convenient path and only works
when the program ships a generator. Accepting a completion *file* is the one
that matters: distributions already ship completions for programs that have no
generator at all — `/usr/share/bash-completion/completions/*`, zsh
`site-functions`, the Homebrew and Nix prefixes — and those are frequently
packager-written for old tools whose `--help` is the worst in the first place.
It also makes the parser testable from a corpus instead of from installed
binaries, lets someone fix the *source* and re-import rather than hand-editing
generated JSON, and covers binaries mcpx cannot run at all. So extraction is
sugar over the file parser; build the parser first. Best version of the idea:
`import` searches the system completion directories by name before it ever
probes a binary.

Also settled here: a generated declaration records the binary's version, so
`mcpx adapter check` can say "written against jq 1.7, found 1.8" instead of
drifting silently. That was #83's open question.

**79. Can I just say `git` is an MCP that takes the subcommand and the flags?**
Not ideal but fast. → **#295**, as `--bare`, opt-in and loud.

Worth keeping *why* it is allowed at all, because #83 Approach A rejects the
shell-escape version and the distinction is the whole argument: a bare adapter
is an **argv** hole, not a **shell** hole. The cost is that it has no schema
beyond "array of strings", so the agent gets nothing to reason about and
`mcpx doctor` can only check the binary exists. Hence: required `description`,
a visible marker in `mcpx adapter list`, and probably a settings gate so an
organisation can forbid it centrally.

The sharp edge found while writing it up: **`--bare` on a binary that has its
own argv-level exec escape is equivalent to Approach A.** `git -c
core.pager='sh -c ...'` is a shell escape mcpx cannot see. The sandbox is only
as good as the wrapped program's own argv surface, and the docs have to name
the common offenders.

**80. Go can exec a program so it can't shell escape, right?**
→ **answered: yes, and it already does.** `Spec.Call` builds an argv and runs
`exec.CommandContext(cctx, s.Command, full...)`
(`internal/adapter/adapter.go:237`). No `/bin/sh -c` anywhere in
`internal/adapter`, so no word-splitting, globbing, `;`, `$()`, backticks, pipes
or redirection. The guarantee is real; see 79 for the one place it leaks.

**81. Is there an issue for exec'ing bash, and for Python first-class?**
→ **answered: both already exist, no new issue needed.** Shell and shebangs are
**#106** — its own notes already say "nearly just allow any shebang" and
`--shell||--bash||--shell python3`. Python 1:1 with TypeScript is **#105**,
framed there as "a second codegen backend and a second runner, not a second
product". Both sit under #175.

---

### Languages and reach

**82. Roc.** → **#296.**

The reason it is not just "another interpreter": Roc's platform/application
split means an application is pure and a *platform* supplies every effect it
may have. That is the exact shape of an mcpx script — pure logic whose only
effects are tool calls — and no other language in the runtime list models it
natively. The dream version is mcpx *as* a Roc platform, where the capability
boundary becomes the type system rather than a Deno flag set.

Why it is staged behind a codegen backend anyway: a Roc host is ordinarily
Rust/Zig/C and links against the compiled app, so a Go host means cgo and a
separate toolchain. Plus Roc is pre-1.0 with no compatibility promise. Ship the
boring backend, keep the platform as research.

**83. Iroh.** → **#297.**

Reach a daemon by *identity* rather than by address — no port forwarding, no
public IP, no ssh key copied. ssh (#85) solves encryption and auth and solves
routing only if you can already reach the host, which is exactly the
laptop-behind-NAT case.

The honest finding, kept because it is the part that would otherwise be
rediscovered: **iroh is Rust and mcpx is pure Go**, and `tsnet` (Tailscale,
embeddable in a Go program, mature hole-punching, real ACLs) and `go-libp2p`
deliver most of the same property with no cgo. Where iroh genuinely wins is
`iroh-blobs` — BLAKE3, content-addressed, resumable — which is the same shape
#187 already wants for the dependency cache. If that convergence is the goal,
iroh earns its sidecar; otherwise it probably does not.

---

### The gap both of those exposed

**84. Nobody knows who is calling the daemon.** → **#298**, and the framing in
the first draft was wrong.

Not a bug — it is written down three times as a deliberate choice
(`internal/daemon/server.go:214`, `internal/daemon/origin.go:13`,
`internal/api/openapi.go:40`): unauthenticated, socket file permissions are the
access control, and the TCP bind host is deliberately not defaulted because
"that has to be somebody's decision rather than a default."

The first draft called it *blocking* — a precondition for ssh, p2p and browser
surfaces. → **answered: no.** There is no blocker for adding security and no
blocker for not having it. An unauthenticated local daemon is a complete answer
and stays one. The issue was rewritten to say so, and the auth coupling was
stripped back out of the iroh (#297) and WASM (#299) issues, which should not
carry it.

The rule that came out of it, worth keeping: **do not mix auth with other things
in issues.** Auth work goes in one line — #253 (the interface) with the pure-auth
issues ordered behind it — and other issues reference it without depending on it.

**85. The permissive-interface trick.** → **#300**, and this is the part worth
remembering.

When a spec demands an interface mcpx does not have, the move is to *build the
interface and make it say yes*: accept a user and a password, and reply "good
job, you're authorized." That satisfies every conformance row asserting the
endpoint exists, is well-formed and accepts a valid credential — which is most
of #253's 61 rows — and it cannot break anyone, because a daemon that authorizes
everyone is exactly as secure as a daemon with no interface at all.

The rows that require an **invalid** credential to be *refused* are the ones
that change behaviour and can lock you out of your own daemon. Those get their
own issue (#300), ordered behind #253, blocking nothing.

The one hazard, and it is real: a permissive interface must be *visibly*
permissive. `mcpx doctor` and startup should say "authorisation: permissive" so
nobody ships it believing otherwise.

**86. WASM as a tool source.** → **#299.**

Distinct from #84, which is mcpx compiled *to* WASM. This is mcpx *running* a
`.wasm` as a sandboxed tool source — a module reaches nothing until the host
hands it an import, which is the inverse of `internal/runner/runner.go:43`'s
admission that only Deno has a permission model worth the name.

What makes it cheap where 73 and 74 are expensive: **wazero is a pure-Go WASM
runtime with no cgo**, so `nix build` keeps cross-compiling for free and the
release matrix does not grow. The open tension is that the component model —
which is what would make the capability set a per-module declaration — may
require wasmtime and therefore cgo, forfeiting the advantage. That is the one
piece of research gating the rest.

