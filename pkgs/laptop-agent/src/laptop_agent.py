#!/usr/bin/env python3
"""laptop-agent daemon — resident execution agent on Drew's laptop.

Design: developing-today/code#5343. Single file, Python stdlib only.
Binds the tailnet interface only; every request needs the X-Agent-Token
header (token in ~/.config/laptop-agent/token, mode 600). Startup fails
closed when that token is missing, empty, or whitespace-only.

API (JSON):
  GET  /health                    -> {ok, uptime_s, jobs, load1}
  POST /exec   {cmds:[{cwd, argv|shell}], timeout_s?}
                                  -> {results:[{exit, stdout, stderr,
                                      duration_ms, truncated}]}
  POST /jobs   {cwd, argv|shell, env?} -> {job_id}
  GET  /jobs                      -> {jobs:[{job_id, state, exit, ...}]}
  GET  /jobs/<id>?out_off&err_off -> {state, exit, stdout, stderr,
                                      out_next, err_next, duration_ms}
  POST /jobs/<id>/kill            -> {ok, state}

Job stdout/stderr spool to ~/.local/state/laptop-agent/jobs/<id>/ so a
daemon restart does not lose results: job metadata is persisted next to
the output and the index is rebuilt on startup. Finished jobs reaped
after 24h, spool directory included.
"""
import hmac
import json
import math
import os
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse, parse_qs


def _parse_startup_int(name, raw, requirement, minimum, maximum=None):
    """Validate an integer startup env var, or exit 3 naming it (#1510).

    A bare int() at import crashed with a raw ValueError traceback for
    typos, and out-of-range values either tracebacked later at bind
    (PORT=99999), silently bound an ephemeral port (PORT=0), or started
    a healthy-looking daemon that could never run work (SLOTS=0).
    Fail at startup instead, in the read_token house style.
    """
    try:
        value = int(raw)
    except (TypeError, ValueError):
        value = None
    if (value is None or value < minimum
            or (maximum is not None and value > maximum)):
        print(f"laptop-agent: invalid {name}={raw!r}: {requirement}",
              file=sys.stderr, flush=True)
        sys.exit(3)
    return value


BIND_HOST = os.environ.get("LAPTOP_AGENT_HOST", "100.99.113.103")
BIND_PORT = _parse_startup_int(
    "LAPTOP_AGENT_PORT", os.environ.get("LAPTOP_AGENT_PORT", "4799"),
    "must be an integer 1-65535", 1, 65535)
# Absolute interpreter for `shell` requests: NixOS has no /bin/bash, and
# the package wrapper sets this to the store path.
BASH = os.environ.get("LAPTOP_AGENT_BASH", "/bin/bash")
TOKEN_FILE = Path(os.environ.get(
    "LAPTOP_AGENT_TOKEN_FILE", Path.home() / ".config/laptop-agent/token"))
STATE_DIR = Path(os.environ.get(
    "LAPTOP_AGENT_STATE", Path.home() / ".local/state/laptop-agent"))
MAX_OUT = 1 << 20            # per-stream capture cap for /exec (tail kept)
EXEC_TIMEOUT_DEFAULT = 120
EXEC_TIMEOUT_MAX = 600
JOB_TTL_S = 24 * 3600
REAP_INTERVAL_S = 3600       # background reaper cadence (#1504)
KILL_GRACE_S = 5
SEMA = threading.Semaphore(_parse_startup_int(
    "LAPTOP_AGENT_SLOTS", os.environ.get("LAPTOP_AGENT_SLOTS", "32"),
    "must be an integer >= 1", 1))
STARTED = time.time()

JOBS = {}                    # job_id -> dict
JOBS_LOCK = threading.Lock()


def read_token(path: Path) -> str:
    """Return the agent token, or exit non-zero before binding a listener.

    An empty file previously left TOKEN == "" so a request without the
    header (also "") authenticated successfully (codex P1 on PR #1459).
    """
    try:
        token = path.read_text().strip()
    except OSError as e:
        print(f"laptop-agent: cannot read token file {path}: {e}",
              file=sys.stderr, flush=True)
        sys.exit(3)
    if not token:
        print(f"laptop-agent: token file {path} is empty; refusing to start",
              file=sys.stderr, flush=True)
        sys.exit(3)
    return token


TOKEN = read_token(TOKEN_FILE)


def tail_bytes(data: bytes, cap: int):
    if len(data) <= cap:
        return data, False
    return data[-cap:], True


