#!/usr/bin/env python3
"""flake-tidy: deduplicate, merge, and flatten flake inputs by adding follows declarations.

Actions:
  dedup    Add follows for duplicate inputs (transitive + same-URL root inputs)
  merge    Hoist deep-followed transitive inputs to root, replacing deep follows
  flatten  Hoist transitive-only inputs to root level, then add follows
  all      Run merge then dedup then flatten then dedup again (default)

Flags:
  --dry-run  Show what would change without modifying files
  --check    Like --dry-run but exit 1 if changes needed (for CI)
  --verbose  Show detailed information about skipped items

Config loaded from (in order):
  1. --config FILE (or $FLAKE_TIDY_CONFIG) — a JSON file; works inside a nix
     build sandbox where no `nix` subprocess can reach a store/daemon
  2. nix eval .#flakeTidy --json
  3. nix eval --impure import probe of flake.nix's outputs (lazy: forces only
     the flakeTidy attribute, so it needs no store access)
  4. Defaults — but in --check mode, if no source above could even be
     attempted successfully, the tool errors loudly instead of silently
     checking against defaults (see #476)

Config shape:
  {
    "max-depth": 6,
    "include": {
      "input": ["*"],
      "dedup": ["*"],
      "merge": ["*"],
      "flatten": ["*"]
    },
    "exclude": {
      "input": [],
      "input-url": [],
      "follows": [],
      "follows-url": [],
      "dedup": [],
      "merge": [],
      "flatten": []
    }
  }
"""

from __future__ import annotations

import argparse
import datetime
import functools
import ipaddress
import json
import os
import re
import subprocess
import sys
from collections import defaultdict
from itertools import groupby
from typing import Any
from urllib.parse import quote


# ---------------------------------------------------------------------------
# Config: schema, defaults, validation, loading, merging
# ---------------------------------------------------------------------------

DEFAULT_CONFIG: dict[str, Any] = {
    "max-depth": 6,  # 0 = unlimited
    "include": {
        "input": ["*"],  # global: process all inputs
        "dedup": ["*"],  # dedup: process all
        "merge": ["*"],  # merge: process all
        "flatten": ["*"],  # flatten: process all
    },
    "exclude": {
        "input": [],  # global: excluded from everything
        "input-url": [],  # global: excluded by URL
        "follows": [],  # excluded follows paths (e.g. "home-manager.nixpkgs")
        "follows-url": [],  # excluded follows targets by URL
        "dedup": [],  # input names excluded from dedup only
        "merge": [],  # input names excluded from merge only
        "flatten": [],  # input names excluded from flatten only
    },
}

CONFIG_SCHEMA: dict[str, Any] = {
    "max-depth": int,
    "include": {
        "input": list,
        "dedup": list,
        "merge": list,
        "flatten": list,
    },
    "exclude": {
        "input": list,
        "input-url": list,
        "follows": list,
        "follows-url": list,
        "dedup": list,
        "merge": list,
        "flatten": list,
    },
}


def validate_config(cfg: dict[str, Any]) -> tuple[list[str], list[str]]:
    """Validate config against schema. Returns (errors, warnings)."""
    errors: list[str] = []
    warnings: list[str] = []

    if not isinstance(cfg, dict):
        errors.append("config must be a dict")
        return errors, warnings

    known_top = set(CONFIG_SCHEMA.keys())
    for key in cfg:
        if key not in known_top:
            warnings.append(f"unknown config key: '{key}'")

    if "max-depth" in cfg:
        if not isinstance(cfg["max-depth"], (int, float)):
            errors.append(
                f"max-depth must be int, got {type(cfg['max-depth']).__name__}"
            )
        elif cfg["max-depth"] < 0:
            errors.append("max-depth must be >= 0")

    for section_name in ("include", "exclude"):
        if section_name not in cfg:
            continue
        section = cfg[section_name]
        if not isinstance(section, dict):
            errors.append(f"{section_name} must be a dict")
            continue
        schema_section = CONFIG_SCHEMA.get(section_name, {})
        for key in section:
            if key not in schema_section:
                warnings.append(f"unknown {section_name} key: '{key}'")
            elif not isinstance(section[key], schema_section[key]):
                errors.append(
                    f"{section_name}.{key} must be {schema_section[key].__name__}, "
                    f"got {type(section[key]).__name__}"
                )

    return errors, warnings


def _deep_copy_config(cfg: dict[str, Any]) -> dict[str, Any]:
    """Deep copy a config dict (only dicts and lists, no complex types)."""
    result: dict[str, Any] = {}
    for k, v in cfg.items():
        if isinstance(v, dict):
            result[k] = _deep_copy_config(v)
        elif isinstance(v, list):
            result[k] = list(v)
        else:
            result[k] = v
    return result


