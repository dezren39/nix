"""Record when OpenChamber, OpenCode and mcpx start, stop and drop connections.

Run by the launchd agent org.nixos.service-events (configuration.nix). Every
INTERVAL seconds it samples launchd, the process table and the TCP
connections of the servers it watches, and appends one JSON object per change
to ~/Library/Logs/nix-services/events.jsonl. `service-event` (event.py)
appends to the same file from scripts. Read it with `just service-events`;
docs/services.md describes every event. dezren39/nix#670.

Only names, pids, ports and exit codes are recorded. No environment, command
argument values beyond the program name, or request contents.
"""

import json
import math
import os
import re
import subprocess
import sys
import time

from event import LOG_DIR, append_event, rotate


def parse_interval(raw):
    """SERVICE_EVENTS_INTERVAL must be a positive, finite number of seconds.

    #918: the raw float() parse crashed at import for non-numeric values,
    crashed at the unprotected time.sleep() in main() for negative/nan/inf,
    and busy-looped the samplers at 0. Fail at startup instead, naming the
    variable and the offending value, so a typo is visible in the agent's
    stderr log rather than a silent crash-loop.
    """
    try:
        value = float(raw)
    except (TypeError, ValueError):
        value = math.nan
    if not math.isfinite(value) or value <= 0:
        print(
            f"service-monitor: invalid SERVICE_EVENTS_INTERVAL={raw!r}: "
            "must be a positive finite number of seconds",
            file=sys.stderr,
        )
        raise SystemExit(2)
    return value


INTERVAL = parse_interval(os.environ.get("SERVICE_EVENTS_INTERVAL", "5"))
LABELS = ("org.nixos.openchamber", "org.nixos.mcpx")
LABEL_PREFIXES = ("application.dev.openchamber.desktop",)

# Logs written by launchd (StandardOutPath/StandardErrorPath, opened
# O_APPEND, so a copy followed by truncation in place is safe) or by
# OpenChamber itself. Size-bounded here; dezren39/nix#668.
ROTATED_LOGS = (
    "~/Library/Logs/openchamber.log",
    "~/Library/Logs/mcpx.log",
    "~/Library/Logs/OpenChamber/main.log",
)
LONG_LIVED_S = 30
# #920: a drop only counts as reconnected if a replacement opens a
# connection within this window. Restarts and deploys replace a server in
# seconds to a few minutes; a start hours or days later is a new, unrelated
# process (for launchd-managed servers the (role, ppid=1) key is effectively
# the role alone), and reporting it as a reconnect fabricates a recovery
# with the old drop's count and a multi-day gap.
RECONNECT_WINDOW_S = 600
STATS_EVERY_S = 300
ROTATE_BYTES = 20 * 1024 * 1024
ROTATE_KEEP = 3


def run(argv):
    try:
        out = subprocess.run(argv, capture_output=True, text=True, timeout=20)
        return out.stdout
    except (OSError, subprocess.SubprocessError):
        return ""


def launchd_jobs():
    """Label -> (pid or None, last exit status) for the agents watched."""
    jobs = {}
    for line in run(["launchctl", "list"]).splitlines()[1:]:
        parts = line.split("\t")
        if len(parts) != 3:
            continue
        pid, status, label = parts
        if label in LABELS or label.startswith(LABEL_PREFIXES):
            jobs[label] = (int(pid) if pid.isdigit() else None, status)
    return jobs


def classify(command):
    if "/OpenChamber.app/Contents/MacOS/OpenChamber" in command:
        return "openchamber-desktop"
    if re.search(r"openchamber/packages/web/bin/cli\.js serve|/bin/openchamber serve", command):
        return "openchamber-server"
    if re.search(r"\bbun server/index\.js\b", command):
        return "openchamber-dev"
    if re.search(r"opencode(-wrapped)? serve\b", command):
        return "opencode-service" if "--service" in command else "opencode"
    if re.search(r"\bmcpx daemon\b", command):
        return "mcpx-daemon"
    return None


