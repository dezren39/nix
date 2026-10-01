# Finding the repository without git

How the `repo` and `worktree` scopes decide which repository a caller is in,
why that no longer runs git, and what was decided for every layout git
understands.

```
created:      2026-09-30T07:30:00-05:00
last-updated: 2026-09-30T07:30:00-05:00
status:       implemented
```

This is the decision record for the change. `docs/container.md` §3 is the
short version; it lives here rather than under `docs/decisions/` because that
directory belongs to another area.

---

## 1. Two questions

A server scoped `repo` gets one process per clone -- every linked worktree of
one repository shares it. A server scoped `worktree` gets one per checkout.
The keys are the answers to two questions git already has commands for:

| scope | key | git's command |
| --- | --- | --- |
| `repo` | the common git directory | `git rev-parse --git-common-dir` |
| `worktree` | the top of the checkout | `git rev-parse --show-toplevel` |

Until this change mcpx asked exactly those, by forking git twice per
directory and caching both answers -- negative ones included -- for the
daemon's lifetime. That had four costs, three of which were only found by
testing the replacement:

1. **105 MB of the container image.** Debian's git depends on perl outright.
2. **It did not work in the container anyway.** A checkout mounted into the
   image is owned by the host's uid, the image runs as uid 1000, and git
   refuses a repository someone else owns (`safe.directory`). Every mounted
   checkout, measured: `fatal: detected dubious ownership in repository at
   '/work/proj'`. The old image shipped git so that these scopes would work,
   and in the ordinary way of using it they fell back to per-directory keys.
3. **The daemon's environment leaked into every caller's key.** An
   autostarted daemon inherits the environment of whoever ran mcpx first. A
   git hook exports `GIT_DIR`; a hook that runs mcpx leaves a daemon carrying
   it for hours. With `GIT_DIR` absolute, every later caller -- from any
   directory -- was keyed to that one repository. With `GIT_DIR=.git`, as
   hooks set it, every caller outside a repository root degraded. Both are
   pinned by `TestTheDaemonsOwnGitDirDoesNotPinEveryCaller`, which fails
   against the old code.
4. **The cache made the first answer permanent.** A directory that became a
   repository after the first call there stayed "not a repository" until
   the daemon exited.

## 2. How it works now

Finding a repository is a walk up the directory tree reading a handful of
small files. Git's rules for that walk are fixed, and they are in its source:
`setup.c` (`setup_git_directory_gently_1`, `read_gitfile_gently`,
`is_git_directory`, `check_repository_format_gently`, the three
`setup_*_git_dir` functions) and the config parser in `config.c`.
`internal/config/gitdiscover.go` and `gitformat.go` are a port of those, from
git v2.55.0, function by function and named after the originals so the two can
be read side by side.

At each directory, starting from the caller's cwd with every symlink
resolved (git starts from `getcwd()`, which has already resolved them):

1. `.git` -- a directory that is a repository, or a file naming one;
2. the directory itself, as a bare repository;
3. the parent, unless a ceiling or a filesystem boundary says stop.

Once found, the common directory comes from the `commondir` file if there is
one, and the worktree from where `.git` was found -- unless the repository's
`config` says otherwise through `core.bare` or `core.worktree`.

Native discovery reads `HEAD`, `commondir`, a `.git` file, and from `config`
three keys -- `core.repositoryformatversion`, `core.bare`, `core.worktree` --
plus whatever `extensions.*` holds, since an unknown extension is what makes a
repository unreadable. When `extensions.worktreeConfig` is on it reads the
worktree's `config.worktree` as well. It tests `objects/` and `refs/` for
existence without reading them, which is the rest of what `is_git_directory`
decides on. Nothing else, and it executes nothing.

Every outcome is one of four:

| outcome | meaning | what the scope does |
| --- | --- | --- |
| found | the same answer git gives | keys by it |
| not a repository | git would say so too | keys by directory |
| broken | something on disk git dies on -- a `.git` file naming nothing | keys by directory; `doctor` warns, with the fix |
| unsure | a layout this port will not vouch for | asks git |

