#!/usr/bin/env python3
"""flake-tidy: deduplicate and flatten flake inputs by adding follows declarations.

Actions:
  dedup    Add follows for duplicate inputs (transitive + same-URL root inputs)
  flatten  Hoist transitive-only inputs to root level, then add follows
  all      Run dedup then flatten then dedup again (default)

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
      "flatten": ["*"]
    },
    "exclude": {
      "input": [],
      "input-url": [],
      "follows": [],
      "follows-url": [],
      "dedup": [],
      "flatten": []
    }
  }
"""

from __future__ import annotations

import argparse
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
        "flatten": ["*"],  # flatten: process all
    },
    "exclude": {
        "input": [],  # global: excluded from everything
        "input-url": [],  # global: excluded by URL
        "follows": [],  # excluded follows paths (e.g. "home-manager.nixpkgs")
        "follows-url": [],  # excluded follows targets by URL
        "dedup": [],  # input names excluded from dedup only
        "flatten": [],  # input names excluded from flatten only
    },
}

CONFIG_SCHEMA: dict[str, Any] = {
    "max-depth": int,
    "include": {
        "input": list,
        "dedup": list,
        "flatten": list,
    },
    "exclude": {
        "input": list,
        "input-url": list,
        "follows": list,
        "follows-url": list,
        "dedup": list,
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
    if args.include_flatten:
        config["include"]["flatten"] = args.include_flatten

    # Excludes: CLI appends to config
    if args.exclude_input:
        config["exclude"]["input"].extend(args.exclude_input)
    if args.exclude_dedup:
        config["exclude"]["dedup"].extend(args.exclude_dedup)
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
    elif t in ("tarball", "file"):
        return orig.get("url", "")
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
    """Check if a root input declaration already exists in flake.nix."""
    for line in content.split("\n"):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        # inputs.NAME = { or inputs.NAME.url = or NAME = { inside inputs block
        if re.match(rf"inputs\.{re.escape(input_name)}\s*[.=]", stripped):
            return True
    # Also check inside inputs = { } block
    style, block_start, block_end = detect_inputs_style(content)
    if style == "block" and block_start is not None and block_end is not None:
        lines = content.split("\n")
        for i in range(block_start + 1, block_end):
            stripped = lines[i].strip()
            if stripped.startswith("#"):
                continue
            if re.match(rf"{re.escape(input_name)}\s*[.=]", stripped):
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
        ok, stderr = run_nix_flake_lock(flake_dir)
        if ok:
            applied += len(batch)
        else:
            # Try fallback with --override-input
            input_path, flake_name = extract_failed_input(stderr)
            if input_path:
                print(f"  retrying with --override-input {input_path}...")
                saved_lock_data = json.loads(saved_lock)
                overrides = build_override_inputs(saved_lock_data, input_path)
                if overrides:
                    ok2, _ = run_nix_flake_lock(flake_dir, overrides)
                    if ok2:
                        applied += len(batch)
                        continue

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
    root_node_names = set(root_inputs.values())
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
        key = source_key(node_data)
        has_root_equivalent = False
        for _iname, rnode in root_inputs.items():
            rdata = nodes.get(rnode, {})
            if "original" in rdata and source_key(rdata) == key:
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
            best_name = f"{best_name}-hoisted"

        # Get URL from the first node
        first_node_data = nodes[members[0][0]]
        url = node_original_url(first_node_data)

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


def apply_flatten(
    flake_dir: str,
    proposals: list[dict[str, Any]],
    verbose: bool = False,
) -> tuple[int, list[dict[str, Any]]]:
    """Apply flatten proposals. Returns (applied_count, failed_proposals)."""
    if not proposals:
        return 0, []

    content = read_flake_nix(flake_dir)
    lock_path = os.path.join(flake_dir, "flake.lock")

    saved_content = content
    with open(lock_path) as f:
        saved_lock = f.read()

    applied = 0
    failed: list[dict[str, Any]] = []

    for p in proposals:
        # Add new root input
        if not root_input_exists_in_content(content, p["new_input_name"]):
            if verbose:
                print(f'  add root input: {p["new_input_name"]}.url = "{p["url"]}"')
            content = insert_root_input(content, p["new_input_name"], p["url"])

        # Add follows
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

    print("  locking after flatten...")
    ok, stderr = run_nix_flake_lock(flake_dir)
    if ok:
        applied = len(proposals)
    else:
        # Try with overrides
        input_path, flake_name = extract_failed_input(stderr)
        if input_path:
            print(f"  retrying with --override-input {input_path}...")
            saved_lock_data = json.loads(saved_lock)
            overrides = build_override_inputs(saved_lock_data, input_path)
            if overrides:
                ok2, _ = run_nix_flake_lock(flake_dir, overrides)
                if ok2:
                    return len(proposals), []

        # Back out all flatten changes
        print("  warning: lock failed after flatten, backing out", file=sys.stderr)
        content = saved_content
        write_flake_nix(flake_dir, content)
        run_nixfmt(flake_dir)
        with open(lock_path, "w") as f:
            f.write(saved_lock)
        failed = proposals

    return applied, failed


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

    if applied > 0:
        print(f"  flatten done: hoisted {applied} input(s).")
        run_nixfmt(flake_dir)

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
    """Run all operations: dedup -> flatten -> dedup.

    Returns total number of proposals found.
    """
    total = 0

    print("=== dedup (pass 1) ===")
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
        choices=["dedup", "flatten", "all"],
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