def merge_config(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """Deep merge override into base config."""
    result = _deep_copy_config(base)
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = merge_config(result[key], value)
        else:
            if isinstance(value, list):
                result[key] = list(value)
            elif isinstance(value, dict):
                result[key] = _deep_copy_config(value)
            else:
                result[key] = value
    return result


class ConfigLoadError(Exception):
    """The flakeTidy config could not be loaded from any source."""


def _load_config_file(path: str, verbose: bool = False) -> dict[str, Any]:
    """Load config JSON from an explicit file. Any failure is fatal: the
    caller asked for this exact file, so silently using another source (or
    defaults) would check against the wrong configuration (#476)."""
    try:
        with open(path) as f:
            cfg = json.load(f)
    except OSError as e:
        raise ConfigLoadError(f"cannot read config file {path}: {e}") from e
    except json.JSONDecodeError as e:
        raise ConfigLoadError(f"config file {path} is not valid JSON: {e}") from e
    if not isinstance(cfg, dict):
        raise ConfigLoadError(f"config file {path} must contain a JSON object")
    if verbose:
        print(f"  config: loaded from file {path}")
    return cfg


def _nix_string_literal(value: str) -> str:
    """Render a Python string as a Nix double-quoted string literal.

    Escapes the characters Nix treats specially inside a string —
    backslash, the quote itself, and the `${` antiquotation opener —
    plus the whitespace control characters."""
    escaped = (
        value.replace("\\", "\\\\")
        .replace('"', '\\"')
        .replace("${", "\\${")
        .replace("\n", "\\n")
        .replace("\r", "\\r")
        .replace("\t", "\\t")
    )
    return f'"{escaped}"'


def load_config(
    flake_dir: str,
    verbose: bool = False,
    config_file: str | None = None,
    strict: bool = False,
) -> dict[str, Any]:
    """Load flakeTidy config with fallback chain.

    Sources, in order: an explicit config file (--config /
    $FLAKE_TIDY_CONFIG), `nix eval .#flakeTidy`, and an --impure import
    probe of the flake's outputs. The import probe doubles as the
    "no config declared" detector: it succeeds (with {}) for a flake that
    declares no flakeTidy output, which legitimately means defaults.

    If NO source succeeds — e.g. inside a nix build sandbox, where a `nix`
    subprocess has no store/daemon access — the config is unknowable, not
    empty. In strict mode (check mode) that raises ConfigLoadError instead
    of silently checking against defaults (#476); otherwise it falls back
    to defaults with the failures shown in verbose mode.
    """
    cfg: dict[str, Any] | None = None
    failures: list[str] = []

    explicit = config_file or os.environ.get("FLAKE_TIDY_CONFIG")
    if explicit:
        cfg = _load_config_file(explicit, verbose=verbose)

    # Try 1: nix eval .#flakeTidy --json (full flake evaluation)
    if cfg is None:
        try:
            r = subprocess.run(
                ["nix", "eval", ".#flakeTidy", "--json"],
                capture_output=True,
                text=True,
                cwd=flake_dir,
                timeout=30,
            )
            if r.returncode == 0 and r.stdout.strip():
                cfg = json.loads(r.stdout)
                if verbose:
                    print("  config: loaded from nix eval .#flakeTidy")
            else:
                detail = (r.stderr or r.stdout).strip().splitlines()
                failures.append(
                    "nix eval .#flakeTidy: exit "
                    f"{r.returncode}" + (f": {detail[-1]}" if detail else "")
                )
        except Exception as e:
            failures.append(f"nix eval .#flakeTidy: {e}")

    # Try 2: nix eval --impure import probe. flakeTidy lives under the
    # flake's *outputs*, not at the top level of flake.nix — the old probe
    # read `(import ./flake.nix).flakeTidy`, which is always {} (#476).
    # Calling outputs with {} and selecting only flakeTidy stays lazy, so
    # the probe needs no store access and works with the packaged nix even
    # when full flake evaluation (Try 1) fails.
    probe_ok = False
    if cfg is None:
        # The path must reach Nix as a quoted string, not as a path
        # literal: a path literal cannot contain a space (or several
        # other characters legal in directory names), so interpolating
        # flake_dir unquoted made the probe fail for any checkout under
        # e.g. "~/My Projects" (#487). builtins.toPath needs an absolute
        # path, so resolve flake_dir first (main() already passes one).
        flake_nix = os.path.join(os.path.abspath(flake_dir), "flake.nix")
        expr = (
            f"let f = import (builtins.toPath {_nix_string_literal(flake_nix)}); "
            "o = (f.outputs or (_: {})) {}; "
            "in o.flakeTidy or f.flakeTidy or {}"
        )
        try:
            r = subprocess.run(
                ["nix", "eval", "--impure", "--expr", expr, "--json"],
                capture_output=True,
                text=True,
                cwd=flake_dir,
                timeout=30,
            )
            if r.returncode == 0 and r.stdout.strip():
                cfg = json.loads(r.stdout)
                probe_ok = True
                if verbose:
                    if cfg:
                        print("  config: loaded from import ./flake.nix outputs")
                    else:
                        print("  config: no flakeTidy declared; using defaults")
            else:
                detail = (r.stderr or r.stdout).strip().splitlines()
                failures.append(
                    "nix eval --impure import probe: exit "
                    f"{r.returncode}" + (f": {detail[-1]}" if detail else "")
                )
        except Exception as e:
            failures.append(f"nix eval --impure import probe: {e}")

    if cfg is None:
        if strict:
            raise ConfigLoadError(
                "could not load flakeTidy config from any source; refusing "
                "to check against defaults. Failures: "
                + ("; ".join(failures) if failures else "no source attempted")
                + ". Supply the config with --config FILE (or "
                "$FLAKE_TIDY_CONFIG) when running where `nix eval` cannot "
                "work, e.g. inside a nix build sandbox."
            )
        cfg = {}
        if verbose:
            print("  config: using defaults")
            for failure in failures:
                print(f"  config: ({failure})")

    # Validate
    errors, warnings = validate_config(cfg)
    for w in warnings:
        print(f"  config warning: {w}", file=sys.stderr)
    for e in errors:
        print(f"  config error: {e}", file=sys.stderr)
    if errors:
        if explicit or strict:
            raise ConfigLoadError(
                "flakeTidy config is invalid: " + "; ".join(errors)
            )
        print("  falling back to defaults due to config errors", file=sys.stderr)
        cfg = {}

    return merge_config(DEFAULT_CONFIG, cfg)


def merge_cli_into_config(
    config: dict[str, Any], args: argparse.Namespace
) -> dict[str, Any]:
    """Merge CLI arguments into config. Includes REPLACE, excludes APPEND."""
    config = _deep_copy_config(config)

    if args.max_depth is not None:
        config["max-depth"] = args.max_depth

    # Includes: CLI replaces config
    if args.include:
        config["include"]["input"] = args.include
    if args.include_dedup:
        config["include"]["dedup"] = args.include_dedup
    if args.include_merge:
        config["include"]["merge"] = args.include_merge
    if args.include_flatten:
        config["include"]["flatten"] = args.include_flatten

    # Excludes: CLI appends to config
    if args.exclude_input:
        config["exclude"]["input"].extend(args.exclude_input)
    if args.exclude_dedup:
        config["exclude"]["dedup"].extend(args.exclude_dedup)
    if args.exclude_merge:
        config["exclude"]["merge"].extend(args.exclude_merge)
    if args.exclude_flatten:
        config["exclude"]["flatten"].extend(args.exclude_flatten)

    return config


# ---------------------------------------------------------------------------
# Include / exclude checks
# ---------------------------------------------------------------------------


def _matches_include(include_list: list[str], name: str) -> bool:
    """Check if name is included. ["*"] means include everything."""
    if include_list == ["*"]:
        return True
    return name in include_list


def should_include(config: dict[str, Any], operation: str, name: str) -> bool:
    """Check if an input name is included for a given operation.

    Checks global include.input first, then operation-specific include.
    """
    if not _matches_include(config["include"]["input"], name):
        return False
    op_include = config["include"].get(operation, ["*"])
    return _matches_include(op_include, name)


def should_exclude(
    config: dict[str, Any], operation: str, name: str, url: str = ""
) -> bool:
    """Check if an input name should be excluded from an operation.

    Checks global excludes, then operation-specific excludes.
    """
    if name in config["exclude"]["input"]:
        return True
    if url and url in config["exclude"]["input-url"]:
        return True
    op_excludes = config["exclude"].get(operation, [])
    if name in op_excludes:
        return True
    return False


def is_excluded_follows(config: dict[str, Any], follows_path: str) -> bool:
    """Check if a follows path like 'home-manager.nixpkgs' is excluded."""
    return follows_path in config["exclude"]["follows"]


def is_excluded_follows_url(config: dict[str, Any], url: str) -> bool:
    """Check if a follows target URL is excluded."""
    return url in config["exclude"]["follows-url"]


def is_excluded_full(
    config: dict[str, Any],
    lock: dict,
    operation: str,
    follows_path: str,
    follows_parts: list[str],
    target: str,
    target_node: str,
) -> bool:
    """Combined exclusion check for a proposed follows."""
    nodes = lock["nodes"]

    # Check input exclusion (first input in path)
    if should_exclude(config, operation, follows_parts[0]):
        return True

    # Check target exclusion
    if should_exclude(config, operation, target):
        return True

    # Check follows path exclusion
    if is_excluded_follows(config, follows_path):
        return True

    # Check follows-url exclusion on the target node
    target_data = nodes.get(target_node, {})
    t_url = node_url(target_data)
    if is_excluded_follows_url(config, t_url):
        return True

    return False


# ---------------------------------------------------------------------------
# Lock file analysis
# ---------------------------------------------------------------------------


def load_lock(flake_dir: str) -> dict:
    """Load and parse flake.lock."""
    with open(os.path.join(flake_dir, "flake.lock")) as f:
        return json.load(f)


# Hosts that route repository paths case-insensitively: a git URL to
# one of them whose path differs only in case names the same repo the
# forge branch already keys together by lowercasing owner/repo.
_CASE_INSENSITIVE_PATH_HOSTS = ("github.com", "gitlab.com")

# Well-known default port per scheme (#579): an explicit default port
# (":443" on an https URL) names the same endpoint as no port at all,
# so it is dropped from the grouping key. A non-default port can be a
# genuinely different server and is always kept.
_DEFAULT_PORTS = {"http": 80, "https": 443, "ssh": 22, "git": 9418}

# Default server per forge type (#605): an explicit host naming the
# type's own default (e.g. ?host=github.com on a github input, which
# Nix records verbatim in the lock's original) names the same server
# as no host at all, so it must not split the dedup group — the host
# analogue of the #579 default-port rule. Any other host (GitHub
# Enterprise, a self-hosted GitLab, hg.sr.ht) stays distinguishing.
_FORGE_DEFAULT_HOSTS = {
    "github": "github.com",
    "gitlab": "gitlab.com",
    "sourcehut": "git.sr.ht",
}


def _canonical_ipv6_literal(host: str) -> str:
    """Canonicalize a bracketed IPv6 literal host for grouping (#583).

    `host` carries the leading "[" and no "]" (the caller split on
    "]"). The address is emitted in canonical compressed form, so
    leading zeros, "::" compression, hex case, and the IPv4-embedded
    spelling of one address all key together. A zone/scope suffix
    ("%…") is kept verbatim — it names an interface, not part of the
    address spelling. Contents that do not parse as an IPv6 address
    fall back to the #581 lowercased-verbatim spelling; a malformed
    literal must never raise out of grouping.
    """
    inner = host[1:] if host.startswith("[") else host
    addr, pct, zone = inner.partition("%")
    try:
        canonical = ipaddress.IPv6Address(addr).compressed
    except ValueError:
        return host.lower()
    return "[" + canonical + (pct + zone if pct else "")


def _normalize_vcs_url(url: str) -> str:
    """Canonicalize a git/hg URL spelling for grouping (#555).

    The same repository is routinely written several ways across a
    flake's transitive inputs, and grouping embeds the URL verbatim,
    so the copies never dedup. Rules — each conservative, none merges
    different hosts or (after normalization) different paths:

    - trailing slashes are dropped ("repo/" == "repo"), and one
      trailing ".git" suffix is dropped ("repo.git" == "repo",
      including "repo.git/");
    - scheme and host are lowercased (both are case-insensitive);
    - the path is lowercased only for hosts that route paths
      case-insensitively (see _CASE_INSENSITIVE_PATH_HOSTS) — in
      every spelling family, so an scp-like ssh path gets the same
      rule the "://" branch applies (#562);
    - where the path is case-normalized that way, a ".git" suffix in
      any case (".GIT", ".Git") is also dropped: lowercasing turns it
      into ".git" after the case-sensitive early strip has run
      (#562). At most ONE ".git" suffix is dropped in total (#575):
      if the early strip already removed an exact ".git", the
      post-lowercase strip does not fire, so "repo.git.git" keys as
      "repo.git" (a repository actually named repo.git), not "repo".
      On case-sensitive hosts a ".GIT" suffix is KEPT —
      there "repo.GIT" can be a genuinely different repository from
      "repo", so only the exact ".git" spelling is stripped;
    - an embedded query string is split off before all of the above
      and reattached verbatim (#560): the suffix rules apply to the
      path ahead of it, and case-sensitive query values (dir, ref)
      are never lowercased;
    - an explicit port is dropped when it is the scheme's well-known
      default (http 80, https 443, ssh 22, git 9418) (#579): it names
      the same endpoint as no port. On the compound schemes Nix
      prefixes git/hg flake URLs with ("git+https", "hg+ssh", …) the
      default is the transport's, after the "+" (#595). Any other
      port can be a genuinely different server, so it is kept — but
      in its canonical integer
      spelling (#583): a port is a number, so ":08443" and ":8443"
      name the same server, and an empty port ("host:") names the
      default and is dropped like one. Non-numeric ports stay
      verbatim, as before;
    - a bracketed IPv6 literal host is lowercased like any other host
      (#581): its hex digits are case-insensitive, so "[2001:DB8::1]"
      and "[2001:db8::1]" name the same server. (The #579 port parsing
      reassembled the literal verbatim and skipped the lowercasing
      the plain-host branch applies.) The address itself is also
      emitted in canonical compressed form (#583): leading zeros,
      "::" compression, and the IPv4-embedded spelling are all the
      same address ("[2001:0db8::1]", "[2001:db8:0:0:0:0:0:1]" and
      "[2001:db8::1]"; "[::ffff:192.0.2.1]" and "[::ffff:c000:201]").
      A zone/scope suffix ("%…") is kept verbatim, and bracketed
      contents that do not parse as an IPv6 address fall back to the
      #581 lowercased-verbatim spelling — never an error;
    - one trailing dot is stripped from the host (#584): it is the
      DNS root label, so "github.com." and "github.com" name the same
      server — in every spelling family. Stripping it before the
      _CASE_INSENSITIVE_PATH_HOSTS test also keeps the path case rule
      applying to trailing-dot spellings of forge hosts.

    Deliberate non-rules (#555, decided here so the behaviour is a
    documented choice rather than an accident): ssh spellings
    (git@host:path, ssh://…) and https spellings of the same
    host/path do NOT canonicalize together — they are different
    transports that can resolve through different mirrors and
    credentials. The tarball/file branches likewise keep their raw
    URL past the authority: those name a specific fetched artifact,
    where a path/query spelling difference can be a genuinely
    different location. (Only their scheme + host + port are
    canonicalized, by _canonicalize_url_authority — #611: a DNS host
    spelling can never name a different location.)
    """
    # Split any embedded query off FIRST (#560): a raw git URL may
    # already carry e.g. ?shallow=1 (#528/#544/#546). Treating the URL
    # as one flat string defeated the suffix rules below (".git" is
    # not at the end of "repo.git?shallow=1", so it was never
    # stripped) and lowercased case-sensitive query values (?dir=,
    # ?ref=) on the case-insensitive-path hosts. The query is
    # reattached verbatim at the end.
    u, qsep, query = url.partition("?")
    # Suffix canonicalization on the base spelling: trailing slashes
    # go first so "repo.git/" also loses its one ".git" suffix.
    u = u.rstrip("/")
    stripped_dot_git = False
    if u.endswith(".git"):
        u = u[: -len(".git")].rstrip("/")
        stripped_dot_git = True
    if "://" in u:
        scheme, rest = u.split("://", 1)
        authority, sep, path = rest.partition("/")
        userinfo, at, hostport = authority.rpartition("@")
        if hostport.startswith("[") and "]" in hostport:
            # IPv6 literal: the host runs to "]", the port follows it.
            # Hex digits are case-insensitive — lowercase the literal
            # like the plain-host branch below (#581).
            host, _, tail = hostport.partition("]")
            host = _canonical_ipv6_literal(host) + "]"
            colon, port = (":", tail[1:]) if tail.startswith(":") else ("", "")
        else:
            host, colon, port = hostport.partition(":")
            host = host.lower()
            # #584: a trailing dot is the DNS root label — "github.com."
            # is the same host as "github.com". Strip ONE trailing dot
            # so the spelling does not split the group (and so the
            # _CASE_INSENSITIVE_PATH_HOSTS test below sees the real host).
            if len(host) > 1 and host.endswith("."):
                host = host[:-1]
        # #579: drop an explicit default port — same endpoint, so it
        # must not split the group. Non-default or non-numeric ports
        # (and ports on schemes with no known default) stay verbatim.
        # #589: isdigit() accepts spellings int() rejects (superscript/
        # circled digits) and int() refuses over-long digit strings,
        # so convert once under a guard — an unconvertible port is
        # effectively non-numeric and keys verbatim, never raises.
        port_num = None
        if port.isdigit():
            try:
                port_num = int(port)
            except ValueError:
                port_num = None
        # #595: Nix prefixes git/hg flake URLs with a compound scheme
        # ("git+https", "hg+ssh", …) — the transport after the "+" is
        # what carries the default port, so look that up, not the raw
        # scheme.
        transport = scheme.lower()
        for prefix in ("git+", "hg+"):
            if transport.startswith(prefix):
                transport = transport[len(prefix) :]
                break
        if port_num is not None and _DEFAULT_PORTS.get(transport) == port_num:
            colon, port = "", ""
        elif port == "":
            # #583: an empty port ("host:") names the default port —
            # URL serialization drops the colon — so drop it too,
            # whether or not a port separator was present.
            colon = ""
        elif port_num is not None:
            # #583: a port is a number — emit its canonical integer
            # spelling so ":08443" groups with ":8443".
            port = str(port_num)
        authority = f"{userinfo}{at}{host}{colon}{port}"
        if sep and host in _CASE_INSENSITIVE_PATH_HOSTS:
            path = path.lower()
            # #562: lowercasing may have just turned a ".GIT"/".Git"
            # suffix into ".git" — drop that one suffix too (the
            # early strip above only sees the exact spelling).
            # #575: only when the early strip did NOT already fire —
            # at most one ".git" is dropped in total, otherwise
            # "repo.git.git" would lose both suffixes and group with
            # the different repository "repo".
            if not stripped_dot_git and path.endswith(".git"):
                path = path[: -len(".git")].rstrip("/")
        u = f"{scheme.lower()}://{authority}{sep}{path}"
    elif "@" in u and ":" in u.split("@", 1)[1]:
        # scp-like ssh spelling, git@host:path — lowercase the host;
        # the user keeps its spelling and ssh stays a separate group
        # from https regardless (see docstring). The path gets the
        # same case rule as the "://" branch (#562): hosts in
        # _CASE_INSENSITIVE_PATH_HOSTS route it case-insensitively,
        # so case alone must not split the group within the scp
        # family either.
        userhost, _, path = u.partition(":")
        user, _, host = userhost.rpartition("@")
        host = host.lower()
        # #584: same trailing-dot rule as the "://" branch.
        if len(host) > 1 and host.endswith("."):
            host = host[:-1]
        if host in _CASE_INSENSITIVE_PATH_HOSTS:
            path = path.lower()
            # #575: same at-most-one rule as the "://" branch.
            if not stripped_dot_git and path.endswith(".git"):
                path = path[: -len(".git")].rstrip("/")
        u = f"{user}@{host}:{path}"
    return u + (qsep + query if qsep else "")


def _normalize_ref(ref: str) -> str:
    """Canonicalize a ref spelling for grouping (#555).

    "refs/heads/<name>" and "<name>" name the same branch. Only the
    heads prefix is stripped: a bare "<name>" could equally be a tag,
    so "refs/tags/<name>" is left alone rather than risk merging a
    branch group with a tag group.
    """
    if ref.startswith("refs/heads/"):
        return ref[len("refs/heads/") :]
    return ref


def _canonicalize_url_authority(url: str) -> str:
    """Canonicalize ONLY the scheme + authority of a URL for grouping (#611).

    The tarball/file branches of source_key embed their URL verbatim,
    so they never received the host rules the git/hg branch applies
    via _normalize_vcs_url (#579/#581/#583/#584) and the forge branch
    applies to its host extra (#599/#605): a DNS host is
    case-insensitive, one trailing dot is the DNS root label, and an
    explicit scheme-default port names the same endpoint as no port.
    Two lock nodes fetching the same artifact from the same server
    spelled "EXAMPLE.com", "example.com." and "example.com:443" split
    the dedup group.

    Everything after the authority — userinfo, path, query, fragment —
    stays byte-for-byte verbatim: #555's deliberate non-rule stands
    for those, because a different path or query really can name a
    different artifact. URLs without a "://" authority are returned
    unchanged.

    The authority ends at the first of "/", "?" or "#" (#613): a
    path-less URL such as "https://example.com:443?file=A.tgz" carries
    its query directly after the authority, and splitting on "/" alone
    swallowed the query into the host/port parsing — lowercasing a
    case-sensitive query value into the key (grouping genuinely
    different artifacts) and defeating the default-port drop (the
    "port" parsed as "443?file=…", non-numeric, kept verbatim).
    """
    if "://" not in url:
        return url
    scheme, rest = url.split("://", 1)
    # The tail (delimiter included) is reattached verbatim at the end.
    cut = min(
        (i for i in (rest.find("/"), rest.find("?"), rest.find("#")) if i != -1),
        default=len(rest),
    )
    authority, sep, tail = rest[:cut], rest[cut : cut + 1], rest[cut + 1 :]
    userinfo, at, hostport = authority.rpartition("@")
    if hostport.startswith("[") and "]" in hostport:
        # IPv6 literal: the host runs to "]", the port follows it.
        host, _, porttail = hostport.partition("]")
        host = _canonical_ipv6_literal(host) + "]"
        colon, port = (":", porttail[1:]) if porttail.startswith(":") else ("", "")
    else:
        host, colon, port = hostport.partition(":")
        host = host.lower()
        # #584: one trailing dot is the DNS root label.
        if len(host) > 1 and host.endswith("."):
            host = host[:-1]
    # #579/#583 port rules, mirroring _normalize_vcs_url: drop an
    # explicit default port, drop an empty port, and emit any other
    # numeric port in canonical integer spelling. The guarded int()
    # conversion is #589's: isdigit() accepts spellings int() rejects,
    # and an unconvertible port keys verbatim, never raises.
    port_num = None
    if port.isdigit():
        try:
            port_num = int(port)
        except ValueError:
            port_num = None
    if port_num is not None and _DEFAULT_PORTS.get(scheme.lower()) == port_num:
        colon, port = "", ""
    elif port == "":
        colon = ""
    elif port_num is not None:
        port = str(port_num)
    return f"{scheme.lower()}://{userinfo}{at}{host}{colon}{port}{sep}{tail}"


def source_key(node_data: dict) -> str:
    """Compute a grouping key from a node's 'original' field.

    Nodes with the same source key are candidates for dedup.
    """
    orig = node_data.get("original", {})
    t = orig.get("type", "")

    if t in ("github", "gitlab", "sourcehut"):
        owner = orig.get("owner", "").lower()
        repo = orig.get("repo", "").lower()
        ref = _normalize_ref(orig.get("ref", ""))
        # Any other original field DOES distinguish the source (#535,
        # the git branch's #519 treatment applied to the forge types):
        # host names a different server (GitHub Enterprise, a
        # self-hosted GitLab, hg.sr.ht instead of git.sr.ht) and dir
        # names a different sub-flake of the same repo. Both stay in
        # the key as a sorted JSON suffix; nodes without extra fields
        # keep the exact pre-#535 key shape.
        extras = {
            k: v
            for k, v in orig.items()
            if k not in ("type", "owner", "repo", "ref", "rev")
        }
        if isinstance(extras.get("host"), str):
            # #599: a host is a DNS name — case-insensitive, and one
            # trailing dot is the DNS root label — so the host extra is
            # canonicalized the same way #584 treats hosts inside
            # _normalize_vcs_url. Otherwise one enterprise/self-hosted
            # server written "GitHub.Example.com" and "github.example.com"
            # splits the dedup group despite owner/repo being lowercased.
            host = extras["host"].lower()
            if len(host) > 1 and host.endswith("."):
                host = host[:-1]
            extras["host"] = host
            # #605: an explicit host that IS the type's default server
            # distinguishes nothing — drop it, so the node groups with
            # the identical hostless input (as an explicit default port
            # is dropped in _normalize_vcs_url, #579).
            if host == _FORGE_DEFAULT_HOSTS.get(t):
                del extras["host"]
        extra = f"/{json.dumps(extras, sort_keys=True)}" if extras else ""
        if "rev" in orig:
            # The pinned rev VALUE is deliberately NOT part of the key
            # (#509): two pins of the same repo at different revs must
            # land in one dedup group, or the stale pin is invisible to
            # the tool. Which rev survives is decided in analyze_dedup
            # from the locked lastModified, not by grouping.
            # But pin-NESS is part of the key (#512): a rev-pinned node
            # must never group with an unpinned (default-branch) or
            # ref-only node of the same repo — consolidating those would
            # silently discard a deliberate pin, or freeze a tracking
            # input at a fixed rev. ":" cannot appear in a git ref, so
            # the marker can never collide with a real ref component.
            return f"{t}/{owner}/{repo}/{ref}:rev-pinned{extra}"
        return f"{t}/{owner}/{repo}/{ref}{extra}"
    elif t in ("git", "hg"):
        # URL and ref spellings are canonicalized (#555) so the same
        # repo written with a trailing slash, a ".git" suffix, a
        # differently-cased host, or a fully-qualified branch ref
        # groups with its other spellings. The ORIGINAL url stays in
        # the node — only the grouping key is canonical.
        url = _normalize_vcs_url(orig.get("url", ""))
        ref = _normalize_ref(orig.get("ref", ""))
        # Same rev treatment as the forge types above (#519): the pinned
        # rev VALUE is not part of the key (#509 — two pins of one repo at
        # different revs must group), but pin-NESS is (#512 — a pinned
        # node must never group with an unpinned or ref-only one). Any
        # other original field (e.g. submodules) DOES distinguish the
        # source, so it stays in the key as a sorted JSON suffix.
        # hg (#539) shares git's url+ref/rev shape, so it takes the
        # same branch; before this it fell through to the JSON dump.
        extras = {
            k: v
            for k, v in orig.items()
            if k not in ("type", "url", "ref", "rev")
        }
        extra = f"/{json.dumps(extras, sort_keys=True)}" if extras else ""
        if "rev" in orig:
            return f"{t}/{url}/{ref}:rev-pinned{extra}"
        return f"{t}/{url}/{ref}{extra}"
    elif t == "tarball":
        # dir is a generic flake-ref attribute (#537): two sub-flakes
        # of one tarball must not share a dedup group. Keep the exact
        # pre-#537 key shape when there is no dir.
        extra = (
            f"/{json.dumps({'dir': orig['dir']}, sort_keys=True)}"
            if "dir" in orig
            else ""
        )
        return f"tarball/{_canonicalize_url_authority(orig.get('url', ''))}{extra}"
    elif t == "file":
        extra = (
            f"/{json.dumps({'dir': orig['dir']}, sort_keys=True)}"
            if "dir" in orig
            else ""
        )
        return f"file/{_canonicalize_url_authority(orig.get('url', ''))}{extra}"
    elif t == "path":
        extra = (
            f"/{json.dumps({'dir': orig['dir']}, sort_keys=True)}"
            if "dir" in orig
            else ""
        )
        return f"path/{orig.get('path', '')}{extra}"
    elif t == "indirect":
        return f"indirect/{orig.get('id', '')}"
    else:
        return json.dumps(orig, sort_keys=True)


def is_rev_pinned(node_data: dict) -> bool:
    """Check if a node's 'original' pins an exact rev (#512).

    A rev pin is a deliberate versioning decision; dedup must not pair
    a pinned node with an unpinned or ref-tracking one.
    """
    return "rev" in node_data.get("original", {})


def is_path_input(node_data: dict) -> bool:
    """Check if a node is a local path input (can't be hoisted/followed)."""
    return node_data.get("original", {}).get("type") == "path"


def _encode_query_value(value) -> str:
    """Percent-encode a query value the way Nix's encodeQuery does (#601).

    The lock records flake-ref attribute values decoded, so a value
    interpolated raw into a rebuilt URL does not parse back to itself:
    "&"/"=" split it into extra parameters (a dir named "x&rev=…" would
    forge a rev pin), "#" starts a fragment, "%" mis-decodes, a space
    is not a URL character. Nix's serializer (flakeRefToString) leaves
    alphanumerics and -._~/?:@ raw and percent-encodes every other
    UTF-8 byte as uppercase %XX — mirror that exactly, so ordinary
    values (plain refs, hex revs, normal dirs/hosts) are byte-identical
    and only the corrupt spellings change.
    """
    return quote(str(value), safe="-._~/?:@")


def _encode_path_value(value) -> str:
    """Percent-encode a value placed in a URL path position (#601).

    Used for the forge ref/rev segment and the path: value in the
    Nix-facing builders: "/" stays raw (Nix parses multi-segment forge
    refs and slash-carrying paths exactly as it does today), while the
    characters that break out of the path — "#", "?", "%", spaces and
    other non-URL bytes — are encoded so the value round-trips.
    """
    return quote(str(value), safe="/-._~:@")


def _forge_query(spec: dict, encode: bool = False) -> str:
    """Query string carrying a forge spec's host/dir, or "" (#535).

    host points at a non-default server (GitHub Enterprise, a
    self-hosted GitLab, hg.sr.ht) and dir at a sub-flake of the repo;
    a rebuilt URL without them names a different source than the lock
    records. Order is fixed (host, then dir) so URLs are deterministic.
    The Nix-facing builders pass encode=True (#601): the values are
    percent-encoded so a punctuated host/dir cannot corrupt the URL.
    node_url passes False — it is the readable pattern-matching form.
    """
    enc = _encode_query_value if encode else str
    params = []
    if "host" in spec:
        params.append(f"host={enc(spec['host'])}")
    if "dir" in spec:
        params.append(f"dir={enc(spec['dir'])}")
    return f"?{'&'.join(params)}" if params else ""


def _append_query(url: str, params: list) -> str:
    """Append query params to a URL, joining with & if it already
    carries a query string (#528: a raw git URL may already embed
    e.g. ?shallow=1 — a second ? makes a malformed flake URL).

    Params whose key the URL already carries are skipped (#544):
    emitting the key twice (shallow=1 twice, or conflicting
    dir=old&dir=new) makes the rebuilt flake URL ambiguous — the
    embedded raw URL is the recorded source, so it wins."""
    if not params:
        return url
    if "?" in url:
        embedded = {
            pair.split("=", 1)[0]
            for pair in url.split("?", 1)[1].split("&")
            if pair
        }
        params = [
            p for p in params if p.split("=", 1)[0] not in embedded
        ]
        if not params:
            return url
    sep = "&" if "?" in url else "?"
    return url + sep + "&".join(params)


def _dir_params(spec: dict, encode: bool = False) -> list:
    """[dir=...] if the spec carries the generic dir attribute (#537).

    encode=True percent-encodes the value for a Nix-facing URL (#601).
    """
    if "dir" not in spec:
        return []
    enc = _encode_query_value if encode else str
    return [f"dir={enc(spec['dir'])}"]


def _git_params(
    spec: dict, include_ref_rev: bool = True, encode: bool = False
) -> list:
    """Query params that must survive a git URL rebuild.

    ref/rev name the revision; dir names a sub-flake (#537); the
    boolean fetch attributes submodules (#528), shallow and allRefs
    (#538) change what the fetch retrieves. source_key already treats
    all of them as source-distinguishing — dropping them here rebuilds
    a URL for a different source than the lock records. Booleans are
    emitted as =1 only when true (false is the fetch default).
    encode=True percent-encodes the values for a Nix-facing URL
    (#601); node_url's readable form leaves them raw.
    """
    enc = _encode_query_value if encode else str
    params = []
    if include_ref_rev:
        if "ref" in spec:
            params.append(f"ref={enc(spec['ref'])}")
        if "rev" in spec:
            params.append(f"rev={enc(spec['rev'])}")
    params += _dir_params(spec, encode=encode)
    for field in ("submodules", "shallow", "allRefs"):
        if spec.get(field):
            params.append(f"{field}=1")
    return params


def _hg_params(
    spec: dict, include_ref_rev: bool = True, encode: bool = False
) -> list:
    """Query params that must survive an hg URL rebuild (#539).

    Mercurial flake refs carry ref/rev and the generic dir attribute,
    but none of git's boolean fetch attributes (submodules/shallow/
    allRefs are git-only), so hg gets its own narrower helper rather
    than reusing _git_params. encode=True percent-encodes the values
    for a Nix-facing URL (#601).
    """
    enc = _encode_query_value if encode else str
    params = []
    if include_ref_rev:
        if "ref" in spec:
            params.append(f"ref={enc(spec['ref'])}")
        if "rev" in spec:
            params.append(f"rev={enc(spec['rev'])}")
    params += _dir_params(spec, encode=encode)
    return params


def _with_ref_segment(url: str, orig: dict) -> str:
    """URL with the ref (or truncated rev) as a path segment.

    The segment belongs to the path, so it goes BEFORE any query the
    raw URL already embeds (#546): appending it to the whole string
    lands it inside the query value on a query-carrying URL
    (?shallow=1/main), corrupting that value and hiding the ref."""
    if "ref" in orig:
        seg = f"/{orig['ref']}"
    elif "rev" in orig:
        seg = f"/{orig['rev'][:12]}"
    else:
        return url
    base, sep, query = url.partition("?")
    return base + seg + (sep + query if sep else "")


def node_url(node_data: dict) -> str:
    """Readable URL string for a node."""
    orig = node_data.get("original", {})
    t = orig.get("type", "")
    if t in ("github", "gitlab", "sourcehut"):
        s = f"{t}:{orig.get('owner', '')}/{orig.get('repo', '')}"
        if "ref" in orig:
            s += f"/{orig['ref']}"
        elif "rev" in orig:
            s += f"/{orig['rev'][:12]}"
        # Surface host/dir so exclusion patterns can distinguish a
        # self-hosted or sub-flake input from the plain one (#535).
        return s + _forge_query(orig)
    elif t == "git":
        # The fallback below JSON-dumps the original, which no user
        # exclusion pattern can ever match (#519) — return the URL with
        # the ref/rev readable, like the forge branch above.
        s = _with_ref_segment(orig.get("url", ""), orig)
        # Surface dir and the boolean fetch attributes so exclusion
        # patterns can distinguish a sub-flake / submodule / shallow /
        # allRefs fetch from the plain one (#537, #538, #528).
        # _append_query, not a hard-coded "?": the raw URL may already
        # embed a query, and a second ? is malformed (#544).
        return _append_query(s, _git_params(orig, include_ref_rev=False))
    elif t == "hg":
        # Same JSON-dump fallback problem as git (#539): return the
        # URL with ref/rev readable, surfacing dir via _hg_params.
        s = _with_ref_segment(orig.get("url", ""), orig)
        # _append_query for the same #544 reason as the git branch.
        return _append_query(s, _hg_params(orig, include_ref_rev=False))
    elif t in ("tarball", "file"):
        return _append_query(orig.get("url", ""), _dir_params(orig))
    elif t == "path":
        # The JSON-dump fallback is unmatchable by exclusion patterns
        # (#524) — return the path flake reference, like indirect above.
        return _append_query(
            f"path:{orig.get('path', '')}", _dir_params(orig)
        )
    elif t == "indirect":
        return f"indirect:{orig.get('id', '')}"
    return json.dumps(orig, sort_keys=True)


def node_original_url(node_data: dict) -> str:
    """Build a nix-resolvable URL string from a node's original field."""
    orig = node_data.get("original", {})
    t = orig.get("type", "")
    if t in ("github", "gitlab", "sourcehut"):
        owner = orig.get("owner", "")
        repo = orig.get("repo", "")
        url = f"{t}:{owner}/{repo}"
        if "ref" in orig:
            # The ref lands in a path segment of a Nix-facing URL, so
            # it is percent-encoded (#601): a raw "#" makes Nix reject
            # the ref outright ("unexpected fragment"), a raw "%"
            # mis-decodes. "/" stays raw — Nix parses multi-segment
            # refs exactly as before.
            url += f"/{_encode_path_value(orig['ref'])}"
        elif "rev" in orig:
            # A rev-pinned node with no ref keeps its pin (#521) —
            # dropping it would rebuild the URL at the default branch.
            url += f"/{_encode_path_value(orig['rev'])}"
        # host/dir name a different server / sub-flake (#535) —
        # dropping them rebuilds the URL at the default host/root.
        return url + _forge_query(orig, encode=True)
    elif t == "git":
        # The JSON-dump fallback below is not a URL (#521): callers pass
        # this string to --override-input and write it into flake.nix.
        # Use the repo's own git flake-URL convention (git+<url>?ref=/rev=).
        # dir/submodules/shallow/allRefs survive via _git_params, and
        # _append_query joins with & when the raw URL already carries
        # a query (#528, #537, #538).
        raw = orig.get("url", "")
        url = raw if raw.startswith("git+") else f"git+{raw}"
        return _append_query(url, _git_params(orig, encode=True))
    elif t == "hg":
        # Mirror git (#539): hg+<url>?ref=/rev= is the resolvable
        # flake-ref form Nix accepts for Mercurial inputs.
        raw = orig.get("url", "")
        url = raw if raw.startswith("hg+") else f"hg+{raw}"
        return _append_query(url, _hg_params(orig, encode=True))
    elif t == "tarball":
        return _append_query(orig.get("url", ""), _dir_params(orig, encode=True))
    elif t == "file":
        raw = orig.get("url", "")
        # Prefix with file+ so nix doesn't default to tarball fetching
        if raw.startswith(("https://", "http://")):
            raw = f"file+{raw}"
        return _append_query(raw, _dir_params(orig, encode=True))
    elif t == "path":
        # Callers pass this string to --override-input and write it into
        # flake.nix (#524) — the JSON-dump fallback is not a flake URL.
        # The path itself is encoded (#601): Nix rejects a raw space in
        # a path: ref and serializes it as %20 itself.
        return _append_query(
            f"path:{_encode_path_value(orig.get('path', ''))}",
            _dir_params(orig, encode=True),
        )
    elif t == "indirect":
        return f"indirect:{orig.get('id', '')}"
    return json.dumps(orig, sort_keys=True)


def _locked_url(locked: dict) -> str | None:
    """Nix-resolvable URL from a node's locked field, or None.

    Follow-up to #521: PR #522 taught node_original_url git/sourcehut
    and two of the locked fallbacks sourcehut, but a git-locked node
    behind an indirect original still produced no override at all —
    the locked fallbacks enumerated only the forge types. The forge
    shape is the one those fallbacks have always used (type:owner/repo);
    a git locked node is pinned to its locked rev, mirroring
    node_original_url's git convention — and the forge shape carries
    its locked rev as a path segment too (#609).
    """
    lt = locked.get("type", "")
    if lt in ("github", "gitlab", "sourcehut"):
        # The locked rev is the pin this function exists to preserve:
        # without it the override resolves at the default branch, not
        # the revision the lock records (#609) — the git/hg branches
        # below keep theirs via _git_params/_hg_params, and
        # node_original_url's forge branch keeps it as a path segment
        # for the same reason (#521). host/dir survive too (#535).
        url = f"{lt}:{locked.get('owner', '')}/{locked.get('repo', '')}"
        if "ref" in locked:
            url += f"/{_encode_path_value(locked['ref'])}"
        elif "rev" in locked:
            url += f"/{_encode_path_value(locked['rev'])}"
        return url + _forge_query(locked, encode=True)
    if lt == "tarball":
        # #533: tarball/file/path locked nodes fell through to None, so
        # indirect overrides for them were silently dropped. Mirror
        # node_original_url's conventions for these types.
        raw = locked.get("url", "")
        if not raw:
            return None
        return _append_query(raw, _dir_params(locked, encode=True))
    if lt == "file":
        raw = locked.get("url", "")
        if not raw:
            return None
        if raw.startswith(("https://", "http://")):
            raw = f"file+{raw}"
        return _append_query(raw, _dir_params(locked, encode=True))
    if lt == "path":
        raw = locked.get("path", "")
        if not raw:
            return None
        return _append_query(
            f"path:{_encode_path_value(raw)}",
            _dir_params(locked, encode=True),
        )
    if lt == "git":
        raw = locked.get("url", "")
        if not raw:
            return None
        url = raw if raw.startswith("git+") else f"git+{raw}"
        return _append_query(url, _git_params(locked, encode=True))
    if lt == "hg":
        # #539: hg locked nodes fell through to None, dropping
        # indirect overrides like git did before the fix above.
        raw = locked.get("url", "")
        if not raw:
            return None
        url = raw if raw.startswith("hg+") else f"hg+{raw}"
        return _append_query(url, _hg_params(locked, encode=True))
    return None


def locked_hash(node_data: dict) -> str:
    """Get the narHash or rev from the locked field for comparison."""
    locked = node_data.get("locked", {})
    return locked.get("narHash", locked.get("rev", ""))


def locked_rev(node_data: dict) -> str:
    """The revision a node is actually locked to (falling back to the pin)."""
    locked = node_data.get("locked", {})
    return locked.get("rev", "") or node_data.get("original", {}).get("rev", "")


def locked_last_modified(node_data: dict) -> int:
    """The locked lastModified timestamp, or 0 when the lock omits it."""
    value = node_data.get("locked", {}).get("lastModified")
    return value if isinstance(value, int) else 0


def _rev_survivor_key(node_data: dict) -> tuple[int, str]:
    """Ordering for choosing which rev of a source survives dedup (#509).

    Newest locked lastModified wins; ties break on the rev string so the
    choice is deterministic for a given lock file.
    """
    return (locked_last_modified(node_data), locked_rev(node_data))


# ---------------------------------------------------------------------------
# Graph traversal
# ---------------------------------------------------------------------------


def find_all_paths(lock: dict, target_node: str) -> list[list[tuple[str, str, bool]]]:
    """Find all paths from root to target_node through the lock graph.

    Returns list of paths, where each path is a list of
    (parent_input_name, child_node_name, is_follows) tuples.
    """
    nodes = lock["nodes"]
    results: list[list[tuple[str, str, bool]]] = []

    def dfs(
        current_node: str,
        path: list[tuple[str, str, bool]],
        visited: set[str],
    ) -> None:
        if current_node == target_node and path:
            results.append(list(path))
            return
        if current_node in visited:
            return
        visited.add(current_node)

        node = nodes.get(current_node, {})
        inputs = node.get("inputs", {})
        for input_name, ref in inputs.items():
            if isinstance(ref, str):
                path.append((input_name, ref, False))
                dfs(ref, path, visited)
                path.pop()
            elif isinstance(ref, list):
                resolved = resolve_follows(lock, ref)
                if resolved is not None:
                    path.append((input_name, resolved, True))
                    dfs(resolved, path, visited)
                    path.pop()

        visited.discard(current_node)

    dfs("root", [], set())
    return results


def resolve_follows(
    lock: dict, follows_path: list[str], _resolving: frozenset | None = None
) -> str | None:
    """Resolve a follows path like ["determinate", "nix", "nixpkgs"] to a node name.

    Returns None when the path cannot be resolved, including when the
    follows declarations form a cycle (e.g. ``a`` follows ``b`` while
    ``b`` follows ``a`` in a hand-edited or corrupted lock): a follows
    path that is already being resolved further up the call chain is a
    cycle, not a reason to recurse until RecursionError (#565). Only
    the active resolution chain is tracked, so the same sub-path
    resolved again in a sibling step is not mistaken for a cycle.
    """
    key = tuple(follows_path)
    resolving = _resolving if _resolving is not None else frozenset()
    if key in resolving:
        return None
    resolving = resolving | {key}
    nodes = lock["nodes"]
    current = "root"
    for step in follows_path:
        node = nodes.get(current, {})
        inputs = node.get("inputs", {})
        ref = inputs.get(step)
        if ref is None:
            return None
        if isinstance(ref, str):
            current = ref
        elif isinstance(ref, list):
            resolved = resolve_follows(lock, ref, resolving)
            if resolved is None:
                return None
            current = resolved
        else:
            return None
    return current


def path_to_follows_decl(path: list[tuple[str, str, bool]]) -> str:
    """Convert a path to a dotted follows path like 'home-manager.nixpkgs'."""
    return ".".join(inp for inp, _child, _is_f in path)


def path_depth(path: list[tuple[str, str, bool]]) -> int:
    """Return the depth of a follows path."""
    return len(path)


# ---------------------------------------------------------------------------
# flake.nix file operations
# ---------------------------------------------------------------------------


def read_flake_nix(flake_dir: str) -> str:
    with open(os.path.join(flake_dir, "flake.nix")) as f:
        return f.read()


def write_flake_nix(flake_dir: str, content: str) -> None:
    with open(os.path.join(flake_dir, "flake.nix"), "w") as f:
        f.write(content)


def _brace_counts(line: str) -> tuple[int, int]:
    """Count the structural ``{`` and ``}`` in one line of Nix source.

    Braces inside double-quoted strings and everything from a ``#``
    comment start (outside a string) to end of line are not structural
    and are ignored (#623). Indented (''...'') strings are not tracked —
    the same documented approximation the other line scanners make.
    """
    # Fast path 1: if no braces exist on the line, net brace count is (0, 0).
    if "{" not in line and "}" not in line:
        return 0, 0

    # Fast path 2: if no quotes or comments exist, simple string counting works directly.
    if '"' not in line and "#" not in line:
        return line.count("{"), line.count("}")

    opens = 0
    closes = 0
    in_string = False
    i = 0
    while i < len(line):
        ch = line[i]
        if in_string:
            if ch == "\\":
                i += 1  # skip the escaped character as well
            elif ch == '"':
                in_string = False
        elif ch == '"':
            in_string = True
        elif ch == "#":
            break
        elif ch == "{":
            opens += 1
        elif ch == "}":
            closes += 1
        i += 1
    return opens, closes


def _brace_delta(line: str) -> int:
    """Net structural brace delta of one line (see ``_brace_counts``)."""
    opens, closes = _brace_counts(line)
    return opens - closes


# A Nix attribute name may be spelled bare (``my-input``) or quoted
# (``"my-input"``); the two spellings denote the SAME attribute —
# ``nix-instantiate --parse`` normalises the quoted form away — and a
# name containing characters outside the bare set (dots, slashes) MUST
# be quoted (#705).  The token patterns below match either spelling as
# a single capture (quotes included when present); ``_unquote_name``
# recovers the attribute name from a match.
_QUOTED_NAME = r'"(?:[^"\\]|\\.)*"'
_NAME_TOKEN = rf"({_QUOTED_NAME}|[a-zA-Z_][a-zA-Z0-9_'-]*)"
_SEG_TOKEN = rf"({_QUOTED_NAME}|[a-zA-Z0-9_'-]+)"
_SEG_TOKEN_NC = rf"(?:{_QUOTED_NAME}|[a-zA-Z0-9_'-]+)"


def _unquote_name(token: str) -> str:
    """Attribute name for a matched name token (see ``_NAME_TOKEN``)."""
    if len(token) >= 2 and token.startswith('"') and token.endswith('"'):
        return re.sub(r"\\(.)", r"\1", token[1:-1])
    return token


def _escape_name(name: str) -> str:
    """Nix string escapes for ``name`` inside a quoted spelling (#709).

    ``\\`` -> ``\\\\``, ``"`` -> ``\\"`` and ``${`` -> ``\\${`` — the
    exact transformation ``_quote_name`` writes and ``_unquote_name``
    reverses.  Shared so the writer and the matcher cannot drift.
    """
    return name.replace("\\", "\\\\").replace('"', '\\"').replace("${", "\\${")


def _name_ref(name: str) -> str:
    """Regex matching ``name`` spelled bare or quoted (``"name"``).

    The quoted alternative must match the spelling ``_quote_name``
    actually emits: for a name containing ``"``, ``\\`` or ``${`` that
    spelling carries backslash escapes (``"a\\"b"``, ``"a\\\\b"``,
    ``"a\\${b}"``), so a pattern built from the raw name alone never
    matches the generated line (#721).  Match the escaped quoted
    spelling first, then the plain quoted spelling and the bare
    spelling, so hand-written plain forms keep matching too.
    """
    esc = re.escape(name)
    quoted_esc = re.escape(_escape_name(name))
    if quoted_esc == esc:
        return rf'(?:"{esc}"|{esc})'
    return rf'(?:"{quoted_esc}"|"{esc}"|{esc})'


_BARE_NAME_RE = re.compile(r"^[a-zA-Z_][a-zA-Z0-9_'-]*$")


def _quote_name(name: str) -> str:
    """Spelling of ``name`` for a generated declaration line (#709).

    A bare-spellable name keeps its bare spelling (no churn in
    existing outputs).  Any other name — one containing a dot, slash
    or space, or starting with a digit — MUST be quoted: written bare
    it denotes a different attribute (``inputs.my.input`` is a nested
    attribute, not the single input named ``my.input``), and the
    scanners then report the wrong input as present.
    """
    if _BARE_NAME_RE.match(name):
        return name
    return f'"{_escape_name(name)}"'


def _unquote_attr_names(text: str) -> str:
    """Strip the quotes around attribute names in a declaration line.

    Only quotes in attribute position — followed by ``.`` or ``=`` —
    are stripped, so string *values* (a follows target, a url) keep
    theirs.  Used by the follows checkers, which compare whole lines
    against candidate strings built from bare names (#705).
    """
    return re.sub(
        rf"{_QUOTED_NAME}(?=\s*(?:\.|=))",
        lambda m: _unquote_name(m.group(0)),
        text,
    )


def _own_level_text(line: str, start_depth: int, level: int = 1) -> tuple[str, int]:
    """Return the parts of ``line`` sitting at brace ``level``, plus end depth.

    Scanning is string/comment-aware like ``_brace_counts`` (#623): string
    contents collapse to an empty string literal and everything from a
    ``#`` comment start is dropped, so neither can fake or hide an
    attribute (#646). Text inside a nested block (depth above ``level``)
    or outside the block (depth below it) is replaced by spaces, so
    fragments on either side of a nested block that opens and closes on
    the same line are both returned, each judged at the level it actually
    sits at. ``start_depth`` is the structural depth before the line; the
    returned depth is the structural depth after it.
    """
    out: list[str] = []
    depth = start_depth
    in_string = False
    i = 0
    while i < len(line):
        ch = line[i]
        if in_string:
            if ch == "\\":
                i += 1  # skip the escaped character as well
            elif ch == '"':
                in_string = False
                if depth == level:
                    out.append('"')
        elif ch == '"':
            in_string = True
            if depth == level:
                out.append('"')
        elif ch == "#":
            break
        elif ch == "{":
            depth += 1
            out.append(" ")
        elif ch == "}":
            depth -= 1
            out.append(" ")
        elif depth == level:
            out.append(ch)
        else:
            out.append(" ")
        i += 1
    return "".join(out), depth


def get_input_file_order(content: str) -> list[str]:
    """Parse flake.nix to determine input declaration order.

    Returns a list of root input names in the order they appear in the file.
    """
    order: list[str] = []
    seen: set[str] = set()
    in_inputs_block = False
    brace_depth = 0
    lines = content.split("\n")
    contexts = _block_context_for_lines(content)

    for idx, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        # Dotted style: inputs.NAME.xxx or inputs.NAME = {
        # Only at top level, though: inside the root inputs block an
        # `inputs.NAME...` line is a nested override inside some input's
        # own block, not a root declaration — recording it would invent a
        # root input (and the early continue would skip the line's brace
        # accounting) (#652). The same applies inside a dotted-style
        # parent block (inputs.parent = { ... }): its interior
        # `inputs.child...` lines are the parent's overrides, not root
        # declarations — #653's in_inputs_block guard cannot see them
        # because that content has no `inputs = {` block at all, so
        # consult the block context instead: a dotted line is a root
        # declaration only at top level (context None) or on the parent
        # block's own opening line (context == the declared name)
        # (#652 remainder, context tracking from #655).
        m = None
        if not in_inputs_block:
            dm = re.match(rf"inputs\.{_NAME_TOKEN}\s*[.=]", stripped)
            if dm and (
                contexts[idx] is None or contexts[idx] == _unquote_name(dm.group(1))
            ):
                m = dm
        if m:
            name = _unquote_name(m.group(1))
            if name not in seen:
                order.append(name)
                seen.add(name)
            continue

        # Detect start of inputs = { block. A nested `inputs = {` inside
        # another input's override is not the root block: re-entering here
        # would reset the brace depth and lose every later root input (#640).
        if not in_inputs_block and re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_block = True
            brace_depth = _brace_delta(stripped)
            continue

        if in_inputs_block:
            depth_before = brace_depth
            brace_depth += _brace_delta(stripped)
            if brace_depth <= 0:
                in_inputs_block = False
                continue

            # Only direct children of the root inputs block (depth 1) are
            # root inputs; names inside a nested block belong to that
            # input's override set, not the root (#640).
            if depth_before != 1:
                continue

            m = re.match(rf"{_NAME_TOKEN}\s*[.=]", stripped)
            if m:
                name = _unquote_name(m.group(1))
                if (
                    name
                    not in (
                        "url",
                        "flake",
                        "inputs",
                        "follows",
                        "type",
                        "ref",
                        "rev",
                        "narHash",
                    )
                    and name not in seen
                ):
                    order.append(name)
                    seen.add(name)

    return order


@functools.lru_cache(maxsize=128)
def _block_context_for_lines_cached(content: str) -> tuple[str | None, ...]:
    lines = content.split("\n")
    result: list[str | None] = [None] * len(lines)
    # Stacks of (block_name, brace depth just before the block opened)
    stack: list[tuple[str, int]] = []
    inputs_stack: list[int] = []
    brace_depth = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            if stack:
                result[i] = stack[-1][0]
            continue

        opens, closes = _brace_counts(line)

        # Check for block open: <name> = { (inside the inputs block)
        pushed = False
        m = re.match(rf"^(\s*){_SEG_TOKEN}\s*=\s*\{{", line)
        if m:
            name = _unquote_name(m.group(2))
            if name == "inputs":
                inputs_stack.append(brace_depth)
            elif inputs_stack or stack:
                stack.append((name, brace_depth))
                result[i] = name
                pushed = True

        # Dotted-style block open: inputs.<name>... = { at top level
        # (#655). The generic pattern above cannot see it — its name
        # group stops at the first dot, so "inputs" never matches the
        # following "=". Lines inside such a block are that input's own
        # attributes and nested overrides (inputs.<child>... relative
        # to it), not root declarations. Only the outermost dotted
        # block is pushed: inside an already-tracked block the existing
        # context already governs the lines, and inside a dotted block
        # a nested inputs.<child> = { stays attributed to the outer
        # input until its braces close.
        if not pushed and not stack and not inputs_stack:
            dm = re.match(
                rf"^(\s*)inputs\.{_NAME_TOKEN}(?:\.{_SEG_TOKEN_NC})*\s*=\s*\{{",
                line,
            )
            if dm:
                name = _unquote_name(dm.group(2))
                stack.append((name, brace_depth))
                result[i] = name
                pushed = True

        if not pushed and stack and not stripped.startswith("}"):
            # An ordinary line inside a block. A line starting with
            # "}" is a closing line: it belongs to the enclosing
            # scope, so its context is assigned after the pop below.
            result[i] = stack[-1][0]

        brace_depth += opens - closes

        # Close every block whose opening depth has been reached again
        while stack and brace_depth <= stack[-1][1]:
            stack.pop()
        while inputs_stack and brace_depth <= inputs_stack[-1]:
            inputs_stack.pop()

        if not pushed and stripped.startswith("}") and stack:
            result[i] = stack[-1][0]

    return tuple(result)


def _block_context_for_lines(content: str) -> list[str | None]:
    """Return the enclosing block input name for each line.

    For lines inside ``someInput = { ... };`` returns ``"someInput"``.
    A top-level dotted block (``inputs.someInput = { ... };``) counts
    the same way (#655). For top-level dotted lines (outside any
    block) returns ``None``.

    This is approximate but sufficient: it tracks ``<name> = {`` opens
    and ends a block when the structural brace depth (see
    ``_brace_counts``) returns to the depth at which the block opened.
    Tracking depth instead of looking for a line that is exactly
    ``};`` also closes blocks that open and close on one line
    (``foo = { url = "..."; };``) and blocks whose ``};`` shares a
    line with the last attribute (#628) — exact-line matching left
    those blocks open forever, so every later line in the inputs
    section was attributed to them.
    """
    return list(_block_context_for_lines_cached(content))


@functools.lru_cache(maxsize=128)
def _nested_inputs_prefix_for_lines_cached(content: str) -> tuple[tuple[str, ...] | None, ...]:
    lines = content.split("\n")
    result: list[tuple[str, ...] | None] = [None] * len(lines)
    # Stack of ("inputs", depth) / (name, depth); depth is the brace
    # depth just before that block opened.
    stack: list[tuple[str, int]] = []
    brace_depth = 0

    def prefix_from_stack() -> tuple[str, ...] | None:
        seq = [name for name, _ in stack]
        if seq and seq[0] == "inputs":
            seq = seq[1:]
        if len(seq) >= 2 and seq[-1] == "inputs":
            res = [name for name in seq[:-1] if name != "inputs"]
            return tuple(res)
        return None

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            result[i] = prefix_from_stack()
            continue

        opens, closes = _brace_counts(line)

        m = re.match(rf"^(\s*){_SEG_TOKEN}\s*=\s*\{{", line)
        if m:
            name = _unquote_name(m.group(2))
            if name == "inputs":
                stack.append(("inputs", brace_depth))
            elif stack:
                stack.append((name, brace_depth))

        result[i] = prefix_from_stack()

        brace_depth += opens - closes

        while stack and brace_depth <= stack[-1][1]:
            stack.pop()

    return tuple(result)


def _nested_inputs_prefix_for_lines(content: str) -> list[list[str] | None]:
    """Return the follows-path prefix a nested ``inputs = {`` scope proves.

    For a line inside ``foo = { inputs = { ... }; }`` (a nested override
    block inside the root input ``foo``'s block) returns ``["foo"]``: a
    declaration ``nixpkgs.follows = "...";`` on such a line declares
    ``inputs.foo.inputs.nixpkgs.follows``. Deeper shapes compose the
    same way — inside ``foo``'s nested block, a ``bar = { inputs = {``
    scope returns ``["foo", "bar"]``. Lines not directly inside a nested
    ``inputs`` block return ``None``: in particular a line directly in
    ``foo``'s own block (where ``nixpkgs.follows`` would declare
    ``foo.nixpkgs.follows``, a different attribute) and a line inside a
    named block *within* the nested block both return ``None`` (#647).

    The opening line of the nested block itself also reports the prefix,
    so a one-line ``inputs = { nixpkgs.follows = "..."; };`` is visible
    to callers that judge the segment after the ``{``.
    """
    cached = _nested_inputs_prefix_for_lines_cached(content)
    return [list(x) if x is not None else None for x in cached]


def _nested_relative_path(follows_parts: list[str], prefix: list[str]) -> str | None:
    """Relative follows path for the parts a nested scope does not cover.

    ``prefix`` must be a proper prefix of ``follows_parts``; the rest is
    joined the way dotted declarations spell it inside the nested block:
    ``["nixpkgs"]`` -> ``nixpkgs``, ``["bar", "nixpkgs"]`` ->
    ``bar.inputs.nixpkgs``. Returns ``None`` when the scope does not
    line up with the requested path.
    """
    if len(prefix) >= len(follows_parts) or follows_parts[: len(prefix)] != prefix:
        return None
    rest = follows_parts[len(prefix) :]
    return rest[0] + "".join(f".inputs.{p}" for p in rest[1:])


def _nested_segment(normalized: str) -> str:
    """Segment of a nested ``inputs = {`` opening line after the ``{``.

    For an ordinary line inside the block this is the line itself; for a
    one-line ``inputs = { nixpkgs.follows = "x"; };`` it is the
    declaration between the braces.
    """
    m = re.match(r"inputs\s*=\s*\{", normalized)
    if not m:
        return normalized
    seg = normalized[m.end() :].strip()
    if seg.endswith("}"):
        seg = seg[:-1].strip()
    if seg.endswith(";"):
        seg = seg[:-1].strip()
    return seg


def _nested_segments(normalized: str) -> list[str]:
    """Individual declarations on a nested ``inputs = {`` line (#649).

    A one-line nested block can carry several declarations
    (``inputs = { a.follows = "a"; b.follows = "b"; };``); judging the
    whole segment after the ``{`` as one declaration misses every one
    of them (equality), or all but the first (prefix/match). Split the
    segment on ``;`` outside double-quoted strings so each declaration
    is judged on its own. Ordinary lines return a single-element list.
    """
    m = re.match(r"inputs\s*=\s*\{", normalized)
    if not m:
        return [normalized] if normalized else []
    seg = normalized[m.end() :].strip()
    if seg.endswith("}"):
        seg = seg[:-1].strip()
    parts: list[str] = []
    buf: list[str] = []
    in_string = False
    escaped = False
    for ch in seg:
        if escaped:
            buf.append(ch)
            escaped = False
            continue
        if ch == "\\" and in_string:
            buf.append(ch)
            escaped = True
            continue
        if ch == '"':
            in_string = not in_string
            buf.append(ch)
            continue
        if ch == ";" and not in_string:
            part = "".join(buf).strip()
            if part:
                parts.append(part)
            buf = []
            continue
        buf.append(ch)
    part = "".join(buf).strip()
    if part:
        parts.append(part)
    return parts


def follows_exists_in_content(
    content: str, follows_parts: list[str], target: str
) -> bool:
    """Check if a follows declaration already exists (active) in flake.nix.

    Checks both absolute and relative forms, and is block-context-aware:
    a line ``inputs.nix.inputs.X.follows`` inside a ``determinate = { ... }``
    block is NOT the same as root-level ``nix.inputs.X.follows``.
    """
    # Build candidate strings for ROOT-level scope (block_context == None)
    full = ".".join(f"inputs.{p}" for p in follows_parts) + f'.follows = "{target}"'
    relative_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
        + f'.follows = "{target}"'
        if len(follows_parts) >= 2
        else f'{follows_parts[0]}.follows = "{target}"'
    )
    # Bolt Optimization: Pre-normalize whitespace in candidate strings ONCE
    # outside the loop to eliminate redundant re.sub calls inside the line loop.
    root_candidates = [re.sub(r"\s+", " ", full), re.sub(r"\s+", " ", relative_inputs)]

    # Build candidate strings for BLOCK scope (inside follows_parts[0] block)
    raw_block_candidates: list[str] = []
    if len(follows_parts) >= 2:
        # Inside the block of follows_parts[0], the path is relative
        inner = (
            ".".join(f"inputs.{p}" for p in follows_parts[1:])
            + f'.follows = "{target}"'
        )
        raw_block_candidates.append(inner)
        # bare follows for depth 2
        if len(follows_parts) == 2:
            raw_block_candidates.append(f'follows = "{target}"')
    elif len(follows_parts) == 1:
        # Inside the block of the single part, bare follows matches
        raw_block_candidates.append(f'follows = "{target}"')
    block_candidates = [re.sub(r"\s+", " ", c) for c in raw_block_candidates]

    # Bolt Optimization: _block_context_for_lines and _nested_inputs_prefix_for_lines
    # are cached with @functools.lru_cache to avoid re-parsing flake.nix line-by-line
    # across multiple analysis checks.
    block_context = _block_context_for_lines(content)
    nested_prefixes = _nested_inputs_prefix_for_lines(content)
    lines = content.split("\n")
    first_part = follows_parts[0] if follows_parts else None

    for i, line in enumerate(lines):
        stripped = line.strip().rstrip(";").strip()
        if stripped.startswith("#"):
            continue
        normalized = _unquote_attr_names(re.sub(r"\s+", " ", stripped))

        # Nested inputs scope (#647): a line directly inside a nested
        # ``inputs = { ... }`` override block is judged only against the
        # relative form that scope spells — the root/block candidates
        # would attribute the declaration to the wrong attribute path.
        nested_prefix = nested_prefixes[i]
        if nested_prefix is not None:
            rel = _nested_relative_path(follows_parts, nested_prefix)
            if rel is not None:
                candidate = f'{rel}.follows = "{target}"'
                if re.sub(r"\s+", " ", candidate) in _nested_segments(normalized):
                    return True
            continue

        ctx = block_context[i]

        if ctx is None:
            # Root scope: match full or relative forms
            if normalized in root_candidates:
                return True
        elif ctx == first_part:
            # Inside the block of the first follows_parts element
            if normalized in block_candidates:
                return True

    return False


