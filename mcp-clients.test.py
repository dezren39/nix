#!/usr/bin/env python3
"""Temp-HOME probes for mcp-clients.py.

Covers the two PR #904 fallout bugs:
- #906: one malformed client config must not abort enforcement for the
  remaining clients; the failure is named on stderr and the exit is non-zero.
- #905: a Zed settings.json with a foreign context_servers block gets that
  block replaced with the mcpx-only entry (brace-matched, string/comment
  aware); everything outside the block is preserved byte-for-byte.
- #950: a Zed settings.json whose opening brace is not alone on a line
  ({} , single-line, '{ // comment') still gains context_servers; a file
  with no locatable top-level object fails the run (#906 way) instead of
  silently exiting 0 unenforced.
- #944: Codex servers spelled as inline-table entries under [mcp_servers]
  or as [[mcp_servers.*]] array-of-tables blocks are removed (not just
  plain [mcp_servers.*] tables) and named in the removed: report.
- #990: a header-shaped line inside a multi-line basic or
  literal string value is string content, never a table header --
  the Codex parser tracks multi-line string state so such a line
  cannot switch modes, swallow the rest of the string, and
  corrupt the file.
- #956: Codex servers spelled at the TOML root -- dotted keys
  (mcp_servers.<name>...) or a bare mcp_servers = { ... } inline table
  (single- or multi-line) -- are removed and named; unrelated root keys
  (including multi-line values and non-mcp_servers dotted keys) are
  preserved, and the result parses as valid TOML.

Run: python3 mcp-clients.test.py  (exit 0 = all probes passed)
"""

import json
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

# #929: a commented-out context_servers block before a live foreign block --
# the live block is replaced, the comment is preserved byte-for-byte.
home = fresh_home()
comment = ('  // "context_servers": {\n'
           '  //   "old": {"command": "old"}\n'
           '  // },\n')
zed_path = seed(home, ".config/zed/settings.json",
                '{\n' + comment +
                '  "context_servers": {"other": {"command": "x"}},\n'
                '  "theme": "Dark"\n}\n')
r = run(home)
out = read(zed_path)
check("929 line-comment block does not absorb the replacement",
      '"context_servers": {"mcpx":' in out and '"other"' not in out, out)
check("929 line comment preserved byte-for-byte", comment in out, out)
r2 = run(home)
check("929 second run no-op after comment case", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #929: /* */ block-comment variant -- same expectations.
home = fresh_home()
comment = ('  /* "context_servers": {\n'
           '       "old": {"command": "old"}\n'
           '     }, */\n')
zed_path = seed(home, ".config/zed/settings.json",
                '{\n' + comment +
                '  "context_servers": {"other": {"command": "x"}},\n'
                '  "theme": "Dark"\n}\n')
r = run(home)
out = read(zed_path)
check("929 block-comment block does not absorb the replacement",
      '"context_servers": {"mcpx":' in out and '"other"' not in out, out)
check("929 block comment preserved byte-for-byte", comment in out, out)
shutil.rmtree(home)

# #929: comment-only occurrence -- no live key, so the mcpx block is inserted
# and the comment stays untouched.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n' + comment +
                '  "theme": "Dark"\n}\n')
r = run(home)
out = read(zed_path)
check("929 comment-only occurrence takes the insert path",
      '"context_servers": {"mcpx":' in out and comment in out, out)
r2 = run(home)
check("929 second run no-op after insert", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #929: a string mentioning context_servers before the live key -- the
# scanner skips string literals, so the live block is replaced and the
# string is preserved.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{\n  "note": "see \\"context_servers\\": {\\"x\\"} docs",\n'
                '  "context_servers": {"other": {"command": "x"}}\n}\n')
r = run(home)
out = read(zed_path)
check("929 string mention does not absorb the replacement",
      '"context_servers": {"mcpx":' in out and '"other"' not in out, out)
check("929 string mention preserved",
      '"note": "see \\"context_servers\\": {\\"x\\"} docs"' in out, out)
shutil.rmtree(home)

# #950: empty single-line object {} -- context_servers inserted, output
# parses as JSON, second run no-op.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json", '{}')
r = run(home)
out = read(zed_path)
try:
    parsed = json.loads(out)
except ValueError:
    parsed = {}
check("950 {} gains context_servers", r.returncode == 0
      and set(parsed.get("context_servers", {})) == {"mcpx"}, f"exit={r.returncode} out={out}")
