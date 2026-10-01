# Script runtimes, permission profiles and presets

`mcpx run` and `mcpx exec` hand the generated program to a JavaScript runtime.
This page covers which runtime runs it, what it is allowed to do, and how to
bundle mcpx's own flags under a name. Every setting named here is in
[configuration.md](configuration.md) with its flag and variable.

## Choosing a runtime

`script.runtime` (`--runtime`, `MCPX_SCRIPT_RUNTIME`) is one of:

| Value | Meaning |
| --- | --- |
| `auto` (default) | the first entry of `script.runtimeOrder` that is installed **and** can enforce the permissions asked for |
| `deno`, `bun`, `node` | that binary on `PATH` |
| a name under `script.runtimes` | the declared binary, with the declared kind |
| a path or another name on `PATH` | accepted when its basename, without an executable extension, is `deno`, `bun` or `node` |

Binaries are resolved with Go's `exec.LookPath`, so `PATHEXT` is honoured on
Windows and nothing assumes `.exe`. A name that resolves to something that is
not a JavaScript runtime is refused, saying so:

```
mcpx: bash resolves to /bin/bash, which is not a known JavaScript runtime; declare it under script.runtimes with a kind (deno, bun, node)
```

### Declaring runtimes

```json
{
  "script": {
    "runtimes": {
      "mydeno": { "kind": "deno", "bin": "/opt/deno/bin/deno", "args": ["--unstable-kv"] },
      "node22": { "kind": "node", "bin": "node22" }
    },
    "runtimeOrder": ["mydeno", "bun", "node"]
  }
}
```

`kind` decides how the command line is built; `bin` is a path or a name looked
up on `PATH` (default: the declared name); `args` are runtime options placed
after mcpx's own fixed options and before the permission flags:

| Kind | argv |
| --- | --- |
| deno | `run --quiet --no-check <args> <permission flags> <script>` |
| bun | `run <args> <permission flags> <script>` |
| node | `--no-warnings --experimental-strip-types <args> <permission flags> <script>` |

The script sees `MCPX_RUNTIME` set to the **kind**, not the declared name.

`script.runtimes` and `script.profiles` are single settings holding a JSON
object, so the nearest file that sets one replaces the whole object from
files further away, as with any other setting.

## Permission profiles

`script.permissions` (`--permissions`, `MCPX_PERMISSIONS`) names profiles.
A profile maps a runtime **kind** to the flags that enforce it there. The
built-ins live in `internal/defaults/defaults.json` (`mcpx config --defaults`):

| Profile | deno | node | bun |
| --- | --- | --- | --- |
| `all` (also `none`, `off`, `unsandboxed`) — the default | `--allow-all` | no flags | no flags |
| `net` | `--allow-net --allow-env` | — | — |
| `read` | `--allow-read --allow-env` | — | — |
| `readnet` (also `read-net`) | `--allow-read --allow-net --allow-env` | `--permission --allow-fs-read=*` | — |
| `strict` | `--allow-net=127.0.0.1 --allow-env` | — | — |

**— means refused.** A runtime kind a profile does not list cannot run under
it, and mcpx says which profile, which runtime, and how to define it:

```
mcpx: permission profile "strict" defines no flags for node, so node cannot enforce it (it is defined for deno); use --runtime deno, or define script.profiles.strict.node
```

Under `auto`, a runtime that cannot enforce the profile is skipped rather than
used, so `auto` never falls back to a runtime that would ignore it
([decisions/0003](decisions/0003-declared-vs-enforced-capabilities.md)).

### What each runtime can actually enforce

- **deno** has a complete permission model; every profile above is enforced.
- **node** (checked against v24) has `--permission`, which denies file-system
  reads and writes, child processes, worker threads, addons, WASI and the
  inspector unless re-allowed with `--allow-fs-read`, `--allow-fs-write`,
  `--allow-child-process`, `--allow-worker`, `--allow-addons`, `--allow-wasi`.
  It has **no network restriction**, and it needs read access to load the
  script and the generated client. So node can genuinely enforce `readnet`
  (read anything, network open, no writes, no subprocesses, no workers) and
  nothing narrower in the network or read dimension; `net`, `read` and
  `strict` are therefore not defined for it.
- **bun** has no permission model at all. Only `all` is defined for it, and
  nothing you could define for it would be enforced.

### Defining and overriding profiles

```json
{
  "script": {
    "profiles": {
      "strict": { "deno": ["--allow-net=localhost", "--allow-env"] },
      "tmpwrite": { "deno": ["--allow-write=/tmp"], "node": ["--allow-fs-write=/tmp"] },
      "mine": "--allow-read=/srv --allow-env"
    }
  }
}
```

A user profile replaces a built-in of the same name entirely. An object maps
kinds to flag lists; a string is raw flags used for every kind. Every entry
must be a flag (start with `-`) — a bare path is refused rather than handed
to the runtime as if it were the script.

### Composition

`--permissions read,strict,tmpwrite` applies profiles in the order given:

- the result is the **union** of their flags;
- where two give the same flag (the text before `=`) with different values,
  the **later one wins**, kept in the position the flag first appeared;
- if any named profile has no entry for the runtime's kind, the whole run is
  refused, naming that profile.

So for deno `read,strict` is `--allow-read --allow-env --allow-net=127.0.0.1`,
and `strict,net` ends with `--allow-net` (unrestricted) because `net` came
later.

### Raw flags

`raw:` passes flags through verbatim and takes **the rest of the value**,
commas included, so it goes last:

```
mcpx exec --permissions 'strict,raw:--allow-read=/a,/b --allow-write=/tmp' '...'
```

Raw flags are passed to whichever runtime runs, unchecked against its kind.
A value that looks like flags but is not prefixed with `raw:` is refused with
the explicit spelling, rather than being guessed at.

## Presets

A preset is a named list of mcpx's own flags:

```json
{
  "presets": {
    "ci":    ["--json", "--log-level=warn"],
    "debug": ["--log-level=debug", "--timeout=30s"]
  }
}
```

Select with `--preset ci,debug` (or `--preset=ci,debug`), `MCPX_PRESET`, or
`"preset"` in a config file. Rules:

- presets apply in the order named; a later preset wins over an earlier one;
- any flag given explicitly on the command line wins over every preset;
- presets sit between the environment and the command line in the settings
  precedence (default < file < env < **preset** < flag < runtime override); preset definitions under `presets` are content, not unknown keys;
- each entry is one token: write values as `--flag=value`;
- a preset applies those of its flags the running command accepts and skips
  the rest, because one preset is meant to serve several commands;
- a preset cannot select other presets.

Values a preset supplied are shown as such: `mcpx settings` and
`mcpx settings get` report `preset:ci (--log-level)` as the source, with the
presets and defaults it overrode, and `mcpx config --sources` lists them under
`FROM PRESET`.