def follows_path_has_any_target(content: str, follows_parts: list[str]) -> bool:
    """Check if a follows for the given attribute path exists with ANY target.

    Block-context-aware: a line inside a different block is not a match.
    """
    # Root-level prefixes
    full_prefix = ".".join(f"inputs.{p}" for p in follows_parts) + ".follows"
    relative_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
        + ".follows"
        if len(follows_parts) >= 2
        else f"{follows_parts[0]}.follows"
    )
    root_prefixes = [full_prefix, relative_inputs]

    # Block-level prefixes (inside follows_parts[0] block)
    block_prefixes: list[str] = []
    if len(follows_parts) >= 2:
        inner = ".".join(f"inputs.{p}" for p in follows_parts[1:]) + ".follows"
        block_prefixes.append(inner)
        if len(follows_parts) == 2:
            block_prefixes.append("follows")
    elif len(follows_parts) == 1:
        block_prefixes.append("follows")

    block_context = _block_context_for_lines(content)
    nested_prefixes = _nested_inputs_prefix_for_lines(content)
    lines = content.split("\n")
    first_part = follows_parts[0] if follows_parts else None

    for i, line in enumerate(lines):
        stripped = line.strip().rstrip(";").strip()
        if stripped.startswith("#"):
            continue
        normalized = _unquote_attr_names(re.sub(r"\s+", " ", stripped))

        # Nested inputs scope (#647): see follows_exists_in_content.
        nested_prefix = nested_prefixes[i]
        if nested_prefix is not None:
            rel = _nested_relative_path(follows_parts, nested_prefix)
            if rel is not None:
                pfx = f"{rel}.follows"
                for seg in _nested_segments(normalized):
                    if seg.startswith(pfx + " =") or seg.startswith(pfx + "="):
                        return True
            continue

        ctx = block_context[i]

        if ctx is None:
            for pfx in root_prefixes:
                if normalized.startswith(pfx + " =") or normalized.startswith(
                    pfx + "="
                ):
                    return True
        elif ctx == first_part:
            for pfx in block_prefixes:
                if normalized.startswith(pfx + " =") or normalized.startswith(
                    pfx + "="
                ):
                    return True

    return False


