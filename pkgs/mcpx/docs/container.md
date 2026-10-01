# Running mcpx in a container

What `Containerfile` builds, what it deliberately does not, and what was
measured along the way.

```
created:      2026-09-30T04:00:00-05:00
last-updated: 2026-09-30T07:30:00-05:00
status:       implemented
```

A starting point rather than a shipping image. The goal was the smallest thing
that actually works -- builds, runs, and can execute a script -- so that the
decisions that come next are made against something real instead of a design.

---

## 1. What it is

Three stages:

| stage | image | why |
| --- | --- | --- |
| `build` | `golang:1.27-bookworm` | `go.mod` asks for 1.26.7; the image ships 1.27.1 |
| `bun` | `oven/bun:1-debian` | a pinned bun, so the build does not depend on a release URL |
| runtime | `debian:bookworm-slim` | ordinary, and has the glibc the builder linked against |

All three are pinned by tag *and* digest. The tag says what it is; the digest
says which one. A tag moves, and a build that depends on when it ran is not a
build.

The runtime stage adds `ca-certificates`, copies in `bun` and `mcpx`, drops
to an unprivileged user, and sets `ENTRYPOINT ["mcpx"]`. It has no git; §3
says why that is not a loss.

```
docker build -f Containerfile -t mcpx .
docker run --rm mcpx --version
docker run --rm -v "$PWD/.mcpx.json:/work/.mcpx.json:ro" mcpx ls
```

## 2. The JavaScript runtime

**The image ships bun.** `mcpx exec` and `mcpx run` are the point of mcpx, and
neither can do anything without a JavaScript runtime -- `runner.Detect`
returns `no JavaScript runtime found` and the command fails. An image that
cannot run a script is an image that can only do the things a plain MCP client
could already do.