r2 = run(home)
check("950 second run no-op after {} insert", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #950: complete single-line object -- inserted, theme preserved, parses.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json", '{"theme": "One Dark"}\n')
r = run(home)
out = read(zed_path)
try:
    parsed = json.loads(out)
except ValueError:
    parsed = {}
check("950 single-line object gains context_servers", r.returncode == 0
      and set(parsed.get("context_servers", {})) == {"mcpx"}
      and parsed.get("theme") == "One Dark", f"exit={r.returncode} out={out}")
r2 = run(home)
check("950 second run no-op after single-line insert", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #950: '{ // comment' shape -- comment kept byte-for-byte, block inserted
# after the brace line, remaining content untouched.
home = fresh_home()
zed_path = seed(home, ".config/zed/settings.json",
                '{ // my settings\n  "theme": "One Dark"\n}\n')
r = run(home)
out = read(zed_path)
check("950 brace-with-comment gains context_servers", r.returncode == 0
      and '"context_servers": {"mcpx":' in out, f"exit={r.returncode} out={out}")
check("950 brace-line comment preserved", out.startswith('{ // my settings\n'), out)
check("950 brace-with-comment body preserved", out.endswith('  "theme": "One Dark"\n}\n'), out)
r2 = run(home)
check("950 second run no-op after comment insert", r2.returncode == 0 and read(zed_path) == out)
shutil.rmtree(home)

# #950: no locatable top-level object -- the Zed step fails the #906 way:
# named on stderr, exit non-zero, file unchanged, other clients enforced.
home = fresh_home()
seed(home, ".claude.json", '{"mcpServers": {"other": {"command": "x"}}}')
zed_path = seed(home, ".config/zed/settings.json", 'not an object\n')
original = read(zed_path)
r = run(home)
check("950 uneditable Zed exits non-zero", r.returncode != 0, f"exit={r.returncode}")
check("950 uneditable Zed named on stderr", "Zed" in r.stderr and "settings.json" in r.stderr, r.stderr)
check("950 uneditable Zed file unchanged", read(zed_path) == original)
check("950 Claude still enforced despite Zed failure", '"mcpx"' in read(os.path.join(home, ".claude.json")))
shutil.rmtree(home)

# #944: Codex servers spelled as inline-table entries under the
# [mcp_servers] parent table -- removed, named in the report, [other]
# preserved, second run no-op.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers]\n'
                  'foo = { command = "evil", args = [] }\n'
                  'bar = { command = "evil2", args = [] }\n'
                  '\n'
                  '[other]\n'
                  'x = 1\n')
r = run(home)
out = read(codex_path)
check("944 inline-table entries removed", "evil" not in out and "foo" not in out and "bar" not in out, out)
check("944 inline-table report names removed servers", "removed: bar, foo" in r.stdout, r.stdout)
check("944 inline case leaves only mcpx plus other sections",
      "[mcp_servers.mcpx]" in out and "[other]" in out and "x = 1" in out, out)
r2 = run(home)
check("944 inline second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #944: Codex servers spelled as [[mcp_servers.<name>]] array-of-tables
# blocks -- removed whole, named in the report, following sections kept.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[[mcp_servers.foo]]\n'
                  'command = "evil"\n'
                  '\n'
                  '[[mcp_servers.bar]]\n'
                  'command = "evil2"\n'
                  '\n'
                  '[other]\n'
                  'x = 1\n')
r = run(home)
out = read(codex_path)
check("944 array-table blocks removed", "evil" not in out and "mcp_servers.foo" not in out
      and "mcp_servers.bar" not in out, out)
check("944 array-table report names removed servers", "removed: bar, foo" in r.stdout, r.stdout)
check("944 array case leaves only mcpx plus other sections",
      "[mcp_servers.mcpx]" in out and "[other]" in out and "x = 1" in out, out)
r2 = run(home)
check("944 array second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #944: all three spellings in one file -- plain table, parent inline
# entry, and array table are all removed and all named in the report.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers.plain]\n'
                  'command = "evil"\n'
                  '\n'
                  '[mcp_servers]\n'
                  'inlined = { command = "evil", args = [] }\n'
                  '\n'
                  '[[mcp_servers.arra]]\n'
                  'command = "evil"\n')
r = run(home)
out = read(codex_path)
check("944 mixed spellings all removed", "evil" not in out and "[mcp_servers.plain]" not in out
      and "[[mcp_servers.arra]]" not in out and "inlined" not in out
      and "[mcp_servers.mcpx]" in out, out)
check("944 mixed report names all removed servers",
      "removed: arra, inlined, plain" in r.stdout, r.stdout)
r2 = run(home)
check("944 mixed second run no-op", r2.returncode == 0 and read(codex_path) == out)
shutil.rmtree(home)

# #956: Codex servers spelled as root-level dotted keys -- removed and
# named; unrelated root keys (a non-mcp_servers dotted key, and a kept
# multi-line inline table whose inner lines merely mention mcp_servers)
# are preserved byte-for-byte; result parses and holds only mcpx.
def codex_server_names(text):
    try:
        import tomllib
        return set(tomllib.loads(text).get("mcp_servers", {}))
    except Exception:
        return None


home = fresh_home()
notes_block = ('notes = [\n'
               '  "mcp_servers.evil.command = \\"not real\\"",\n'
               ']\n')
codex_path = seed(home, ".codex/config.toml",
                  'model = "gpt-5"\n'
                  + notes_block +
                  'unrelated.key = "keep"\n'
                  'mcp_servers.evil.command = "evil"\n'
                  'mcp_servers.evil.args = ["--x"]\n'
                  'mcp_servers.sneaky = { command = "evil2" }\n'
                  '\n'
                  '[other]\n'
                  'x = 1\n')
r = run(home)
out = read(codex_path)
check("956 dotted-key servers removed", "evil2" not in out and "sneaky" not in out and "mcp_servers.evil.args" not in out, out)
check("956 dotted-key report names removed servers", "removed: evil, sneaky" in r.stdout, r.stdout)
check("956 dotted-key result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
check("956 unrelated root keys preserved byte-for-byte",
      'model = "gpt-5"\n' in out and notes_block in out and 'unrelated.key = "keep"\n' in out, out)
check("956 dotted case keeps other sections", "[other]" in out and "x = 1" in out, out)
r2 = run(home)
check("956 dotted second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #956: a root-level mcp_servers inline table on one line -- foreign
# servers dropped and named, the result parses (previously declaring
# mcpx on top of the surviving table made config.toml invalid TOML).
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  'mcp_servers = { evil = { command = "evil" }, other2 = { command = "evil2" } }\n'
                  'model = "gpt-5"\n')
r = run(home)
out = read(codex_path)
check("956 root inline-table servers removed", "evil" not in out and "other2" not in out, out)
check("956 root inline-table report names removed servers",
      "removed: evil, other2" in r.stdout, r.stdout)
check("956 root inline-table result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
check("956 root inline-table keeps model", 'model = "gpt-5"' in out, out)
r2 = run(home)
check("956 root inline-table second run no-op",
      r2.returncode == 0 and read(codex_path) == out and r2.stdout == "", f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #956: the same inline table spread over several lines, one server with
# a nested multi-line table of its own -- all dropped and named, the
# nested command line is not mistaken for another server.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  'model = "gpt-5"\n'
                  'mcp_servers = {\n'
                  '  evil = { command = "evil" },\n'
                  '  multi = {\n'
                  '    command = "evil2"\n'
                  '  }\n'
                  '}\n')
r = run(home)
out = read(codex_path)
check("956 multi-line inline-table servers removed", "evil" not in out and "multi" not in out, out)
check("956 multi-line inline-table report names removed servers",
      "removed: evil, multi" in r.stdout, r.stdout)
check("956 multi-line inline-table result parses with only mcpx",
      codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("956 multi-line inline-table second run no-op",
      r2.returncode == 0 and read(codex_path) == out and r2.stdout == "", f"stdout={r2.stdout!r}")
shutil.rmtree(home)


# #990: a header-shaped line inside a multi-line basic string in an
# ordinary table is string content -- the fixture from the issue must
# round-trip: string preserved byte-for-byte, the following table
# survives, no server reported removed, output valid TOML.
home = fresh_home()
string_block = ('instructions = """\n'
                'See this example TOML:\n'
                '[mcp_servers.evil]\n'
                'command = "evil"\n'
                '"""\n')
codex_path = seed(home, ".codex/config.toml",
                  'model = "gpt-5"\n'
                  '\n'
                  '[profiles.demo]\n'
                  + string_block +
                  '\n'
                  '[profiles.demo2]\n'
                  'model = "gpt-5-mini"\n')
r = run(home)
out = read(codex_path)
check("990 basic-string fixture preserved byte-for-byte", string_block in out, out)
check("990 basic-string following table survives", "[profiles.demo2]" in out and 'model = "gpt-5-mini"' in out, out)
check("990 basic-string reports no server removed", "removed: none" in r.stdout, r.stdout)
check("990 basic-string result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990 basic-string second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990: the same for a multi-line literal string, with a REAL
# [mcp_servers.real] table alongside -- the real one is still removed
# and reported, the ghost inside the literal string is not.
home = fresh_home()
literal_block = ("literal = '''\n"
                 "[mcp_servers.ghost]\n"
                 'command = "ghost"\n'
                 "'''\n")
codex_path = seed(home, ".codex/config.toml",
                  '[profiles.demo]\n'
                  + literal_block +
                  '\n'
                  '[mcp_servers.real]\n'
                  'command = "real"\n')
r = run(home)
out = read(codex_path)
check("990 literal-string preserved byte-for-byte", literal_block in out, out)
check("990 real server alongside string removed", "[mcp_servers.real]" not in out and 'command = "real"' not in out, out)
check("990 report names only the real server", "removed: real" in r.stdout and "ghost" not in r.stdout, r.stdout)
check("990 literal-string result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990 literal-string second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 at the TOML root: a dotted mcp_servers line inside a root-level
# multi-line string is not a server; a real root dotted server next to
# it is still removed and named.
home = fresh_home()
root_block = ('instructions = """\n'
              'mcp_servers.evil.command = "evil"\n'
              '"""\n')
codex_path = seed(home, ".codex/config.toml",
                  root_block +
                  'mcp_servers.real.command = "real"\n')
r = run(home)
out = read(codex_path)
check("990 root string content preserved byte-for-byte", root_block in out, out)
check("990 root report names only the real server", "removed: real" in r.stdout, r.stdout)
check("990 root result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990 root second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 under the [mcp_servers] parent: an entry whose value is a
# multi-line string containing a header-shaped line -- the entry is
# dropped as one entry, the phantom header inside its string is not
# treated as a table (and not reported).
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers]\n'
                  'notes = """\n'
                  '[mcp_servers.phantom]\n'
                  '"""\n')
r = run(home)
out = read(codex_path)
check("990 parent string entry dropped whole", "notes" not in out and "phantom" not in out, out)
check("990 parent report has no phantom", "phantom" not in r.stdout, r.stdout)
check("990 parent result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990 parent second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)


# #990 review follow-up: an escaped quote directly before the closing
# delimiter of a multi-line basic string in a DROPPED table -- the
# string still closes there, so the following kept table survives.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers.old]\n'
                  'command = """echo \\""""\n'
                  '\n'
                  '[profiles.demo]\n'
                  'model = "gpt-5-mini"\n')
r = run(home)
out = read(codex_path)
check("990r escaped-quote string closes, old removed", "removed: old" in r.stdout, r.stdout)
check("990r escaped-quote following table survives", "[profiles.demo]" in out and 'model = "gpt-5-mini"' in out, out)
check("990r escaped-quote result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990r escaped-quote second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 review follow-up: a multi-line string that opens on a
# continuation line of a dropped array value -- its content lines are
# string content, never assignments, and the table after the dropped
# one survives.
home = fresh_home()
array_block = ("args = [\n"
               "  '''help text\n"
               'example = """\n'
               "  ''',\n"
               "]\n")
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers.old]\n'
                  + array_block +
                  '\n'
                  '[profiles.demo]\n'
                  'model = "gpt-5-mini"\n')
r = run(home)
out = read(codex_path)
check("990r array-string old removed and reported", "removed: old" in r.stdout and "[mcp_servers.old]" not in out, out)
check("990r array-string following table survives", "[profiles.demo]" in out and 'model = "gpt-5-mini"' in out, out)
check("990r array-string result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990r array-string second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 review follow-up: a quoted server name containing '=' under
# the [mcp_servers] parent -- the assignment separator is the one
# outside the quotes, so its multi-line string value does not leak
# string state into the following table.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  '[mcp_servers]\n'
                  '"old=name" = { command = """echo""" }\n'
                  '\n'
                  '[profiles.demo]\n'
                  'model = "gpt-5-mini"\n')
r = run(home)
out = read(codex_path)
check("990r quoted-name entry removed and reported", "old=name" in r.stdout and "old=name" not in out, out)
check("990r quoted-name following table survives", "[profiles.demo]" in out and 'model = "gpt-5-mini"' in out, out)
check("990r quoted-name result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990r quoted-name second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 review follow-up: two multi-line strings starting on the same
# line of a kept value -- the scan continues after the first closing
# delimiter, so the second string's content is not mistaken for TOML
# and a real server table after it is still removed.
home = fresh_home()
items_block = ('items = ["""first""", """second\n'
               'x = """\n'
               ']\n')
codex_path = seed(home, ".codex/config.toml",
                  '[profiles.demo]\n'
                  + items_block +
                  '\n'
                  '[mcp_servers.old]\n'
                  'command = "old"\n')
r = run(home)
out = read(codex_path)
check("990r two-strings value preserved byte-for-byte", items_block in out, out)
check("990r two-strings real server removed", "removed: old" in r.stdout and 'command = "old"' not in out, out)
check("990r two-strings result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990r two-strings second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)


# #990 re-review follow-up: a multi-line string that opens on an
# array continuation line of a KEPT table -- kept values outside the
# TOML root track bracket depth too, so the string content line is
# never scanned as an assignment and the real server after the array
# is still removed.
home = fresh_home()
kept_array_block = ("items = [\n"
                    "  '''content\n"
                    "example = \"\"\"\n"
                    "  ''',\n"
                    "]\n")
codex_path = seed(home, ".codex/config.toml",
                  "[profiles.demo]\n"
                  + kept_array_block +
                  "\n"
                  "[mcp_servers.old]\n"
                  "command = \"old\"\n")
r = run(home)
out = read(codex_path)
check("990rr kept-array value preserved byte-for-byte", kept_array_block in out, out)
check("990rr kept-array real server removed", "removed: old" in r.stdout and "command = \"old\"" not in out, out)
check("990rr kept-array result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990rr kept-array second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 re-review follow-up: a multi-line literal string that ends
# with a content quote (a closing run of four quotes) in a dropped
# server array -- the whole run is the delimiter end, so the second
# string on the line still opens and the following kept table is
# not silently deleted.
home = fresh_home()
codex_path = seed(home, ".codex/config.toml",
                  "[mcp_servers.old]\n"
                  "args = ['''one'''', '''two\n"
                  "example = \"\"\"\n"
                  "''']\n"
                  "\n"
                  "[profiles.demo]\n"
                  "model = \"gpt-5-mini\"\n")
r = run(home)
out = read(codex_path)
check("990rr quote-run old removed and reported", "removed: old" in r.stdout and "[mcp_servers.old]" not in out, out)
check("990rr quote-run following table survives", "[profiles.demo]" in out and "model = \"gpt-5-mini\"" in out, out)
check("990rr quote-run result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990rr quote-run second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

# #990 re-review follow-up: a root array line that closes one
# multi-line string and opens another -- the outside text between
# them (an opening bracket) counts toward the kept value depth, so
# a bracket-shaped line inside the array is not mistaken for a
# table header and root server removal still happens.
home = fresh_home()
root_array_block = ("items = [\"\"\"first\n"
                    "\"\"\", [\"\"\"second\n"
                    "\"\"\"],\n"
                    "[\"not-a-table\"]\n"
                    "]\n")
codex_path = seed(home, ".codex/config.toml",
                  root_array_block +
                  "mcp_servers.old.command = \"old\"\n")
r = run(home)
out = read(codex_path)
check("990rr outside-text value preserved byte-for-byte", root_array_block in out, out)
check("990rr outside-text root server removed", "removed: old" in r.stdout and "mcp_servers.old" not in out, out)
check("990rr outside-text result parses with only mcpx", codex_server_names(out) == {"mcpx"}, out)
r2 = run(home)
check("990rr outside-text second run no-op", r2.returncode == 0 and read(codex_path) == out and r2.stdout == "",
      f"stdout={r2.stdout!r}")
shutil.rmtree(home)

print(f"\n{'ALL PASS' if not FAILURES else 'FAILURES: ' + ', '.join(FAILURES)}")
sys.exit(1 if FAILURES else 0)
