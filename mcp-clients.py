"""Make mcpx the only MCP server every MCP client on this Mac is configured with.

Run by home-manager activation (homeUser.nix, mcpClients) on every switch, so
a client that adds servers of its own is put back. Each client gets one
stdio entry, `mcpx --config <config> serve`: mcpx's gateway (mcpx_search,
mcpx_call, mcpx_exec ...) in front of every server in mcpx.config.nix. The
--config is required: without it mcpx looks for project config upward from
the client's working directory, a repository's .mcp.json changes the daemon
it looks for, and with daemon.autostart off nothing is listening there.

Only the MCP key of each file is changed; everything else is kept. The first
time a file is changed, a copy is kept in ~/.local/state/mcp-clients/backup.
Files that do not exist are left alone (the client is not set up here).
`--dry-run` reports what would change. dezren39/nix#671.

Each client is handled independently: one client's malformed or unreadable
file is reported on stderr and the remaining clients are still enforced;
the run exits non-zero if any client failed (#906).
"""

import json
import os
import re
import shutil
import sys

HOME = os.path.expanduser("~")
MCPX = "/run/current-system/sw/bin/mcpx"
ARGS = ["--config", os.path.join(HOME, ".config/mcpx/config.json"), "serve"]
BACKUP_DIR = os.path.join(HOME, ".local/state/mcp-clients/backup")
DRY_RUN = "--dry-run" in sys.argv


def write(path, text, what):
    with open(path) as fh:
        if fh.read() == text:
            return
    print(f"mcp-clients: {path}: {what}{' (dry run)' if DRY_RUN else ''}")
    if DRY_RUN:
        return
    os.makedirs(BACKUP_DIR, exist_ok=True)
    backup = os.path.join(BACKUP_DIR, path.replace("/", "%"))
    if not os.path.exists(backup):
        shutil.copy2(path, backup)
    tmp = f"{path}.mcp-clients.tmp"
    with open(tmp, "w") as fh:
        fh.write(text)
    shutil.copymode(path, tmp)
    os.replace(tmp, path)


def json_client(path, entry):
    """A JSON file whose top-level mcpServers holds the servers (Copilot CLI, Claude Code)."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        data = json.load(fh)
    want = {"mcpx": entry}
    if data.get("mcpServers") == want:
        return
    removed = sorted(set(data.get("mcpServers") or {}) - {"mcpx"})
    data["mcpServers"] = want
    write(path, json.dumps(data, indent=2) + "\n", f"mcpServers = mcpx only (removed: {', '.join(removed) or 'none'})")


def codex(path):
    """~/.codex/config.toml: drop every [mcp_servers.*] table, add [mcp_servers.mcpx]."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        lines = fh.read().splitlines()
    kept, removed, skipping = [], set(), False
    for line in lines:
        header = re.match(r"\s*\[\s*([^\]]+?)\s*\]\s*(#.*)?$", line)
        if header:
            name = header.group(1)
            skipping = name.startswith("mcp_servers.")
            if skipping:
                removed.add(name.split(".")[1])
        if not skipping:
            kept.append(line)
    while kept and not kept[-1].strip():
        kept.pop()
    block = [
        "",
        "[mcp_servers.mcpx]",
        f"command = {json.dumps(MCPX)}",
        f"args = {json.dumps(ARGS)}",
    ]
    text = "\n".join(kept + block).lstrip("\n") + "\n"
    write(path, text, f"mcp_servers = mcpx only (removed: {', '.join(sorted(removed - {'mcpx'})) or 'none'})")


def _object_span(text, start):
    """Index just past the balanced {...} whose '{' is at start, skipping
    double-quoted strings (with backslash escapes) and // and /* */ comments,
    so braces inside strings or comments do not end the object early.
    None when the object never balances."""
    depth, i, n = 0, start, len(text)
    while i < n:
        c = text[i]
        if c == '"':
            i += 1
            while i < n and text[i] != '"':
                i += 2 if text[i] == "\\" else 1
            if i >= n:
                return None
            i += 1
        elif c == "/" and text[i + 1 : i + 2] == "/":
            j = text.find("\n", i)
            i = n if j < 0 else j + 1
        elif c == "/" and text[i + 1 : i + 2] == "*":
            j = text.find("*/", i + 2)
            if j < 0:
                return None
            i = j + 2
        elif c == "{":
            depth += 1
            i += 1
        elif c == "}":
            depth -= 1
            i += 1
            if depth == 0:
                return i
        else:
            i += 1
    return None