def tail_file(path: Path, cap: int = MAX_OUT):
    """Return the last `cap` bytes of a spool file and whether it was cut.

    Never reads the whole file into memory, so a noisy command cannot
    exhaust the daemon.
    """
    try:
        with open(path, "rb") as f:
            f.seek(0, os.SEEK_END)
            size = f.tell()
            start = max(0, size - cap)
            f.seek(start)
            data = f.read(cap)
        return data, start > 0
    except OSError:
        return b"", False


def _kill_group(pid: int, sig: int):
    try:
        os.killpg(pid, sig)
    except OSError:
        pass


def _group_alive(pid: int) -> bool:
    try:
        os.killpg(pid, 0)
        return True
    except OSError:
        return False


def terminate_group(pid: int):
    """SIGTERM the group, then SIGKILL it if anything still lives.

    The group is checked independently of the leader: the leader can exit
    while a SIGTERM-ignoring child keeps running (codex finding on PR
    #1459), so the second phase must not be keyed on the leader's poll().
    """
    _kill_group(pid, signal.SIGTERM)
    deadline = time.time() + KILL_GRACE_S
    while time.time() < deadline:
        if not _group_alive(pid):
            return
        time.sleep(0.1)
    if _group_alive(pid):
        _kill_group(pid, signal.SIGKILL)


def run_one(spec, timeout_s):
    cwd = spec.get("cwd") or str(Path.home())
    if not isinstance(spec, dict):
        raise ValueError("command spec must be an object")
    if spec.get("env") is not None and not isinstance(spec.get("env"), dict):
        raise ValueError("command env must be an object")
    env = dict(os.environ)
    env.update(spec.get("env") or {})
    if spec.get("shell") is not None:
        argv = [BASH, "-lc", spec["shell"]]
    elif isinstance(spec.get("argv"), (list, tuple)) and spec["argv"]:
        argv = list(spec["argv"])
    else:
        raise ValueError("command spec requires 'argv' or 'shell'")
    t0 = time.time()
    tmpdir = Path(tempfile.mkdtemp(prefix="exec-", dir=str(STATE_DIR / "exec")))
    out_path, err_path = tmpdir / "stdout", tmpdir / "stderr"
    try:
        with open(out_path, "wb") as out_f, open(err_path, "wb") as err_f:
            try:
                p = subprocess.Popen(argv, cwd=cwd, env=env,
                                     stdout=out_f, stderr=err_f,
                                     start_new_session=True)
            except Exception as e:  # cwd missing, argv[0] missing, ...
                return {"exit": None, "error": f"{type(e).__name__}: {e}",
                        "stdout": "", "stderr": "",
                        "duration_ms": int((time.time() - t0) * 1000),
                        "truncated": False}
            timed_out = False
            try:
                p.wait(timeout=timeout_s)
            except subprocess.TimeoutExpired:
                timed_out = True
                terminate_group(p.pid)
                try:
                    p.wait(timeout=KILL_GRACE_S)
                except subprocess.TimeoutExpired:
                    pass
        out, out_tr = tail_file(out_path)
        err, err_tr = tail_file(err_path)
        result = {"exit": None if timed_out else p.returncode,
                  "stdout": out.decode("utf-8", "replace"),
                  "stderr": err.decode("utf-8", "replace"),
                  "duration_ms": int((time.time() - t0) * 1000),
                  "truncated": out_tr or err_tr}
        if timed_out:
            result["timeout"] = True
        return result
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)


def job_dir(job_id):
    return STATE_DIR / "jobs" / job_id


def _write_meta(job):
    meta = {"id": job["id"], "cwd": job["cwd"], "cmd": job["cmd_repr"],
            "started_at": job["started_at"], "ended_at": job["ended_at"],
            "exit": job["exit"], "state": job["state"],
            "interrupted": job.get("interrupted", False)}
    d = job_dir(job["id"])
    try:
        d.mkdir(parents=True, exist_ok=True)
        tmp = d / "meta.json.tmp"
        tmp.write_text(json.dumps(meta))
        os.replace(tmp, d / "meta.json")
    except OSError:
        pass