def root_input_exists_in_content(content: str, input_name: str) -> bool:
    """Check if a root input declaration already exists in flake.nix.

    Only matches direct root input declarations, not references to the name
    inside another input's block (e.g. ``inputs.parent.inputs.CHILD...``
    inside a parent block is NOT a root declaration of CHILD).
    """
    esc = _name_ref(input_name)
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)

    if style == "block" and block_start is not None and block_end is not None:
        # Track brace depth inside inputs = { ... } to only match at depth 1
        depth = 0
        for i in range(block_start, len(lines)):
            stripped = lines[i].strip()
            if stripped.startswith("#"):
                continue
            # Check BEFORE counting this line's braces
            if i > block_start and depth == 1:
                # NAME = { or NAME.url = etc.
                if re.match(rf"{esc}\s*[.=]", stripped):
                    return True
            depth += _brace_delta(stripped)
            if depth <= 0 and i > block_start:
                break
    else:
        # Dotted style: look for top-level inputs.NAME declarations.
        # Lines inside a dotted-style parent block (inputs.parent =
        # { ... }) are that parent's attributes/overrides, not root
        # declarations (#655) — except the block's own opening line,
        # which IS the root declaration of the parent itself.
        block_ctx = _block_context_for_lines(content)
        for i, line in enumerate(lines):
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            ctx = block_ctx[i]
            if ctx is not None:
                if ctx == input_name and re.match(
                    rf"inputs\.{esc}(?:\.{_SEG_TOKEN_NC})*\s*=\s*\{{", stripped
                ):
                    return True
                continue
            if re.match(rf"inputs\.{esc}\s*=", stripped):
                return True
            if re.match(rf"inputs\.{esc}\.(url|follows|flake)\s*=", stripped):
                return True
            # inputs.NAME.inputs.X.follows is a shallow override on NAME
            if re.match(
                rf"inputs\.{esc}\.inputs\.{_SEG_TOKEN_NC}\.(follows|url)\s*=",
                stripped,
            ):
                return True
    return False


def find_commented_follows(
    content: str, follows_parts: list[str], target: str
) -> tuple[int, str] | None:
    """Find a commented-out follows line matching our intent."""
    target_part = follows_parts[-1] if follows_parts else ""
    patterns = []

    # Each segment may be spelled bare or quoted (#709); the target is
    # a string value, so its quotes are part of the pattern, not a
    # name spelling.
    def _path_ref(parts: list[str]) -> str:
        return r"\.".join(rf"inputs\.{_name_ref(p)}" for p in parts)

    patterns.append(
        rf'#\s*{_path_ref(follows_parts)}\.follows\s*=\s*"{re.escape(target)}"'
    )

    if len(follows_parts) >= 2:
        patterns.append(
            rf'#\s*{_path_ref(follows_parts[1:])}\.follows\s*=\s*"{re.escape(target)}"'
        )

    if target_part:
        patterns.append(
            rf'#\s*inputs\.{_name_ref(target_part)}\.follows\s*=\s*"{re.escape(target)}"'
        )

    lines = content.split("\n")
    for i, line in enumerate(lines):
        stripped = line.strip()
        if not stripped.startswith("#"):
            continue
        for pat in patterns:
            if re.search(pat, stripped):
                return (i, line)
    return None


def uncomment_line(content: str, line_number: int) -> str:
    """Uncomment a line by removing the leading #."""
    lines = content.split("\n")
    line = lines[line_number]
    lines[line_number] = re.sub(r"^(\s*)#\s?", r"\1", line, count=1)
    return "\n".join(lines)


def find_input_block_end(content: str, input_name: str) -> tuple[int, str] | None:
    """Find the closing brace line for an input block.

    Returns (line_number_of_closing, indent) or None if not a block.
    """
    lines = content.split("\n")
    block_start = None
    in_inputs_block = False
    inputs_brace_depth = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        if re.match(rf"inputs\.{_name_ref(input_name)}\s*=\s*\{{", stripped):
            block_start = i
            break

        if not in_inputs_block and re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_block = True
            inputs_brace_depth = _brace_delta(stripped)
            continue

        if in_inputs_block:
            # Only a direct child of the inputs block (depth 1) can be the
            # root input's block; a same-name block nested inside another
            # input's ``inputs = { ... }`` override must not shadow it.
            if inputs_brace_depth == 1 and re.match(
                rf"{_name_ref(input_name)}\s*=\s*\{{", stripped
            ):
                block_start = i
                break
            inputs_brace_depth += _brace_delta(stripped)
            if inputs_brace_depth <= 0:
                in_inputs_block = False
                continue

    if block_start is None:
        return None

    depth = 0
    for i in range(block_start, len(lines)):
        depth += _brace_delta(lines[i])
        if depth <= 0:
            indent = "    "
            for j in range(block_start + 1, i):
                m = re.match(r"^(\s+)", lines[j])
                if m:
                    indent = m.group(1)
                    break
            return (i, indent)
    return None


def detect_inputs_style(
    content: str,
) -> tuple[str, int | None, int | None]:
    """Detect whether inputs use block style or dotted style.

    Returns ("block", start_line, end_line) or ("dotted", None, None).
    """
    lines = content.split("\n")
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if re.match(r"inputs\s*=\s*\{", stripped):
            depth = _brace_delta(stripped)
            for j in range(i + 1, len(lines)):
                depth += _brace_delta(lines[j])
                if depth <= 0:
                    return ("block", i, j)
            return ("block", i, len(lines) - 1)
        if re.match(r"inputs\.(?:[a-zA-Z_]|\"[^\"])", stripped):
            return ("dotted", None, None)
    return ("dotted", None, None)


def find_input_dotted_line(content: str, input_name: str) -> tuple[int | None, bool]:
    """Find the last dotted-style line for an input at ROOT scope.

    Returns (line_number, is_inside_block) or (None, False).
    Only considers lines that are at root scope (not inside another input's
    block) to avoid inserting follows into the wrong block.
    """
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)
    block_ctx = _block_context_for_lines(content)

    last = None
    inside_block = False

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        ctx = block_ctx[i]
        # Only match lines at root scope (context is None)
        if ctx is not None:
            continue
        if re.match(rf"inputs\.{_name_ref(input_name)}\.", stripped):
            last = i
            inside_block = False
        if style == "block" and block_start is not None and block_end is not None:
            if block_start < i < block_end:
                if re.match(rf"{_name_ref(input_name)}\.", stripped):
                    last = i
                    inside_block = True

    return (last, inside_block) if last is not None else (None, False)


def find_input_url_line(content: str, input_name: str) -> int | None:
    """Find the line number of a root input's ``url = ...`` declaration.

    Handles both block style (``name = { url = "..."; ... }``) and dotted
    style (``inputs.name.url = "...";``).  Returns the 0-based line index,
    or *None* if no url line is found.
    """
    lines = content.split("\n")
    esc = _name_ref(input_name)
    style, block_start, block_end = detect_inputs_style(content)
    block_ctx = _block_context_for_lines(content)

    # --- Block style: look inside the input's own block ---
    if style == "block" and block_start is not None and block_end is not None:
        in_target = False
        depth = 0
        for i in range(block_start + 1, block_end):
            stripped = lines[i].strip()
            if stripped.startswith("#"):
                continue
            # Detect block open: ``name = {`` — only at the top level of
            # the inputs block (depth 0 while searching). A same-name block
            # nested inside another input's ``inputs = { ... }`` override
            # is a different input and must not shadow the root one.
            if not in_target:
                if depth == 0 and re.match(rf"{esc}\s*=\s*\{{", stripped):
                    in_target = True
                    depth = _brace_delta(stripped)
                    # Check if url is on the same line — the line is
                    # shared with the block header, so callers must not
                    # delete it whole (#700; remove_input_url_line
                    # excises just the assignment).
                    if re.search(r"\burl\s*=", stripped):
                        return i
                    if depth <= 0:
                        # One-line block without a url on the line itself
                        break
                    continue
                depth += _brace_delta(stripped)
                continue
            if in_target:
                depth += _brace_delta(stripped)
                # Only the block's own top-level url counts (depth 1)
                if depth == 1 and re.match(r"url\s*=", stripped):
                    return i
                if depth <= 0:
                    break

    # --- Dotted style: ``inputs.name.url = ...`` at root scope ---
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if block_ctx[i] is not None:
            continue
        if re.match(rf"(?:inputs\.)?{esc}\.url\s*=", stripped):
            return i

    return None


def _excise_url_assignment(line: str, input_name: str) -> str | None:
    """Remove just the ``url = ...;`` assignment from one line (#700).

    The assignment may share the line with other declarations — a
    one-line block (``foo = { url = "..."; follows = "nixpkgs"; };``),
    a block header (``foo = { url = "...";``), or a dotted line carrying
    a second assignment — in which case deleting the whole line would
    take the rest of the input with it.  Returns the line with only the
    url assignment excised (``""`` when nothing else remains), or *None*
    when no url assignment can be excised safely: no terminating ``;``
    on the line, or the only ``url =`` on it belongs to a nested
    attribute path / a deeper block level.
    """
    esc = _name_ref(input_name)

    # Dotted form: the assignment starts the line's own content.
    m = re.match(rf"\s*(?:inputs\.)?{esc}\.url\s*=\s*", line)
    if m:
        start = len(line) - len(line.lstrip())
        value_start = m.end()
    else:
        # Block form: find a ``url`` token at a statement boundary
        # (start of line, after ``{`` or after ``;``), string/comment
        # aware like ``_brace_counts``.  On the input's own block-opening
        # line its attributes sit one level below the header's ``{``;
        # on any other line they sit at the line's starting level.
        stripped = line.strip()
        is_open_line = (
            re.match(rf"(?:inputs\.)?{esc}\s*=\s*\{{", stripped) is not None
        )
        target_depth = 1 if is_open_line else 0
        start = -1
        value_start = -1
        depth = 0
        in_string = False
        i = 0
        while i < len(line):
            ch = line[i]
            if in_string:
                if ch == "\\":
                    i += 1  # skip the escaped character as well
                elif ch == '"':
                    in_string = False
                i += 1
                continue
            if ch == '"':
                in_string = True
            elif ch == "#":
                break
            elif ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
            elif depth == target_depth and (
                line.startswith("url", i)
                and (i == 0 or not (line[i - 1].isalnum() or line[i - 1] in "_'-"))
            ):
                prev = line[:i].rstrip()
                if not prev or prev.endswith("{") or prev.endswith(";"):
                    vm = re.match(r"\s*=\s*", line[i + 3 :])
                    if vm:
                        start = i
                        value_start = i + 3 + vm.end()
                        break
            i += 1
        if start < 0:
            return None

    # The assignment ends at the first ``;`` outside a string.
    in_string = False
    semi = -1
    i = value_start
    while i < len(line):
        ch = line[i]
        if in_string:
            if ch == "\\":
                i += 1  # skip the escaped character as well
            elif ch == '"':
                in_string = False
        elif ch == '"':
            in_string = True
        elif ch == "#":
            break
        elif ch == ";":
            semi = i
            break
        i += 1
    if semi < 0:
        return None

    left = line[:start]
    right = line[semi + 1 :]
    if left.strip() and right.strip():
        return left.rstrip() + " " + right.strip()
    if left.strip():
        return left.rstrip()
    if right.strip():
        # ``left`` is pure indentation (or empty); keep it so a trailing
        # comment or a following assignment keeps its indent.
        return left + right.strip()
    return ""


def remove_input_url_line(content: str, input_name: str) -> tuple[str, bool]:
    """Remove the ``url = ...`` assignment for a root input.

    Returns ``(new_content, was_removed)``.  If the input has no url line
    the content is returned unchanged.  When the url shares its line
    with other declarations only the assignment is excised (#700); if
    it cannot be excised safely the content is returned unchanged.
    """
    line_no = find_input_url_line(content, input_name)
    if line_no is None:
        return content, False
    lines = content.split("\n")
    excised = _excise_url_assignment(lines[line_no], input_name)
    if excised is None:
        return content, False
    if excised.strip() == "":
        del lines[line_no]
    else:
        lines[line_no] = excised
    return "\n".join(lines), True


def _follows_target_for_input(content: str, input_name: str) -> str | None:
    """Return the follows target name for a root input, or *None*.

    Matches both block style (``follows = "target";``) and dotted style
    (``inputs.name.follows = "target";`` / ``name.follows = "target";``).
    """
    lines = content.split("\n")
    esc = _name_ref(input_name)
    style, block_start, block_end = detect_inputs_style(content)

    # Block style: look inside the input's block for bare ``follows = "X";``
    if style == "block" and block_start is not None and block_end is not None:
        in_target = False
        depth = 0
        for i in range(block_start + 1, block_end):
            stripped = lines[i].strip()
            if stripped.startswith("#"):
                continue
            if not in_target:
                # Only a block opened at the top level of the inputs block
                # (depth 0 while searching) is the root input's block; a
                # same-name block nested in another input's override is a
                # different input and must not shadow it.
                if depth == 0 and re.match(rf"{esc}\s*=\s*\{{", stripped):
                    in_target = True
                    depth = _brace_delta(stripped)
                    m = re.search(r'\bfollows\s*=\s*"([^"]+)"', stripped)
                    if m:
                        return m.group(1)
                    if depth <= 0:
                        # One-line block without a follows on the line
                        break
                    continue
                depth += _brace_delta(stripped)
                continue
            if in_target:
                depth += _brace_delta(stripped)
                # Only the block's own top-level follows counts (depth 1)
                if depth == 1:
                    m = re.match(r'follows\s*=\s*"([^"]+)"', stripped)
                    if m:
                        return m.group(1)
                if depth <= 0:
                    break

    # Dotted style — root scope only: a ``inputs.NAME.follows`` line
    # inside another input's block is a nested override of THAT input's
    # NAME input, not a declaration about the root input.
    block_ctx = _block_context_for_lines(content)
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if block_ctx[i] is not None:
            continue
        m = re.match(rf'(?:inputs\.)?{esc}\.follows\s*=\s*"([^"]+)"', stripped)
        if m:
            return m.group(1)

    return None


def insert_follows_in_content(
    content: str, follows_parts: list[str], target: str
) -> str:
    """Insert a follows declaration into flake.nix content.

    follows_parts: list like ["home-manager", "nixpkgs"]
    target: string like "nixpkgs"
    """
    parent_input = follows_parts[0]
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)
    in_block = style == "block"

    if len(follows_parts) == 1:
        # Root-level follows
        block_info = find_input_block_end(content, parent_input)
        if block_info is not None:
            line_no, indent = block_info
            lines.insert(line_no, f'{indent}follows = "{target}";')
            return "\n".join(lines)

        last_line, is_inside = find_input_dotted_line(content, parent_input)
        if last_line is not None:
            indent = re.match(r"^(\s*)", lines[last_line]).group(1)
            if is_inside:
                nix_line = f'{_quote_name(parent_input)}.follows = "{target}";'
            else:
                nix_line = f'inputs.{_quote_name(parent_input)}.follows = "{target}";'
            lines.insert(last_line + 1, f"{indent}{nix_line}")
            return "\n".join(lines)

        if in_block:
            nix_line = f'    {_quote_name(parent_input)}.follows = "{target}";'
        else:
            nix_line = f'    inputs.{_quote_name(parent_input)}.follows = "{target}";'
        return _insert_before_inputs_end(lines, nix_line, in_block)

    # Multi-level follows
    full_nix = (
        ".".join(f"inputs.{_quote_name(p)}" for p in follows_parts)
        + f'.follows = "{target}";'
    )
    relative_to_parent = (
        ".".join(f"inputs.{_quote_name(p)}" for p in follows_parts[1:])
        + f'.follows = "{target}";'
    )
    relative_to_inputs = (
        _quote_name(follows_parts[0])
        + "."
        + ".".join(f"inputs.{_quote_name(p)}" for p in follows_parts[1:])
        + f'.follows = "{target}";'
    )

    block_info = find_input_block_end(content, parent_input)
    if block_info is not None:
        line_no, indent = block_info
        lines.insert(line_no, f"{indent}{relative_to_parent}")
        return "\n".join(lines)

    last_line, is_inside = find_input_dotted_line(content, parent_input)
    if last_line is not None:
        indent = re.match(r"^(\s*)", lines[last_line]).group(1)
        if is_inside:
            lines.insert(last_line + 1, f"{indent}{relative_to_inputs}")
        else:
            lines.insert(last_line + 1, f"{indent}{full_nix}")
        return "\n".join(lines)

    if in_block:
        return _insert_before_inputs_end(lines, f"    {relative_to_inputs}", True)
    else:
        return _insert_before_inputs_end(lines, f"    {full_nix}", False)