def _jsonc_loads(text):
    """json.loads with // and /* */ comments removed (strings kept intact)."""
    out, i, n = [], 0, len(text)
    while i < n:
        c = text[i]
        if c == '"':
            j = i + 1
            while j < n and text[j] != '"':
                j += 2 if text[j] == "\\" else 1
            out.append(text[i : j + 1])
            i = j + 1
        elif c == "/" and text[i + 1 : i + 2] == "/":
            j = text.find("\n", i)
            i = n if j < 0 else j
        elif c == "/" and text[i + 1 : i + 2] == "*":
            j = text.find("*/", i + 2)
            if j < 0:
                raise ValueError("unterminated /* comment")
            i = j + 2
        else:
            out.append(c)
            i += 1
    return json.loads("".join(out))


def _replace_context_servers(text, entry):
    """Swap the existing "context_servers" object value for the mcpx-only
    entry, preserving everything outside that span (and its trailing comma,
    when present) byte-for-byte. Returns the text unchanged when the existing
    value already is the mcpx-only entry, or None when no balanced object
    value can be located — the caller then warns and leaves the file alone
    rather than guessing (#905)."""
    m = re.search(r'"context_servers"\s*:\s*', text)
    if not m or m.end() >= len(text) or text[m.end()] != "{":
        return None
    end = _object_span(text, m.end())
    if end is None:
        return None
    try:
        if _jsonc_loads(text[m.end() : end]) == entry:
            return text
    except ValueError:
        pass
    rest = text[end:]
    comma = re.match(r"[ \t]*,", rest)
    tail = rest[comma.end() :] if comma else rest
    return text[: m.start()] + f'"context_servers": {json.dumps(entry)}' + ("," if comma else "") + tail


def zed(path):
    """Zed's settings.json is JSON with comments, so it is edited as text: a
    context_servers block naming only mcpx is added after the opening brace.
    One that already exists is replaced in place — its whole value span,
    located by brace matching — so a foreign block does not keep servers
    that bypass mcpx (#905). A block whose span cannot be located is left
    unchanged with a warning, never a guessed edit."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        text = fh.read()
    entry = {"mcpx": {"command": MCPX, "args": ARGS, "env": {}}}
    block = f'  "context_servers": {json.dumps(entry)},\n'
    if block in text:
        return
    if '"context_servers"' in text:
        replaced = _replace_context_servers(text, entry)
        if replaced is None:
            print(f"mcp-clients: {path}: has its own context_servers; not changed. Make mcpx its only entry: {json.dumps(entry)}",
                  file=sys.stderr)
            return
        if replaced != text:
            write(path, replaced, "context_servers = mcpx only (replaced existing block)")
        return
    m = re.search(r"^\{[ \t]*\n", text, re.M)
    if not m:
        print(f"mcp-clients: {path}: no top-level '{{' line; not changed", file=sys.stderr)
        return
    write(path, text[: m.end()] + block + text[m.end():], "context_servers = mcpx")


def main():
    # Each client runs independently (#906): an exception in one step is
    # reported with the client and file named, the remaining clients are
    # still enforced, and any failure makes the run exit non-zero so the
    # partial enforcement is not mistaken for full success.
    steps = [
        ("Copilot CLI", os.path.join(HOME, ".copilot/mcp-config.json"),
         lambda p: json_client(p, {"type": "local", "command": MCPX, "args": ARGS, "tools": ["*"]})),
        ("Claude Code", os.path.join(HOME, ".claude.json"),
         lambda p: json_client(p, {"type": "stdio", "command": MCPX, "args": ARGS, "env": {}})),
        ("Codex", os.path.join(HOME, ".codex/config.toml"), codex),
        ("Zed", os.path.join(HOME, ".config/zed/settings.json"), zed),
    ]
    failed = []
    for name, path, step in steps:
        try:
            step(path)
        except Exception as exc:
            failed.append(name)
            print(f"mcp-clients: {name} ({path}): FAILED: {exc}", file=sys.stderr)
    if failed:
        print(f"mcp-clients: {len(failed)} of {len(steps)} clients not enforced: {', '.join(failed)}",
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