def processes():
    """Pid -> (role, ppid, program name) for the processes watched."""
    procs = {}
    for line in run(["ps", "-axo", "pid=,ppid=,command="]).splitlines():
        m = re.match(r"\s*(\d+)\s+(\d+)\s+(.*)", line)
        if not m:
            continue
        role = classify(m.group(3))
        if role:
            # The desktop app's path has a space ("Nix Apps"); name it outright.
            program = "OpenChamber" if role == "openchamber-desktop" else os.path.basename(m.group(3).split(" ")[0])
            procs[int(m.group(1))] = (role, int(m.group(2)), program)
    return procs


def sockets(pids):
    """Pid -> (listening ports, inbound connections as 'local->peer')."""
    result = {pid: (set(), set()) for pid in pids}
    if not pids:
        return result
    out = run(["lsof", "-nP", "-a", "-p", ",".join(map(str, pids)), "-iTCP", "-F", "pnT"])
    pid = None
    name = None
    for line in out.splitlines():
        tag, value = line[:1], line[1:]
        if tag == "p":
            pid = int(value)
        elif tag == "n":
            name = value
        elif tag == "T" and value.startswith("ST=") and pid in result:
            state = value[3:]
            if state == "LISTEN":
                result[pid][0].add(name.rsplit(":", 1)[-1])
            elif state == "ESTABLISHED" and "->" in name:
                result[pid][1].add(name)
    inbound = {}
    for pid, (ports, conns) in result.items():
        mine = {c for c in conns if c.split("->")[0].rsplit(":", 1)[-1] in ports}
        inbound[pid] = (ports, mine)
    return inbound


