#!/usr/bin/env python3
"""flake-tidy: deduplicate flake inputs by adding follows declarations."""

import argparse
import json
import os
import re
import subprocess
import sys
from collections import defaultdict


# ---------------------------------------------------------------------------
# Config loading (fallback chain: nix eval .#flakeTidy -> import -> defaults)
# ---------------------------------------------------------------------------

DEFAULT_CONFIG = {
    "exclude": {
        "input": [],
        "input-url": [],
        "follows": [],
        "follows-url": [],
    },
    "max-depth": 0,  # 0 = unlimited
}


def load_config(flake_dir):
    """Load flakeTidy config from flake outputs with fallback chain."""
    cfg = None

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
    except Exception:
        pass

    # Try 2: nix eval --impure --expr '(import ./flake.nix).flakeTidy or {}' --json
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
        except Exception:
            pass

    # Try 3: regex parse let flakeTidy = { ... }; from flake.nix
    if cfg is None:
        try:
            with open(os.path.join(flake_dir, "flake.nix")) as f:
                content = f.read()
            # Look for flakeTidy = { in a let binding — very rough
            m = re.search(
                r"flakeTidy\s*=\s*\{([^}]*(?:\{[^}]*\}[^}]*)*)\};",
                content,
                re.DOTALL,
            )
            if m:
                # This is too fragile for nested Nix; skip and use defaults
                pass
        except Exception:
            pass

    if cfg is None:
        cfg = {}

    # Merge with defaults
    result = dict(DEFAULT_CONFIG)
    result["exclude"] = dict(DEFAULT_CONFIG["exclude"])
    if "exclude" in cfg and isinstance(cfg["exclude"], dict):
        for k in DEFAULT_CONFIG["exclude"]:
            if k in cfg["exclude"] and isinstance(cfg["exclude"][k], list):
                result["exclude"][k] = cfg["exclude"][k]
    if "max-depth" in cfg and isinstance(cfg["max-depth"], int):
        result["max-depth"] = cfg["max-depth"]

    return result


# ---------------------------------------------------------------------------
# Lock file analysis
# ---------------------------------------------------------------------------


def load_lock(flake_dir):
    with open(os.path.join(flake_dir, "flake.lock")) as f:
        return json.load(f)


def source_key(node_data):
    """Compute a grouping key from a node's 'original' field.

    Nodes with the same source key are candidates for dedup.
    """
    orig = node_data.get("original", {})
    t = orig.get("type", "")

    if t in ("github", "gitlab", "sourcehut"):
        owner = orig.get("owner", "").lower()
        repo = orig.get("repo", "").lower()
        # If pinned to a specific rev, that's its own unique thing
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
        # Fallback: JSON of original
        return json.dumps(orig, sort_keys=True)


def is_path_input(node_data):
    """Check if a node is a local path input (can't be hoisted/followed)."""
    orig = node_data.get("original", {})
    return orig.get("type") == "path"


def node_url(node_data):
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


def locked_hash(node_data):
    """Get the narHash or rev from the locked field for comparison."""
    locked = node_data.get("locked", {})
    return locked.get("narHash", locked.get("rev", ""))


# ---------------------------------------------------------------------------
# Graph traversal
# ---------------------------------------------------------------------------


def find_all_paths(lock, target_node):
    """Find all paths from root to target_node through the lock graph.

    Returns list of paths, where each path is a list of
    (parent_input_name, child_node_name, is_follows) tuples.
    Only follows direct edges (strings), not follows edges (arrays).
    """
    nodes = lock["nodes"]
    results = []

    def dfs(current_node, path, visited):
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
                # Direct reference
                path.append((input_name, ref, False))
                dfs(ref, path, visited)
                path.pop()
            elif isinstance(ref, list):
                # Follows reference — resolve to find target node
                resolved = resolve_follows(lock, ref)
                if resolved is not None:
                    path.append((input_name, resolved, True))
                    dfs(resolved, path, visited)
                    path.pop()

        visited.discard(current_node)

    dfs("root", [], set())
    return results


def resolve_follows(lock, follows_path):
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


def path_to_follows_decl(path):
    """Convert a path to a flake.nix follows declaration path.

    path: list of (input_name, child_node, is_follows) tuples
    Returns: dotted input path like "home-manager.nixpkgs"
    """
    parts = []
    for input_name, _child, _is_follows in path:
        parts.append(input_name)
    return ".".join(parts)