Bun because it is the smallest of the three. Measured on this machine
(linux/arm64, the binary inside each project's own Debian image):

| runtime | binary |
| --- | --- |
| bun 1.4.2 | 79.4 MB |
| deno (`denoland/deno:debian`) | 85.0 MB |
| node 22 (`node:22-bookworm-slim`) | 122.2 MB |

Smaller margins than the x86-64 figures usually quoted, but the same order.

`runner.Resolve` picks the first of `script.runtimeOrder` (by default **deno, bun, node**) on `PATH`, so an image
with only bun would find bun anyway. `MCPX_SCRIPT_RUNTIME=bun` is set anyway,
because relying on absence to select a default means adding a second runtime
later silently changes which one runs. The setting is `script.runtime`
(`--script-runtime`, `script.runtime` in config); naming a runtime the image
does not contain is an error, not a fallback, so anyone who wants deno's
permission model has to add deno to the image.

That is the one real cost of choosing bun: bun has no permission model, so
any `script.permissions` profile other than `all` is refused under it (see
[runtimes.md](runtimes.md)). Node enforces the `readnet` profile through
`--permission`; deno enforces all of them.
Inside a container that is a smaller gap than it is on a laptop, but it is a
gap.

## 3. No git

The `repo` and `worktree` sharing scopes key server instances by the
repository a call came from. They used to ask git, and the first version of
this image installed git so they could: 105 MB, most of it perl.

They no longer ask git. mcpx reads `.git` itself -- a port of git's own
discovery rules, compared against `git rev-parse` case by case in the test
suite -- and runs git only for the few layouts that port does not vouch for:
a repository format newer than it knows, an extension it has not heard of, a
config file it cannot parse. Without git those key by directory, and `mcpx
doctor` says which file it could not read. The full record, every case and
what was decided for it, is `docs/git-discovery.md`.

Taking git out cost nothing, because in this image it had not been working:

```
$ docker run --rm -v "$PWD:/work:ro" --entrypoint git mcpx:before -C /work/proj \
    rev-parse --git-common-dir
fatal: detected dubious ownership in repository at '/work/proj'
To add an exception for this directory, call:

	git config --global --add safe.directory /work/proj
```

A mounted checkout belongs to the host's uid, the image runs as 1000, and git
refuses a repository someone else owns. So with git installed, the repo and
worktree scopes fell back to per-directory keys for every mounted checkout --
the situation the old version of this section said git was there to prevent.
Native discovery does not check ownership: `safe.directory` exists to stop
git running configuration from a repository someone else controls, and
discovery runs nothing.

What was verified in the image with no git on `PATH`, one daemon for the whole
run, a real repository with a linked worktree (made with
`worktree.useRelativePaths`, so its paths survive the mount) and a submodule,
a bare repository, and a tmpfs mounted inside the checkout:

```
INSTANCE  CALLS  KEY
byrepo#2  4      repo:/work/proj/.git              proj, proj/sub, proj-wt/sub, and exec from proj-wt
byrepo#3  1      repo:/work/proj/.git/modules/mod  the submodule is its own repository
bytree#2  2      worktree:/work/proj               proj and proj/sub
bytree#3  2      worktree:/work/proj-wt            proj-wt/sub and exec
bytree#4  1      worktree:/work/proj/mod
bytree#5  1      cwd:/work/bare.git                no worktree: core.bare is true in /work/bare.git/config
bytree#6  1      cwd:/work/proj/mnt/inner          discovery stops at the mount point /work/proj/mnt
tiny#1    1      global
```

The old image's git, told to trust the mount with `safe.directory=*`, gave
the same answers for the submodule, the bare repository and the mount -- and
refused the other three directories with `unknown repository extension
found: relativeworktrees`, because Debian's git is 2.39 and the checkout was
made by 2.55. With `GIT_DISCOVERY_ACROSS_FILESYSTEM=1` in the
container's environment the mounted directory keys to `worktree:/work/proj`,
as it would in git. `mcpx ls` listed all three namespaces, `call` and `exec`
(on bun) reached each, and `mcpx doctor -v` from the linked worktree said:

```
ok    git   repo /work/proj/.git, worktree /work/proj-wt; resolved natively; no git on PATH,
            which only matters for a repository native discovery cannot read
```

A repository that does need git is visible, not silent: the key falls back to
the directory, the daemon logs why once, and `doctor` turns the line into a
warning that says to install git. Adding it back is one package in the
`apt-get install` line.

## 4. Static linking: a goal, not a state

The image links against glibc today. `CGO_ENABLED` is unset in an ordinary Go
build, which means *enabled* on a native build, and two packages change
behaviour when it is:

- `internal/logging/trace.go:9` imports `os/user`, which under cgo calls
  `getpwuid_r` through libc rather than parsing `/etc/passwd`;
- `net` is reachable from most of the tree, and under cgo its resolver is
  `getaddrinfo`, which honours `nsswitch.conf`.

So the binary is dynamically linked and will only run on a base image with a
compatible glibc. That is why the runtime stage is `debian:bookworm-slim`
rather than something smaller: it is the same glibc the builder used.

The Containerfile takes `ARG CGO_ENABLED` and uses it in the build step, so
the switch is already wired. It defaults to `1` because that is what the rest
of the build assumes, not because `0` fails.

**`CGO_ENABLED=0` builds, and the result is static and works.** Measured,
not assumed:

```
$ docker build -f Containerfile --build-arg CGO_ENABLED=0 -t mcpx:cgo0 .
$ docker run --rm --entrypoint sh mcpx:cgo0 -c 'ldd /usr/local/bin/mcpx'
	not a dynamic executable
$ docker run --rm mcpx:cgo0 --version
mcpx 0.1.0
$ docker run --rm -v "$PWD:/work:ro" mcpx:cgo0 \
    exec 'const r = await tiny.echo({ message: "cgo0" }); console.log(String(r));'
you said: cgo0
```

Same size to the byte-ish -- 14 811 298 against 14 811 401 -- so the pure-Go
`os/user` and resolver cost nothing here.

That is the surprise worth recording: **mcpx itself is not what stands in the
way of a static image.** The argument defaults to `1` because this file
records a goal rather than making the decision, but the decision is one
argument away and nothing observable breaks.

What a genuinely static *image* would still need:

1. A decision about `os/user` and `net`, for users rather than for this
   image. The pure-Go implementations are not equivalent: `os/user` parses
   `/etc/passwd` and does not consult NSS, and the resolver stops honouring
   `nsswitch.conf`. Inside a container neither matters. Outside one,
   somebody's directory service does.
2. Something to do about bun. `scratch` or distroless only pays off once
   *everything* in the image is static, and bun is not -- it is dynamically
   linked against glibc. A static mcpx beside a dynamic bun on
   `debian:bookworm-slim` saves nothing at all. Going further means dropping
   `exec` from the image, or a musl build of everything.
None of that is blocked by this file. git used to be a third item here; it
is no longer in the image (§3).

## 5. Where the size goes

Measured on linux/arm64 with the digests pinned in `Containerfile`, before
and after §3, both built on the same machine:

| layer | with git | without |
| --- | --- | --- |
| `debian:bookworm-slim` | 108 MB | 108 MB |
| `apt-get install` | 105 MB (`ca-certificates git`) | 10.4 MB (`ca-certificates`) |
| bun | 79.4 MB | 79.4 MB |
| mcpx | 14.8 MB | 14.9 MB |
| **layers, uncompressed** | **307 MB** | **213 MB** |
| compressed content | 106 MB | 75.6 MB |
| `docker images` disk usage | 413 MB | 288 MB |

The 413 MB this document used to quote is the last row, which is the
uncompressed layers plus the compressed blobs they were unpacked from. The
layers are what a running container occupies; the compressed size is what a
pull transfers.

`git` costing more than bun was not expected: with `--no-install-recommends`
already set, the weight is perl, which Debian's git depends on outright.
What remains of the apt layer is `ca-certificates` and the openssl it pulls
in, for the registry. `perl-base` is still in the image -- it is part of
Debian's essential set, in the base layer -- but nothing in mcpx uses it.

## 6. What was deliberately left out

- **No CI job.** Building an image on every push is a cost, and the workflow
  restructure is its own piece of work.
- **No container tests.** The e2e suite spawns daemons and binds unix sockets;
  running it inside an image is a different harness, not a flag.
- **No scratch or distroless variant**, for the reason in §4.2.
- **Not wired into `package.nix`.** The Nix build and the container build are
  two ways to produce the same binary; making one depend on the other buys
  nothing yet.

## 7. What to watch

The `.dockerignore` excludes `result` -- a dangling symlink into the Nix store
that does not exist inside the build. If a `nix build` has been run in the
tree and this file is removed, the container build will fail on a broken
symlink, and the error will not say why.