def load_persisted_jobs():
    """Rebuild JOBS from the spool directory so job IDs survive restarts."""
    base = STATE_DIR / "jobs"
    try:
        entries = list(base.iterdir())
    except OSError:
        return
    for d in entries:
        if not d.is_dir():
            continue
        try:
            meta = json.loads((d / "meta.json").read_text())
        except (OSError, ValueError):
            continue
        # Issue #1513: validate timestamps on the raw meta before any
        # consumer (float() conversion, running->interrupted, summary,
        # reaper) touches them. Valid is missing/None (defaults apply)
        # or a finite int/float that is not a bool (bool is an int
        # subclass, so float(True) == 1.0 would otherwise slip through).
        # Strings (even numeric ones), lists, objects and bools are
        # malformed, as are non-finite floats (issue #1523: json.loads
        # parses the NaN/Infinity/-Infinity literals to float, and
        # int() duration arithmetic raises on them): skip the whole
        # entry like an unreadable/invalid-JSON dir, naming the dir
        # on stderr, and leave the spool on disk.
        def _bad_ts(value):
            if value is None:
                return False
            if isinstance(value, bool) or not isinstance(value, (int, float)):
                return True
            try:
                return not math.isfinite(value)
            except OverflowError:
                # Oversized int (e.g. 10**400 from a JSON integer):
                # isfinite() converts to float and raises instead of
                # returning False. Such a timestamp is malformed for
                # every consumer, so skip the entry rather than
                # aborting startup (Codex review on PR #1534).
                return True

        _bad_fields = [name for name in ("started_at", "ended_at")
                       if _bad_ts(meta.get(name))]
        if _bad_fields:
            print(f"laptop-agent: skipping persisted job {d}: "
                  f"bad {'/'.join(_bad_fields)} in meta.json",
                  file=sys.stderr, flush=True)
            continue
        job_id = meta.get("id") or d.name
        state = meta.get("state") or "done"
        exit_code = meta.get("exit")
        ended_at = meta.get("ended_at")
        interrupted = False
        if state == "running":
            # The process did not survive the restart: nobody can poll it.
            state = "done"
            exit_code = None
            interrupted = True
            # A finite end time is what lets the reaper apply TTL to the
            # restored job's spool; leaving it null retains it forever.
            ended_at = time.time()
        JOBS[job_id] = {"id": job_id, "proc": None,
                        "cwd": meta.get("cwd") or str(Path.home()),
                        "cmd_repr": meta.get("cmd") or "",
                        "started_at": float(meta.get("started_at") or 0.0),
                        "ended_at": ended_at,
                        "exit": exit_code, "state": state,
                        "interrupted": interrupted, "_files": ()}
        if interrupted:
            _write_meta(JOBS[job_id])


def job_state(job):
    if job["state"] == "running":
        proc = job["proc"]
        if proc is not None and proc.poll() is None:
            return "running"
        job["state"] = "done"
        if proc is not None:
            job["exit"] = proc.returncode
        if job["ended_at"] is None:
            job["ended_at"] = time.time()
        _write_meta(job)
    return job["state"]


def job_public(job, out_off=0, err_off=0):
    state = job_state(job)
    d = job_dir(job["id"])
    out = b""
    err = b""
    try:
        with open(d / "stdout", "rb") as f:
            f.seek(out_off)
            out = f.read(MAX_OUT)
    except OSError:
        pass
    try:
        with open(d / "stderr", "rb") as f:
            f.seek(err_off)
            err = f.read(MAX_OUT)
    except OSError:
        pass
    return {"job_id": job["id"], "state": state,
            "exit": job["exit"] if state == "done" else None,
            "cwd": job["cwd"], "cmd": job["cmd_repr"],
            "started_at": job["started_at"],
            "interrupted": job.get("interrupted", False),
            "duration_ms": int(((job["ended_at"] or time.time())
                                - job["started_at"]) * 1000),
            "stdout": out.decode("utf-8", "replace"),
            "stderr": err.decode("utf-8", "replace"),
            "out_next": out_off + len(out), "err_next": err_off + len(err)}


def job_summary(job):
    """Summary serializer for the GET /jobs list route.

    Returns the documented list shape (docstring above): identity,
    state, and spool sizes/cursors only.  Unlike job_public it never
    reads or embeds stdout/stderr payloads, so polling the list costs
    O(jobs), not O(jobs x 2 x MAX_OUT).  out_next/err_next are the
    spool sizes, i.e. the offsets a client would page from in the
    single-job route.
    """
    state = job_state(job)
    d = job_dir(job["id"])
    try:
        out_size = (d / "stdout").stat().st_size
    except OSError:
        out_size = 0
    try:
        err_size = (d / "stderr").stat().st_size
    except OSError:
        err_size = 0
    return {"job_id": job["id"], "state": state,
            "exit": job["exit"] if state == "done" else None,
            "cwd": job["cwd"], "cmd": job["cmd_repr"],
            "started_at": job["started_at"],
            "interrupted": job.get("interrupted", False),
            "duration_ms": int(((job["ended_at"] or time.time())
                                - job["started_at"]) * 1000),
            "out_bytes": out_size, "err_bytes": err_size,
            "out_next": out_size, "err_next": err_size}