def _last_dotted_anchor(lines: list[str], content: str) -> int:
    """Index of the last root-scope dotted inputs line (insertion anchor).

    Comment lines and lines attributed to another input's block by
    ``_block_context_for_lines`` are not anchors: a dotted-style parent
    block (``inputs.parent = { ... };``) contains ``inputs.child.…`` lines
    that must not attract a root insertion into the parent's block (#696).
    The parent block itself is a root input, so its opening line anchors
    at the block's closing line instead.
    """
    block_ctx = _block_context_for_lines(content)
    anchor = 0
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if block_ctx[i] is None:
            if re.match(r"inputs\.", stripped):
                anchor = i
            continue
        m = re.match(rf"inputs\.{_SEG_TOKEN}\s*=\s*\{{", stripped)
        if m and block_ctx[i] == _unquote_name(m.group(1)):
            end = find_input_block_end(content, _unquote_name(m.group(1)))
            if end is not None:
                anchor = max(anchor, end[0])
    return anchor


def insert_root_input(
    content: str, input_name: str, url: str, *, flake: bool = True
) -> str:
    """Add a new root input declaration to flake.nix.

    Uses dotted style inside block, or top-level dotted style.
    If *flake* is False, also adds ``<name>.flake = false;``.
    """
    if root_input_exists_in_content(content, input_name):
        return content

    style, block_start, block_end = detect_inputs_style(content)
    lines = content.split("\n")

    if style == "block" and block_end is not None:
        indent = "    "
        if block_start is not None:
            for i in range(block_start + 1, block_end):
                m = re.match(r"^(\s+)", lines[i])
                if m and lines[i].strip():
                    indent = m.group(1)
                    break
        new_line = f'{indent}{_quote_name(input_name)}.url = "{url}";'
        lines.insert(block_end, new_line)
        if not flake:
            lines.insert(
                block_end + 1, f"{indent}{_quote_name(input_name)}.flake = false;"
            )
    else:
        last_input_line = _last_dotted_anchor(lines, content)
        indent = "    "
        if last_input_line > 0:
            m = re.match(r"^(\s*)", lines[last_input_line])
            if m:
                indent = m.group(1)
        new_line = f'{indent}inputs.{_quote_name(input_name)}.url = "{url}";'
        lines.insert(last_input_line + 1, new_line)
        if not flake:
            lines.insert(
                last_input_line + 2,
                f"{indent}inputs.{_quote_name(input_name)}.flake = false;",
            )

    return "\n".join(lines)


def _insert_before_inputs_end(
    lines: list[str], new_line: str, in_block: bool = True
) -> str:
    """Insert a line before the end of the inputs section."""
    if in_block:
        in_inputs = False
        depth = 0
        for i, line in enumerate(lines):
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            # A nested `inputs = {` inside another input's override is not
            # the root block: re-entering here would reset the depth and
            # insert the new line inside the override instead (#640).
            if not in_inputs and re.match(r"inputs\s*=\s*\{", stripped):
                in_inputs = True
                depth = _brace_delta(stripped)
                continue
            if in_inputs:
                depth += _brace_delta(stripped)
                if depth <= 0:
                    lines.insert(i, new_line)
                    return "\n".join(lines)

    for i, line in enumerate(lines):
        if re.match(r"\s*outputs\s*=", line.strip()):
            lines.insert(i, new_line)
            return "\n".join(lines)

    lines.insert(2, new_line)
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Formatting and locking
# ---------------------------------------------------------------------------


def run_nixfmt(flake_dir: str) -> None:
    """Run nixfmt on flake.nix."""
    flake_path = os.path.join(flake_dir, "flake.nix")
    try:
        subprocess.run(
            ["nixfmt", flake_path],
            capture_output=True,
            text=True,
            timeout=30,
        )
    except Exception as e:
        print(f"  warning: nixfmt failed: {e}", file=sys.stderr)


def run_nix_flake_lock(
    flake_dir: str, override_inputs: dict[str, str] | None = None
) -> tuple[bool, str]:
    """Run nix flake lock to regenerate the lockfile."""
    cmd = ["nix", "flake", "lock"]
    if override_inputs:
        for path, url in override_inputs.items():
            cmd.extend(["--override-input", path, url])
    r = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        cwd=flake_dir,
        timeout=120,
    )
    if r.returncode != 0:
        print(f"  error: nix flake lock failed:\n{r.stderr}", file=sys.stderr)
        return False, r.stderr
    return True, ""


def extract_failed_input(stderr: str) -> tuple[str | None, str | None]:
    """Parse nix flake lock stderr to find the failed input.

    Returns (input_path, flake_name) or (None, None).
    """
    input_path = None
    flake_name = None
    # Nix wraps the name in single quotes verbatim.  A quoted attribute
    # name may contain almost any character — the content scanners
    # accept any quoted name (#705) — so a character set for the name
    # will always lag the next legal one: the apostrophe for primed
    # names (#691) and the dot for dotted names (#710) each arrived as
    # their own bug, "+", " " or "@" names still missed (#712), and
    # #713's structural parse still truncated a name at an apostrophe
    # followed by a character outside its lookahead set (a'!b -> a,
    # #714) — worse than a miss, because the callers trust the
    # truncated prefix and override/match a different input.
    # Parse the wrapper instead of the name.  A quote is name text,
    # never the wrapper's close, when it can only continue a name or
    # a word: (a) a doubled quote is a prime plus the next quote
    # ('nixpkgs'' is the wrapped nixpkgs', 'nixpkgs''' the wrapped
    # nixpkgs''); (b) a quote immediately followed by "/" ends a
    # primed path segment ('parent'/child) — no message template puts
    # a "/" right after the closing quote; (c) a quote with a word
    # character on both sides is an apostrophe inside a word (a'b in
    # a name, user's in following prose).  The closing quote is the
    # first remaining quote that leaves the name a plausible path
    # (non-empty, no empty segment, no leading/trailing whitespace)
    # and an even number of delimiter quotes in the rest of the line
    # — following text quotes its own words in pairs ('…' and then
    # 'other'), so a close that strands one delimiter quote is a name
    # apostrophe, whatever character follows it (!, %, ~, :, a space,
    # a non-ASCII letter).  #720 counted every rest quote, so a path
    # primed in two segments ('a'/b'') looked closed at its first
    # prime and a possessive in following text (user's) was taken as
    # a close, swallowing prose into the name (#722).  The parity
    # likewise discounts a word-final possessive (users', James'):
    # its word is a standalone prose word, preceded by whitespace
    # and by further prose after the candidate close, whereas a
    # delimiter closing a span in the rest is preceded by that
    # span's opening quote and a name quote in the rest is the
    # first word after an in-name apostrophe (the bar of
    # 'foo' bar') — #726 counted only the medial user's shape, so
    # a plural possessive still skewed the parity and the true
    # close was rejected (#727).  #728 judged each rest quote on
    # its own, so a multi-word span's closing quote ('power
    # users') met that standalone-word test and was discounted,
    # and a hyphenated compound's possessive (end-users') was
    # never recognised because the word walk stopped at the
    # hyphen: the parity now tracks quoted spans in the rest (a
    # span's closing quote always counts) and the possessive
    # walk crosses a flanked hyphen (#731).  A span quoting a
    # prime-ending name ends in a run of quotes ('power
    # users'') whose second prime the span tracker mistook
    # for a new span's opening, stranding the parity again:
    # a closing run now counts as the single delimiter it
    # is, its earlier primes being name text (#733) — unless
    # the run's last quote is also the opening of the next,
    # adjacently written span ('a''b'), in which case the
    # run counts as close plus opening (#743).  #744 judged
    # that by the single character after the run — a word
    # character meant close plus opening, anything else a
    # single close — and misjudged in both directions: a
    # prose suffix after a primed span ('users''s) was taken
    # for a new span that never closes, and an adjacent span
    # starting with punctuation ('a''-b') was collapsed to a
    # single close.  The run is now judged by the rest of
    # the line: it counts as close plus opening only when a
    # span opened at its last quote actually closes later
    # (_closing_run_opens_next_span, #745).  #747's lookahead
    # inside that judgement recognised an independent span's
    # opening after a non-space separator by the characters
    # around it alone, which also matches a punctuation-
    # ending adjacent span's genuine close carrying a glued
    # suffix ('a''b-'s): that quote is now an opening only
    # when a span opened at it also closes later (#756).
    # #757's helper for that judgement took the next quote
    # as the candidate's close whenever the prose before it
    # ended in a separator outside its four-char set
    # ('a''b-'s flake:'other'): the candidate close is now
    # itself displaced when it has the opening signature
    # and a span opened at it closes later (#760).
    # A span in the following text that never closes still
    # counted its opening quote in the parity, so it could
    # balance the wrapper's genuine close seen from an
    # in-name candidate and yield a truncated prefix
    # ('a'!b' ' -> a, the #714 harm through the back door).
    # A rest that ends inside an unclosed span is not
    # balanced, so its count is forced odd for every
    # candidate and the line yields no match (#772).
    # If no quote qualifies, the name is genuinely ambiguous and
    # the line yields no match rather than a truncated prefix.
    for line in stderr.split("\n"):
        name = _wrapped_name(line, "while updating the flake input '")
        if name is not None:
            input_path = name
        name = _wrapped_name(line, "cannot find flake 'flake:")
        if name is not None:
            flake_name = name
    return input_path, flake_name


def _wrapped_name(line: str, prefix: str) -> str | None:
    """Extract the single-quoted name after prefix on one stderr line.

    Both callers' prefixes end exactly where the name text begins
    (the input form's prefix ends with the opening quote; the
    registry form's ends with "flake:", just after its opening
    quote).  Returns None when the prefix is absent or no quote on
    the line can be the wrapper's closing quote (see
    extract_failed_input for the name-text rules).
    """
    start = line.find(prefix)
    if start < 0:
        return None
    start += len(prefix)
    for i in range(start, len(line)):
        if line[i] != "'":
            continue
        if i + 1 < len(line) and line[i + 1] == "'":
            continue  # doubled quote: a prime in the name, not the close
        if i + 1 < len(line) and line[i + 1] == "/":
            continue  # prime ending a path segment: 'parent'/child
        if _is_word_apostrophe(line, i):
            continue  # apostrophe inside a word: a'b, not the close
        name = line[start:i]
        if not _plausible_input_path(name):
            continue
        if _rest_delimiter_quotes(line, i) % 2 == 0:
            return name
    return None


def _is_word_apostrophe(line: str, i: int) -> bool:
    """True when line[i] is an apostrophe inside a word — a word
    character on both sides (a'b in a name, user's in prose).  Such a
    quote is never a wrapper delimiter: a closing quote is followed
    by a space, punctuation or the end of the line, and a quoted span
    in following text is bounded the same way."""
    return (
        0 < i < len(line) - 1
        and (line[i - 1].isalnum() or line[i - 1] == "_")
        and (line[i + 1].isalnum() or line[i + 1] == "_")
    )


def _is_final_possessive(line: str, i: int, j: int) -> bool:
    """True when line[j], in the rest after candidate close i, is a
    word-final possessive apostrophe (users', James'): a word
    character before it, no word character after it, the word it
    ends is a standalone prose word (the character before that
    word is whitespace), and at least one further word of prose
    stands between the candidate close and that word.  The word
    it ends may be a hyphenated compound (end-users'): the walk
    back crosses a hyphen flanked by word characters, so the
    compound's first segment is what must be standalone — #728's
    walk stopped at the hyphen, found the last segment preceded
    by "-" instead of whitespace, and never discounted the
    possessive (#731).  The two prose conditions are what
    separate a possessive in following text from a delimiter: a
    quote closing a quoted span in the rest ends a word the
    span's opening quote precedes ('other'); a quote closing the
    wrapper itself, seen in the rest of an earlier in-name
    candidate, either ends a name fragment a name character
    precedes (the b of a'!b) or is the first word after that
    candidate (the bar of 'foo' bar') — both keep counting as
    delimiters, so the #714 continuation cases keep their parity
    (#727).  This judgement is only consulted for a quote outside
    any quoted span in the rest: _rest_delimiter_quotes tracks
    span state, so a multi-word span's closing quote is never
    judged here at all (#731)."""
    if not (line[j - 1].isalnum() or line[j - 1] == "_"):
        return False
    if j + 1 < len(line) and (line[j + 1].isalnum() or line[j + 1] == "_"):
        return False
    k = j
    while k > 0 and (
        line[k - 1].isalnum()
        or line[k - 1] == "_"
        or (
            line[k - 1] == "-"
            and k > 1
            and (line[k - 2].isalnum() or line[k - 2] == "_")
        )
    ):
        k -= 1
    if not (k > 0 and line[k - 1].isspace()):
        return False
    return " " in line[i + 1 : j].strip()


def _closing_run_opens_next_span(line: str, run_end: int) -> bool:
    """True when a span opened at line[run_end] — the last quote
    of a closing run, taken as the opening of an adjacently
    written next span — actually closes later in the line.

    The next non-apostrophe quote after the run is that span's
    would-be close.  It can close the span only when the span's
    content is non-empty and does not end in whitespace (a
    quoted name never trails a space): when no quote follows
    the run at all, the character after the run is a prose
    suffix on the span that just closed ('users''s flake,
    'users''-owned), and when the next quote is preceded by
    whitespace it is the opening of a later, independent span
    ('users''s … 'other'), not this one's close.  #744 judged
    the run by the single character after it, so the first
    shape was mistaken for an opening and the genuinely
    adjacent punctuation-start spans ('a''-b', 'a''/nix/…')
    were collapsed to a single close (#745).

    The whitespace test alone misses an independent span whose
    opening quote is preceded by a non-space separator —
    'users''s flake, ('other')', 'users''s flake:'other'' —
    where the separator is the content's last character, is
    not whitespace, and so passed the test above (#747).  An
    opening quote is recognised directly: a non-word character
    before it (the separator: space, '(', ':', '[', '-', '/',
    '"' …) and span content after it — a character that is
    neither whitespace nor the prose punctuation that follows
    a close (')]}.,;:!?').  A genuine close is preceded by its
    span's content, so when it ends in punctuation the
    character after it is that prose punctuation, whitespace
    or the end of the line, never a content character.

    That last claim is false for a glued prose suffix: an
    adjacently written span that ends in punctuation and
    carries a suffix — 'a''b-'s, the 'users''s paradigm of
    #744 with a punctuation-ending second span — has a word
    character after its genuine close, so the single
    character after the would-be close cannot decide the
    question on its own (#756).  The opening signature is
    therefore only a candidate: the quote is an opening only
    when a span opened at it actually closes later
    (_span_opened_at_closes) — 'other' after a separator
    closes at its own quote, while the suffix after 'b-'
    reaches no closing quote with plausible span content
    (what follows it is prose, ending in whitespace or an
    opening separator before any later span's quote)."""
    start = run_end + 1
    for j in range(start, len(line)):
        if line[j] != "'":
            continue
        if _is_word_apostrophe(line, j):
            continue
        content = line[start:j]
        if not content or content[-1].isspace():
            return False
        if not (line[j - 1].isalnum() or line[j - 1] == "_"):
            after = line[j + 1] if j + 1 < len(line) else ""
            if (
                after
                and not after.isspace()
                and after not in ")]}.,;:!?'"
                and _span_opened_at_closes(line, j)
            ):
                return False
        return True
    return False


def _span_opened_at_closes(line: str, opening: int) -> bool:
    """True when a span opened at line[opening] closes later:
    the next non-apostrophe quote after it has plausible span
    content before it — non-empty, not ending in whitespace
    (a quoted name never trails a space) and not ending in an
    opening separator ('(', '[', '{' or '"'), which stands
    outside a span in prose and so cannot be its last content
    character.  When no quote follows at all, or the next
    quote's would-be content fails those tests, what follows
    the opening candidate is prose — a glued suffix such as
    the 's of 'b-'s, plus the rest of the sentence — not a
    span, so the candidate quote never opens one (#756).

    The content test alone is not enough when the prose after
    a glued suffix ends in a separator other than the four
    above — 'a''b-'s flake:'other', and likewise with ',',
    ';', '-', '/', '=', ']', ')', '}', '.', '!' or '?' as the
    separator before a later independent span.  The would-be
    content 's flake:' then ends in that separator, which is
    not whitespace and not in the four, so the later span's
    opening quote was taken for this candidate's close and
    the suffix was believed to open a span (#760).  That
    quote is recognised as an opening the same way #756
    recognised the candidate itself: a non-word character
    before it (whatever separator the prose happens to end
    in — the family is judged generally, not by listing its
    members) and span content after it, and the reading
    holds only when a span opened at that quote itself
    closes later (this function, one span further on).  A
    genuine close is not displaced by the recursion: a
    punctuation-ending span's close with a glued suffix
    ('b-'s …) reaches no closing quote with plausible
    content through its suffix prose, exactly as before,
    because the suffix's own candidate close — the later
    span's opening — is displaced the same way."""
    start = opening + 1
    for k in range(start, len(line)):
        if line[k] != "'":
            continue
        if _is_word_apostrophe(line, k):
            continue
        content = line[start:k]
        if not content or content[-1].isspace() or content[-1] in '([{"':
            return False
        if not (line[k - 1].isalnum() or line[k - 1] == "_"):
            after = line[k + 1] if k + 1 < len(line) else ""
            if (
                after
                and not after.isspace()
                and after not in ")]}.,;:!?'"
                and _span_opened_at_closes(line, k)
            ):
                return False
        return True
    return False


def _rest_delimiter_quotes(line: str, i: int) -> int:
    """Delimiter quotes in line after index i: single quotes that are
    not word apostrophes.  Following text quotes its own words in
    pairs, so a genuine closing quote leaves an even count; a
    possessive like user's is an apostrophe, not a delimiter, and
    must not skew the parity (#722) — and neither is a word-final
    possessive like users', whose apostrophe is word-final rather
    than medial (#727).

    The scan tracks quoted spans in the rest: a quote with no
    word character before it opens a span, and the next
    non-apostrophe quote closes it and counts as a delimiter
    whatever word it ends.  #728 judged every quote on its own,
    so the closing quote of a multi-word span ('power users')
    met the standalone-word possessive test — its last word is
    preceded by the space inside the span — was discounted, and
    the genuine close left an odd count and was rejected (#731).
    A word-final quote outside any span is closing-like (a
    possessive, discounted per _is_final_possessive, or the
    wrapper's own close seen from an earlier in-name candidate,
    which counts) and never opens a span, so a possessive after
    an in-name apostrophe's genuine close is still judged on its
    own rather than swallowed by a phantom span.

    A span quoting a prime-ending name ends in a run of quotes
    ('power users''): the primes before the run's last quote are
    name text and the last quote is the span's close, so the
    whole run is the span's single closing delimiter.  Counting
    the run per quote closed the span at its first prime and
    then treated the second prime — preceded by a quote, not a
    word character — as the opening of a new span that was
    never closed, stranding the parity and rejecting the
    genuine close (#733).  A multi-quote run can instead be
    two delimiters — this span's close and the opening of
    the next span written adjacently ('a''b') — and both
    count, with the next span tracked (#743).  #744 told the
    two apart by the one character after the run, which
    misjudges both ways: a word character can be a prose
    suffix on the primed span ('users''s flake — no span
    follows), and a punctuation character can start the next
    span ('a''-b').  The run is therefore judged by the
    rest of the line: _closing_run_opens_next_span asks
    whether a span opened at the run's last quote actually
    closes later (#745) — and whether the quote it finds
    there closes anything at all, rather than opening an
    independent span after a non-space separator (#747),
    a reading that itself holds only when a span opened at
    that quote closes later, lest a punctuation-ending
    adjacent span's close with a glued suffix ('a''b-'s)
    be taken for such an opening (#756) — and that later
    closing judgement itself displaces a candidate close
    that opens a further independent span after any
    separator, not only the four it first listed (#760).
    A span that is still open at the end of the line never
    closes, so it cannot balance the genuine close: if the
    count reached is even, it is forced odd (#772)."""
    count = 0
    in_span = False
    j = i + 1
    while j < len(line):
        if line[j] != "'":
            j += 1
            continue
        if _is_word_apostrophe(line, j):
            j += 1
            continue
        if in_span:
            # Scan the whole closing run before judging it: the
            # rest of the line decides what its last quote is.
            run_end = j
            while run_end + 1 < len(line) and line[run_end + 1] == "'":
                run_end += 1
            count += 1  # the span's close; earlier run quotes are primes
            if run_end > j and _closing_run_opens_next_span(line, run_end):
                # The run's last quote opens the next span,
                # written adjacently ('a''b', 'a''-b') — it is a
                # delimiter too, and the span it opens stays
                # tracked so its own close counts.  Collapsing
                # the whole run to the close — the #733 reading,
                # right when the quoted phrase really ends there
                # ('power users'' , 'users''s) — ate that opening,
                # flipped the parity and rejected the genuine
                # close (#743).
                count += 1
                j = run_end + 1
                continue  # in_span stays True: the next span is open
            in_span = False
            j = run_end + 1
            continue
        if not (line[j - 1].isalnum() or line[j - 1] == "_"):
            count += 1
            in_span = True
            j += 1
            continue
        if _is_final_possessive(line, i, j):
            j += 1
            continue
        count += 1
        j += 1
    if in_span and count % 2 == 0:
        # The rest ends inside a span that never closes: its
        # opening quote counted above, but it has no closing
        # partner, so the rest is not balanced — force the
        # count odd rather than let an unclosed span make an
        # in-name candidate look balanced and yield a
        # truncated prefix ('a'!b' ' -> a, #772).
        count += 1
    return count