**Git is the fallback only for "unsure".** It is asked once, as `git
rev-parse --path-format=absolute --git-common-dir --show-toplevel`: the
first answer is printed before the second fails, so a bare repository still
yields its common directory from one process. It runs with git's own list of
repository-local variables removed (`GIT_DIR`, `GIT_WORK_TREE`,
`GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY` and eleven more -- `git rev-parse
--local-env-vars`), so it answers the question native discovery answers. With
no git on `PATH`, "unsure" keys by directory, and the reason says what could
not be read and that git is not there to ask.

**Nothing is cached.** One resolution five directories deep costs 0.073–0.081
ms (`BenchmarkDiscoverGit`, `-benchtime 300x -count=5`, M3 Pro / macOS / APFS);
forking `git rev-parse --show-toplevel` from the same place costs 9.7 ms on the
same machine, so the native answer is about 125× cheaper. A cache would save
under a tenth of a millisecond per call and bring back the stale answers of
§1.4.

`mcpx doctor` reports what the two scopes resolve to from the current
directory and what resolved them. A missing git is not a warning; it becomes
one only when a repository here needs it.

## 3. Every case, and what was decided

"Parity" means the case is built with the real git and native's answer is
compared with `git rev-parse` for both questions. Test names are in
`internal/config/gitdiscover_test.go` unless marked e2e.

### Checkouts

