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
  1. nix eval .#flakeTidy --json
  2. nix eval --impure --expr '(import ./flake.nix).flakeTidy or {}' --json
  3. Defaults

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
import json
import os
import re
import subprocess
import sys
from collections import defaultdict
from itertools import groupby
from typing import Any


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


def load_config(flake_dir: str, verbose: bool = False) -> dict[str, Any]:
    """Load flakeTidy config from flake outputs with fallback chain."""
    cfg: dict[str, Any] | None = None

    # Try 1: nix eval .#flakeTidy --json
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
    except Exception:
        pass

    # Try 2: nix eval --impure
    if cfg is None:
        try:
            r = subprocess.run(
                [
                    "nix",
                    "eval",
                    "--impure",
                    "--expr",
                    f"let f = import {flake_dir}/flake.nix; in f.flakeTidy or {{}}",
                    "--json",
                ],
                capture_output=True,
                text=True,
                cwd=flake_dir,
                timeout=30,
            )
            if r.returncode == 0 and r.stdout.strip():
                loaded = json.loads(r.stdout)
                if loaded:
                    cfg = loaded
                    if verbose:
                        print("  config: loaded from import ./flake.nix")
        except Exception:
            pass

    if cfg is None:
        cfg = {}
        if verbose:
            print("  config: using defaults")

    # Validate
    errors, warnings = validate_config(cfg)
    for w in warnings:
        print(f"  config warning: {w}", file=sys.stderr)
    for e in errors:
        print(f"  config error: {e}", file=sys.stderr)
    if errors:
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


def source_key(node_data: dict) -> str:
    """Compute a grouping key from a node's 'original' field.

    Nodes with the same source key are candidates for dedup.
    """
    orig = node_data.get("original", {})
    t = orig.get("type", "")

    if t in ("github", "gitlab", "sourcehut"):
        owner = orig.get("owner", "").lower()
        repo = orig.get("repo", "").lower()
        if "rev" in orig and "ref" not in orig:
            return f"{t}/{owner}/{repo}/rev={orig['rev']}"
        ref = orig.get("ref", "")
        return f"{t}/{owner}/{repo}/{ref}"
    elif t == "tarball":
        return f"tarball/{orig.get('url', '')}"
    elif t == "file":
        return f"file/{orig.get('url', '')}"
    elif t == "path":
        return f"path/{orig.get('path', '')}"
    elif t == "indirect":
        return f"indirect/{orig.get('id', '')}"
    else:
        return json.dumps(orig, sort_keys=True)


def is_path_input(node_data: dict) -> bool:
    """Check if a node is a local path input (can't be hoisted/followed)."""
    return node_data.get("original", {}).get("type") == "path"


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
        return s
    elif t in ("tarball", "file"):
        return orig.get("url", "")
    elif t == "indirect":
        return f"indirect:{orig.get('id', '')}"
    return json.dumps(orig, sort_keys=True)


def node_original_url(node_data: dict) -> str:
    """Build a nix-resolvable URL string from a node's original field."""
    orig = node_data.get("original", {})
    t = orig.get("type", "")
    if t in ("github", "gitlab"):
        owner = orig.get("owner", "")
        repo = orig.get("repo", "")
        url = f"{t}:{owner}/{repo}"
        if "ref" in orig:
            url += f"/{orig['ref']}"
        return url
    elif t == "tarball":
        return orig.get("url", "")
    elif t == "file":
        raw = orig.get("url", "")
        # Prefix with file+ so nix doesn't default to tarball fetching
        if raw.startswith(("https://", "http://")):
            return f"file+{raw}"
        return raw
    elif t == "indirect":
        return f"indirect:{orig.get('id', '')}"
    return json.dumps(orig, sort_keys=True)


def locked_hash(node_data: dict) -> str:
    """Get the narHash or rev from the locked field for comparison."""
    locked = node_data.get("locked", {})
    return locked.get("narHash", locked.get("rev", ""))


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