def _plausible_input_path(name: str) -> bool:
    """True when name can be a flake input path or registry name:
    non-empty, no empty "/" segment, no leading or trailing
    whitespace.  A supposed close that leaves prose in the name
    ('nixpkgs' and then 'other -> nixpkgs' and then ) fails the
    whitespace rule, so an ambiguous line yields no match instead of
    a swallowed name (#722)."""
    if not name or name != name.strip():
        return False
    return all(segment for segment in name.split("/"))


def build_override_inputs(lock: dict, input_path: str) -> dict[str, str]:
    """Build --override-input args from the existing lock for a failed input path."""
    nodes = lock["nodes"]
    parts = input_path.split("/")
    current = "root"
    for part in parts:
        node = nodes.get(current, {})
        inputs = node.get("inputs", {})
        ref = inputs.get(part)
        if ref is None:
            return {}
        if isinstance(ref, str):
            current = ref
        elif isinstance(ref, list):
            resolved = resolve_follows(lock, ref)
            if resolved is None:
                return {}
            current = resolved

    node_data = nodes.get(current, {})
    url = node_original_url(node_data)
    # An indirect: result is the very reference Nix just failed to
    # resolve (#568) — overriding with it cannot progress, and
    # returning it here shadows the locked-URL fallback
    # (build_override_from_flake_name) in run_nix_flake_lock_robust.
    # Every other node_original_url consumer already guards this.
    if url and not url.startswith("indirect:"):
        return {input_path: url}
    return {}


# Known flake registry names that Nix can resolve without --override-input
KNOWN_REGISTRY_FLAKES = frozenset(
    {
        "nixpkgs",
        "flake-utils",
        "systems",
        "flake-compat",
        "flake-parts",
        "home-manager",
        "darwin",
        "nix-darwin",
        "treefmt-nix",
        "pre-commit-hooks",
        "devshell",
        "hercules-ci-effects",
        "dream2nix",
        "fenix",
        "rust-overlay",
        "naersk",
        "poetry2nix",
    }
)


def build_override_from_flake_name(
    lock: dict, input_path: str, flake_name: str | None
) -> dict[str, str]:
    """Fallback: find a node by indirect id or name and build URL from locked field."""
    if not flake_name:
        return {}
    nodes = lock.get("nodes", {})
    # First pass: look for a node whose original is indirect:flake_name
    for _node_name, node_data in nodes.items():
        orig = node_data.get("original", {})
        if orig.get("type") == "indirect" and orig.get("id") == flake_name:
            locked_url = _locked_url(node_data.get("locked", {}))
            if locked_url:
                return {input_path: locked_url}
    # Second pass: look for a node whose name matches flake_name
    for node_name, node_data in nodes.items():
        if node_name == flake_name or node_name.startswith(f"{flake_name}_"):
            url = node_original_url(node_data)
            if url and not url.startswith("indirect:"):
                return {input_path: url}
    return {}


def compute_indirect_overrides(
    lock: dict, flake_content: str | None = None
) -> dict[str, str]:
    """Pre-compute --override-input for inputs that Nix cannot resolve on its own.

    Handles two cases:

    1. **Indirect registry references** — transitive inputs whose ``original``
       uses ``"type": "indirect"`` with an id not in the standard Nix flake
       registries (e.g. ``flake:cl-nix-lite``).

    2. **Deep follows overrides** — when flake.nix has declarations like
       ``inputs.mac-app-util.inputs.cl-nix-lite.inputs.systems.follows = "systems"``
       Nix must resolve ``mac-app-util/cl-nix-lite`` but may fail if the
       upstream flake uses a non-standard reference.  We detect these by
       parsing the flake.nix content for deep input-override patterns and
       proactively providing ``--override-input`` for each transitive input
       that Nix would need to fetch.
    """
    nodes = lock.get("nodes", {})
    overrides: dict[str, str] = {}

    # --- Case 1: indirect registry refs not in known set ---
    def walk(node_name: str, prefix: str, ancestors: frozenset = frozenset()) -> None:
        # Per-path ancestor guard (#563): a lock graph whose string
        # refs form a cycle (A inputs B, B inputs A) otherwise recurses
        # until RecursionError crashes the whole run. The guard is
        # per-path, NOT a global visited set — overrides are keyed by
        # path, so a diamond (same node via several distinct paths)
        # must still be walked once per path.
        if node_name in ancestors:
            return
        ancestors = ancestors | {node_name}
        node = nodes.get(node_name, {})
        for inp_name, ref in node.get("inputs", {}).items():
            path = f"{prefix}/{inp_name}" if prefix else inp_name
            if isinstance(ref, list):
                # follows – skip, Nix resolves these via the follows chain
                continue
            if isinstance(ref, str):
                child = nodes.get(ref, {})
                orig = child.get("original", {})
                if (
                    orig.get("type") == "indirect"
                    and orig.get("id") not in KNOWN_REGISTRY_FLAKES
                ):
                    # Try to build a real URL from the node
                    locked_url = _locked_url(child.get("locked", {}))
                    if locked_url:
                        overrides[path] = locked_url
                    else:
                        url = node_original_url(child)
                        if url and not url.startswith("indirect:"):
                            overrides[path] = url
                walk(ref, path, ancestors)

    walk("root", "")

    # --- Case 2: deep follows overrides in flake.nix ---
    if flake_content:
        _add_deep_follows_overrides(lock, flake_content, overrides)

    return overrides


def _add_deep_follows_overrides(
    lock: dict, content: str, overrides: dict[str, str]
) -> None:
    """Parse flake.nix for deep input-override patterns and add overrides.

    Looks for patterns like::

        mac-app-util = {
          inputs.cl-nix-lite.inputs.systems.follows = "systems";
        };

    or top-level::

        inputs.mac-app-util.inputs.cl-nix-lite.inputs.systems.follows = "systems";

    For each unique transitive input path (``mac-app-util/cl-nix-lite`` etc.),
    walks the lock graph to find the node and build a resolvable URL for
    ``--override-input``.
    """
    nodes = lock.get("nodes", {})

    # Collect all transitive input paths that Nix would need to fetch.
    # We need to handle two flake.nix styles:
    #
    # 1. Block style:  <root-input> = { inputs.X.inputs.Y.follows = "Z"; };
    #    Here X is a transitive input of <root-input>.
    #
    # 2. Dotted style: inputs.<root>.inputs.X.inputs.Y.follows = "Z";
    #    Here X is a transitive input of <root>.

    deep_paths: set[str] = set()

    # --- Parse block-style: track current input block context ---
    current_block: str | None = None
    brace_depth = 0
    in_inputs_section = False

    for line in content.split("\n"):
        stripped = line.strip()

        # Detect top-level inputs section. A nested `inputs = {` inside
        # another input's override is not the root section: re-entering
        # here would reset the brace depth and drop every deep path
        # declared after the nested block (#640).
        if not in_inputs_section and re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_section = True
            brace_depth = 1
            continue

        if in_inputs_section:
            # Count braces
            opens, closes = _brace_counts(stripped)

            # Detect named input block: <name> = {
            if current_block is None and brace_depth == 1:
                m = re.match(rf"{_SEG_TOKEN}\s*=\s*\{{", stripped)
                if m:
                    current_block = _unquote_name(m.group(1))
                    brace_depth += opens  # count the { we just matched
                    brace_depth -= closes
                    # Check for deep follows inside this block on the same line
                    # (unlikely but handle it)
                    continue

            # Inside a named input block
            if current_block is not None:
                # Look for: inputs.X.inputs.Y.follows (or .url, etc.)
                m_deep = re.match(
                    rf"inputs\.{_SEG_TOKEN}(?:\.inputs\.{_SEG_TOKEN})+",
                    stripped,
                )
                if m_deep:
                    parts = [
                        _unquote_name(t)
                        for t in re.findall(rf"inputs\.{_SEG_TOKEN}", stripped)
                    ]
                    # Full path is current_block / parts[0] / parts[1] / ...
                    # The transitive inputs that need resolving are the
                    # intermediate ones: current_block/parts[0],
                    # current_block/parts[0]/parts[1], etc.
                    for i in range(1, len(parts) + 1):
                        path = current_block + "/" + "/".join(parts[:i])
                        deep_paths.add(path)

                # Also catch: inputs.X.follows (X is transitive of current_block)
                m_shallow = re.match(
                    rf"inputs\.{_SEG_TOKEN}\.(follows|url)\b", stripped
                )
                if m_shallow and not m_deep:
                    # inputs.X.follows inside block = current_block/X is transitive
                    # But this is only relevant if X is NOT the root input itself
                    # Actually this is a follows *of* the root input, Nix handles
                    # this without needing to fetch X. Skip.
                    pass

            brace_depth += opens
            brace_depth -= closes

            if current_block is not None and brace_depth <= 1:
                current_block = None
            if brace_depth <= 0:
                in_inputs_section = False

    # --- Parse dotted-style: inputs.A.inputs.B.inputs.C.follows ---
    for line in content.split("\n"):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        m = re.match(
            rf"inputs\.{_SEG_TOKEN}(?:\.inputs\.{_SEG_TOKEN})+",
            stripped,
        )
        if m and not in_inputs_section:
            parts = [
                _unquote_name(t) for t in re.findall(rf"inputs\.{_SEG_TOKEN}", stripped)
            ]
            if len(parts) >= 2:
                for i in range(2, len(parts) + 1):
                    path = "/".join(parts[:i])
                    deep_paths.add(path)

    # --- Resolve each deep path via the lock graph ---
    for path in deep_paths:
        if path in overrides:
            continue
        parts = path.split("/")
        # Walk the lock to find the node for this path
        current = "root"
        valid = True
        for part in parts:
            node = nodes.get(current, {})
            inputs = node.get("inputs", {})
            ref = inputs.get(part)
            if ref is None:
                valid = False
                break
            if isinstance(ref, str):
                current = ref
            elif isinstance(ref, list):
                resolved = resolve_follows(lock, ref)
                if resolved is None:
                    valid = False
                    break
                current = resolved
        if not valid:
            continue
        node_data = nodes.get(current, {})
        url = node_original_url(node_data)
        if url and not url.startswith("indirect:"):
            overrides[path] = url


def run_nix_flake_lock_robust(
    flake_dir: str,
    lock_data: dict | None = None,
    flake_content: str | None = None,
    extra_overrides: dict[str, str] | None = None,
    max_retries: int = 5,
) -> tuple[bool, str]:
    """Run nix flake lock with proactive indirect-input overrides and retry loop.

    1. If ``lock_data`` is provided, pre-computes overrides for all indirect
       inputs not in the standard registries and for deep follows overrides
       found in ``flake_content``.
    2. Merges any ``extra_overrides`` on top.
    3. Runs ``nix flake lock``.
    4. On failure, parses stderr for the failed input, adds a new override from
       the lock data, and retries up to ``max_retries`` times.
    """
    overrides: dict[str, str] = {}
    if lock_data:
        overrides.update(compute_indirect_overrides(lock_data, flake_content))
    if extra_overrides:
        overrides.update(extra_overrides)

    for attempt in range(max_retries + 1):
        ok, stderr = run_nix_flake_lock(flake_dir, overrides if overrides else None)
        if ok:
            return True, ""
        # Parse the failure and try to add a new override
        input_path, flake_name = extract_failed_input(stderr)
        if not input_path or input_path in overrides:
            # Can't make progress – return the failure
            return False, stderr
        # Try path-based lookup first
        if lock_data:
            new_ov = build_override_inputs(lock_data, input_path)
            if not new_ov:
                new_ov = build_override_from_flake_name(
                    lock_data, input_path, flake_name
                )
            if new_ov:
                overrides.update(new_ov)
                print(
                    f"  retry {attempt + 1}: adding --override-input"
                    f" {input_path} -> {list(new_ov.values())[0]}",
                )
                continue
        # No new override found – give up
        return False, stderr
    return False, stderr


# ---------------------------------------------------------------------------
# Dedup logic
# ---------------------------------------------------------------------------


def analyze_dedup(
    lock: dict,
    content: str,
    config: dict[str, Any],
    verbose: bool = False,
) -> list[dict[str, Any]]:
    """Analyze the lock graph and return dedup proposals.

    Does NOT modify any files. Returns list of proposal dicts.
    """
    nodes = lock["nodes"]
    root_inputs = nodes.get("root", {}).get("inputs", {})
    file_order = get_input_file_order(content)
    max_depth = config["max-depth"]

    # Build source key groups
    groups: dict[str, list[str]] = defaultdict(list)
    for node_name, node_data in nodes.items():
        if node_name == "root" or "original" not in node_data:
            continue
        key = source_key(node_data)
        groups[key].append(node_name)

    proposals: list[dict[str, Any]] = []

    for key, members in groups.items():
        if len(members) < 2:
            continue
        if any(is_path_input(nodes[m]) for m in members):
            continue

        root_members = []
        transitive_members = []
        for m in members:
            if m in root_inputs.values():
                root_members.append(m)
            else:
                transitive_members.append(m)

        # --- Same source, different revs (#509) ---
        # source_key does not embed the pinned rev VALUE (only pin-ness,
        # #512), so a group of pinned nodes can hold one source at several
        # locked revs. The survivor is the newest
        # locked lastModified (rev string breaks ties, deterministically);
        # every other member follows it. Without evidence that the revs
        # really diverge — distinct rev pins in 'original', or locked
        # lastModified on both sides — the historical hash guard below
        # still applies unchanged.
        member_revs = {locked_rev(nodes[m]) for m in members if locked_rev(nodes[m])}
        rev_divergent = len(member_revs) > 1
        orig_revs = {
            nodes[m].get("original", {}).get("rev")
            for m in members
            if nodes[m].get("original", {}).get("rev")
        }

        def _pick_canonical(candidates: list[str]) -> str:
            """First candidate in file order, or the rev survivor (#509)."""
            if rev_divergent:
                return max(candidates, key=lambda n: _rev_survivor_key(nodes[n]))
            return candidates[0]

        def _rev_dedup_ok(other_node: str, canon_node: str) -> bool:
            """True when pointing other_node at canon_node's rev is the
            #509 consolidation (different rev, survivor already chosen),
            not a hash mismatch to guard against."""
            if not rev_divergent:
                return False
            if locked_rev(nodes[other_node]) == locked_rev(nodes[canon_node]):
                return False
            if len(orig_revs) > 1:
                return True
            return "lastModified" in nodes[other_node].get("locked", {}) and (
                "lastModified" in nodes[canon_node].get("locked", {})
            )

        def _annotate_rev(p: dict[str, Any], other_node: str, canon_node: str) -> None:
            if rev_divergent and locked_rev(nodes[other_node]) != locked_rev(
                nodes[canon_node]
            ):
                p["rev_dedup"] = True
                p["kept_rev"] = locked_rev(nodes[canon_node])
                p["dropped_rev"] = locked_rev(nodes[other_node])
                p["desc"] += (
                    f" (same source at rev {p['dropped_rev']}; keeping rev"
                    f" {p['kept_rev']} — newest lastModified"
                    f" {locked_last_modified(nodes[canon_node])})"
                )

        # --- Root dedup: same-URL consolidation ---
        if len(root_members) >= 2:
            root_to_input = {}
            for iname, nname in root_inputs.items():
                if nname in root_members:
                    root_to_input[nname] = iname

            def _root_pos(node: str) -> int:
                name = root_to_input.get(node, "")
                return file_order.index(name) if name in file_order else 9999

            sorted_roots = sorted(root_members, key=_root_pos)
            canonical_node = _pick_canonical(sorted_roots)
            canonical_name = root_to_input.get(canonical_node, "")
            canonical_hash = locked_hash(nodes[canonical_node])

            for other in [r for r in sorted_roots if r != canonical_node]:
                other_name = root_to_input.get(other, "")

                if not should_include(config, "dedup", other_name):
                    if verbose:
                        print(f"  skip {other_name}: not in dedup includes")
                    continue
                if should_exclude(config, "dedup", other_name):
                    if verbose:
                        print(f"  skip {other_name}: excluded from dedup")
                    continue
                if should_exclude(
                    config, "dedup", canonical_name, node_url(nodes[canonical_node])
                ):
                    continue

                other_hash = locked_hash(nodes[other])
                rev_ok = _rev_dedup_ok(other, canonical_node)
                if other_hash != canonical_hash and not rev_ok:
                    if verbose:
                        print(
                            f"  skip {other_name}: hash differs from {canonical_name}"
                        )
                    continue

                follows_path = other_name
                if is_excluded_follows(config, follows_path):
                    continue

                proposal = {
                    "follows_parts": [other_name],
                    "target": canonical_name,
                    "follows_path": follows_path,
                    "source_key": key,
                    "desc": f"{other_name} -> follows {canonical_name}",
                    "operation": "dedup",
                    "hash_verified": not rev_ok,
                }
                _annotate_rev(proposal, other, canonical_node)
                proposals.append(proposal)

        # --- Transitive dedup ---
        canonical_node = None
        canonical_name = None

        if root_members:
            root_to_input = {}
            for iname, nname in root_inputs.items():
                if nname in root_members:
                    root_to_input[nname] = iname

            def _root_pos2(node: str) -> int:
                name = root_to_input.get(node, "")
                return file_order.index(name) if name in file_order else 9999

            canonical_node = _pick_canonical(sorted(root_members, key=_root_pos2))
            canonical_name = root_to_input.get(canonical_node, "")
        else:
            # No root input — skip (flatten mode would handle this)
            continue

        if should_exclude(config, "dedup", canonical_name):
            continue
        if is_excluded_follows_url(config, node_url(nodes[canonical_node])):
            continue

        for trans_node in transitive_members:
            trans_url = node_url(nodes[trans_node])
            if should_exclude(config, "dedup", "", trans_url):
                continue

            # Never downgrade: a transitive node locked to a NEWER rev
            # than the root survivor stays put (#509) — follows can only
            # target root inputs, so there is nothing to point it at.
            if rev_divergent and _rev_survivor_key(
                nodes[trans_node]
            ) > _rev_survivor_key(nodes[canonical_node]):
                if verbose:
                    print(
                        f"  skip {trans_node}: rev {locked_rev(nodes[trans_node])}"
                        f" is newer than root {canonical_name}"
                    )
                continue

            all_paths = find_all_paths(lock, trans_node)

            for path in all_paths:
                if not path:
                    continue
                _last_inp, _last_child, last_is_follows = path[-1]
                if last_is_follows:
                    continue

                # Skip paths with intermediate follows edges
                has_intermediate_follows = any(is_f for _, _, is_f in path[:-1])
                if has_intermediate_follows:
                    if verbose:
                        fp = ".".join(inp for inp, _, _ in path)
                        print(f"  skip {fp}: intermediate follows in path")
                    continue

                depth = path_depth(path)
                if max_depth and depth > max_depth:
                    if verbose:
                        fp = ".".join(inp for inp, _, _ in path)
                        print(f"  skip {fp}: depth {depth} > max {max_depth}")
                    continue

                follows_parts = [inp for inp, _, _ in path]
                follows_path = ".".join(follows_parts)

                # Check includes
                if not should_include(config, "dedup", follows_parts[0]):
                    continue

                if is_excluded_full(
                    config,
                    lock,
                    "dedup",
                    follows_path,
                    follows_parts,
                    canonical_name,
                    canonical_node,
                ):
                    continue

                proposal = {
                    "follows_parts": follows_parts,
                    "target": canonical_name,
                    "follows_path": follows_path,
                    "source_key": key,
                    "desc": f"{follows_path} -> follows {canonical_name}",
                    "operation": "dedup",
                }
                _annotate_rev(proposal, trans_node, canonical_node)
                proposals.append(proposal)

    # --- Cross-type hash dedup ---
    # source_key grouping above misses cases where the original type differs
    # (e.g. github vs indirect) but locked content is byte-identical.
    # Fall back to locked hash comparison for these stragglers, preferring
    # explicit types (github/gitlab/sourcehut/git/hg) over indirect.
    # git must be listed (#526): unlisted types default to 99, which ranked
    # an explicit git URL below indirect — git roots were never chosen and
    # git transitive inputs were wrongly proposed as follows to indirect.
    # hg must be listed for the same reason (#539).
    _TYPE_PREF = {
        "github": 0,
        "gitlab": 1,
        "sourcehut": 2,
        "git": 3,
        "hg": 4,
        "tarball": 5,
        "file": 6,
        "path": 7,
        "indirect": 8,
    }

    grouped_nodes: set[str] = set()
    for _gk, gmembers in groups.items():
        if len(gmembers) >= 2:
            grouped_nodes.update(gmembers)

    # Map locked hash -> best root node
    root_by_hash: dict[str, tuple[str, str]] = {}  # hash -> (node_name, input_name)
    for iname, nname in root_inputs.items():
        if not isinstance(nname, str):
            continue
        ndata = nodes.get(nname, {})
        if "original" not in ndata or "locked" not in ndata:
            continue
        h = locked_hash(ndata)
        if not h:
            continue
        cur_type = ndata.get("original", {}).get("type", "")
        if h in root_by_hash:
            existing_type = (
                nodes[root_by_hash[h][0]].get("original", {}).get("type", "")
            )
            if _TYPE_PREF.get(cur_type, 99) < _TYPE_PREF.get(existing_type, 99):
                root_by_hash[h] = (nname, iname)
        else:
            root_by_hash[h] = (nname, iname)

    for node_name, node_data in nodes.items():
        if node_name == "root" or "original" not in node_data:
            continue
        if node_name in grouped_nodes:
            continue
        if node_name in root_inputs.values():
            continue
        if is_path_input(node_data):
            continue

        h = locked_hash(node_data)
        if not h or h not in root_by_hash:
            continue

        canon_node, canon_iname = root_by_hash[h]

        # Only act when source_key actually differs (same-key was handled above)
        if source_key(node_data) == source_key(nodes[canon_node]):
            continue

        # Prefer explicit type at root; skip if transitive is MORE explicit
        trans_type = node_data.get("original", {}).get("type", "")
        canon_type = nodes[canon_node].get("original", {}).get("type", "")
        if _TYPE_PREF.get(trans_type, 99) < _TYPE_PREF.get(canon_type, 99):
            continue

        # Never pair a rev-pinned node with an unpinned one, even when
        # the locked content happens to be identical today (#512): a
        # follows would discard the pin (or freeze the tracking input).
        if is_rev_pinned(node_data) != is_rev_pinned(nodes[canon_node]):
            continue

        if should_exclude(config, "dedup", canon_iname):
            continue
        if is_excluded_follows_url(config, node_url(nodes[canon_node])):
            continue

        trans_url = node_url(node_data)
        if should_exclude(config, "dedup", "", trans_url):
            continue

        all_paths = find_all_paths(lock, node_name)

        for path in all_paths:
            if not path:
                continue
            _last_inp, _last_child, last_is_follows = path[-1]
            if last_is_follows:
                continue

            has_intermediate_follows = any(is_f for _, _, is_f in path[:-1])
            if has_intermediate_follows:
                if verbose:
                    fp = ".".join(inp for inp, _, _ in path)
                    print(f"  skip {fp}: intermediate follows in path")
                continue

            depth = path_depth(path)
            if max_depth and depth > max_depth:
                if verbose:
                    fp = ".".join(inp for inp, _, _ in path)
                    print(f"  skip {fp}: depth {depth} > max {max_depth}")
                continue

            follows_parts = [inp for inp, _, _ in path]
            follows_path = ".".join(follows_parts)

            if not should_include(config, "dedup", follows_parts[0]):
                continue

            if is_excluded_full(
                config,
                lock,
                "dedup",
                follows_path,
                follows_parts,
                canon_iname,
                canon_node,
            ):
                continue

            proposals.append(
                {
                    "follows_parts": follows_parts,
                    "target": canon_iname,
                    "follows_path": follows_path,
                    "source_key": f"cross-type:{source_key(node_data)}={source_key(nodes[canon_node])}",
                    "desc": f"{follows_path} -> follows {canon_iname} (cross-type)",
                    "operation": "dedup",
                }
            )

    # Deduplicate by follows_path
    seen_paths: set[str] = set()
    unique: list[dict[str, Any]] = []
    for p in proposals:
        if p["follows_path"] not in seen_paths:
            seen_paths.add(p["follows_path"])
            unique.append(p)
    proposals = unique

    # Filter out already existing
    new_proposals = [
        p
        for p in proposals
        if not follows_exists_in_content(content, p["follows_parts"], p["target"])
    ]
    return new_proposals


