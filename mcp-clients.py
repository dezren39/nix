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


def _split_dotted(name):
    """Split a TOML dotted name on dots outside quotes, unquoting each part."""
    parts, buf, quote, i = [], [], None, 0
    while i < len(name):
        c = name[i]
        if quote:
            buf.append(c)
            if quote == '"' and c == "\\" and i + 1 < len(name):
                buf.append(name[i + 1])
                i += 2
                continue
            if c == quote:
                quote = None
            i += 1
        elif c in "\"'":
            quote = c
            buf.append(c)
            i += 1
        elif c == ".":
            parts.append("".join(buf).strip())
            buf = []
            i += 1
        else:
            buf.append(c)
            i += 1
    parts.append("".join(buf).strip())
    out = []
    for part in parts:
        if len(part) >= 2 and part[0] == part[-1] and part[0] in "\"'":
            try:
                out.append(json.loads(part) if part[0] == '"' else part[1:-1])
            except ValueError:
                out.append(part[1:-1])
        else:
            out.append(part)
    return out


def _value_bracket_depth(text):
    """Net open-bracket depth of a TOML value, ignoring brackets inside
    strings and comments, so a multi-line inline table or array value is
    known to continue on the following lines."""
    depth, i, n = 0, 0, len(text)
    while i < n:
        c = text[i]
        if c == "#":
            break
        if text.startswith('"""', i) or text.startswith("'''", i):
            j = text.find(text[i : i + 3], i + 3)
            i = n if j < 0 else j + 3
        elif c == '"':
            i += 1
            while i < n and text[i] != '"':
                i += 2 if text[i] == "\\" else 1
            i += 1
        elif c == "'":
            j = text.find("'", i + 1)
            i = n if j < 0 else j + 1
        elif c in "{[(":
            depth += 1
            i += 1
        elif c in "}])":
            depth -= 1
            i += 1
        else:
            i += 1
    return depth


def _first_key(line):
    """(name, rest) for the first key of a TOML assignment line, or
    (None, line) when the line does not start with a key followed by '='."""
    i, n = 0, len(line)
    while i < n and line[i] in " \t":
        i += 1
    if i >= n:
        return None, line
    if line[i] in "\"'":
        q, j = line[i], i + 1
        while j < n and line[j] != q:
            j += 2 if q == '"' and line[j] == "\\" else 1
        if j >= n:
            return None, line
        raw, rest = line[i : j + 1], j + 1
        try:
            name = json.loads(raw) if q == '"' else raw[1:-1]
        except ValueError:
            name = raw[1:-1]
    else:
        m = re.match(r"[A-Za-z0-9_-]+", line[i:])
        if not m:
            return None, line
        name, rest = m.group(0), i + m.end()
    # The key (possibly dotted) must be followed by '='.
    tail = line[rest:]
    if not re.match(r"(\s*\.\s*(\"[^\"]*\"|'[^']*'|[A-Za-z0-9_-]+))*\s*=", tail):
        return None, line
    return name, line


