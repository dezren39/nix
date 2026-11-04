#!/usr/bin/env python3
"""laptop-agent daemon — resident execution agent on Drew's laptop.

Design: developing-today/code#5343. Single file, Python stdlib only.
Binds the tailnet interface only; every request needs the X-Agent-Token
header (token in ~/.config/laptop-agent/token, mode 600).

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
daemon restart does not lose results. Finished jobs reaped after 24h.
"""
import hmac
import json
import os
import secrets
import signal
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse, parse_qs

BIND_HOST = os.environ.get("LAPTOP_AGENT_HOST", "100.99.113.103")
BIND_PORT = int(os.environ.get("LAPTOP_AGENT_PORT", "4799"))
TOKEN_FILE = Path(os.environ.get(
    "LAPTOP_AGENT_TOKEN_FILE", Path.home() / ".config/laptop-agent/token"))
STATE_DIR = Path(os.environ.get(
    "LAPTOP_AGENT_STATE", Path.home() / ".local/state/laptop-agent"))
MAX_OUT = 1 << 20            # per-stream capture cap for /exec (tail kept)
EXEC_TIMEOUT_DEFAULT = 120
EXEC_TIMEOUT_MAX = 600
JOB_TTL_S = 24 * 3600
SEMA = threading.Semaphore(int(os.environ.get("LAPTOP_AGENT_SLOTS", "32")))
STARTED = time.time()

TOKEN = TOKEN_FILE.read_text().strip()
JOBS = {}                    # job_id -> dict
JOBS_LOCK = threading.Lock()


def tail_bytes(data: bytes, cap: int):
    if len(data) <= cap:
        return data, False
    return data[-cap:], True


def run_one(spec, timeout_s):
    cwd = spec.get("cwd") or str(Path.home())
    env = dict(os.environ)
    env.update(spec.get("env") or {})
    if spec.get("shell") is not None:
        argv = ["/bin/bash", "-lc", spec["shell"]]
    else:
        argv = list(spec["argv"])
    t0 = time.time()
    try:
        p = subprocess.run(argv, cwd=cwd, env=env, capture_output=True,
                           timeout=timeout_s)
        out, out_tr = tail_bytes(p.stdout, MAX_OUT)
        err, err_tr = tail_bytes(p.stderr, MAX_OUT)
        return {"exit": p.returncode,
                "stdout": out.decode("utf-8", "replace"),
                "stderr": err.decode("utf-8", "replace"),
                "duration_ms": int((time.time() - t0) * 1000),
                "truncated": out_tr or err_tr}
    except subprocess.TimeoutExpired as e:
        return {"exit": None, "timeout": True,
                "stdout": (e.stdout or b"").decode("utf-8", "replace"),
                "stderr": (e.stderr or b"").decode("utf-8", "replace"),
                "duration_ms": int((time.time() - t0) * 1000),
                "truncated": False}
    except Exception as e:  # cwd missing, argv[0] missing, ...
        return {"exit": None, "error": f"{type(e).__name__}: {e}",
                "stdout": "", "stderr": "",
                "duration_ms": int((time.time() - t0) * 1000),
                "truncated": False}


def job_dir(job_id):
    return STATE_DIR / "jobs" / job_id


def job_state(job):
    if job["proc"] is not None and job["proc"].poll() is None:
        return "running"
    return "done"


def job_public(job, out_off=0, err_off=0):
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
    state = job_state(job)
    return {"job_id": job["id"], "state": state,
            "exit": (job["proc"].returncode if state == "done"
                     and job["proc"] is not None else None),
            "cwd": job["cwd"], "cmd": job["cmd_repr"],
            "started_at": job["started_at"],
            "duration_ms": int(((job["ended_at"] or time.time())
                                - job["started_at"]) * 1000),
            "stdout": out.decode("utf-8", "replace"),
            "stderr": err.decode("utf-8", "replace"),
            "out_next": out_off + len(out), "err_next": err_off + len(err)}


def reap_old_jobs():
    now = time.time()
    with JOBS_LOCK:
        dead = [j for j in JOBS.values()
                if job_state(j) == "done"
                and (j["ended_at"] or now) < now - JOB_TTL_S]
        for j in dead:
            JOBS.pop(j["id"], None)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):  # quiet; journal gets tracebacks only
        pass

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _authed(self):
        given = self.headers.get("X-Agent-Token", "")
        return hmac.compare_digest(given, TOKEN)

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b"{}"
        return json.loads(raw or b"{}")

    def do_GET(self):
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
                items = [job_public(j) for j in JOBS.values()]
            return self._send(200, {"jobs": items})
        if path.path.startswith("/jobs/"):
            job_id = path.path.split("/")[2]
            q = parse_qs(path.query)
            with JOBS_LOCK:
                job = JOBS.get(job_id)
            if job is None:
                return self._send(404, {"error": "no such job"})
            return self._send(200, job_public(
                job, int(q.get("out_off", [0])[0]),
                int(q.get("err_off", [0])[0])))
        return self._send(404, {"error": "no such route"})

    def do_POST(self):
        if not self._authed():
            return self._send(401, {"error": "bad token"})
        path = urlparse(self.path).path
        if path == "/exec":
            body = self._body()
            timeout = min(int(body.get("timeout_s", EXEC_TIMEOUT_DEFAULT)),
                          EXEC_TIMEOUT_MAX)
            cmds = body.get("cmds") or []
            if not isinstance(cmds, list) or not cmds:
                return self._send(400, {"error": "cmds required"})
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
            if body.get("shell") is not None:
                argv = ["/bin/bash", "-lc", body["shell"]]
                cmd_repr = body["shell"]
            else:
                argv = list(body["argv"])
                cmd_repr = " ".join(argv)
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
                return self._send(400, {"error": f"{type(e).__name__}: {e}"})
            job = {"id": job_id, "proc": proc, "cwd": cwd,
                   "cmd_repr": cmd_repr, "started_at": time.time(),
                   "ended_at": None, "_files": (out_f, err_f)}
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
            threading.Thread(target=watch, daemon=True).start()
            reap_old_jobs()
            return self._send(200, {"job_id": job_id})
        if path.startswith("/jobs/") and path.endswith("/kill"):
            job_id = path.split("/")[2]
            with JOBS_LOCK:
                job = JOBS.get(job_id)
            if job is None:
                return self._send(404, {"error": "no such job"})
            if job_state(job) == "running":
                try:
                    os.killpg(job["proc"].pid, signal.SIGTERM)
                except OSError:
                    pass

                def hard_kill(j=job):
                    time.sleep(5)
                    if j["proc"].poll() is None:
                        try:
                            os.killpg(j["proc"].pid, signal.SIGKILL)
                        except OSError:
                            pass
                threading.Thread(target=hard_kill, daemon=True).start()
            return self._send(200, {"ok": True, "job_id": job_id})
        return self._send(404, {"error": "no such route"})


def main():
    (STATE_DIR / "jobs").mkdir(parents=True, exist_ok=True)
    srv = ThreadingHTTPServer((BIND_HOST, BIND_PORT), Handler)
    print(f"laptop-agent listening on {BIND_HOST}:{BIND_PORT}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