def apply_dedup(
    flake_dir: str,
    proposals: list[dict[str, Any]],
    verbose: bool = False,
) -> tuple[int, list[dict[str, Any]]]:
    """Apply dedup proposals. Returns (applied_count, failed_proposals)."""
    if not proposals:
        return 0, []

    content = read_flake_nix(flake_dir)
    lock = load_lock(flake_dir)

    # Sort by depth, apply in batches
    proposals_sorted = sorted(proposals, key=lambda p: len(p["follows_parts"]))
    depth_batches = []
    for depth, grp in groupby(proposals_sorted, key=lambda p: len(p["follows_parts"])):
        depth_batches.append((depth, list(grp)))

    applied = 0
    failed: list[dict[str, Any]] = []

    for depth, batch in depth_batches:
        saved_content = content
        lock_path = os.path.join(flake_dir, "flake.lock")
        with open(lock_path) as f:
            saved_lock = f.read()

        for p in batch:
            commented = find_commented_follows(content, p["follows_parts"], p["target"])
            if commented is not None:
                line_no, original_line = commented
                if verbose:
                    print(f"  uncomment: {original_line.strip()}")
                content = uncomment_line(content, line_no)
            else:
                if verbose:
                    print(f"  add: {p['desc']}")
                content = insert_follows_in_content(
                    content, p["follows_parts"], p["target"]
                )

            # When a root-level input now follows another and the hashes
            # are known to match, its url line is redundant and conflicts
            # with the new follows — remove it. Same for a #509 rev dedup:
            # the input's own rev pin conflicts with the follows.
            if len(p["follows_parts"]) == 1 and (
                p.get("hash_verified") or p.get("rev_dedup")
            ):
                input_name = p["follows_parts"][0]
                content, removed = remove_input_url_line(content, input_name)
                if removed:
                    reason = (
                        f"superseded rev {p['dropped_rev']}; follows"
                        f" {p['target']} at rev {p['kept_rev']}"
                        if p.get("rev_dedup")
                        else f"hash-aligned with {p['target']}"
                    )
                    print(f"  remove url: {input_name} ({reason})")

        write_flake_nix(flake_dir, content)
        run_nixfmt(flake_dir)
        content = read_flake_nix(flake_dir)

        print(f"  locking (depth {depth})...")
        saved_lock_data = json.loads(saved_lock)
        ok, stderr = run_nix_flake_lock_robust(
            flake_dir, lock_data=saved_lock_data, flake_content=content
        )
        if ok:
            applied += len(batch)
        else:
            is_preexisting = "has both a flake reference and a follows" in stderr
            if is_preexisting:
                print(
                    "  error: nix flake lock failed due to a pre-existing"
                    " url+follows conflict in flake.nix (not caused by tidy).",
                    file=sys.stderr,
                )
            else:
                print(
                    f"  warning: lock failed for depth-{depth} follows, backing out",
                    file=sys.stderr,
                )
            # Back out this batch
            for p in batch:
                print(f"    skipped: {p['desc']}", file=sys.stderr)
                failed.append(p)
            content = saved_content
            write_flake_nix(flake_dir, content)
            run_nixfmt(flake_dir)
            content = read_flake_nix(flake_dir)
            with open(lock_path, "w") as f:
                f.write(saved_lock)

    return applied, failed


def dedup(
    flake_dir: str,
    config: dict[str, Any],
    dry_run: bool = False,
    check: bool = False,
    verbose: bool = False,
) -> int:
    """Main dedup action. Returns number of proposals found (for check mode)."""
    total_proposals = 0
    total_added = 0
    round_num = 0

    while True:
        round_num += 1
        if verbose:
            print(f"\n--- dedup round {round_num} ---")

        lock = load_lock(flake_dir)
        content = read_flake_nix(flake_dir)
        proposals = analyze_dedup(lock, content, config, verbose=verbose)

        total_proposals += len(proposals)

        if not proposals:
            if verbose:
                print("  no new follows to add.")
            break

        print(f"  dedup: {len(proposals)} follows to add:")
        for p in proposals:
            suffix = ""
            if (
                len(p["follows_parts"]) == 1
                and (p.get("hash_verified") or p.get("rev_dedup"))
                and find_input_url_line(content, p["follows_parts"][0]) is not None
            ):
                suffix = " (will remove redundant url)"
            print(f"    {p['desc']}{suffix}")

        if dry_run or check:
            break

        applied, failed = apply_dedup(flake_dir, proposals, verbose=verbose)
        total_added += applied

        if failed:
            print(f"\n  {len(failed)} follows could not be applied (lock failures):")
            for p in failed:
                print(f"    {p['desc']}")

        if applied == 0:
            break

    if total_added > 0 and not dry_run and not check:
        print(f"  dedup done: added {total_added} follows in {round_num} round(s).")
        run_nixfmt(flake_dir)

    return total_proposals


# ---------------------------------------------------------------------------
# Merge logic: hoist deep-followed transitive inputs to root
# ---------------------------------------------------------------------------


def _parse_deep_follows(content: str) -> list[dict[str, Any]]:
    """Parse flake.nix for deep follows patterns.

    Detects patterns like::

        parent = {
          inputs.CHILD.inputs.X.follows = "target";
        };

    the nested-block spelling of the same declaration::

        parent = {
          inputs = {
            CHILD.inputs.X.follows = "target";
          };
        };

    or top-level dotted style::

        inputs.parent.inputs.CHILD.inputs.X.follows = "target";

    Returns a list of dicts:
        {
            "parent": "mac-app-util",
            "child": "cl-nix-lite",
            "sub_input": "systems",
            "target": "systems",
            "line_number": 53,
            "line": "      inputs.cl-nix-lite.inputs.systems.follows = ...",
        }
    """
    results: list[dict[str, Any]] = []
    lines = content.split("\n")
    nested_prefixes = _nested_inputs_prefix_for_lines(content)
    in_inputs_section = False
    current_block: str | None = None
    brace_depth = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        # Detect top-level inputs = { block. A nested `inputs = {` inside
        # another input's override is not the root block: re-entering here
        # would reset the brace depth and drop every deep follows declared
        # after the nested block (#640).
        if not in_inputs_section and re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_section = True
            brace_depth = _brace_delta(stripped)
            continue

        if in_inputs_section:
            opens, closes = _brace_counts(stripped)

            # Detect named input block at depth 1
            if current_block is None and brace_depth == 1:
                m = re.match(rf"{_SEG_TOKEN}\s*=\s*\{{", stripped)
                if m:
                    current_block = _unquote_name(m.group(1))

            # Inside a named block: look for inputs.CHILD.inputs.SUB.follows
            if current_block is not None:
                # Nested-block form (#647): directly inside the parent's
                # nested ``inputs = { ... }`` override, the same deep
                # follows is spelled CHILD.inputs.SUB.follows.
                if nested_prefixes[i] == [current_block]:
                    for seg in _nested_segments(stripped):
                        m = re.match(
                            rf'{_SEG_TOKEN}\.inputs\.{_SEG_TOKEN}\.follows\s*=\s*"([^"]+)"',
                            seg,
                        )
                        if m:
                            results.append(
                                {
                                    "parent": current_block,
                                    "child": _unquote_name(m.group(1)),
                                    "sub_input": _unquote_name(m.group(2)),
                                    "target": m.group(3),
                                    "line_number": i,
                                    "line": line,
                                }
                            )
                m = re.match(
                    rf'inputs\.{_SEG_TOKEN}\.inputs\.{_SEG_TOKEN}\.follows\s*=\s*"([^"]+)"',
                    stripped,
                )
                if m:
                    results.append(
                        {
                            "parent": current_block,
                            "child": _unquote_name(m.group(1)),
                            "sub_input": _unquote_name(m.group(2)),
                            "target": m.group(3),
                            "line_number": i,
                            "line": line,
                        }
                    )

            brace_depth += opens
            brace_depth -= closes

            if current_block is not None and brace_depth <= 1:
                current_block = None
            if brace_depth <= 0:
                in_inputs_section = False

        # Dotted-style: inputs.PARENT.inputs.CHILD.inputs.SUB.follows
        m = re.match(
            rf'inputs\.{_SEG_TOKEN}\.inputs\.{_SEG_TOKEN}\.inputs\.{_SEG_TOKEN}\.follows\s*=\s*"([^"]+)"',
            stripped,
        )
        if m and not in_inputs_section:
            results.append(
                {
                    "parent": _unquote_name(m.group(1)),
                    "child": _unquote_name(m.group(2)),
                    "sub_input": _unquote_name(m.group(3)),
                    "target": m.group(4),
                    "line_number": i,
                    "line": line,
                }
            )

    return results


def _remove_lines(content: str, line_numbers: set[int]) -> str:
    """Remove specific lines from content by line number."""
    lines = content.split("\n")
    return "\n".join(line for i, line in enumerate(lines) if i not in line_numbers)


def _resolve_transitive_url(lock: dict, parent: str, child: str) -> str | None:
    """Look up the URL for a transitive input (parent/child) from the lock file."""
    nodes = lock.get("nodes", {})
    root_node = nodes.get("root", {})
    root_inputs = root_node.get("inputs", {})

    # Find the parent's node name
    parent_ref = root_inputs.get(parent)
    if not parent_ref or not isinstance(parent_ref, str):
        return None

    parent_node = nodes.get(parent_ref, {})
    parent_inputs = parent_node.get("inputs", {})

    # Find the child's node name
    child_ref = parent_inputs.get(child)
    if not child_ref:
        return None

    if isinstance(child_ref, list):
        child_node_name = resolve_follows(lock, child_ref)
    else:
        child_node_name = child_ref

    if not child_node_name:
        return None

    child_node = nodes.get(child_node_name, {})
    url = node_original_url(child_node)
    if url and not url.startswith("indirect:"):
        return url

    # Fallback: try locked field
    return _locked_url(child_node.get("locked", {}))


def insert_root_input_block(
    content: str, input_name: str, url: str, sub_follows: list[tuple[str, str]]
) -> str:
    """Add a new root input as a block with sub-follows declarations.

    Inserts::

        NAME = {
          url = "URL";
          inputs.X.follows = "target-x";
          inputs.Y.follows = "target-y";
        };

    into the inputs section.
    """
    if root_input_exists_in_content(content, input_name):
        return content

    style, block_start, block_end = detect_inputs_style(content)
    lines = content.split("\n")

    indent = "    "
    if style == "block" and block_start is not None and block_end is not None:
        for k in range(block_start + 1, block_end):
            m = re.match(r"^(\s+)", lines[k])
            if m and lines[k].strip():
                indent = m.group(1)
                break

    # Build the block
    block_lines = [f"{indent}{_quote_name(input_name)} = {{"]
    block_lines.append(f'{indent}  url = "{url}";')
    for sub_input, target in sub_follows:
        block_lines.append(
            f'{indent}  inputs.{_quote_name(sub_input)}.follows = "{target}";'
        )
    block_lines.append(f"{indent}}};")

    if style == "block" and block_end is not None:
        for line_str in reversed(block_lines):
            lines.insert(block_end, line_str)
    else:
        # Dotted style: find last root-scope input line. The block must be
        # spelled inputs.NAME = { ... }; — a bare NAME = { ... } at the
        # flake's top level is an unrelated attribute, not an input (#696).
        last_input_line = _last_dotted_anchor(lines, content)
        dotted_lines = [f"{indent}inputs.{_quote_name(input_name)} = {{"] + block_lines[1:]
        for j, line_str in enumerate(dotted_lines):
            lines.insert(last_input_line + 1 + j, line_str)

    return "\n".join(lines)


def analyze_merge(
    lock: dict,
    content: str,
    config: dict[str, Any],
    verbose: bool = False,
) -> list[dict[str, Any]]:
    """Analyze flake.nix for deep follows that need merging.

    Detects patterns where flake.nix overrides sub-inputs of a transitive
    input (e.g. ``mac-app-util.inputs.cl-nix-lite.inputs.systems.follows``).

    These cause ``nix flake lock`` to fail when the transitive input name
    is not in the flake registry.

    Returns merge proposals that hoist the transitive input to root with
    the sub-follows applied directly, plus a simple follows on the parent.
    """
    deep_follows = _parse_deep_follows(content)
    if not deep_follows:
        return []

    # Group by (parent, child)
    groups: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    for df in deep_follows:
        groups[(df["parent"], df["child"])].append(df)

    proposals: list[dict[str, Any]] = []

    for (parent, child), entries in groups.items():
        # Check includes/excludes
        if not should_include(config, "merge", parent):
            if verbose:
                print(f"  skip merge {parent}/{child}: parent not in merge includes")
            continue
        if should_exclude(config, "merge", parent):
            if verbose:
                print(f"  skip merge {parent}/{child}: parent excluded from merge")
            continue
        if should_exclude(config, "merge", child):
            if verbose:
                print(f"  skip merge {parent}/{child}: child excluded from merge")
            continue

        # Skip if child is already a root input
        if root_input_exists_in_content(content, child):
            if verbose:
                print(f"  skip merge {parent}/{child}: {child} is already a root input")
            continue

        # Look up the URL for the transitive input
        url = _resolve_transitive_url(lock, parent, child)
        if not url:
            if verbose:
                print(
                    f"  skip merge {parent}/{child}: "
                    f"could not resolve URL from lock file"
                )
            continue

        # Collect sub-input follows: (sub_input_name, target)
        sub_follows: list[tuple[str, str]] = []
        line_numbers: set[int] = set()
        for entry in entries:
            sub_follows.append((entry["sub_input"], entry["target"]))
            line_numbers.add(entry["line_number"])

        proposals.append(
            {
                "parent": parent,
                "child": child,
                "url": url,
                "sub_follows": sub_follows,
                "line_numbers": line_numbers,
                "desc": (
                    f"merge {parent}/{child}: "
                    f"hoist {child} to root, "
                    f'add {parent}.inputs.{child}.follows = "{child}"'
                ),
                "operation": "merge",
            }
        )

    return proposals


def apply_merge(
    flake_dir: str,
    proposals: list[dict[str, Any]],
    verbose: bool = False,
) -> tuple[int, list[dict[str, Any]]]:
    """Apply merge proposals. Returns (applied_count, failed_proposals)."""
    if not proposals:
        return 0, []

    content = read_flake_nix(flake_dir)
    lock_path = os.path.join(flake_dir, "flake.lock")

    saved_content = content
    with open(lock_path) as f:
        saved_lock = f.read()

    # Apply all merge proposals
    for p in proposals:
        # 1. Remove the old deep follows lines
        if verbose:
            for ln in sorted(p["line_numbers"]):
                print(f"  remove deep follows line {ln + 1}")
        content = _remove_lines(content, p["line_numbers"])

        # 2. Add the new root input block with sub-follows
        if verbose:
            print(f'  add root input: {p["child"]} = {{ url = "{p["url"]}"; ... }}')
        content = insert_root_input_block(
            content, p["child"], p["url"], p["sub_follows"]
        )

        # 3. Add follows on the parent: parent.inputs.CHILD.follows = "CHILD"
        follows_parts = [p["parent"], p["child"]]
        if not follows_exists_in_content(content, follows_parts, p["child"]):
            if verbose:
                print(f"  add follows: {p['parent']}.{p['child']} -> {p['child']}")
            content = insert_follows_in_content(content, follows_parts, p["child"])

    write_flake_nix(flake_dir, content)
    run_nixfmt(flake_dir)
    content = read_flake_nix(flake_dir)

    print("  locking after merge...")
    saved_lock_data = json.loads(saved_lock)
    ok, stderr = run_nix_flake_lock_robust(
        flake_dir, lock_data=saved_lock_data, flake_content=content
    )
    if ok:
        return len(proposals), []
    else:
        # Back out all merge changes
        print("  warning: lock failed after merge, backing out", file=sys.stderr)
        content = saved_content
        write_flake_nix(flake_dir, content)
        run_nixfmt(flake_dir)
        with open(lock_path, "w") as f:
            f.write(saved_lock)
        return 0, proposals


def merge(
    flake_dir: str,
    config: dict[str, Any],
    dry_run: bool = False,
    check: bool = False,
    verbose: bool = False,
) -> int:
    """Main merge action. Returns number of proposals found."""
    lock = load_lock(flake_dir)
    content = read_flake_nix(flake_dir)
    proposals = analyze_merge(lock, content, config, verbose=verbose)

    if not proposals:
        if verbose:
            print("  merge: no deep follows to hoist.")
        return 0

    print(f"  merge: {len(proposals)} transitive input(s) to hoist:")
    for p in proposals:
        print(f"    + {p['child']} ({p['url']}) from {p['parent']}")
        for sub_input, target in p["sub_follows"]:
            print(f'      {p["child"]}.inputs.{sub_input}.follows = "{target}"')
        print(f'      {p["parent"]}.inputs.{p["child"]}.follows = "{p["child"]}"')

    if dry_run or check:
        return len(proposals)

    applied, failed = apply_merge(flake_dir, proposals, verbose=verbose)

    if failed:
        print(f"\n  {len(failed)} merge proposals could not be applied:")
        for p in failed:
            print(f"    {p['desc']}")

    if applied > 0:
        print(f"  merge done: hoisted {applied} transitive input(s) to root.")
        run_nixfmt(flake_dir)

    return len(proposals)


# ---------------------------------------------------------------------------
# Flatten logic
# ---------------------------------------------------------------------------