| case | decision | test |
| --- | --- | --- |
| main clone, from its root or any depth below | same as git | parity: `main clone` |
| linked worktree, git's relative `commondir` (`../..`) | same as git: `.git` file → private dir → `commondir` → common dir | parity: `linked worktree with git's relative commondir` |
| linked worktree, absolute `commondir` | same | parity |
| linked worktree with relative paths (`worktree.useRelativePaths`, git 2.48; sets `extensions.relativeWorktrees` and format 1) | same; the extension is one native knows | parity |
| linked worktree moved by hand | same as git: still resolves, from its new place; the stale back-pointer is not consulted | parity |
| main clone moved: its worktrees' `.git` files name nothing | same as git: an error, not a fallback. `doctor` warns and names `git worktree repair` | parity; `TestWithoutGitABrokenGitfileIsAWarning` |
| **submodule** | **its own repository**, as git says: common dir `.git/modules/<name>`, top the submodule's directory. A submodule has its own HEAD, objects and history; a server scoped to "the repository" that saw the superproject's instead would be looking at the wrong code | parity: `submodule is its own repository` (built from a local path, no network) |
| nested repositories | innermost wins, as in git | parity |
| `--separate-git-dir`, a relative `.git` file, CRLF endings | same | parity |
| `.git` file path through a symlink and `..` | resolved by the kernel, not lexically, as git's `chdir` does | parity |
| `.git` is a symlink to a git directory elsewhere | same: the symlink's target | parity |
| symlinked checkout; `/tmp` vs `/private/tmp` | the physical path, as git; both spellings key alike | parity; `TestGitParityTmpAndPrivateTmp` |
| a directory that is both a bare repository and holds a `.git` | `.git` wins, as in git's order | parity |

### Git directories

| case | decision | test |
| --- | --- | --- |
| bare repository, from itself or inside it | common dir yes, worktree no -- `worktree` keys by directory | parity |
| inside `.git`, or a worktree's private dir | same: a git directory, no worktree | parity |
| worktree of a bare clone | has a worktree: a linked worktree ignores the common `core.bare` | parity |
| bare clone with `extensions.worktreeConfig` | its worktrees become bare, as in git -- git's documented trap, reproduced rather than corrected | parity |
| `HEAD` as a symlink into `refs/`, detached SHA-1, detached SHA-256 | valid | parity |
| reftable repository | same | parity |
| stray `.git` directory that is not a repository | skipped; discovery continues upward | parity |
| nested repository whose `HEAD` is almost valid (40 non-hex characters, a symlink out of `refs/`, `ref:` outside `refs/`), or has no `objects/` | skipped, as git validates it | parity |

### `.git` files and entries git refuses

Git stops at these rather than skipping them, so mcpx does too -- skipping
would find the enclosing repository and share its instance. Every one is
built inside a real repository so that skipping would be caught.

| case | test |
| --- | --- |
| garbage; `gitdir:` without the space; no path; a path that does not exist; a path that is not a repository; trailing spaces (part of the path, as in git) | parity: `broken .git file: …` |
| a `.git` file over git's 1 MiB ceiling (`git.gitfileMaxBytes`) | parity |
| a `.git` that is neither file nor directory (fifo) | parity against git ≥ 2.54, which made this fatal; older git skipped it, and mcpx follows current git |
| an empty `commondir` | parity |
| a directory discovery cannot look inside | `a directory discovery cannot look inside` (git cannot `chdir` there at all) |
| cwd that does not exist, or is a file | `cwd that does not exist, and cwd that is a file` |

### The repository's config

| case | decision | test |
| --- | --- | --- |
| `core.worktree` absolute, relative (to the git dir, not the common dir), or with only its last component missing | same as git | parity |
| `core.worktree` git cannot enter | git dies before printing either answer, so both scopes fall back | parity |
| `core.bare`; `core.bare` with `core.worktree` | no worktree (git warns that the pair makes no sense) | parity |
| per-worktree `config.worktree` with `worktreeConfig` | read, as git reads it | parity |
| no `config`; a `config` without `repositoryformatversion` | as git: everything else in it is discarded, so `core.bare = true` there means nothing | parity |
| `[include]` setting `core.worktree` | ignored, as git ignores it here: the repository format is read without includes | parity |
| upper-case names, `[core "x"]`, `[foo "core"]`, `[core.x]`, quoting, comments, continuation lines, a BOM, CRLF, bare keys, numeric booleans | parsed as git parses them | parity; `TestGitConfigParserAgainstGitsOwn` compares values with `git config --file` |
| an unknown extension in a version-0 repository | ignored, as git does | parity |
| format version > 1; an unknown extension in version 1; a version-1 extension in version 0; a syntax error; a value git refuses (`bare = maybe`, `objectformat = md5`, a bare `worktree` key); a value git takes that this port does not parse (`bare = 1k`) | **unsure → git.** The git on the machine may be newer than this port | parity, marked unsure |

### Environment

| variable | decision | test |
| --- | --- | --- |
| `GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY` and the rest of git's local list | **Not honoured**, and removed from the fallback's environment. The key is computed in the daemon, for many callers, and the daemon's environment belongs to whoever started it -- honouring it is §1.3. The *caller's* variables never reach the daemon (`CallContext` carries the cwd, not the environment), and they do not need to: everywhere git itself sets `GIT_DIR` -- hooks, aliases, `submodule foreach` -- it also puts the cwd inside that repository, so discovery from the cwd reaches the same answer | `TestGitEnvironmentThatNamesARepositoryIsIgnored`; `TestGitLocalEnvCoversWhatGitCallsLocal` checks the list against the installed git; e2e `TestTheDaemonsOwnGitDirDoesNotPinEveryCaller` |
| `GIT_CEILING_DIRECTORIES` | **Honoured**, with git's exact rules: relative entries ignored, entries symlink-resolved unless they follow an empty entry, a ceiling never examined from below it but honoured-as-absent when the walk starts there. It can only make keys narrower, and it lives in shell profiles, so a user who set it expects mcpx to agree with their shell | parity: `TestGitParityCeilingDirectories`, eight cases |
| `GIT_DISCOVERY_ACROSS_FILESYSTEM`, and mount points | **Honoured**: discovery stops where the device changes, as git's does. A value that is not a boolean is unsure | `TestDiscoveryStopsAtAFilesystemBoundary` (a boundary cannot be mounted without privileges, so the device lookup is substituted); verified with a real tmpfs in the image, `docs/container.md` §3 |

### Deliberate differences from git

| case | git | mcpx | why | test |
| --- | --- | --- | --- | --- |
| a repository owned by another user (`safe.directory`) | refuses | resolves | `safe.directory` stops git *running* configuration from a repository someone else controls. Native discovery runs nothing from it; it reads three small files and five keys. And the refusal is exactly what broke the container (§1.2). The fallback does not override git's check, so an unsure layout in a foreign repository still falls back, with git's reason | `TestGitOwnershipIsNotCheckedNatively` |
| an implicit bare repository under `safe.bareRepository=explicit` | refuses | resolves | the same reasoning: the setting protects against embedded bare repositories' configuration | `TestGitSafeBareRepositoryIsNotCheckedNatively` |
| a cwd spelled in the wrong case on a case-insensitive volume | on-disk spelling | the spelling given | Getting the on-disk spelling needs `F_GETPATH`, reachable from Go only outside the `unsafe` rules; the `cwd` scope has always behaved this way; the error over-isolates. Belongs in `canonical()` for every scope: [#190](https://github.com/dezren39/nix/issues/190) | `TestGitCaseOfAWrongCaseCwdIsKept` asserts the difference is case and nothing else |
| git older than 2.31 as the fallback | prints `--path-format=absolute` back | refuses its answer | the echoed option would otherwise be read as a path. Only reachable for unsure layouts | `TestGitFallbackRejectsAGitWithoutPathFormat` |

Windows is not handled: drive letters, `\`, and `//server/share` ceilings are
not ported, because mcpx does not build there (`internal/runner` uses
`Setpgid` and `Kill`).