def resolve_follows(lock: dict, follows_path: list[str]) -> str | None:
    """Resolve a follows path like ["determinate", "nix", "nixpkgs"] to a node name."""
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
            resolved = resolve_follows(lock, ref)
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


def get_input_file_order(content: str) -> list[str]:
    """Parse flake.nix to determine input declaration order.

    Returns a list of root input names in the order they appear in the file.
    """
    order: list[str] = []
    seen: set[str] = set()
    in_inputs_block = False
    brace_depth = 0
    lines = content.split("\n")

    for line in lines:
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        # Dotted style: inputs.NAME.xxx or inputs.NAME = {
        m = re.match(r"inputs\.([a-zA-Z_][a-zA-Z0-9_'-]*)\s*[.=]", stripped)
        if m:
            name = m.group(1)
            if name not in seen:
                order.append(name)
                seen.add(name)
            continue

        # Detect start of inputs = { block
        if re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_block = True
            brace_depth = stripped.count("{") - stripped.count("}")
            continue

        if in_inputs_block:
            brace_depth += stripped.count("{") - stripped.count("}")
            if brace_depth <= 0:
                in_inputs_block = False
                continue

            m = re.match(r"([a-zA-Z_][a-zA-Z0-9_'-]*)\s*[.=]", stripped)
            if m:
                name = m.group(1)
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


def follows_exists_in_content(
    content: str, follows_parts: list[str], target: str
) -> bool:
    """Check if a follows declaration already exists (active) in flake.nix.

    Checks both absolute and relative forms.
    """
    full = ".".join(f"inputs.{p}" for p in follows_parts) + f'.follows = "{target}"'
    relative_parent = (
        ".".join(f"inputs.{p}" for p in follows_parts[1:]) + f'.follows = "{target}"'
        if len(follows_parts) >= 2
        else None
    )
    relative_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
        + f'.follows = "{target}"'
        if len(follows_parts) >= 2
        else f'{follows_parts[0]}.follows = "{target}"'
    )
    bare_follows = f'follows = "{target}"' if len(follows_parts) == 1 else None

    candidates = [
        c for c in [full, relative_parent, relative_inputs, bare_follows] if c
    ]

    for line in content.split("\n"):
        stripped = line.strip().rstrip(";").strip()
        if stripped.startswith("#"):
            continue
        normalized = re.sub(r"\s+", " ", stripped)
        for c in candidates:
            c_normalized = re.sub(r"\s+", " ", c)
            if normalized == c_normalized:
                return True
    return False