def analyze_flatten(
    lock: dict,
    content: str,
    config: dict[str, Any],
    verbose: bool = False,
) -> list[dict[str, Any]]:
    """Analyze the lock graph and return flatten proposals.

    A flatten proposal hoists a transitive-only input to root level.
    """
    nodes = lock["nodes"]
    root_inputs = nodes.get("root", {}).get("inputs", {})
    root_node_names = set(v for v in root_inputs.values() if isinstance(v, str))
    max_depth = config["max-depth"]

    # Find all transitive-only nodes
    transitive_candidates: dict[str, list[list[tuple[str, str, bool]]]] = {}

    for node_name, node_data in nodes.items():
        if node_name == "root" or "original" not in node_data:
            continue
        if node_name in root_node_names:
            continue
        if is_path_input(node_data):
            continue

        # Check if any root input has the same source already
        # Match by source_key first, then fall back to locked hash so that
        # cross-type matches (e.g. indirect vs github) are caught by dedup.
        key = source_key(node_data)
        node_hash = locked_hash(node_data)
        has_root_equivalent = False
        for _iname, rnode in root_inputs.items():
            if not isinstance(rnode, str):
                continue
            rdata = nodes.get(rnode, {})
            if "original" not in rdata:
                continue
            if source_key(rdata) == key:
                has_root_equivalent = True
                break
            if node_hash and locked_hash(rdata) == node_hash:
                has_root_equivalent = True
                break
        if has_root_equivalent:
            if verbose:
                print(f"  skip flatten {node_name}: root equivalent exists (use dedup)")
            continue

        # Find all paths from root to this node
        all_paths = find_all_paths(lock, node_name)
        valid_paths = []
        for path in all_paths:
            if max_depth and len(path) > max_depth:
                continue
            # Skip paths with intermediate follows
            if any(is_f for _, _, is_f in path[:-1]):
                if verbose:
                    fp = ".".join(inp for inp, _, _ in path)
                    print(f"  skip flatten path {fp}: intermediate follows")
                continue
            # Skip if last edge is already a follows
            if path and path[-1][2]:
                continue
            valid_paths.append(path)

        if valid_paths:
            transitive_candidates[node_name] = valid_paths

    if not transitive_candidates:
        return []

    # Group by source_key to avoid creating duplicate root inputs
    source_groups: dict[str, list[tuple[str, list]]] = defaultdict(list)
    for node_name, paths in transitive_candidates.items():
        key = source_key(nodes[node_name])
        source_groups[key].append((node_name, paths))

    proposals: list[dict[str, Any]] = []

    for key, members in source_groups.items():
        # Pick the best name for the new root input
        name_counts: dict[str, int] = defaultdict(int)
        for _node_name, paths in members:
            for path in paths:
                last_input = path[-1][0]
                name_counts[last_input] += 1

        best_name = (
            max(name_counts, key=name_counts.get) if name_counts else members[0][0]
        )

        # Check includes/excludes
        if not should_include(config, "flatten", best_name):
            if verbose:
                print(f"  skip flatten {best_name}: not in flatten includes")
            continue
        if should_exclude(config, "flatten", best_name):
            if verbose:
                print(f"  skip flatten {best_name}: excluded from flatten")
            continue

        # Get URL from the first node
        first_node_data = nodes[members[0][0]]
        url = node_original_url(first_node_data)
        node_hash = locked_hash(first_node_data)
        is_non_flake = first_node_data.get("flake", True) is False

        # If an existing root input has the same source or locked hash,
        # reuse it instead of creating a new root input.
        reuse_existing = False
        for iname, iref in root_inputs.items():
            if not isinstance(iref, str):
                continue
            rdata = nodes.get(iref, {})
            if "original" not in rdata:
                continue
            if source_key(rdata) == key or (
                node_hash and locked_hash(rdata) == node_hash
            ):
                best_name = iname
                reuse_existing = True
                if verbose:
                    print(
                        f"  flatten: reusing existing root input"
                        f" '{iname}' for {members[0][0]}"
                    )
                break

        if not reuse_existing:
            # Avoid name collision with existing root inputs
            if best_name in {iname for iname in root_inputs}:
                for alt_name in sorted(name_counts, key=name_counts.get, reverse=True):
                    if alt_name not in root_inputs:
                        best_name = alt_name
                        break
                else:
                    best_name = f"{best_name}-hoisted"

            # Avoid collision with other proposals
            existing_names = {p["new_input_name"] for p in proposals}
            if best_name in existing_names:
                base = best_name
                suffix = 2
                best_name = f"{base}-hoisted"
                while best_name in existing_names:
                    best_name = f"{base}-{suffix}"
                    suffix += 1

        # Skip indirect URLs — they can't be used as root input URLs since Nix
        # may not be able to resolve them from the global flake registries.
        if url.startswith("indirect:"):
            if verbose:
                print(f"  skip flatten {best_name}: indirect URL ({url})")
            continue

        # Collect all follows needed
        follows_list: list[list[str]] = []
        for _node_name, paths in members:
            for path in paths:
                follows_parts = [inp for inp, _, _ in path]
                follows_path = ".".join(follows_parts)

                if is_excluded_follows(config, follows_path):
                    continue
                if follows_exists_in_content(content, follows_parts, best_name):
                    continue
                # Skip if a follows for this path already exists with ANY
                # target (e.g. dedup already added one pointing elsewhere).
                if follows_path_has_any_target(content, follows_parts):
                    continue

                follows_list.append(follows_parts)

        if not follows_list and root_input_exists_in_content(content, best_name):
            # All follows already exist and root input already exists — nothing
            # to do for this proposal.
            if verbose:
                print(
                    f"  skip flatten {best_name}: root input exists"
                    f" and all follows paths already covered"
                )
            continue

        if not follows_list and not root_input_exists_in_content(content, best_name):
            # Still add the root input even without follows (makes it available)
            pass

        proposals.append(
            {
                "new_input_name": best_name,
                "url": url,
                "source_key": key,
                "follows": follows_list,
                "flake": not is_non_flake,
                "desc": f"hoist {best_name} ({url})",
                "operation": "flatten",
            }
        )

    return proposals


def _match_failure_to_proposal(
    input_path: str | None,
    proposals: list[dict[str, Any]],
) -> dict[str, Any] | None:
    """Match a failed input path from stderr to one of our proposals."""
    if not input_path:
        return None
    for p in proposals:
        name = p["new_input_name"]
        if input_path == name or input_path.startswith(name + "/"):
            return p
    return None


def apply_flatten(
    flake_dir: str,
    proposals: list[dict[str, Any]],
    verbose: bool = False,
) -> tuple[int, list[dict[str, Any]]]:
    """Apply flatten proposals using batch-with-elimination.

    Applies all proposals at once and locks.  If locking fails, identifies
    the bad proposal from stderr, removes it, and retries with the rest.
    Failed proposals carry ``error`` and ``failed_at`` keys for diagnostics.
    Returns (applied_count, failed_proposals).
    """
    if not proposals:
        return 0, []

    lock_path = os.path.join(flake_dir, "flake.lock")

    # Save original state
    original_content = read_flake_nix(flake_dir)
    with open(lock_path) as f:
        original_lock = f.read()
    original_lock_data = json.loads(original_lock)

    remaining = list(proposals)
    failed: list[dict[str, Any]] = []

    while remaining:
        # Reset to original state
        write_flake_nix(flake_dir, original_content)
        with open(lock_path, "w") as f:
            f.write(original_lock)

        # Apply all remaining proposals
        content = original_content
        for p in remaining:
            if not root_input_exists_in_content(content, p["new_input_name"]):
                if verbose:
                    print(f'  add root input: {p["new_input_name"]}.url = "{p["url"]}"')
                content = insert_root_input(
                    content,
                    p["new_input_name"],
                    p["url"],
                    flake=p.get("flake", True),
                )
            for follows_parts in p["follows"]:
                if not follows_exists_in_content(
                    content, follows_parts, p["new_input_name"]
                ):
                    # Also skip if the path already has a follows with a
                    # different target (e.g. dedup placed one earlier).
                    if follows_path_has_any_target(content, follows_parts):
                        if verbose:
                            fp = ".".join(follows_parts)
                            print(f"  skip follows: {fp} (already has a follows)")
                        continue
                    if verbose:
                        fp = ".".join(follows_parts)
                        print(f"  add follows: {fp} -> {p['new_input_name']}")
                    content = insert_follows_in_content(
                        content, follows_parts, p["new_input_name"]
                    )

        write_flake_nix(flake_dir, content)
        run_nixfmt(flake_dir)
        content = read_flake_nix(flake_dir)

        if verbose:
            print(f"  locking {len(remaining)} proposal(s)...")
        ok, stderr = run_nix_flake_lock_robust(
            flake_dir, lock_data=original_lock_data, flake_content=content
        )
        if ok:
            return len(remaining), failed

        # Lock failed — identify the bad proposal
        input_path, _flake_name = extract_failed_input(stderr)
        bad = _match_failure_to_proposal(input_path, remaining)

        now = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")

        if bad is None:
            # Check if it's a pre-existing flake.nix issue (url+follows)
            is_preexisting = "has both a flake reference and a follows" in stderr
            if is_preexisting:
                print(
                    "  error: nix flake lock failed due to a pre-existing"
                    " url+follows conflict in flake.nix (not caused by tidy).",
                    file=sys.stderr,
                )
                print(
                    "  fix the conflict manually, then re-run.",
                    file=sys.stderr,
                )
            elif verbose:
                print(
                    "  warning: lock failed, cannot identify failing proposal",
                    file=sys.stderr,
                )
            for p in remaining:
                p["error"] = stderr
                p["failed_at"] = now
            failed.extend(remaining)
            remaining = []
            break

        # Remove bad proposal and retry
        bad["error"] = stderr
        bad["failed_at"] = now
        if verbose:
            print(
                f"  removing failed proposal: {bad['new_input_name']}, retrying...",
                file=sys.stderr,
            )
        failed.append(bad)
        remaining.remove(bad)

    # Restore original state if nothing succeeded
    if not remaining:
        write_flake_nix(flake_dir, original_content)
        run_nixfmt(flake_dir)
        with open(lock_path, "w") as f:
            f.write(original_lock)

    return len(proposals) - len(failed), failed


def _first_error_line(stderr: str) -> str:
    """Extract the first meaningful (non-warning) line from nix stderr."""
    for line in stderr.split("\n"):
        stripped = line.strip()
        if stripped and not stripped.startswith("warning:"):
            if len(stripped) > 200:
                return stripped[:200] + "..."
            return stripped
    return "unknown error"


def insert_flatten_failure_comments(content: str, failed: list[dict[str, Any]]) -> str:
    """Insert commented-out flatten proposals with error diagnostics."""
    if not failed:
        return content

    style, _block_start, block_end = detect_inputs_style(content)
    lines = content.split("\n")

    # Detect indent from existing input lines
    indent = "    "
    for line in lines:
        m = re.match(r"^(\s+)\S+\.(url|follows)\s*=", line)
        if m:
            indent = m.group(1)
            break

    # Build comment blocks for all failures (reversed so insert order is stable)
    for p in reversed(failed):
        name = p["new_input_name"]
        url = p["url"]
        error_line = _first_error_line(p.get("error", ""))
        failed_at = p.get("failed_at", "unknown")

        comment_lines = [
            f"{indent}# [flake-tidy] flatten failed ({failed_at}): {name}",
            f"{indent}# error: {error_line}",
            f'{indent}# {name}.url = "{url}";',
        ]
        if not p.get("flake", True):
            comment_lines.append(f"{indent}# {name}.flake = false;")
        for follows_parts in p.get("follows", []):
            if len(follows_parts) == 1:
                comment_lines.append(
                    f'{indent}# {follows_parts[0]}.follows = "{name}";'
                )
            else:
                deep = ".".join(f"inputs.{part}" for part in follows_parts[1:])
                comment_lines.append(
                    f'{indent}# {follows_parts[0]}.{deep}.follows = "{name}";'
                )

        block = "\n".join(comment_lines)

        if style == "block" and block_end is not None:
            lines.insert(block_end, block)
        else:
            # Insert after last input line
            last_input = 0
            for i, line in enumerate(lines):
                if re.match(r"\s*(inputs\.)?\S+\.(url|follows)\s*=", line):
                    last_input = i
            lines.insert(last_input + 1, block)

    return "\n".join(lines)


def flatten(
    flake_dir: str,
    config: dict[str, Any],
    dry_run: bool = False,
    check: bool = False,
    verbose: bool = False,
) -> int:
    """Main flatten action. Returns number of proposals found."""
    lock = load_lock(flake_dir)
    content = read_flake_nix(flake_dir)
    proposals = analyze_flatten(lock, content, config, verbose=verbose)

    if not proposals:
        if verbose:
            print("  flatten: no inputs to hoist.")
        return 0

    print(f"  flatten: {len(proposals)} inputs to hoist:")
    for p in proposals:
        print(f"    + {p['new_input_name']} ({p['url']})")
        for fp in p["follows"]:
            print(f"      {'.'.join(fp)} -> follows {p['new_input_name']}")

    if dry_run or check:
        return len(proposals)

    applied, failed = apply_flatten(flake_dir, proposals, verbose=verbose)

    if failed:
        print(f"\n  {len(failed)} flatten proposals could not be applied:")
        for p in failed:
            print(f"    {p['desc']}")
        # Leave commented-out diagnostics in flake.nix
        content = read_flake_nix(flake_dir)
        content = insert_flatten_failure_comments(content, failed)
        write_flake_nix(flake_dir, content)

    if applied > 0 or failed:
        run_nixfmt(flake_dir)

    if applied > 0:
        print(f"  flatten done: hoisted {applied} input(s).")
    if failed:
        print(f"  {len(failed)} failure(s) left as comments in flake.nix.")

    return len(proposals)


# ---------------------------------------------------------------------------
# Main orchestrator
# ---------------------------------------------------------------------------


def check_url_follows_conflicts(content: str) -> list[str]:
    """Detect root inputs that have both a URL and a top-level follows.

    Nix rejects ``{ url = ...; follows = ...; }`` so we warn early rather than
    let every ``nix flake lock`` call fail cryptically.
    """
    conflicts: list[str] = []
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)

    if style == "block" and block_start is not None and block_end is not None:
        # Walk through input blocks. Only a block's OWN attributes count:
        # a url/follows inside a nested ``inputs = { ... }`` override (or
        # behind a dotted ``inputs.X.`` prefix) belongs to that nested
        # input, not to the root input (#642 — the same nested-override
        # blindness #634/#640 fixed in the other scanners). So each line
        # is judged only on its own-level text (``_own_level_text``:
        # structural text at the block's direct-attribute depth, with
        # string contents and comments excluded — #646), and an attribute
        # only counts when it stands alone at the start of the judged
        # text or right after a ``;``.
        current_block: str | None = None
        has_url = False
        has_follows = False
        depth = 0

        def _judge(segment: str) -> None:
            nonlocal has_url, has_follows
            if re.search(r"(?:^|;)\s*url\s*=", segment):
                has_url = True
            if re.search(r"(?:^|;)\s*follows\s*=", segment):
                has_follows = True

        def _finish_block() -> None:
            nonlocal current_block, has_url, has_follows, depth
            if has_url and has_follows and current_block is not None:
                conflicts.append(current_block)
            current_block = None
            has_url = False
            has_follows = False
            depth = 0

        for i in range(block_start + 1, block_end):
            stripped = lines[i].strip()
            if stripped.startswith("#"):
                continue
            # Detect block start: ``name = {``
            m = re.match(rf"{_SEG_TOKEN}\s*=\s*\{{", stripped)
            if m and depth == 0:
                current_block = _unquote_name(m.group(1))
                has_url = False
                has_follows = False
                # Attributes on the opening line itself count, at the
                # block's own level.
                segment, depth = _own_level_text(stripped, 0)
                _judge(segment)
                if depth <= 0:
                    # One-line block: finalise it here, or its conflict
                    # is never reported (#642).
                    _finish_block()
                continue
            if current_block:
                # Judge whatever part of the line sits at the block's own
                # level — including text after a nested block that closes
                # on this same line (#646).
                segment, depth = _own_level_text(stripped, depth)
                _judge(segment)
                if depth <= 0:
                    _finish_block()

    # Also check dotted-style: inputs.name.url and inputs.name.follows
    # Only match direct follows (name.follows = "x"), NOT deep follows
    # (name.inputs.sub.follows = "x").
    # Only consider lines outside of block-style sub-blocks (depth 0).
    url_names: set[str] = set()
    follows_names: set[str] = set()
    depth = 0
    in_inputs = style == "block" and block_start is not None
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if in_inputs and block_start is not None:
            if i <= block_start:
                continue
            if block_end is not None and i >= block_end:
                break
            # Track brace depth to skip sub-block interiors
            depth += _brace_delta(stripped)
            if depth > 0:
                continue
            depth = max(depth, 0)
        m = re.match(rf"(?:inputs\.)?{_SEG_TOKEN}\.url\s*=", stripped)
        if m:
            url_names.add(_unquote_name(m.group(1)))
        m = re.match(rf"(?:inputs\.)?{_SEG_TOKEN}\.follows\s*=", stripped)
        if m:
            follows_names.add(_unquote_name(m.group(1)))

    for name in url_names & follows_names:
        if name not in conflicts:
            conflicts.append(name)

    return conflicts


def run_all(
    flake_dir: str,
    config: dict[str, Any],
    dry_run: bool = False,
    check: bool = False,
    verbose: bool = False,
) -> int:
    """Run all operations: merge -> dedup -> flatten -> dedup.

    Returns total number of proposals found.
    """
    # Pre-flight: fix or warn about inputs with both url and follows.
    # When the lock hash of the input matches the follows target, the url
    # is redundant so we remove it automatically.
    content = read_flake_nix(flake_dir)
    conflicts = check_url_follows_conflicts(content)
    if conflicts:
        lock = load_lock(flake_dir)
        nodes = lock.get("nodes", {})
        root_inputs = nodes.get("root", {}).get("inputs", {})
        fixed: list[str] = []
        unfixed: list[str] = []
        for name in conflicts:
            # Resolve both the conflicting input and its follows target
            node_name = root_inputs.get(name, "")
            if not isinstance(node_name, str) or node_name not in nodes:
                unfixed.append(name)
                continue
            inp_hash = locked_hash(nodes[node_name])
            # Find the follows target from content
            target_name = _follows_target_for_input(content, name)
            if not target_name:
                unfixed.append(name)
                continue
            target_node = root_inputs.get(target_name, "")
            if not isinstance(target_node, str) or target_node not in nodes:
                unfixed.append(name)
                continue
            target_hash = locked_hash(nodes[target_node])
            if inp_hash and inp_hash == target_hash:
                content, removed = remove_input_url_line(content, name)
                if removed:
                    fixed.append(name)
                    print(
                        f"  fix: removed url from {name}"
                        f" (hash-aligned with follows target {target_name})"
                    )
                else:
                    unfixed.append(name)
            else:
                unfixed.append(name)
        if fixed and not dry_run and not check:
            write_flake_nix(flake_dir, content)
            run_nixfmt(flake_dir)
        if unfixed:
            print(
                "  warning: these inputs have both url and follows"
                " (hashes do not align, fix manually):"
            )
            for name in unfixed:
                print(f"    {name}")
            print()

    total = 0

    print("=== merge ===")
    total += merge(flake_dir, config, dry_run=dry_run, check=check, verbose=verbose)

    print("\n=== dedup (pass 1) ===")
    total += dedup(flake_dir, config, dry_run=dry_run, check=check, verbose=verbose)

    print("\n=== flatten ===")
    flatten_count = flatten(
        flake_dir, config, dry_run=dry_run, check=check, verbose=verbose
    )
    total += flatten_count

    if flatten_count > 0 and not dry_run and not check:
        print("\n=== dedup (pass 2, post-flatten) ===")
        total += dedup(flake_dir, config, dry_run=dry_run, check=check, verbose=verbose)

    return total


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    """Build the CLI argument parser."""
    parser = argparse.ArgumentParser(
        prog="flake-tidy",
        description="Deduplicate and flatten flake inputs by adding follows declarations.",
    )
    parser.add_argument(
        "action",
        nargs="?",
        default="all",
        choices=["dedup", "merge", "flatten", "all"],
        help="Action to perform (default: all)",
    )
    parser.add_argument(
        "--flake-dir",
        default=".",
        help="Path to the flake directory (default: current directory)",
    )
    parser.add_argument(
        "--config",
        dest="config_file",
        default=None,
        metavar="FILE",
        help="Load flakeTidy config from this JSON file instead of `nix eval` "
        "(also via $FLAKE_TIDY_CONFIG). Required where a nix subprocess "
        "cannot work, e.g. inside a nix build sandbox.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would change without modifying files",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Exit 1 if changes are needed (for CI)",
    )
    parser.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Show detailed information about skipped items",
    )
    parser.add_argument(
        "--max-depth",
        type=int,
        default=None,
        help="Maximum depth to traverse (default: 6, 0=unlimited)",
    )

    # Includes (replace config values)
    parser.add_argument(
        "--include",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Only process these inputs (replaces config include.input)",
    )
    parser.add_argument(
        "--include-dedup",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Only dedup these inputs (replaces config include.dedup)",
    )
    parser.add_argument(
        "--include-merge",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Only merge these inputs (replaces config include.merge)",
    )
    parser.add_argument(
        "--include-flatten",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Only flatten these inputs (replaces config include.flatten)",
    )

    # Excludes (append to config values)
    parser.add_argument(
        "--exclude-input",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Exclude these inputs from all operations (appends to config)",
    )
    parser.add_argument(
        "--exclude-dedup",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Exclude these inputs from dedup (appends to config)",
    )
    parser.add_argument(
        "--exclude-merge",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Exclude these inputs from merge (appends to config)",
    )
    parser.add_argument(
        "--exclude-flatten",
        nargs="+",
        default=None,
        metavar="NAME",
        help="Exclude these inputs from flatten (appends to config)",
    )

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()

    flake_dir = os.path.abspath(args.flake_dir)

    if not os.path.isfile(os.path.join(flake_dir, "flake.lock")):
        print(f"error: no flake.lock in {flake_dir}", file=sys.stderr)
        sys.exit(1)
    if not os.path.isfile(os.path.join(flake_dir, "flake.nix")):
        print(f"error: no flake.nix in {flake_dir}", file=sys.stderr)
        sys.exit(1)

    verbose = args.verbose
    dry_run = args.dry_run
    check = args.check

    # Step 0: Format first (unless check mode)
    if not check:
        print("formatting flake.nix...")
        run_nixfmt(flake_dir)

    # Step 1: Load and merge config. Check mode is strict: a config that
    # cannot be loaded from any source is a loud error, never a silent
    # fall back to defaults (#476).
    try:
        config = load_config(
            flake_dir,
            verbose=verbose,
            config_file=args.config_file,
            strict=check,
        )
    except ConfigLoadError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(2)
    config = merge_cli_into_config(config, args)

    if verbose:
        print(f"config: {json.dumps(config, indent=2)}")

    # Show active config summary
    has_excludes = any(
        config["exclude"][k] for k in config["exclude"] if config["exclude"][k]
    )
    has_includes = any(config["include"][k] != ["*"] for k in config["include"])
    if has_excludes:
        print("exclusions:")
        for k, v in config["exclude"].items():
            if v:
                print(f"  {k}: {v}")
    if has_includes:
        print("includes:")
        for k, v in config["include"].items():
            if v != ["*"]:
                print(f"  {k}: {v}")
    print(f"max-depth: {config['max-depth']}")

    # Step 2: Run the requested action
    total_proposals = 0

    if args.action == "dedup":
        total_proposals = dedup(
            flake_dir, config, dry_run=dry_run, check=check, verbose=verbose
        )
    elif args.action == "merge":
        total_proposals = merge(
            flake_dir, config, dry_run=dry_run, check=check, verbose=verbose
        )
    elif args.action == "flatten":
        total_proposals = flatten(
            flake_dir, config, dry_run=dry_run, check=check, verbose=verbose
        )
    elif args.action == "all":
        total_proposals = run_all(
            flake_dir, config, dry_run=dry_run, check=check, verbose=verbose
        )

    # Step 3: Final format (if we made changes)
    if not dry_run and not check and total_proposals > 0:
        run_nixfmt(flake_dir)

    # Step 4: Report and exit
    if check:
        if total_proposals > 0:
            print(f"\ncheck failed: {total_proposals} change(s) needed.")
            sys.exit(1)
        else:
            print("\ncheck passed: no changes needed.")
            sys.exit(0)

    if dry_run:
        if total_proposals > 0:
            print(f"\ndry run: {total_proposals} change(s) would be made.")
        else:
            print("\ndry run: no changes needed.")


if __name__ == "__main__":
    main()