class Monitor:
    def __init__(self):
        self.jobs = {}
        self.procs = {}
        self.conns = {}  # pid -> {conn: first_seen}
        # (role, parent pid) -> (time, count, pid): a replacement has the same
        # role and the same manager, so a new connection to it is a reconnect.
        self.drops = {}
        self.stats = {}  # pid -> [opened, closed] since the last conn.stats
        self.first = True
        self.last_rotate = 0.0
        self.last_stats = time.time()

    def emit(self, event, **fields):
        append_event("service-monitor", event, **fields)

    def tick(self):
        now = time.time()
        jobs = launchd_jobs()
        for label in sorted(set(jobs) | set(self.jobs)):
            old = self.jobs.get(label, (None, None))
            new = jobs.get(label, (None, None))
            if old[0] == new[0]:
                continue
            if old[0] is not None:
                self.emit("service.exit", label=label, pid=old[0], last_exit_status=new[1], loaded=label in jobs)
            if new[0] is not None:
                # The first sample only reports what is already running.
                self.emit("service.running" if self.first else "service.start", label=label, pid=new[0],
                          previous_pid=old[0])
        self.jobs = jobs

        procs = processes()
        for pid in sorted(set(self.procs) - set(procs)):
            role, ppid, program = self.procs[pid]
            open_conns = self.conns.pop(pid, {})
            self.emit("process.exit", role=role, pid=pid, ppid=ppid, program=program, open_connections=len(open_conns))
            if open_conns:
                self.drops[(role, ppid)] = (now, len(open_conns), pid)
                self.emit("conn.drop", role=role, pid=pid, count=len(open_conns), cause="server-exit",
                          ports=sorted({c.split("->")[0].rsplit(":", 1)[-1] for c in open_conns}))
        for pid in sorted(set(procs) - set(self.procs)):
            role, ppid, program = procs[pid]
            parent = procs.get(ppid, (None,))[0] or ("launchd" if ppid == 1 else None)
            self.emit("process.running" if self.first else "process.start", role=role, pid=pid, ppid=ppid,
                      parent_role=parent, program=program)
        self.procs = procs
        self.first = False

        # #920: expire drops older than the reconnect window before they can
        # be consumed below, so a stale entry is discarded, not reported as
        # a reconnect of an unrelated later start.
        self.drops = {key: drop for key, drop in self.drops.items() if now - drop[0] <= RECONNECT_WINDOW_S}

        servers = [pid for pid, (role, _, _) in procs.items() if role != "mcpx-daemon"]
        for pid, (ports, conns) in sockets(servers).items():
            role = procs[pid][0]
            seen = self.conns.setdefault(pid, {})
            opened = conns - set(seen)
            closed = set(seen) - conns
            stats = self.stats.setdefault(pid, [0, 0])
            stats[0] += len(opened)
            stats[1] += len(closed)
            # Short requests come and go every few seconds and only feed
            # conn.stats. A connection that lived LONG_LIVED_S or more is a
            # stream or socket a client keeps open, so its closing is logged.
            lived = sorted(round(now - seen[c]) for c in closed if now - seen[c] >= LONG_LIVED_S)
            if lived:
                self.emit("conn.close", role=role, pid=pid, ports=sorted(ports), count=len(lived),
                          lived_s=[lived[0], lived[-1]], open_now=len(conns))
            for c in opened:
                seen[c] = now
            for c in closed:
                del seen[c]
            key = (role, procs[pid][1])
            drop = self.drops.get(key)
            if drop and opened and pid != drop[2]:
                self.emit("conn.reconnect", role=role, pid=pid, previous_pid=drop[2], gap_s=round(now - drop[0], 1),
                          dropped=drop[1], reconnected=len(opened))
                del self.drops[key]

        if now - self.last_stats >= STATS_EVERY_S:
            for pid, (opened, closed) in sorted(self.stats.items()):
                if (opened or closed) and pid in procs:
                    self.emit("conn.stats", role=procs[pid][0], pid=pid, opened=opened, closed=closed,
                              open_now=len(self.conns.get(pid, {})), window_s=round(now - self.last_stats))
            self.stats = {}
            self.last_stats = now

        if now - self.last_rotate > 60:
            self.last_rotate = now
            for path in ROTATED_LOGS:
                rotated = rotate(os.path.expanduser(path), ROTATE_BYTES, ROTATE_KEEP, copy_truncate=True)
                if rotated:
                    self.emit("log.rotate", path=path, bytes=rotated)


USAGE = "usage: service-monitor"


def main(argv):
    # #1199: the launchd caller passes no arguments, and this command takes
    # none. A stray --help or typo'd flag used to be silently ignored and
    # start a second long-lived monitor writing real events into the shared
    # log. Reject arguments before any work, in the package's house style
    # (service-event: usage at exit 0 for --help, named error at exit 2).
    args = argv[1:]
    if args in (["--help"], ["-h"]):
        print(USAGE)
        return 0
    if args:
        print(
            f"service-monitor: invalid argument {args[0]!r}: this command takes no arguments",
            file=sys.stderr,
        )
        print(USAGE, file=sys.stderr)
        return 2
    # #1200: a log dir that cannot be created (e.g. ~/Library is a regular
    # file) used to kill the monitor with a raw traceback at exit 1 before
    # any monitor.start/monitor.error could exist -- a silent launchd
    # crash-loop. Name the directory and exit 2, like the #918 startup
    # validation.
    try:
        os.makedirs(LOG_DIR, exist_ok=True)
    except OSError as exc:
        print(
            f"service-monitor: cannot create log directory {LOG_DIR}: {exc}",
            file=sys.stderr,
        )
        return 2
    monitor = Monitor()
    append_event("service-monitor", "monitor.start", interval_s=INTERVAL, pid=os.getpid())
    while True:
        try:
            monitor.tick()
        except Exception as exc:  # never die: launchd would only restart us
            append_event("service-monitor", "monitor.error", error=repr(exc)[:300])
        time.sleep(INTERVAL)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
