#!/usr/bin/env python3
"""Temp-HOME probes for mcp-clients.py.

Covers the two PR #904 fallout bugs:
- #906: one malformed client config must not abort enforcement for the
  remaining clients; the failure is named on stderr and the exit is non-zero.
- #905: a Zed settings.json with a foreign context_servers block gets that
  block replaced with the mcpx-only entry (brace-matched, string/comment
  aware); everything outside the block is preserved byte-for-byte.

Run: python3 mcp-clients.test.py  (exit 0 = all probes passed)
"""

import os
import shutil
import subprocess
import sys
import tempfile

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "mcp-clients.py")
FAILURES = []


def run(home, *args):
    env = dict(os.environ, HOME=home)
    return subprocess.run([sys.executable, SCRIPT, *args],
                          env=env, capture_output=True, text=True)


def seed(home, rel, text):
    path = os.path.join(home, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(text)
    return path


def read(path):
    with open(path) as fh:
        return fh.read()


def check(name, cond, detail=""):
    print(f"{'PASS' if cond else 'FAIL'}: {name}" + (f" -- {detail}" if detail and not cond else ""))
    if not cond:
        FAILURES.append(name)


def fresh_home():
    return tempfile.mkdtemp(prefix="mcp-clients-test-")


# #906: malformed first client (Copilot) -- the rest are still enforced,
# the bad file is named on stderr, exit is non-zero.
home = fresh_home()
seed(home, ".copilot/mcp-config.json", "{not json")
seed(home, ".claude.json", '{"mcpServers": {"other": {"command": "x"}}}')
seed(home, ".codex/config.toml", '[mcp_servers.other]\ncommand = "x"\n')
seed(home, ".config/zed/settings.json", '{\n  "theme": "Dark"\n}\n')
r = run(home)
check("906 exit non-zero on malformed Copilot", r.returncode != 0, f"exit={r.returncode}")
check("906 stderr names the client and file",
      "Copilot CLI" in r.stderr and ".copilot/mcp-config.json" in r.stderr, r.stderr)
check("906 Claude still enforced", '"mcpx"' in read(os.path.join(home, ".claude.json")))
check("906 Codex still enforced", "[mcp_servers.mcpx]" in read(os.path.join(home, ".codex/config.toml")))
check("906 Zed still enforced", '"context_servers"' in read(os.path.join(home, ".config/zed/settings.json")))
shutil.rmtree(home)

# Happy path: all four well-formed, everything enforced, exit 0, second run no-op.
home = fresh_home()
seed(home, ".copilot/mcp-config.json", '{"mcpServers": {"other": {"command": "x"}}}')
seed(home, ".claude.json", '{"mcpServers": {"other": {"command": "x"}}}')
seed(home, ".codex/config.toml", '[mcp_servers.other]\ncommand = "x"\n')
seed(home, ".config/zed/settings.json", '{\n  "theme": "Dark"\n}\n')
r = run(home)
check("happy exit 0", r.returncode == 0, f"exit={r.returncode} stderr={r.stderr}")
r2 = run(home)
check("happy second run is a silent no-op", r2.returncode == 0 and r2.stdout == "" and r2.stderr == "",
      f"stdout={r2.stdout!r} stderr={r2.stderr!r}")
shutil.rmtree(home)

# #905: foreign nested context_servers (middle key) with a brace inside a
# string and a // comment -- fully replaced, neighbours preserved, backup kept.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n  "theme": "Dark",\n'
                '  "context_servers": {\n'
                '    "evil}brace": {"command": "evil", "args": ["}"]},\n'
                '    // a } comment\n'
                '    "other": {"command": "x"}\n'
                '  },\n'
                '  "font_size": 14\n}\n')
original = read(zed_path)
r = run(home)
out = read(zed_path)
check("905 foreign block replaced", '"context_servers": {"mcpx":' in out and "evil" not in out, out)
check("905 surrounding keys preserved",
      out.startswith('{\n  "theme": "Dark",\n') and out.endswith('"font_size": 14\n}\n'), out)
backup = os.path.join(home, ".local/state/mcp-clients/backup", zed_path.replace("/", "%"))
check("905 backup holds the pre-change file", os.path.exists(backup) and read(backup) == original)
r2 = run(home)
check("905 second run no-op", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #905: context_servers as the last key (no trailing comma) -- replaced, no comma added.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n  "theme": "Dark",\n  "context_servers": {"other": {"command": "x"}}\n}\n')
r = run(home)
out = read(zed_path)
check("905 last-key block replaced without trailing comma",
      '"context_servers": {"mcpx":' in out and out.endswith('}\n}\n'), out)
shutil.rmtree(home)

# #905: already-mcpx but hand-formatted -- left byte-identical, no backup.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n  "context_servers": {\n    "mcpx": {\n'
                '      "command": "/run/current-system/sw/bin/mcpx",\n'
                f'      "args": ["--config", "{home}/.config/mcpx/config.json", "serve"],\n'
                '      "env": {}\n    }\n  }\n}\n')
original = read(zed_path)
r = run(home)
check("905 hand-formatted mcpx-only untouched",
      read(zed_path) == original and not os.path.exists(os.path.join(home, ".local")))
shutil.rmtree(home)

# #905: unbalanced context_servers value -- warning, file unchanged, exit 0.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n  "context_servers": {\n    "other": {"command": "x"\n')
original = read(zed_path)
r = run(home)
check("905 unbalanced block left unchanged with warning",
      r.returncode == 0 and read(zed_path) == original and "not changed" in r.stderr,
      f"exit={r.returncode} stderr={r.stderr}")
shutil.rmtree(home)

print(f"\n{'ALL PASS' if not FAILURES else 'FAILURES: ' + ', '.join(FAILURES)}")
sys.exit(1 if FAILURES else 0)