def codex(path):
    """~/.codex/config.toml: drop every mcp_servers entry, however spelled
    ([mcp_servers.<name>] tables, [[mcp_servers.<name>]] array tables, and
    inline entries under the [mcp_servers] parent table), then add
    [mcp_servers.mcpx]."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        lines = fh.read().splitlines()
    kept, removed = [], set()
    skipping = False      # inside a dropped mcp_servers table / array table
    in_parent = False     # inside the bare [mcp_servers] parent table
    value_depth = 0       # >0: still consuming a dropped multi-line value
    for line in lines:
        if value_depth > 0:
            value_depth += _value_bracket_depth(line)
            if value_depth < 0:
                value_depth = 0
            continue
        array_header = re.match(r"\s*\[\[\s*(.+?)\s*\]\]\s*(#.*)?$", line)
        header = None if array_header else re.match(r"\s*\[\s*([^\]]+?)\s*\]\s*(#.*)?$", line)
        if array_header or header:
            name = (array_header or header).group(1)
            parts = _split_dotted(name)
            in_parent = False
            if parts and parts[0] == "mcp_servers" and len(parts) >= 2:
                skipping = True
                removed.add(parts[1])
            elif parts == ["mcp_servers"] and header:
                # Parent table itself stays; its entries are dropped below.
                skipping = False
                in_parent = True
                kept.append(line)
            elif parts == ["mcp_servers"]:
                # [[mcp_servers]] array of unnamed tables: drop it whole.
                skipping = True
            else:
                skipping = False
                kept.append(line)
            continue
        if skipping:
            continue
        if in_parent:
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                kept.append(line)
                continue
            name, _ = _first_key(line)
            if name is None:
                kept.append(line)
                continue
            removed.add(name)
            eq = line.find("=")
            value_depth = _value_bracket_depth(line[eq + 1 :]) if eq >= 0 else 0
            if value_depth < 0:
                value_depth = 0
            continue
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


def _live_key_span(text, key):
    """(key_start, value_start) of the live top-level `key` in JSONC text,
    or None. Scans the text skipping // and /* */ comments and string
    literals, tracking brace depth, so an occurrence inside a comment or
    a string — or nested inside another object — is not mistaken for the
    live key (#929). value_start is the index just past the ':' and any
    whitespace following it."""
    i, n, depth = 0, len(text), 0
    while i < n:
        c = text[i]
        if c == "/" and text[i + 1 : i + 2] == "/":
            j = text.find("\n", i)
            i = n if j < 0 else j + 1
        elif c == "/" and text[i + 1 : i + 2] == "*":
            j = text.find("*/", i + 2)
            if j < 0:
                return None
            i = j + 2
        elif c == '"':
            start = i
            i += 1
            while i < n and text[i] != '"':
                i += 2 if text[i] == "\\" else 1
            if i >= n:
                return None
            i += 1
            if depth != 1:
                continue
            try:
                name = json.loads(text[start:i])
            except ValueError:
                continue
            j = i
            while j < n and text[j] in " \t\r\n":
                j += 1
            if j >= n or text[j] != ":":
                continue
            if name != key:
                continue
            j += 1
            while j < n and text[j] in " \t\r\n":
                j += 1
            return start, j
        elif c == "{":
            depth += 1
            i += 1
        elif c == "}":
            depth -= 1
            i += 1
        else:
            i += 1
    return None


def _replace_context_servers(text, entry):
    """Swap the existing "context_servers" object value for the mcpx-only
    entry, preserving everything outside that span (and its trailing comma,
    when present) byte-for-byte. Returns the text unchanged when the existing
    value already is the mcpx-only entry, or None when no balanced object
    value can be located — the caller then warns and leaves the file alone
    rather than guessing (#905). The key is located with _live_key_span, so
    a commented-out or quoted "context_servers" block cannot absorb the
    replacement (#929)."""
    live = _live_key_span(text, "context_servers")
    if live is None:
        return None
    key_start, value_start = live
    if value_start >= len(text) or text[value_start] != "{":
        return None
    end = _object_span(text, value_start)
    if end is None:
        return None
    try:
        if _jsonc_loads(text[value_start:end]) == entry:
            return text
    except ValueError:
        pass
    rest = text[end:]
    comma = re.match(r"[ \t]*,", rest)
    tail = rest[comma.end() :] if comma else rest
    return text[:key_start] + f'"context_servers": {json.dumps(entry)}' + ("," if comma else "") + tail


def _only_ws_comments(s):
    """True when s holds nothing but whitespace and // /* */ comments."""
    i, n = 0, len(s)
    while i < n:
        if s[i] in " \t\r\n":
            i += 1
        elif s.startswith("//", i):
            j = s.find("\n", i)
            i = n if j < 0 else j + 1
        elif s.startswith("/*", i):
            j = s.find("*/", i + 2)
            if j < 0:
                return False
            i = j + 2
        else:
            return False
    return True


def _top_level_brace(text):
    """Index of the top-level object's opening '{', skipping leading
    whitespace and comments, or None when the first meaningful character
    is not '{' (#950)."""
    i, n = 0, len(text)
    while i < n:
        if text[i] in " \t\r\n":
            i += 1
        elif text.startswith("//", i):
            j = text.find("\n", i)
            i = n if j < 0 else j + 1
        elif text.startswith("/*", i):
            j = text.find("*/", i + 2)
            if j < 0:
                return None
            i = j + 2
        elif text[i] == "{":
            return i
        else:
            return None
    return None


def zed(path):
    """Zed's settings.json is JSON with comments, so it is edited as text: a
    context_servers block naming only mcpx is added after the opening brace.
    One that already exists is replaced in place — its whole value span,
    located by brace matching — so a foreign block does not keep servers
    that bypass mcpx (#905). A block whose span cannot be located is left
    unchanged with a warning, never a guessed edit. The insert path accepts
    any opening-brace shape ({} , single-line, '{ // comment'); a file with
    no locatable top-level object raises instead of silently skipping
    enforcement (#950)."""
    if not os.path.exists(path):
        return
    with open(path) as fh:
        text = fh.read()
    entry = {"mcpx": {"command": MCPX, "args": ARGS, "env": {}}}
    block = f'  "context_servers": {json.dumps(entry)},\n'
    if _live_key_span(text, "context_servers") is not None:
        replaced = _replace_context_servers(text, entry)
        if replaced is None:
            print(f"mcp-clients: {path}: has its own context_servers; not changed. Make mcpx its only entry: {json.dumps(entry)}",
                  file=sys.stderr)
            return
        if replaced != text:
            write(path, replaced, "context_servers = mcpx only (replaced existing block)")
        return
    # Insert path (#950): the opening brace need not be alone on its line.
    # Locate the top-level '{' wherever it sits; insert as the first member
    # in a shape that fits the file, preserving everything else byte-for-byte.
    # A file with no locatable top-level object is a client failure (#906
    # path: main names it and the run exits non-zero), never a silent skip.
    start = _top_level_brace(text)
    if start is None:
        raise ValueError("no top-level '{' object; not changed")
    end = _object_span(text, start)
    if end is None:
        raise ValueError("top-level object never closes; not changed")
    if _only_ws_comments(text[start + 1 : end - 1]):
        # Empty object: the entry goes just before the closing brace,
        # with no trailing comma.
        entry_text = f'"context_servers": {json.dumps(entry)}'
        write(path, text[: end - 1] + entry_text + text[end - 1 :], "context_servers = mcpx")
        return
    line_end = text.find("\n", start)
    if line_end >= 0 and _only_ws_comments(text[start + 1 : line_end]):
        # '{' (plus at most a comment) ends its line — the conventional
        # shape, and the '{ // comment' shape: insert on the next line.
        write(path, text[: line_end + 1] + block + text[line_end + 1 :], "context_servers = mcpx")
        return
    # Content shares the opening line (single-line object, or a member
    # after a block comment): insert directly after the '{'.
    inline = f' "context_servers": {json.dumps(entry)},'
    write(path, text[: start + 1] + inline + text[start + 1 :], "context_servers = mcpx")


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