def path_to_nix_line(path, target):
    """Generate the nix follows line for a path.

    Returns: 'inputs.home-manager.inputs.nixpkgs.follows = "nixpkgs";'
    """
    parts = []
    for input_name, _child, _is_follows in path:
        parts.append(f"inputs.{input_name}")
    return f'{".".join(parts)}.follows = "{target}";'


def path_depth(path):
    """Return the depth of a follows path (number of input levels)."""
    return len(path)


# ---------------------------------------------------------------------------
# flake.nix file operations
# ---------------------------------------------------------------------------


def read_flake_nix(flake_dir):
    path = os.path.join(flake_dir, "flake.nix")
    with open(path) as f:
        return f.read()


def write_flake_nix(flake_dir, content):
    path = os.path.join(flake_dir, "flake.nix")
    with open(path, "w") as f:
        f.write(content)


def get_input_file_order(content):
    """Parse flake.nix to determine input declaration order.

    Returns a list of root input names in the order they appear in the file.
    """
    order = []
    seen = set()
    # Match both block-style: inputs.foo = { and inputs = { ... foo = {
    # and dotted style: inputs.foo.url = "..."
    # Also handle: foo = { inside an inputs = { } block

    # Strategy: find the inputs block, then extract names in order
    # First try to find inputs = { ... } block
    in_inputs_block = False
    brace_depth = 0
    lines = content.split("\n")

    for line in lines:
        stripped = line.strip()
        # Skip comments
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

            # Inside inputs block: NAME = { or NAME.url = "..."
            m = re.match(
                r"([a-zA-Z_][a-zA-Z0-9_'-]*)\s*[.=]",
                stripped,
            )
            if m:
                name = m.group(1)
                # Skip common non-input keys
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


def follows_exists_in_content(content, follows_parts, target):
    """Check if a follows declaration already exists (active) in flake.nix.

    Checks both absolute and relative forms.
    """
    # Full form: inputs.A.inputs.B.follows = "target"
    full = ".".join(f"inputs.{p}" for p in follows_parts) + f'.follows = "{target}"'
    # Relative to parent block: inputs.B.follows = "target"
    relative_parent = (
        ".".join(f"inputs.{p}" for p in follows_parts[1:]) + f'.follows = "{target}"'
        if len(follows_parts) >= 2
        else None
    )
    # Relative to inputs block: A.inputs.B.follows = "target"
    relative_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
        + f'.follows = "{target}"'
        if len(follows_parts) >= 2
        else f'{follows_parts[0]}.follows = "{target}"'
    )
    # Root-level single: follows = "target" (inside a block for this input)
    bare_follows = f'follows = "{target}"' if len(follows_parts) == 1 else None

    candidates = [full, relative_parent, relative_inputs, bare_follows]
    candidates = [c for c in candidates if c]

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


def find_commented_follows(content, follows_parts, target):
    """Find a commented-out follows line matching our intent.

    follows_parts: list of input names forming the path, e.g. ["home-manager", "nixpkgs"]
    target: the follows target, e.g. "nixpkgs"

    Returns (line_number, original_line) if found, else None.
    """
    # Build patterns to match various comment styles
    # Inside a block: # inputs.nixpkgs.follows = "nixpkgs";
    # Or relative:    # nixpkgs.follows = "nixpkgs";
    # We're looking for the last part of the chain with .follows = "target"
    target_part = follows_parts[-1] if follows_parts else ""
    patterns = []

    # Full dotted path from root
    full_path = ".".join(f"inputs.{p}" for p in follows_parts)
    patterns.append(rf'#\s*{re.escape(full_path)}\.follows\s*=\s*"{re.escape(target)}"')

    # Relative path (inside a block for the parent input)
    if len(follows_parts) >= 2:
        # e.g., inside home-manager block: # inputs.nixpkgs.follows = "nixpkgs";
        relative = ".".join(f"inputs.{p}" for p in follows_parts[1:])
        patterns.append(
            rf'#\s*{re.escape(relative)}\.follows\s*=\s*"{re.escape(target)}"'
        )

    # Even shorter: just the child input name
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