def root_input_exists_in_content(content: str, input_name: str) -> bool:
    """Check if a root input declaration already exists in flake.nix.

    Only matches direct root input declarations, not references to the name
    inside another input's block (e.g. ``inputs.parent.inputs.CHILD...``
    inside a parent block is NOT a root declaration of CHILD).
    """
    esc = re.escape(input_name)
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
            depth += stripped.count("{") - stripped.count("}")
            if depth <= 0 and i > block_start:
                break
    else:
        # Dotted style: look for top-level inputs.NAME declarations
        for line in lines:
            stripped = line.strip()
            if stripped.startswith("#"):
                continue
            if re.match(rf"inputs\.{esc}\s*=", stripped):
                return True
            if re.match(rf"inputs\.{esc}\.(url|follows|flake)\s*=", stripped):
                return True
            # inputs.NAME.inputs.X.follows is a shallow override on NAME
            if re.match(
                rf"inputs\.{esc}\.inputs\.[a-zA-Z0-9_'-]+\.(follows|url)\s*=",
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

    full_path = ".".join(f"inputs.{p}" for p in follows_parts)
    patterns.append(rf'#\s*{re.escape(full_path)}\.follows\s*=\s*"{re.escape(target)}"')

    if len(follows_parts) >= 2:
        relative = ".".join(f"inputs.{p}" for p in follows_parts[1:])
        patterns.append(
            rf'#\s*{re.escape(relative)}\.follows\s*=\s*"{re.escape(target)}"'
        )

    if target_part:
        patterns.append(
            rf'#\s*inputs\.{re.escape(target_part)}\.follows\s*=\s*"{re.escape(target)}"'
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

        if re.match(rf"inputs\.{re.escape(input_name)}\s*=\s*\{{", stripped):
            block_start = i
            break

        if re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_block = True
            inputs_brace_depth = stripped.count("{") - stripped.count("}")
            continue

        if in_inputs_block:
            inputs_brace_depth += stripped.count("{") - stripped.count("}")
            if inputs_brace_depth <= 0:
                in_inputs_block = False
                continue
            if re.match(rf"{re.escape(input_name)}\s*=\s*\{{", stripped):
                block_start = i
                break

    if block_start is None:
        return None

    depth = 0
    for i in range(block_start, len(lines)):
        depth += lines[i].count("{") - lines[i].count("}")
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
            depth = stripped.count("{") - stripped.count("}")
            for j in range(i + 1, len(lines)):
                depth += lines[j].count("{") - lines[j].count("}")
                if depth <= 0:
                    return ("block", i, j)
            return ("block", i, len(lines) - 1)
        if re.match(r"inputs\.[a-zA-Z_]", stripped):
            return ("dotted", None, None)
    return ("dotted", None, None)


def find_input_dotted_line(content: str, input_name: str) -> tuple[int | None, bool]:
    """Find the last dotted-style line for an input.

    Returns (line_number, is_inside_block) or (None, False).
    """
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)

    last = None
    inside_block = False

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if re.match(rf"inputs\.{re.escape(input_name)}\.", stripped):
            last = i
            inside_block = False
        if style == "block" and block_start is not None and block_end is not None:
            if block_start < i < block_end:
                if re.match(rf"{re.escape(input_name)}\.", stripped):
                    last = i
                    inside_block = True

    return (last, inside_block) if last is not None else (None, False)


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
                nix_line = f'{parent_input}.follows = "{target}";'
            else:
                nix_line = f'inputs.{parent_input}.follows = "{target}";'
            lines.insert(last_line + 1, f"{indent}{nix_line}")
            return "\n".join(lines)

        if in_block:
            nix_line = f'    {parent_input}.follows = "{target}";'
        else:
            nix_line = f'    inputs.{parent_input}.follows = "{target}";'
        return _insert_before_inputs_end(lines, nix_line, in_block)

    # Multi-level follows
    full_nix = (
        ".".join(f"inputs.{p}" for p in follows_parts) + f'.follows = "{target}";'
    )
    relative_to_parent = (
        ".".join(f"inputs.{p}" for p in follows_parts[1:]) + f'.follows = "{target}";'
    )
    relative_to_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
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


def insert_root_input(content: str, input_name: str, url: str) -> str:
    """Add a new root input declaration to flake.nix.

    Uses dotted style inside block, or top-level dotted style.
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
        new_line = f'{indent}{input_name}.url = "{url}";'
        lines.insert(block_end, new_line)
    else:
        last_input_line = 0
        for i, line in enumerate(lines):
            if re.match(r"\s*inputs\.", line.strip()):
                last_input_line = i
        indent = "    "
        if last_input_line > 0:
            m = re.match(r"^(\s*)", lines[last_input_line])
            if m:
                indent = m.group(1)
        new_line = f'{indent}inputs.{input_name}.url = "{url}";'
        lines.insert(last_input_line + 1, new_line)

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
            if re.match(r"inputs\s*=\s*\{", stripped):
                in_inputs = True
                depth = stripped.count("{") - stripped.count("}")
                continue
            if in_inputs:
                depth += stripped.count("{") - stripped.count("}")
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
    for line in stderr.split("\n"):
        m = re.search(r"while updating the flake input '([^']+)'", line)
        if m:
            input_path = m.group(1)
        m = re.search(r"cannot find flake 'flake:([^']+)'", line)
        if m:
            flake_name = m.group(1)
    return input_path, flake_name


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
    if url:
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
            locked = node_data.get("locked", {})
            lt = locked.get("type", "")
            if lt in ("github", "gitlab"):
                url = f"{lt}:{locked.get('owner', '')}/{locked.get('repo', '')}"
                return {input_path: url}
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
    def walk(node_name: str, prefix: str) -> None:
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
                    locked = child.get("locked", {})
                    lt = locked.get("type", "")
                    if lt in ("github", "gitlab"):
                        url = f"{lt}:{locked.get('owner', '')}/{locked.get('repo', '')}"
                        overrides[path] = url
                    else:
                        url = node_original_url(child)
                        if url and not url.startswith("indirect:"):
                            overrides[path] = url
                walk(ref, path)

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

        # Detect top-level inputs section
        if re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_section = True
            brace_depth = 1
            continue

        if in_inputs_section:
            # Count braces
            opens = stripped.count("{")
            closes = stripped.count("}")

            # Detect named input block: <name> = {
            if current_block is None and brace_depth == 1:
                m = re.match(r"([a-zA-Z0-9_-]+)\s*=\s*\{", stripped)
                if m:
                    current_block = m.group(1)
                    brace_depth += opens  # count the { we just matched
                    brace_depth -= closes
                    # Check for deep follows inside this block on the same line
                    # (unlikely but handle it)
                    continue

            # Inside a named input block
            if current_block is not None:
                # Look for: inputs.X.inputs.Y.follows (or .url, etc.)
                m_deep = re.match(
                    r"inputs\.([a-zA-Z0-9_-]+)(?:\.inputs\.([a-zA-Z0-9_-]+))+",
                    stripped,
                )
                if m_deep:
                    parts = re.findall(r"inputs\.([a-zA-Z0-9_-]+)", stripped)
                    # Full path is current_block / parts[0] / parts[1] / ...
                    # The transitive inputs that need resolving are the
                    # intermediate ones: current_block/parts[0],
                    # current_block/parts[0]/parts[1], etc.
                    for i in range(1, len(parts) + 1):
                        path = current_block + "/" + "/".join(parts[:i])
                        deep_paths.add(path)

                # Also catch: inputs.X.follows (X is transitive of current_block)
                m_shallow = re.match(
                    r"inputs\.([a-zA-Z0-9_-]+)\.(follows|url)\b", stripped
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
            r"inputs\.([a-zA-Z0-9_-]+)(?:\.inputs\.([a-zA-Z0-9_-]+))+",
            stripped,
        )
        if m and not in_inputs_section:
            parts = re.findall(r"inputs\.([a-zA-Z0-9_-]+)", stripped)
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
            canonical_node = sorted_roots[0]
            canonical_name = root_to_input.get(canonical_node, "")
            canonical_hash = locked_hash(nodes[canonical_node])

            for other in sorted_roots[1:]:
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
                if other_hash != canonical_hash:
                    if verbose:
                        print(
                            f"  skip {other_name}: hash differs from {canonical_name}"
                        )
                    continue

                follows_path = other_name
                if is_excluded_follows(config, follows_path):
                    continue

                proposals.append(
                    {
                        "follows_parts": [other_name],
                        "target": canonical_name,
                        "follows_path": follows_path,
                        "source_key": key,
                        "desc": f"{other_name} -> follows {canonical_name}",
                        "operation": "dedup",
                    }
                )

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

            canonical_node = sorted(root_members, key=_root_pos2)[0]
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

                proposals.append(
                    {
                        "follows_parts": follows_parts,
                        "target": canonical_name,
                        "follows_path": follows_path,
                        "source_key": key,
                        "desc": f"{follows_path} -> follows {canonical_name}",
                        "operation": "dedup",
                    }
                )

    # --- Cross-type hash dedup ---
    # source_key grouping above misses cases where the original type differs
    # (e.g. github vs indirect) but locked content is byte-identical.
    # Fall back to locked hash comparison for these stragglers, preferring
    # explicit types (github/gitlab) over indirect.
    _TYPE_PREF = {
        "github": 0,
        "gitlab": 1,
        "sourcehut": 2,
        "tarball": 3,
        "file": 4,
        "path": 5,
        "indirect": 6,
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
            # Back out this batch
            print(
                f"  warning: lock failed for depth-{depth} follows, backing out",
                file=sys.stderr,
            )
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
            print(f"    {p['desc']}")

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
    in_inputs_section = False
    current_block: str | None = None
    brace_depth = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        # Detect top-level inputs = { block
        if re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_section = True
            brace_depth = stripped.count("{") - stripped.count("}")
            continue

        if in_inputs_section:
            opens = stripped.count("{")
            closes = stripped.count("}")

            # Detect named input block at depth 1
            if current_block is None and brace_depth == 1:
                m = re.match(r"([a-zA-Z0-9_'-]+)\s*=\s*\{", stripped)
                if m:
                    current_block = m.group(1)

            # Inside a named block: look for inputs.CHILD.inputs.SUB.follows
            if current_block is not None:
                m = re.match(
                    r'inputs\.([a-zA-Z0-9_\'-]+)\.inputs\.([a-zA-Z0-9_\'-]+)\.follows\s*=\s*"([^"]+)"',
                    stripped,
                )
                if m:
                    results.append(
                        {
                            "parent": current_block,
                            "child": m.group(1),
                            "sub_input": m.group(2),
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
            r'inputs\.([a-zA-Z0-9_\'-]+)\.inputs\.([a-zA-Z0-9_\'-]+)\.inputs\.([a-zA-Z0-9_\'-]+)\.follows\s*=\s*"([^"]+)"',
            stripped,
        )
        if m and not in_inputs_section:
            results.append(
                {
                    "parent": m.group(1),
                    "child": m.group(2),
                    "sub_input": m.group(3),
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
    locked = child_node.get("locked", {})
    lt = locked.get("type", "")
    if lt in ("github", "gitlab"):
        return f"{lt}:{locked.get('owner', '')}/{locked.get('repo', '')}"

    return None


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
    block_lines = [f"{indent}{input_name} = {{"]
    block_lines.append(f'{indent}  url = "{url}";')
    for sub_input, target in sub_follows:
        block_lines.append(f'{indent}  inputs.{sub_input}.follows = "{target}";')
    block_lines.append(f"{indent}}};")

    if style == "block" and block_end is not None:
        for line_str in reversed(block_lines):
            lines.insert(block_end, line_str)
    else:
        # Dotted style: find last input line
        last_input_line = 0
        for k, line_str in enumerate(lines):
            if re.match(r"\s*inputs\.", line_str.strip()):
                last_input_line = k
        for j, line_str in enumerate(block_lines):
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

        # Get URL from the first node
        first_node_data = nodes[members[0][0]]
        url = node_original_url(first_node_data)

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

                follows_list.append(follows_parts)

        if not follows_list and not root_input_exists_in_content(content, best_name):
            # Still add the root input even without follows (makes it available)
            pass

        proposals.append(
            {
                "new_input_name": best_name,
                "url": url,
                "source_key": key,
                "follows": follows_list,
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
                content = insert_root_input(content, p["new_input_name"], p["url"])
            for follows_parts in p["follows"]:
                if not follows_exists_in_content(
                    content, follows_parts, p["new_input_name"]
                ):
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
            # Can't identify — all remaining fail
            if verbose:
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

    # Step 1: Load and merge config
    config = load_config(flake_dir, verbose=verbose)
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
