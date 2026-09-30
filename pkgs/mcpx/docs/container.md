# Running mcpx in a container

What `Containerfile` builds, what it deliberately does not, and what was
measured along the way.

```
created:      2026-09-30T04:00:00-05:00
last-updated: 2026-09-30T04:00:00-05:00
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
| `build` | `golang:1.26-bookworm` | `go.mod` asks for 1.26.7; the image ships 1.26.8 |
| `bun` | `oven/bun:1-debian` | a pinned bun, so the build does not depend on a release URL |
| runtime | `debian:bookworm-slim` | ordinary, has glibc, has an apt for git |

All three are pinned by tag *and* digest. The tag says what it is; the digest
says which one. A tag moves, and a build that depends on when it ran is not a
build.

The runtime stage adds `git` and `ca-certificates`, copies in `bun` and
`mcpx`, drops to an unprivileged user, and sets `ENTRYPOINT ["mcpx"]`.

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

`runner.Detect` picks the first of **deno, bun, node** on `PATH`, so an image
with only bun would find bun anyway. `MCPX_SCRIPT_RUNTIME=bun` is set anyway,
because relying on absence to select a default means adding a second runtime
later silently changes which one runs. The setting is `script.runtime`
(`--script-runtime`, `script.runtime` in config); naming a runtime the image
does not contain is an error, not a fallback, so anyone who wants deno's
permission model has to add deno to the image.

That is the one real cost of choosing bun: `script.permissions` only does
something under deno. Bun and node run with the privileges of the process.
Inside a container that is a smaller gap than it is on a laptop, but it is a
gap.

## 3. git is not optional

The `repo` and `worktree` sharing scopes key server instances by the
repository the call came from. Without `git` on `PATH` they fall back to a
per-directory key, silently -- the same config produces different pooling
behaviour depending on what happens to be installed. So it is installed,
not left to whoever extends the image.

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
3. The same for `git`, which is a distribution binary with distribution
   dependencies -- and an expensive one: see §5.

None of that is blocked by this file.

## 5. Where the 413 MB goes

```
108 MB  debian:bookworm-slim
105 MB  apt-get install ca-certificates git
 79 MB  bun
 15 MB  mcpx
```

`git` costing more than bun was not expected. `--no-install-recommends` is
already set; the weight is perl, which git's Debian packaging depends on
outright. Getting rid of it means a git built without its perl subcommands, or
accepting the degraded per-directory keying that §3 exists to avoid. Recorded
rather than solved -- it is a real 105 MB and it deserves its own decision.

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