def uncomment_line(content, line_number):
    """Uncomment a line by removing the leading # (preserving indentation)."""
    lines = content.split("\n")
    line = lines[line_number]
    # Remove the first # and optional space after it
    lines[line_number] = re.sub(r"^(\s*)#\s?", r"\1", line, count=1)
    return "\n".join(lines)


def find_input_block_end(content, input_name):
    """Find the line number of the closing brace for an input block.

    Handles:
      inputs.NAME = { ... };
      NAME = { ... };  (inside inputs = { } block)

    Returns (line_number_of_closing, indent) or None if not a block.
    """
    lines = content.split("\n")
    # Find the opening of this input's block
    block_start = None
    in_inputs_block = False
    inputs_brace_depth = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        # Check for inputs.NAME = {
        if re.match(rf"inputs\.{re.escape(input_name)}\s*=\s*\{{", stripped):
            block_start = i
            break

        # Check for inputs = { block
        if re.match(r"inputs\s*=\s*\{", stripped):
            in_inputs_block = True
            inputs_brace_depth = stripped.count("{") - stripped.count("}")
            continue

        if in_inputs_block:
            inputs_brace_depth += stripped.count("{") - stripped.count("}")
            if inputs_brace_depth <= 0:
                in_inputs_block = False
                continue

            # Inside inputs block: NAME = {
            if re.match(rf"{re.escape(input_name)}\s*=\s*\{{", stripped):
                block_start = i
                break

    if block_start is None:
        return None

    # Find matching closing brace
    depth = 0
    for i in range(block_start, len(lines)):
        depth += lines[i].count("{") - lines[i].count("}")
        if depth <= 0:
            # Determine indentation from lines inside the block
            indent = "    "
            for j in range(block_start + 1, i):
                m = re.match(r"^(\s+)", lines[j])
                if m:
                    indent = m.group(1)
                    break
            return (i, indent)

    return None


def detect_inputs_style(content):
    """Detect whether inputs use block style (inputs = { }) or dotted style.

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
        # Top-level dotted style
        if re.match(r"inputs\.[a-zA-Z_]", stripped):
            return ("dotted", None, None)
    return ("dotted", None, None)


def find_input_dotted_line(content, input_name):
    """Find the last dotted-style line for an input.

    Handles both styles:
      - Top level: inputs.foo.url = "..."
      - Inside inputs block: foo.url = "..."

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
        # Top-level dotted style
        if re.match(rf"inputs\.{re.escape(input_name)}\.", stripped):
            last = i
            inside_block = False
        # Inside inputs block style
        if style == "block" and block_start is not None:
            if block_start < i < block_end:
                if re.match(rf"{re.escape(input_name)}\.", stripped):
                    last = i
                    inside_block = True

    return (last, inside_block) if last is not None else (None, False)


def insert_follows_in_content(content, follows_parts, target):
    """Insert a follows declaration into flake.nix content.

    follows_parts: list like ["home-manager", "nixpkgs"]
    target: string like "nixpkgs"

    Handles both input declaration styles:
    - Block style: inputs = { foo = { ... }; bar.url = "..."; }
    - Dotted style: inputs.foo = { ... }; inputs.bar.url = "...";
    """
    parent_input = follows_parts[0]
    lines = content.split("\n")
    style, block_start, block_end = detect_inputs_style(content)
    in_block = style == "block"

    if len(follows_parts) == 1:
        # Root-level follows: make this input follow another entirely

        # Try block style: insert `follows = "target";` inside the brace block
        block_info = find_input_block_end(content, parent_input)
        if block_info is not None:
            line_no, indent = block_info
            lines.insert(line_no, f'{indent}follows = "{target}";')
            return "\n".join(lines)

        # Dotted style — add a line after the input's last line
        last_line, is_inside = find_input_dotted_line(content, parent_input)
        if last_line is not None:
            indent = re.match(r"^(\s*)", lines[last_line]).group(1)
            if is_inside:
                # Inside inputs block: use relative path
                nix_line = f'{parent_input}.follows = "{target}";'
            else:
                nix_line = f'inputs.{parent_input}.follows = "{target}";'
            lines.insert(last_line + 1, f"{indent}{nix_line}")
            return "\n".join(lines)

        # Fallback
        if in_block:
            nix_line = f'    {parent_input}.follows = "{target}";'
        else:
            nix_line = f'    inputs.{parent_input}.follows = "{target}";'
        return _insert_before_inputs_end(lines, nix_line, in_block)

    # Multi-level follows: e.g. follows_parts = ["mac-app-util", "treefmt-nix"]
    # Full nix line (for top-level dotted style):
    full_nix = (
        ".".join(f"inputs.{p}" for p in follows_parts) + f'.follows = "{target}";'
    )
    # Relative to parent (for inside parent's block):
    relative_to_parent = (
        ".".join(f"inputs.{p}" for p in follows_parts[1:]) + f'.follows = "{target}";'
    )
    # Relative to inputs block (for inside inputs = { } block, no parent block):
    relative_to_inputs = (
        follows_parts[0]
        + "."
        + ".".join(f"inputs.{p}" for p in follows_parts[1:])
        + f'.follows = "{target}";'
    )

    # Try to insert inside the parent input's block
    block_info = find_input_block_end(content, parent_input)
    if block_info is not None:
        line_no, indent = block_info
        lines.insert(line_no, f"{indent}{relative_to_parent}")
        return "\n".join(lines)

    # No brace block — add as a dotted line after the parent
    last_line, is_inside = find_input_dotted_line(content, parent_input)
    if last_line is not None:
        indent = re.match(r"^(\s*)", lines[last_line]).group(1)
        if is_inside:
            lines.insert(last_line + 1, f"{indent}{relative_to_inputs}")
        else:
            lines.insert(last_line + 1, f"{indent}{full_nix}")
        return "\n".join(lines)

    # Fallback: add before end of inputs section
    if in_block:
        return _insert_before_inputs_end(lines, f"    {relative_to_inputs}", True)
    else:
        return _insert_before_inputs_end(lines, f"    {full_nix}", False)