def reap_old_jobs():
    now = time.time()
    with JOBS_LOCK:
        dead = []
        for j in list(JOBS.values()):
            if job_state(j) == "done" and (j["ended_at"] or now) < now - JOB_TTL_S:
                dead.append(j)
        for j in dead:
            JOBS.pop(j["id"], None)
    for j in dead:
        # Output is uncapped, so retention must remove the spool too.
        shutil.rmtree(job_dir(j["id"]), ignore_errors=True)


class FramingError(ValueError):
    """Invalid request framing: the next request boundary is unknown,
    so the connection must be closed after the 400 response."""


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):  # quiet; journal gets tracebacks only
        pass

    def _send(self, code, obj, close=False):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        if close:
            self.send_header("Connection", "close")
            self.close_connection = True
        self.end_headers()
        self.wfile.write(body)

    def _authed(self):
        given = self.headers.get("X-Agent-Token") or ""
        if not given:
            return False
        return hmac.compare_digest(given, TOKEN)

    def _body(self):
        try:
            n = int(self.headers.get("Content-Length") or 0)
        except (TypeError, ValueError):
            raise FramingError("invalid Content-Length header")
        if n < 0:
            raise FramingError("invalid Content-Length header")
        raw = self.rfile.read(n) if n else b"{}"
        try:
            body = json.loads(raw or b"{}")
        except ValueError:
            raise ValueError("request body is not valid JSON")
        if not isinstance(body, dict):
            raise ValueError("request body must be a JSON object")
        return body

    def _send_bad_request(self, e):
        try:
            self._send(400, {"error": str(e) or "bad request"},
                       close=isinstance(e, FramingError))
        except Exception:
            pass

    def do_GET(self):
        try:
            self._do_GET()
        except (ValueError, KeyError, TypeError, AttributeError,
                IndexError) as e:
            self._send_bad_request(e)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def _do_GET(self):
        if not self._authed():
            return self._send(401, {"error": "bad token"})
        path = urlparse(self.path)
        if path.path == "/health":
            with JOBS_LOCK:
                n = len(JOBS)
            return self._send(200, {"ok": True,
                                    "uptime_s": int(time.time() - STARTED),
                                    "jobs": n, "load1": os.getloadavg()[0]})
        if path.path == "/jobs":
            with JOBS_LOCK:
                items = [job_summary(j) for j in JOBS.values()]
            return self._send(200, {"jobs": items})
        if path.path.startswith("/jobs/"):
            job_id = path.path.split("/")[2]
            q = parse_qs(path.query)
            with JOBS_LOCK:
                job = JOBS.get(job_id)
            if job is None:
                return self._send(404, {"error": "no such job"})
            try:
                out_off = int(q.get("out_off", [0])[0])
                err_off = int(q.get("err_off", [0])[0])
            except (TypeError, ValueError):
                raise ValueError("out_off/err_off must be integers")
            if out_off < 0 or err_off < 0:
                raise ValueError("out_off/err_off must be >= 0")
            return self._send(200, job_public(job, out_off, err_off))
        return self._send(404, {"error": "no such route"})

    def do_POST(self):
        try:
            self._do_POST()
        except (ValueError, KeyError, TypeError, AttributeError,
                IndexError) as e:
            self._send_bad_request(e)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def _do_POST(self):
        if not self._authed():
            return self._send(401, {"error": "bad token"})
        path = urlparse(self.path).path
        if path == "/exec":
            body = self._body()
            try:
                timeout = int(body.get("timeout_s", EXEC_TIMEOUT_DEFAULT))
            except (TypeError, ValueError):
                return self._send(400,
                                  {"error": "timeout_s must be an integer"})
            timeout = max(1, min(timeout, EXEC_TIMEOUT_MAX))
            cmds = body.get("cmds") or []
            if not isinstance(cmds, list) or not cmds:
                return self._send(400, {"error": "cmds required"})
            for c in cmds:
                if not isinstance(c, dict) or (
                        c.get("shell") is None and not (
                            isinstance(c.get("argv"), (list, tuple))
                            and c["argv"])):
                    return self._send(
                        400, {"error": "each cmd requires 'argv' or 'shell'"})
                if (c.get("env") is not None
                        and not isinstance(c.get("env"), dict)):
                    return self._send(
                        400, {"error": "cmd env must be an object"})
            if not SEMA.acquire(timeout=5):
                return self._send(503, {"error": "busy"})
            try:
                results = [run_one(c, timeout) for c in cmds]
            finally:
                SEMA.release()
            return self._send(200, {"results": results})
        if path == "/jobs":
            body = self._body()
            cwd = body.get("cwd") or str(Path.home())
            if body.get("env") is not None and not isinstance(
                    body.get("env"), dict):
                return self._send(400, {"error": "env must be an object"})
            if body.get("shell") is not None:
                argv = [BASH, "-lc", body["shell"]]
                cmd_repr = body["shell"]
            elif isinstance(body.get("argv"), (list, tuple)) and body["argv"]:
                argv = list(body["argv"])
                cmd_repr = " ".join(str(a) for a in argv)
            else:
                return self._send(400, {"error": "argv or shell required"})
            job_id = secrets.token_hex(6)
            d = job_dir(job_id)
            d.mkdir(parents=True, exist_ok=True)
            env = dict(os.environ)
            env.update(body.get("env") or {})
            out_f = open(d / "stdout", "wb")
            err_f = open(d / "stderr", "wb")
            try:
                proc = subprocess.Popen(argv, cwd=cwd, env=env,
                                        stdout=out_f, stderr=err_f,
                                        start_new_session=True)
            except Exception as e:
                for f in (out_f, err_f):
                    f.close()
                shutil.rmtree(d, ignore_errors=True)
                return self._send(400, {"error": f"{type(e).__name__}: {e}"})
            job = {"id": job_id, "proc": proc, "cwd": cwd,
                   "cmd_repr": cmd_repr, "started_at": time.time(),
                   "ended_at": None, "exit": None, "state": "running",
                   "interrupted": False, "_files": (out_f, err_f)}
            _write_meta(job)
            with JOBS_LOCK:
                JOBS[job_id] = job

            def watch(j=job):
                j["proc"].wait()
                j["ended_at"] = time.time()
                for f in j["_files"]:
                    try:
                        f.close()
                    except OSError:
                        pass
                job_state(j)
            threading.Thread(target=watch, daemon=True).start()
            reap_old_jobs()
            return self._send(200, {"job_id": job_id})
        if path.startswith("/jobs/") and path.endswith("/kill"):
            job_id = path.split("/")[2]
            with JOBS_LOCK:
                job = JOBS.get(job_id)
            if job is None:
                return self._send(404, {"error": "no such job"})
            if job_state(job) == "running" and job["proc"] is not None:
                pid = job["proc"].pid
                _kill_group(pid, signal.SIGTERM)

                def hard_kill(p=pid):
                    deadline = time.time() + KILL_GRACE_S
                    while time.time() < deadline:
                        if not _group_alive(p):
                            return
                        time.sleep(0.1)
                    if _group_alive(p):
                        _kill_group(p, signal.SIGKILL)
                threading.Thread(target=hard_kill, daemon=True).start()
            return self._send(200, {"ok": True, "job_id": job_id})
        return self._send(404, {"error": "no such route"})


def _reaper_loop():
    """Reap on a timer so the TTL fires without a new job (#1504).

    reap_old_jobs() used to run only from the POST /jobs path, so a
    daemon serving only /exec + /health + job polls never reaped and
    finished jobs' uncapped spool dirs accumulated forever.
    """
    while True:
        time.sleep(REAP_INTERVAL_S)
        try:
            reap_old_jobs()
        except Exception as e:  # a reaper fault must not kill the daemon
            print(f"laptop-agent: background reap failed: {e}",
                  file=sys.stderr, flush=True)


def main():
    (STATE_DIR / "jobs").mkdir(parents=True, exist_ok=True)
    (STATE_DIR / "exec").mkdir(parents=True, exist_ok=True)
    load_persisted_jobs()
    reap_old_jobs()
    threading.Thread(target=_reaper_loop, daemon=True).start()
    srv = ThreadingHTTPServer((BIND_HOST, BIND_PORT), Handler)
    print(f"laptop-agent listening on {BIND_HOST}:{BIND_PORT}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
