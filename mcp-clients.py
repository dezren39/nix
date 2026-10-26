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


def zed(path):
    """Zed's settings.json is JSON with comments, so it is edited as text: a
    context_servers block naming only mcpx is added after the opening brace.
    One that already exists is replaced only when it is that same block."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        text = fh.read()
    entry = {"mcpx": {"command": MCPX, "args": ARGS, "env": {}}}
    block = f'  "context_servers": {json.dumps(entry)},\n'
    if block in text:
        return
    if '"context_servers"' in text:
        print(f"mcp-clients: {path}: has its own context_servers; not changed. Make mcpx its only entry: {json.dumps(entry)}",
              file=sys.stderr)
        return
    m = re.search(r"^\{[ \t]*\n", text, re.M)
    if not m:
        print(f"mcp-clients: {path}: no top-level '{{' line; not changed", file=sys.stderr)
        return
    write(path, text[: m.end()] + block + text[m.end():], "context_servers = mcpx")


def main():
    json_client(os.path.join(HOME, ".copilot/mcp-config.json"),
                {"type": "local", "command": MCPX, "args": ARGS, "tools": ["*"]})
    json_client(os.path.join(HOME, ".claude.json"),
                {"type": "stdio", "command": MCPX, "args": ARGS, "env": {}})
    codex(os.path.join(HOME, ".codex/config.toml"))
    zed(os.path.join(HOME, ".config/zed/settings.json"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