## 4. How it was tested

**Parity.** 54 subtests in six `TestGitParity*` functions build
repositories with the real git in a fenced temporary root --
`GIT_CEILING_DIRECTORIES` at its parent, a private global config, no system
config, none of the local variables -- and compare native discovery with `git
rev-parse` for both questions. A case that expects git to refuse first checks
that it does, so a fixture that accidentally built a valid repository cannot
pass as a refusal. They skip only when git is absent (the Nix sandbox) or
older than a feature they need. With git 2.55 on macOS none skip; with
Debian's git 2.39 on Linux (the `golang:*-bookworm` builder image, as a non-root
user) 52 pass and the two needing 2.45 and 2.48 skip. That run found the one
portability gap so far: git 2.39 counts `GIT_INTERNAL_SUPER_PREFIX` as
repository-local and newer git does not, so the scrub list is the union.

**Without git.** `TestScopesResolveWithoutGit` and three siblings clear `PATH`
and build repositories by hand -- `HEAD`, `objects/`, `refs/`, and the two
files `git worktree add` writes -- so they run in the Nix sandbox too. The e2e
`TestRepoAndWorktreeScopesResolveWithNoGitInstalled` does the same through the
real binary and a live daemon, with `call` and `exec`, and reads the keys from
`mcpx status` and the answer from `mcpx doctor`.

**Falsified.** Every test that asserts new behaviour was run against code
without it:

- The three e2e tests, against the unchanged `origin/main`: all fail, for the
  reasons they name (keys are `cwd:` without git; every caller keyed to
  `other/.git` under an absolute `GIT_DIR`; the shared instance gone after an
  exec).
- The unit tests, by mutation: 44 single-line changes to the port -- the
  walk order, the ceiling arithmetic, each gitfile refusal, each HEAD rule,
  `has_common`, the config parser's quoting, trimming and subsections, the
  format checks, the environment scrub -- each run against the suite on the
  final code. 41 are killed, each by a named test. The three that survive
  cannot change behaviour: two treat relative `GIT_CEILING_DIRECTORIES`
  entries differently, but a relative entry can never be a prefix of the
  absolute path discovery walks; the third reads the fallback's second line
  even when git fails, and git prints no second line when it fails. Five
  mutations first written so that they did not compile were rewritten, since
  a compile error proves nothing about a test. The first runs found five
  gaps, now covered: the order of the `.git` and bare checks at one level, a
  section whose *subsection* is `core`, `HEAD` values that are almost valid,
  a `.git` that cannot be stat'd, and a `.git` file over the size ceiling.

**In the image.** §3 of `docs/container.md`, "No git".

## 5. Found along the way

- **A sessionless `mcpx exec` stopped every shared instance it touched.**
  `CallContext.CallerOwned` returned true for any key when the session was
  ephemeral, so the release at the end of a run stopped the `global`, `repo`
  and `worktree` instances too -- contradicting `ReleaseCaller`'s own
  comment. Fixed in `scope.go`: an ephemeral caller owns its session's key and
  its pid's key, nothing shared. `TestOnlyKeysTheCallerMintedAreCallerOwned`,
  e2e `TestASessionlessExecLeavesASharedInstanceRunning`.
- **The degraded-scope log line says "per-call"** when the key is the
  directory: [#191](https://github.com/dezren39/nix/issues/191). Not fixed
  here; `internal/daemon/registry.go` is another area's.
- **Debian bookworm's git (2.39) refuses a checkout made by a current git**
  that used `worktree.useRelativePaths`: `unknown repository extension found:
  relativeworktrees`. Native discovery reads it, so the image is now more
  capable than the git it used to ship.