def _insert_before_inputs_end(lines, new_line, in_block=True):
    """Insert a line before the end of the inputs section."""
    if in_block:
        # Find the inputs = { } block's closing brace
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

    # Last resort: add before outputs
    for i, line in enumerate(lines):
        if re.match(r"\s*outputs\s*=", line.strip()):
            lines.insert(i, new_line)
            return "\n".join(lines)

    # Give up: append near top
    lines.insert(2, new_line)
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Exclusion checks
# ---------------------------------------------------------------------------


def is_excluded_input(config, input_name):
    return input_name in config["exclude"]["input"]


def is_excluded_input_url(config, url):
    return url in config["exclude"]["input-url"]


def is_excluded_follows(config, follows_path):
    """Check if a follows path like 'home-manager.nixpkgs' is excluded."""
    return follows_path in config["exclude"]["follows"]


def is_excluded_follows_url(config, url):
    return url in config["exclude"]["follows-url"]


def is_excluded(config, lock, follows_path, follows_parts, target, target_node):
    """Combined exclusion check."""
    nodes = lock["nodes"]

    # Check input exclusion (the input being followed)
    if is_excluded_input(config, follows_parts[0]):
        return True

    # Check target exclusion
    if is_excluded_input(config, target):
        return True

    # Check follows path exclusion
    if is_excluded_follows(config, follows_path):
        return True

    # Check input-url exclusion
    for part in follows_parts:
        # We can't easily get the URL for intermediate parts, check parent
        pass

    # Check follows-url exclusion on the target node
    target_data = nodes.get(target_node, {})
    target_url = node_url(target_data)
    if is_excluded_follows_url(config, target_url):
        return True

    # Check input-url on the source node being redirected
    # Find the node at the end of the follows path
    # The last element leads to the node we're redirecting
    return False


# ---------------------------------------------------------------------------
# Formatting
# ---------------------------------------------------------------------------


def run_nixfmt(flake_dir):
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


def run_nix_flake_lock(flake_dir):
    """Run nix flake lock to regenerate the lockfile."""
    r = subprocess.run(
        ["nix", "flake", "lock"],
        capture_output=True,
        text=True,
        cwd=flake_dir,
        timeout=120,
    )
    if r.returncode != 0:
        print(f"  error: nix flake lock failed:\n{r.stderr}", file=sys.stderr)
        return False
    return True


# ---------------------------------------------------------------------------
# Main dedup logic
# ---------------------------------------------------------------------------


def dedup(flake_dir, dry_run=False, verbose=False):
    """Main dedup action."""

    # Step 0: Format first
    print("formatting flake.nix...")
    run_nixfmt(flake_dir)

    # Step 1: Load config
    config = load_config(flake_dir)
    max_depth = config["max-depth"]

    if any(config["exclude"][k] for k in config["exclude"]):
        print("exclusions:")
        for k, v in config["exclude"].items():
            if v:
                print(f"  {k}: {v}")
    if max_depth:
        print(f"max-depth: {max_depth}")

    round_num = 0
    total_added = 0

    while True:
        round_num += 1
        print(f"\n--- round {round_num} ---")

        # Load lock and content fresh each round
        lock = load_lock(flake_dir)
        content = read_flake_nix(flake_dir)
        nodes = lock["nodes"]
        root_inputs = nodes.get("root", {}).get("inputs", {})

        # Get file order for root inputs
        file_order = get_input_file_order(content)

        # Build source key groups
        groups = defaultdict(list)
        for node_name, node_data in nodes.items():
            if node_name == "root":
                continue
            if "original" not in node_data:
                continue
            key = source_key(node_data)
            groups[key].append(node_name)

        proposals = []

        for key, members in groups.items():
            if len(members) < 2:
                continue

            # Skip path-based inputs entirely
            if any(is_path_input(nodes[m]) for m in members):
                continue

            # Separate root inputs from transitive
            root_members = []
            transitive_members = []
            for m in members:
                if m in root_inputs.values():
                    root_members.append(m)
                else:
                    transitive_members.append(m)

            # --- Root dedup: make later root inputs follow the first ---
            if len(root_members) >= 2:
                # Sort by file order
                root_to_input_name = {}
                for input_name, node_name in root_inputs.items():
                    if node_name in root_members:
                        root_to_input_name[node_name] = input_name

                def root_file_pos(node):
                    name = root_to_input_name.get(node, "")
                    if name in file_order:
                        return file_order.index(name)
                    return 9999

                sorted_roots = sorted(root_members, key=root_file_pos)
                canonical_node = sorted_roots[0]
                canonical_name = root_to_input_name.get(canonical_node, "")
                canonical_hash = locked_hash(nodes[canonical_node])

                for other in sorted_roots[1:]:
                    other_name = root_to_input_name.get(other, "")

                    if is_excluded_input(config, other_name):
                        continue
                    if is_excluded_input(config, canonical_name):
                        continue
                    if is_excluded_input_url(config, node_url(nodes[other])):
                        continue

                    # Only merge if same hash
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
                        }
                    )

            # --- Transitive dedup: make transitive deps follow root input ---
            canonical_node = None
            canonical_name = None

            if root_members:
                # Prefer the root input that's first in file order
                root_to_input_name = {}
                for input_name, node_name in root_inputs.items():
                    if node_name in root_members:
                        root_to_input_name[node_name] = input_name

                def root_file_pos2(node):
                    name = root_to_input_name.get(node, "")
                    if name in file_order:
                        return file_order.index(name)
                    return 9999

                canonical_node = sorted(root_members, key=root_file_pos2)[0]
                canonical_name = root_to_input_name.get(canonical_node, "")
            else:
                # No root input — pick shallowest transitive
                # For now skip (flatten mode would handle this)
                continue

            if is_excluded_input(config, canonical_name):
                continue
            if is_excluded_follows_url(config, node_url(nodes[canonical_node])):
                continue

            for trans_node in transitive_members:
                trans_url = node_url(nodes[trans_node])
                if is_excluded_input_url(config, trans_url):
                    continue

                # Find all paths from root to this transitive node
                all_paths = find_all_paths(lock, trans_node)

                for path in all_paths:
                    # Only generate follows for paths ending with a direct reference
                    if not path:
                        continue
                    last_input_name, last_child, last_is_follows = path[-1]
                    if last_is_follows:
                        # Already follows something, skip
                        continue

                    # Check depth
                    depth = path_depth(path)
                    if max_depth and depth > max_depth:
                        continue

                    follows_parts = [inp for inp, _child, _is_f in path]
                    follows_path = ".".join(follows_parts)

                    if is_excluded(
                        config,
                        lock,
                        follows_path,
                        follows_parts,
                        canonical_name,
                        canonical_node,
                    ):
                        continue

                    # Build the follows target — for root inputs it's just the name
                    proposals.append(
                        {
                            "follows_parts": follows_parts,
                            "target": canonical_name,
                            "follows_path": follows_path,
                            "source_key": key,
                            "desc": f"{follows_path} -> follows {canonical_name}",
                        }
                    )

        # Deduplicate proposals by follows_path (keep first)
        seen_paths = set()
        unique_proposals = []
        for p in proposals:
            if p["follows_path"] not in seen_paths:
                seen_paths.add(p["follows_path"])
                unique_proposals.append(p)
        proposals = unique_proposals

        # Filter out proposals that are already in the file
        new_proposals = []
        for p in proposals:
            if not follows_exists_in_content(content, p["follows_parts"], p["target"]):
                new_proposals.append(p)
        proposals = new_proposals

        if not proposals:
            print("no new follows to add.")
            break

        print(f"found {len(proposals)} follows to add:")
        for p in proposals:
            print(f"  {p['desc']}")

        if dry_run:
            print("\ndry run — not modifying files.")
            break

        # Group proposals by depth and apply in depth order.
        # If a batch fails nix flake lock, back out that batch and continue.
        from itertools import groupby

        proposals_sorted = sorted(proposals, key=lambda p: len(p["follows_parts"]))
        depth_batches = []
        for depth, grp in groupby(
            proposals_sorted, key=lambda p: len(p["follows_parts"])
        ):
            depth_batches.append((depth, list(grp)))

        round_added = 0
        failed_proposals = []

        for depth, batch in depth_batches:
            # Save state before applying this batch
            saved_content = content
            saved_lock = None
            lock_path = os.path.join(flake_dir, "flake.lock")
            with open(lock_path) as f:
                saved_lock = f.read()

            # Apply batch
            for p in batch:
                commented = find_commented_follows(
                    content, p["follows_parts"], p["target"]
                )
                if commented is not None:
                    line_no, original_line = commented
                    print(f"  uncomment: {original_line.strip()}")
                    content = uncomment_line(content, line_no)
                else:
                    print(f"  add: {p['desc']}")
                    content = insert_follows_in_content(
                        content, p["follows_parts"], p["target"]
                    )

            write_flake_nix(flake_dir, content)
            run_nixfmt(flake_dir)
            # Re-read after format (nixfmt may change content)
            content = read_flake_nix(flake_dir)

            print(f"  locking (depth {depth})...")
            if run_nix_flake_lock(flake_dir):
                round_added += len(batch)
            else:
                # Back out this batch
                print(
                    f"  warning: lock failed for depth-{depth} follows, backing out",
                    file=sys.stderr,
                )
                for p in batch:
                    print(f"    skipped: {p['desc']}", file=sys.stderr)
                    failed_proposals.append(p)
                content = saved_content
                write_flake_nix(flake_dir, content)
                run_nixfmt(flake_dir)
                content = read_flake_nix(flake_dir)
                with open(lock_path, "w") as f:
                    f.write(saved_lock)

        total_added += round_added

        if failed_proposals:
            print(
                f"\n  {len(failed_proposals)} follows could not be applied (lock failures):"
            )
            for p in failed_proposals:
                print(f"    {p['desc']}")

        if round_added == 0:
            print("no follows could be applied this round.")
            break

    if total_added and not dry_run:
        print(f"\ndone. added {total_added} follows across {round_num} round(s).")
        # Final format
        run_nixfmt(flake_dir)
    elif not total_added:
        print("\nno duplicates found.")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(
        prog="flake-tidy",
        description="Deduplicate flake inputs by adding follows declarations.",
    )
    parser.add_argument(
        "action",
        choices=["dedup"],
        help="Action to perform",
    )
    parser.add_argument(
        "--flake-dir",
        default=".",
        help="Path to the flake directory (default: current directory)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Show what would be changed without modifying files",
    )
    parser.add_argument(
        "--verbose",
        action="store_true",
        help="Show more details about skipped items",
    )

    args = parser.parse_args()

    flake_dir = os.path.abspath(args.flake_dir)

    if not os.path.isfile(os.path.join(flake_dir, "flake.lock")):
        print(f"error: no flake.lock in {flake_dir}", file=sys.stderr)
        sys.exit(1)
    if not os.path.isfile(os.path.join(flake_dir, "flake.nix")):
        print(f"error: no flake.nix in {flake_dir}", file=sys.stderr)
        sys.exit(1)

    if args.action == "dedup":
        dedup(flake_dir, dry_run=args.dry_run, verbose=args.verbose)


if __name__ == "__main__":
    main()
