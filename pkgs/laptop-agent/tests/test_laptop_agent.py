#!/usr/bin/env python3
"""Tests for laptop-agent startup auth and bounded output capture.

Run with: python3 -m unittest discover -s pkgs/laptop-agent/tests
Wired as the flake check `laptop-agent-tests` (Linux).
"""
import contextlib
import http.client
import io
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from unittest.mock import patch

AGENT = Path(__file__).resolve().parents[1] / "src" / "laptop_agent.py"


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class EmptyTokenTest(unittest.TestCase):
    """PR #1459 codex P1: an empty/whitespace token must fail closed."""

    def _run_with_token(self, text):
        with tempfile.NamedTemporaryFile("w", suffix="-token",
                                         delete=False) as f:
            f.write(text)
            token = f.name
        try:
            env = dict(os.environ,
                       LAPTOP_AGENT_TOKEN_FILE=token,
                       LAPTOP_AGENT_HOST="127.0.0.1",
                       LAPTOP_AGENT_PORT=str(free_port()),
                       LAPTOP_AGENT_STATE=tempfile.mkdtemp())
            return subprocess.run([sys.executable, str(AGENT)], env=env,
                                  capture_output=True, text=True, timeout=20)
        finally:
            os.unlink(token)

    def test_empty_token_refuses_to_start(self):
        p = self._run_with_token("")
        self.assertEqual(p.returncode, 3, p.stderr)
        self.assertIn("refusing to start", p.stderr)

    def test_whitespace_token_refuses_to_start(self):
        p = self._run_with_token("  \n\t\n")
        self.assertEqual(p.returncode, 3, p.stderr)

    def test_missing_token_file_refuses_to_start(self):
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE="/nonexistent/token",
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(free_port()),
                   LAPTOP_AGENT_STATE=tempfile.mkdtemp())
        p = subprocess.run([sys.executable, str(AGENT)], env=env,
                           capture_output=True, text=True, timeout=20)
        self.assertEqual(p.returncode, 3, p.stderr)


class StartupEnvValidationTest(unittest.TestCase):
    """#1510: invalid PORT/SLOTS must exit 3 naming var+value, no traceback."""

    def _run_with_env(self, extra):
        with tempfile.NamedTemporaryFile("w", suffix="-token",
                                         delete=False) as f:
            f.write("test-token\n")
            token = f.name
        try:
            env = dict(os.environ,
                       LAPTOP_AGENT_TOKEN_FILE=token,
                       LAPTOP_AGENT_HOST="127.0.0.1",
                       LAPTOP_AGENT_PORT=str(free_port()),
                       LAPTOP_AGENT_STATE=tempfile.mkdtemp())
            env.update(extra)
            return subprocess.run([sys.executable, str(AGENT)], env=env,
                                  capture_output=True, text=True, timeout=20)
        finally:
            os.unlink(token)

    def test_invalid_port_refuses_to_start(self):
        for value in ("abc", "0", "99999", "-1"):
            with self.subTest(LAPTOP_AGENT_PORT=value):
                p = self._run_with_env({"LAPTOP_AGENT_PORT": value})
                self.assertEqual(p.returncode, 3, p.stderr)
                self.assertIn("invalid LAPTOP_AGENT_PORT=", p.stderr)
                self.assertIn(repr(value), p.stderr)
                self.assertNotIn("Traceback", p.stderr)

    def test_invalid_slots_refuses_to_start(self):
        for value in ("abc", "0", "-1"):
            with self.subTest(LAPTOP_AGENT_SLOTS=value):
                p = self._run_with_env({"LAPTOP_AGENT_SLOTS": value})
                self.assertEqual(p.returncode, 3, p.stderr)
                self.assertIn("invalid LAPTOP_AGENT_SLOTS=", p.stderr)
                self.assertIn(repr(value), p.stderr)
                self.assertNotIn("Traceback", p.stderr)


class AuthHeaderTest(unittest.TestCase):
    """A request without the header must be rejected, not treated as ""."""

    def test_missing_header_rejected(self):
        token = "s" * 32
        with tempfile.NamedTemporaryFile("w", delete=False) as f:
            f.write(token + "\n")
            token_path = f.name
        port = free_port()
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE=token_path,
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(port),
                   LAPTOP_AGENT_STATE=tempfile.mkdtemp())
        proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.time() + 10
            while time.time() < deadline:
                try:
                    urllib.request.urlopen(
                        f"http://127.0.0.1:{port}/health", timeout=1)
                except urllib.error.HTTPError as e:
                    self.assertEqual(e.code, 401)
                    break
                except (urllib.error.URLError, ConnectionError):
                    time.sleep(0.1)
            else:
                self.fail("agent did not start")
            req = urllib.request.Request(
                f"http://127.0.0.1:{port}/health",
                headers={"X-Agent-Token": token})
            with urllib.request.urlopen(req, timeout=5) as r:
                self.assertEqual(r.status, 200)
                self.assertTrue(json.loads(r.read())["ok"])
        finally:
            proc.terminate()
            proc.wait(timeout=10)
            os.unlink(token_path)


class TailFileTest(unittest.TestCase):
    def test_job_public_samples_state_before_reading_spools(self):
        with tempfile.NamedTemporaryFile("w", suffix="-token",
                                         delete=False) as f:
            f.write("tail-test-token\n")
            token_path = f.name
        os.environ.setdefault("LAPTOP_AGENT_TOKEN_FILE", token_path)
        sys.path.insert(0, str(AGENT.parent))
        import laptop_agent as la

        with tempfile.TemporaryDirectory() as state_dir:
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                job_id = "job-public-race"
                spool = la.job_dir(job_id)
                spool.mkdir(parents=True)
                (spool / "stdout").write_bytes(b"")
                (spool / "stderr").write_bytes(b"")
                job = {"id": job_id, "state": "running", "proc": None,
                       "exit": None, "cwd": "/", "cmd_repr": "test",
                       "started_at": time.time(), "ended_at": None,
                       "interrupted": False}

                def finish_job(current):
                    (spool / "stdout").write_bytes(b"final output")
                    current["state"] = "done"
                    current["exit"] = 0
                    current["ended_at"] = time.time()
                    return "done"

                with patch.object(la, "job_state", side_effect=finish_job) as state:
                    result = la.job_public(job)

        state.assert_called_once_with(job)
        self.assertEqual(result["state"], "done")
        self.assertEqual(result["stdout"], "final output")

    def test_tail_caps_and_flags(self):
        with tempfile.NamedTemporaryFile("w", delete=False) as t:
            t.write("tail-test-token\n")
            token_path = t.name
        os.environ.setdefault("LAPTOP_AGENT_TOKEN_FILE", token_path)
        sys.path.insert(0, str(AGENT.parent))
        import laptop_agent as la
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(b"x" * 100 + b"END")
            path = Path(f.name)
        try:
            data, cut = la.tail_file(path, 10)
            self.assertEqual(data, b"xxxxxxxEND")
            self.assertTrue(cut)
            data, cut = la.tail_file(path, 1000)
            self.assertEqual(len(data), 103)
            self.assertFalse(cut)
        finally:
            os.unlink(path)



class JobsListShapeTest(unittest.TestCase):
    """Issue #1503: GET /jobs is a summary; output lives on /jobs/<id>.

    Oracle: the module docstring's documented shapes -- the list route
    documents {job_id, state, exit, ...} with no output fields, while
    the single-job route documents stdout/stderr plus paging cursors.
    """

    def _import_agent(self):
        with tempfile.NamedTemporaryFile("w", suffix="-token",
                                         delete=False) as f:
            f.write("jobs-list-test-token\n")
            token_path = f.name
        os.environ.setdefault("LAPTOP_AGENT_TOKEN_FILE", token_path)
        sys.path.insert(0, str(AGENT.parent))
        import laptop_agent as la
        return la

    def test_summary_has_no_output_but_single_job_shape_does(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                job_id = "job-list-shape"
                spool = la.job_dir(job_id)
                spool.mkdir(parents=True)
                (spool / "stdout").write_bytes(b"known stdout payload")
                (spool / "stderr").write_bytes(b"known stderr payload")
                job = {"id": job_id, "state": "done", "proc": None,
                       "exit": 0, "cwd": "/", "cmd_repr": "test",
                       "started_at": time.time(), "ended_at": time.time(),
                       "interrupted": False}
                summary = la.job_summary(job)
                full = la.job_public(job)
        for key in ("job_id", "state", "exit", "cwd", "cmd",
                    "started_at", "duration_ms", "interrupted",
                    "out_next", "err_next"):
            self.assertIn(key, summary)
        self.assertNotIn("stdout", summary)
        self.assertNotIn("stderr", summary)
        self.assertNotIn("known stdout payload", json.dumps(summary))
        self.assertNotIn("known stderr payload", json.dumps(summary))
        self.assertEqual(summary["out_next"], len(b"known stdout payload"))
        self.assertEqual(summary["err_next"], len(b"known stderr payload"))
        self.assertEqual(full["stdout"], "known stdout payload")
        self.assertEqual(full["stderr"], "known stderr payload")

    def test_list_route_returns_summary_single_route_returns_output(self):
        token = "jobs-list-http-token"
        with tempfile.NamedTemporaryFile("w", delete=False) as f:
            f.write(token + "\n")
            token_path = f.name
        port = free_port()
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE=token_path,
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(port),
                   LAPTOP_AGENT_STATE=tempfile.mkdtemp())
        proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE)
        headers = {"X-Agent-Token": token,
                   "Content-Type": "application/json"}

        def call(method, path, body=None):
            data = json.dumps(body).encode() if body is not None else None
            req = urllib.request.Request(
                f"http://127.0.0.1:{port}{path}", data=data,
                headers=headers, method=method)
            with urllib.request.urlopen(req, timeout=5) as r:
                return json.loads(r.read())

        try:
            deadline = time.time() + 10
            while True:
                try:
                    if call("GET", "/health")["ok"]:
                        break
                except Exception:
                    if time.time() > deadline:
                        raise
                    time.sleep(0.1)
            job_id = call("POST", "/jobs", {
                "cwd": "/",
                "argv": [sys.executable, "-c",
                         "import sys; sys.stdout.write('k' * 64); "
                         "sys.stderr.write('e' * 64)"]})["job_id"]
            deadline = time.time() + 10
            single = None
            while time.time() < deadline:
                single = call("GET", f"/jobs/{job_id}")
                if single["state"] == "done":
                    break
                time.sleep(0.1)
            self.assertEqual(single["state"], "done")
            self.assertEqual(single["stdout"], "k" * 64)
            self.assertEqual(single["stderr"], "e" * 64)
            listing = call("GET", "/jobs")
            entry = [j for j in listing["jobs"] if j["job_id"] == job_id]
            self.assertEqual(len(entry), 1)
            self.assertNotIn("stdout", entry[0])
            self.assertNotIn("stderr", entry[0])
            self.assertNotIn("k" * 64, json.dumps(entry[0]))
            self.assertNotIn("e" * 64, json.dumps(entry[0]))
            self.assertEqual(entry[0]["out_next"], 64)
            self.assertEqual(entry[0]["err_next"], 64)
        finally:
            proc.terminate()
            proc.wait(timeout=10)
            os.unlink(token_path)



class JobSlotsTest(unittest.TestCase):
    """Issue #1545: POST /jobs shares the LAPTOP_AGENT_SLOTS pool.

    Oracle: with SLOTS=1 and one long-running job held, a second
    POST /jobs gets 503 busy (after the bounded 5s wait, like /exec)
    instead of running concurrently, /exec is likewise busy (shared
    pool), and once the held job ends its slot is released so a new
    job is accepted again.
    """

    def test_second_job_rejected_while_slot_held_then_accepted(self):
        token = "jobs-slots-test-token"
        with tempfile.NamedTemporaryFile("w", delete=False) as f:
            f.write(token + "\n")
            token_path = f.name
        port = free_port()
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE=token_path,
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(port),
                   LAPTOP_AGENT_SLOTS="1",
                   LAPTOP_AGENT_STATE=tempfile.mkdtemp())
        proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE)
        headers = {"X-Agent-Token": token,
                   "Content-Type": "application/json"}

        def call(method, path, body=None):
            data = json.dumps(body).encode() if body is not None else None
            req = urllib.request.Request(
                f"http://127.0.0.1:{port}{path}", data=data,
                headers=headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=20) as r:
                    return r.status, json.loads(r.read())
            except urllib.error.HTTPError as e:
                return e.code, json.loads(e.read())

        try:
            deadline = time.time() + 10
            while True:
                try:
                    status, body = call("GET", "/health")
                    if status == 200 and body["ok"]:
                        break
                except Exception:
                    if time.time() > deadline:
                        raise
                    time.sleep(0.1)
            status, body = call("POST", "/jobs", {
                "cwd": "/",
                "argv": [sys.executable, "-c",
                         "import time; time.sleep(30)"]})
            self.assertEqual(status, 200, body)
            job_id = body["job_id"]
            # The single slot is held for the job's lifetime: a second
            # job must not run concurrently.
            status, body = call("POST", "/jobs", {
                "cwd": "/", "argv": [sys.executable, "-c", "pass"]})
            self.assertEqual(status, 503, body)
            self.assertEqual(body.get("error"), "busy")
            listing_status, listing = call("GET", "/jobs")
            self.assertEqual(listing_status, 200, listing)
            running = [j for j in listing["jobs"]
                       if j["state"] == "running"]
            self.assertEqual([j["job_id"] for j in running], [job_id])
            # Shared pool: /exec is busy too while the job holds the slot.
            status, body = call("POST", "/exec", {
                "cmds": [{"argv": [sys.executable, "-c", "pass"]}]})
            self.assertEqual(status, 503, body)
            # Killing the held job releases the slot for new work.
            status, body = call("POST", f"/jobs/{job_id}/kill")
            self.assertEqual(status, 200, body)
            deadline = time.time() + 15
            while time.time() < deadline:
                status, body = call("POST", "/jobs", {
                    "cwd": "/", "argv": [sys.executable, "-c", "pass"]})
                if status == 200:
                    break
                time.sleep(0.2)
            self.assertEqual(status, 200, body)
        finally:
            proc.terminate()
            proc.wait(timeout=10)
            os.unlink(token_path)

    def test_spool_creation_failure_returns_400_and_releases_slot(self):
        token = "jobs-spool-failure-test-token"
        with tempfile.NamedTemporaryFile("w", delete=False) as f:
            f.write(token + "\n")
            token_path = f.name
        state_dir = tempfile.mkdtemp()
        port = free_port()
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE=token_path,
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(port),
                   LAPTOP_AGENT_SLOTS="1",
                   LAPTOP_AGENT_STATE=state_dir)
        proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE)
        headers = {"X-Agent-Token": token,
                   "Content-Type": "application/json"}

        def call(method, path, body=None):
            data = json.dumps(body).encode() if body is not None else None
            req = urllib.request.Request(
                f"http://127.0.0.1:{port}{path}", data=data,
                headers=headers, method=method)
            try:
                with urllib.request.urlopen(req, timeout=20) as r:
                    return r.status, json.loads(r.read())
            except urllib.error.HTTPError as e:
                with e:
                    return e.code, json.loads(e.read())

        try:
            deadline = time.time() + 10
            while True:
                try:
                    status, body = call("GET", "/health")
                    if status == 200 and body["ok"]:
                        break
                except Exception:
                    if time.time() > deadline:
                        raise
                    time.sleep(0.1)

            # Replace the initialized spool parent after startup.  mkdir for
            # the job's directory now fails with ENOTDIR, after slot acquire.
            jobs_dir = Path(state_dir) / "jobs"
            jobs_dir.rmdir()
            jobs_dir.write_text("not a directory")
            status, body = call("POST", "/jobs", {
                "cwd": "/", "argv": [sys.executable, "-c", "pass"]})
            self.assertEqual(status, 400, body)
            self.assertIn("error", body)

            jobs_dir.unlink()
            jobs_dir.mkdir()
            status, body = call("POST", "/jobs", {
                "cwd": "/", "argv": [sys.executable, "-c", "pass"]})
            self.assertEqual(status, 200, body)
            job_id = body["job_id"]
            deadline = time.time() + 10
            while time.time() < deadline:
                status, job = call("GET", f"/jobs/{job_id}")
                if status == 200 and job.get("state") == "done":
                    break
                time.sleep(0.05)
            self.assertEqual(status, 200, job)
            self.assertEqual(job.get("state"), "done", job)

            status, body = call("POST", "/exec", {
                "cmds": [{"argv": [sys.executable, "-c", "pass"]}]})
            self.assertEqual(status, 200, body)
        finally:
            proc.terminate()
            proc.wait(timeout=10)
            proc.stdout.close()
            proc.stderr.close()
            os.unlink(token_path)
            shutil.rmtree(state_dir, ignore_errors=True)


class MalformedRequestTest(unittest.TestCase):
    """Issue #1502: malformed input returns 400 JSON, never a drop.

    Oracle: for every malformed shape, the response is a well-formed
    JSON body carrying an "error" key with status 400, AND a
    subsequent authed GET /health on the same daemon returns 200 --
    the invariant is that arbitrary body/query bytes can never kill
    the handler or wedge the connection.
    """

    def setUp(self):
        self.token = "<MASKED_SECRET_001>"
        with tempfile.NamedTemporaryFile("w", delete=False) as f:
            f.write(self.token + "\n")
            self.token_path = f.name
        self.port = free_port()
        env = dict(os.environ,
                   LAPTOP_AGENT_TOKEN_FILE=self.token_path,
                   LAPTOP_AGENT_HOST="127.0.0.1",
                   LAPTOP_AGENT_PORT=str(self.port),
                   LAPTOP_AGENT_STATE=tempfile.mkdtemp())
        self.proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                     stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE)
        deadline = time.time() + 10
        while time.time() < deadline:
            try:
                status, body = self._call("GET", "/health")
                if status == 200 and body.get("ok"):
                    return
            except Exception:
                time.sleep(0.1)
        self.tearDown()
        self.fail("agent did not start")

    def tearDown(self):
        self.proc.terminate()
        self.proc.wait(timeout=10)
        os.unlink(self.token_path)

    def _conn(self):
        return http.client.HTTPConnection("127.0.0.1", self.port, timeout=5)

    def _call(self, method, path, body=None, raw=None, conn=None):
        c = conn or self._conn()
        headers = {"X-Agent-Token": self.token,
                   "Content-Type": "application/json"}
        data = raw if raw is not None else (
            json.dumps(body).encode() if body is not None else None)
        c.request(method, path, body=data, headers=headers)
        r = c.getresponse()
        payload = r.read()
        if conn is None:
            c.close()
        try:
            parsed = json.loads(payload) if payload else {}
        except ValueError:
            parsed = {}
        return r.status, parsed

    def _assert_400_then_health(self, method, path, **kw):
        status, body = self._call(method, path, **kw)
        self.assertEqual(status, 400, body)
        self.assertIn("error", body)
        status, body = self._call("GET", "/health")
        self.assertEqual(status, 200)
        self.assertTrue(body["ok"])

    def test_invalid_json_body(self):
        self._assert_400_then_health("POST", "/exec", raw=b"{not json")

    def test_non_object_json_body(self):
        self._assert_400_then_health("POST", "/exec", raw=b"[1, 2]")

    def test_bad_content_length_header(self):
        conn = self._conn()
        conn.putrequest("POST", "/exec")
        conn.putheader("X-Agent-Token", self.token)
        conn.putheader("Content-Type", "application/json")
        conn.putheader("Content-Length", "abc")
        conn.endheaders()
        r = conn.getresponse()
        body = json.loads(r.read())
        self.assertEqual(r.status, 400, body)
        self.assertIn("error", body)
        status, body = self._call("GET", "/health")
        self.assertEqual(status, 200)
        self.assertTrue(body["ok"])

    def test_bad_content_length_with_body_closes_connection(self):
        # Codex P2 on PR #1508: unread body bytes after a framing 400
        # would otherwise be parsed as the next request. Oracle: the
        # 400 carries Connection: close, and a fresh connection's
        # authed /health still returns 200.
        conn = self._conn()
        conn.putrequest("POST", "/exec")
        conn.putheader("X-Agent-Token", self.token)
        conn.putheader("Content-Type", "application/json")
        conn.putheader("Content-Length", "abc")
        conn.endheaders(b"{}")
        r = conn.getresponse()
        body = json.loads(r.read())
        self.assertEqual(r.status, 400, body)
        self.assertIn("error", body)
        self.assertEqual(r.getheader("Connection"), "close")
        conn.close()
        status, body = self._call("GET", "/health")
        self.assertEqual(status, 200)
        self.assertTrue(body["ok"])

    def test_exec_timeout_not_an_integer(self):
        self._assert_400_then_health(
            "POST", "/exec",
            body={"cmds": [{"argv": [sys.executable, "-c", "pass"]}],
                  "timeout_s": "abc"})

    def test_exec_negative_timeout_is_clamped(self):
        status, body = self._call(
            "POST", "/exec",
            body={"cmds": [{"cwd": os.getcwd(),
                             "argv": [sys.executable, "-c", "pass"]}],
                  "timeout_s": -5})
        self.assertEqual(status, 200, body)
        self.assertEqual(body["results"][0]["exit"], 0)
        self.assertNotIn("timeout", body["results"][0])

    def test_exec_cmd_without_argv_or_shell(self):
        self._assert_400_then_health(
            "POST", "/exec", body={"cmds": [{"cwd": "/"}]})

    def test_jobs_without_argv_or_shell(self):
        self._assert_400_then_health("POST", "/jobs", body={})

    def test_jobs_argv_not_a_list(self):
        self._assert_400_then_health(
            "POST", "/jobs", body={"argv": "echo hi"})

    def test_job_query_offsets_malformed(self):
        status, body = self._call(
            "POST", "/jobs",
            body={"cwd": os.getcwd(),
                  "argv": [sys.executable, "-c", "pass"]})
        self.assertEqual(status, 200, body)
        job_id = body["job_id"]
        self._assert_400_then_health(
            "GET", f"/jobs/{job_id}?out_off=abc")
        self._assert_400_then_health(
            "GET", f"/jobs/{job_id}?err_off=-1")


class PersistedJobTimestampTest(unittest.TestCase):
    """Issue #1513: non-numeric persisted timestamps must not crash
    startup or poison /jobs reads.

    Oracle: a spool dir whose meta.json timestamps are malformed
    (string incl. numeric string, bool, list, object) is skipped
    whole -- exactly one stderr line naming its dir, nothing added
    to JOBS, spool left on disk -- while missing/None keep today's
    defaults and well-formed jobs restore exactly as before,
    including the running->interrupted conversion.
    """

    def _import_agent(self):
        with tempfile.NamedTemporaryFile("w", suffix="-token",
                                         delete=False) as f:
            f.write("persisted-ts-test-token\n")
            token_path = f.name
        os.environ.setdefault("LAPTOP_AGENT_TOKEN_FILE", token_path)
        sys.path.insert(0, str(AGENT.parent))
        import laptop_agent as la
        return la

    def _write_job(self, state_dir, job_id, **meta):
        spool = Path(state_dir) / "jobs" / job_id
        spool.mkdir(parents=True)
        meta.setdefault("id", job_id)
        meta.setdefault("cwd", "/")
        meta.setdefault("cmd", "test")
        meta.setdefault("exit", 0)
        meta.setdefault("state", "done")
        (spool / "meta.json").write_text(json.dumps(meta))
        (spool / "stdout").write_bytes(b"")
        (spool / "stderr").write_bytes(b"")
        return spool

    def _load(self, la, state_dir):
        la.JOBS.clear()
        err = io.StringIO()
        with patch.object(la, "STATE_DIR", Path(state_dir)):
            with contextlib.redirect_stderr(err):
                la.load_persisted_jobs()
        return err.getvalue()

    def tearDown(self):
        la_mod = sys.modules.get("laptop_agent")
        if la_mod is not None:
            la_mod.JOBS.clear()

    def test_started_at_string_skipped_healthy_kept_stderr_names_dir(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            bad = self._write_job(state_dir, "bad-started",
                                  started_at="not-a-number",
                                  ended_at=200.0)
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertNotIn("bad-started", la.JOBS)
            self.assertIn("healthy", la.JOBS)
            self.assertTrue(bad.exists())
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 1, stderr)
            self.assertIn(str(bad), lines[0])
            self.assertIn("started_at", lines[0])

    def test_ended_at_string_skipped_summary_and_reaper_succeed(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            bad = self._write_job(state_dir, "bad-ended",
                                  started_at=100.0, ended_at="never")
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertNotIn("bad-ended", la.JOBS)
            self.assertIn("healthy", la.JOBS)
            self.assertTrue(bad.exists())
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 1, stderr)
            self.assertIn(str(bad), lines[0])
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                summaries = [la.job_summary(j)
                             for j in list(la.JOBS.values())]
                la.reap_old_jobs()
            self.assertEqual([s["job_id"] for s in summaries],
                             ["healthy"])

    def test_done_null_ended_at_stamped_and_reapable(self):
        # Issue #1532: a restored done job with ended_at=null kept the
        # null, so the reaper read it as "now" (spool retained forever)
        # and duration_ms grew with wall-clock time. Oracle: at load it
        # gets a finite stamp, duration is stable across reads, and
        # the reaper removes it (index + spool) once past TTL.
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            now = time.time()
            spool = self._write_job(state_dir, "done-null-ended",
                                    started_at=now - 48 * 3600,
                                    ended_at=None)
            stderr = self._load(la, state_dir)
            self.assertEqual(stderr, "")
            job = la.JOBS["done-null-ended"]
            self.assertIsInstance(job["ended_at"], float)
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                first = la.job_summary(job)["duration_ms"]
                second = la.job_summary(job)["duration_ms"]
                self.assertEqual(first, second)
                with patch.object(la, "JOB_TTL_S", -1):
                    la.reap_old_jobs()
            self.assertNotIn("done-null-ended", la.JOBS)
            self.assertFalse(spool.exists())
    def test_non_object_meta_skipped_healthy_kept(self):
        # Issue #1522: meta.json that is valid JSON but not an object
        # gets the same skip contract as malformed timestamps.
        la = self._import_agent()
        shapes = [("[1, 2, 3]", "list-meta"), ('"hello"', "string-meta"),
                  ("42", "number-meta"), ("true", "bool-meta"),
                  ("null", "null-meta")]
        for raw, job_id in shapes:
            with self.subTest(shape=raw):
                with tempfile.TemporaryDirectory() as state_dir:
                    spool = Path(state_dir) / "jobs" / job_id
                    spool.mkdir(parents=True)
                    (spool / "meta.json").write_text(raw)
                    (spool / "stdout").write_bytes(b"")
                    (spool / "stderr").write_bytes(b"")
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertIn("healthy", la.JOBS)
                    self.assertTrue(spool.exists())
                    lines = [ln for ln in stderr.splitlines() if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(str(spool), lines[0])

    def test_other_malformed_shapes_are_skipped(self):
        la = self._import_agent()
        cases = [
            ("bool-started", {"started_at": True, "ended_at": 150.0}),
            ("list-ended", {"started_at": 100.0, "ended_at": [150.0]}),
            ("numeric-string", {"started_at": "123", "ended_at": 150.0}),
            ("object-started", {"started_at": {"t": 1},
                                "ended_at": 150.0}),
        ]
        for job_id, stamps in cases:
            with self.subTest(job_id=job_id):
                with tempfile.TemporaryDirectory() as state_dir:
                    self._write_job(state_dir, job_id, **stamps)
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertIn("healthy", la.JOBS)
                    lines = [ln for ln in stderr.splitlines()
                             if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(job_id, lines[0])

    def test_nonfinite_timestamps_are_skipped(self):
        la = self._import_agent()
        cases = [
            ("nan-started", {"started_at": float("nan"),
                             "ended_at": 150.0}),
            ("nan-ended", {"started_at": 100.0,
                           "ended_at": float("nan")}),
            ("inf-started", {"started_at": float("inf"),
                             "ended_at": 150.0}),
            ("neg-inf-started", {"started_at": float("-inf"),
                                 "ended_at": 150.0}),
            ("inf-ended", {"started_at": 100.0,
                           "ended_at": float("inf")}),
            ("neg-inf-ended", {"started_at": 100.0,
                               "ended_at": float("-inf")}),
        ]
        for job_id, stamps in cases:
            with self.subTest(job_id=job_id):
                with tempfile.TemporaryDirectory() as state_dir:
                    bad = self._write_job(state_dir, job_id, **stamps)
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertIn("healthy", la.JOBS)
                    self.assertTrue(bad.exists())
                    lines = [ln for ln in stderr.splitlines()
                             if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(str(bad), lines[0])

    def test_oversized_int_timestamps_are_skipped(self):
        # Codex review on PR #1534: math.isfinite(10**400) raises
        # OverflowError (int too large to convert to float) instead
        # of returning False, so an oversized integer timestamp must
        # be skipped whole like the other malformed shapes, not
        # escape load_persisted_jobs() and abort startup.
        la = self._import_agent()
        cases = [
            ("huge-started", {"started_at": 10**400, "ended_at": 150.0}),
            ("huge-ended", {"started_at": 100.0, "ended_at": 10**400}),
        ]
        for job_id, stamps in cases:
            with self.subTest(job_id=job_id):
                with tempfile.TemporaryDirectory() as state_dir:
                    bad = self._write_job(state_dir, job_id, **stamps)
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertIn("healthy", la.JOBS)
                    self.assertTrue(bad.exists())
                    lines = [ln for ln in stderr.splitlines()
                             if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(str(bad), lines[0])

    def test_nonfinite_skipped_summary_and_public_succeed(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            self._write_job(state_dir, "nan-started",
                            started_at=float("nan"), ended_at=150.0)
            self._write_job(state_dir, "inf-ended",
                            started_at=100.0, ended_at=float("inf"))
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertEqual(sorted(la.JOBS), ["healthy"])
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 2, stderr)
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                summaries = [la.job_summary(j)
                             for j in list(la.JOBS.values())]
                publics = [la.job_public(j)
                           for j in list(la.JOBS.values())]
                la.reap_old_jobs()
            self.assertEqual([s["job_id"] for s in summaries],
                             ["healthy"])
            self.assertEqual(summaries[0]["duration_ms"], 50000)
            self.assertEqual([p["job_id"] for p in publics],
                             ["healthy"])
            self.assertEqual(publics[0]["duration_ms"], 50000)

    def test_well_formed_restore_unchanged_and_running_interrupted(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            self._write_job(state_dir, "done-job",
                            started_at=100.0, ended_at=150.5)
            self._write_job(state_dir, "running-job", state="running",
                            exit=None, started_at=100.0, ended_at=None)
            self._write_job(state_dir, "no-stamps")
            stderr = self._load(la, state_dir)
            self.assertEqual(stderr, "")
            done = la.JOBS["done-job"]
            self.assertEqual(done["started_at"], 100.0)
            self.assertEqual(done["ended_at"], 150.5)
            self.assertEqual(done["state"], "done")
            self.assertFalse(done["interrupted"])
            running = la.JOBS["running-job"]
            self.assertEqual(running["state"], "done")
            self.assertTrue(running["interrupted"])
            self.assertIsInstance(running["ended_at"], float)
            self.assertEqual(running["started_at"], 100.0)
            bare = la.JOBS["no-stamps"]
            self.assertEqual(bare["started_at"], 0.0)
            # #1532 supersedes the #1513 default for a done job: a
            # null/missing ended_at gets a finite load-time stamp.
            self.assertIsInstance(bare["ended_at"], float)

    def test_non_string_id_skipped_healthy_kept(self):
        # Issue #1548: a persisted meta id that is not a string must
        # not crash startup (list/object: unhashable at the JOBS
        # assignment) or poison /jobs and the reaper (number/bool:
        # TypeError at job_dir). Oracle: same skip contract as
        # #1513/#1522 -- skipped whole, exactly one stderr line naming
        # its dir, spool left on disk, healthy siblings unchanged.
        la = self._import_agent()
        cases = [
            ("numeric-id", 42),
            ("zero-id", 0),
            ("bool-id", True),
            ("false-id", False),
            ("list-id", ["a", "b"]),
            ("object-id", {"a": 1}),
        ]
        for job_id, bad_id in cases:
            with self.subTest(job_id=job_id, bad_id=repr(bad_id)):
                with tempfile.TemporaryDirectory() as state_dir:
                    bad = self._write_job(state_dir, job_id, id=bad_id,
                                          started_at=100.0,
                                          ended_at=150.0)
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertEqual(sorted(la.JOBS), ["healthy"])
                    self.assertTrue(bad.exists())
                    lines = [ln for ln in stderr.splitlines()
                             if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(str(bad), lines[0])
                    self.assertIn("id", lines[0])

    def test_non_string_id_running_entry_skipped_no_meta_write(self):
        # A running entry is the _write_meta crash vector: restore
        # converts it to interrupted and _write_meta calls
        # job_dir(job["id"]) before its try block. A non-string id
        # must be skipped before any of that, spool untouched.
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            bad = self._write_job(state_dir, "running-bad-id", id=42,
                                  state="running", exit=None,
                                  started_at=100.0, ended_at=None)
            before = (bad / "meta.json").read_text()
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertEqual(sorted(la.JOBS), ["healthy"])
            self.assertEqual((bad / "meta.json").read_text(), before)
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 1, stderr)

    def test_non_string_id_summary_public_and_reaper_succeed(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            self._write_job(state_dir, "list-id-job", id=[1, 2],
                            started_at=100.0, ended_at=150.0)
            self._write_job(state_dir, "numeric-id-job", id=7,
                            started_at=100.0, ended_at=150.0)
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertEqual(sorted(la.JOBS), ["healthy"])
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 2, stderr)
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                summaries = [la.job_summary(j)
                             for j in list(la.JOBS.values())]
                publics = [la.job_public(j)
                           for j in list(la.JOBS.values())]
                la.reap_old_jobs()
            self.assertEqual([s["job_id"] for s in summaries],
                             ["healthy"])
            self.assertEqual([p["job_id"] for p in publics],
                             ["healthy"])

    def test_missing_null_empty_id_falls_back_to_dir_name(self):
        # Acceptance #1548: missing/null/empty-string id keeps the
        # current fallback to the spool directory name.
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            self._write_job(state_dir, "null-id", id=None,
                            started_at=100.0, ended_at=150.0)
            self._write_job(state_dir, "empty-id", id="",
                            started_at=100.0, ended_at=150.0)
            missing = self._write_job(state_dir, "missing-id",
                                      started_at=100.0, ended_at=150.0)
            meta = json.loads((missing / "meta.json").read_text())
            del meta["id"]
            (missing / "meta.json").write_text(json.dumps(meta))
            stderr = self._load(la, state_dir)
            self.assertEqual(stderr, "")
            self.assertEqual(sorted(la.JOBS),
                             ["empty-id", "missing-id", "null-id"])

    def test_parent_string_id_is_skipped_without_reaping_outside_jobs(self):
        # Issue #1554: a persisted traversal id must not escape the jobs
        # directory through the public serializers or startup reaper.
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            state = Path(state_dir)
            bad = self._write_job(
                state_dir, "bad-parent-id", id="..",
                started_at=100.0, ended_at=time.time() - 2 * la.JOB_TTL_S)
            healthy = self._write_job(
                state_dir, "healthy", started_at=100.0, ended_at=time.time())
            sentinel = state / "keep-me"
            sentinel.write_text("outside the jobs directory")

            stderr = self._load(la, state_dir)
            self.assertEqual(sorted(la.JOBS), ["healthy"])
            self.assertTrue(bad.exists())
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 1, stderr)
            self.assertIn(str(bad), lines[0])
            self.assertIn("id", lines[0])

            with patch.object(la, "STATE_DIR", state):
                summaries = [la.job_summary(j)
                             for j in list(la.JOBS.values())]
                publics = [la.job_public(j)
                           for j in list(la.JOBS.values())]
                la.reap_old_jobs()

            self.assertEqual([s["job_id"] for s in summaries], ["healthy"])
            self.assertEqual([p["job_id"] for p in publics], ["healthy"])
            self.assertTrue(sentinel.exists())
            self.assertTrue(healthy.exists())
            self.assertTrue(bad.exists())

    def test_separator_and_cross_job_ids_are_skipped(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            bad_separator = self._write_job(
                state_dir, "bad-separator", id="../escape", state="running",
                exit=None, started_at=100.0, ended_at=None)
            bad_separator_meta = (bad_separator / "meta.json").read_text()
            cross_job = self._write_job(
                state_dir, "cross-job", id="healthy",
                started_at=100.0, ended_at=time.time() - 2 * la.JOB_TTL_S)
            healthy = self._write_job(
                state_dir, "healthy", started_at=100.0, ended_at=time.time())

            stderr = self._load(la, state_dir)
            self.assertEqual(sorted(la.JOBS), ["healthy"])
            self.assertEqual(la.JOBS["healthy"]["cmd_repr"], "test")
            self.assertTrue(bad_separator.exists())
            self.assertEqual((bad_separator / "meta.json").read_text(),
                             bad_separator_meta)
            self.assertFalse((Path(state_dir) / "escape").exists())
            self.assertTrue(cross_job.exists())
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 2, stderr)
            self.assertTrue(all(any(str(spool) in line for line in lines)
                                for spool in (bad_separator, cross_job)),
                            stderr)

            with patch.object(la, "STATE_DIR", Path(state_dir)):
                la.reap_old_jobs()
            self.assertTrue(bad_separator.exists())
            self.assertTrue(cross_job.exists())
            self.assertTrue(healthy.exists())

    def test_job_dir_rejects_non_child_and_symlink_paths(self):
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            state = Path(state_dir)
            jobs = state / "jobs"
            jobs.mkdir()
            outside = state / "outside"
            outside.mkdir()
            (jobs / "linked").symlink_to(outside, target_is_directory=True)
            with patch.object(la, "STATE_DIR", state):
                for job_id in ("..", ".", "../outside", "nested/job",
                               "nested\\job", "linked"):
                    with self.subTest(job_id=job_id):
                        with self.assertRaises(ValueError):
                            la.job_dir(job_id)

    def test_persisted_interrupted_true_round_trips_two_loads(self):
        # Issue #1556: _write_meta persists interrupted:true for a
        # restart-interrupted done job, but the restore hardcoded
        # interrupted=False, so a second restart dropped the marker.
        # Oracle: the flag restores faithfully and round-trips
        # unchanged through _write_meta across repeated loads.
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            self._write_job(state_dir, "interrupted-job",
                            interrupted=True, exit=None,
                            started_at=100.0, ended_at=150.0)
            self._write_job(state_dir, "plain-job",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertEqual(stderr, "")
            job = la.JOBS["interrupted-job"]
            self.assertTrue(job["interrupted"])
            self.assertEqual(job["state"], "done")
            with patch.object(la, "STATE_DIR", Path(state_dir)):
                self.assertTrue(la.job_public(job)["interrupted"])
                la._write_meta(job)
                meta = json.loads((Path(state_dir) / "jobs"
                                   / "interrupted-job"
                                   / "meta.json").read_text())
            self.assertIs(meta["interrupted"], True)
            stderr = self._load(la, state_dir)
            self.assertEqual(stderr, "")
            self.assertTrue(la.JOBS["interrupted-job"]["interrupted"])
            self.assertFalse(la.JOBS["plain-job"]["interrupted"])

    def test_invalid_state_skipped_healthy_kept(self):
        # Issue #1556: a persisted state outside {running, done} was
        # restored verbatim, so /jobs served a nonexistent state and
        # the recorded exit code was suppressed. Oracle: same skip
        # contract as #1513/#1522/#1548 -- skipped whole, exactly one
        # stderr line naming its dir, spool left on disk, healthy
        # siblings unchanged.
        la = self._import_agent()
        cases = [
            ("bogus-state", "bogus-state"),
            ("numeric-state", 7),
            ("list-state", ["done"]),
            ("bool-state", True),
        ]
        for job_id, bad_state in cases:
            with self.subTest(job_id=job_id, bad_state=repr(bad_state)):
                with tempfile.TemporaryDirectory() as state_dir:
                    bad = self._write_job(state_dir, job_id,
                                          state=bad_state,
                                          started_at=100.0,
                                          ended_at=150.0)
                    self._write_job(state_dir, "healthy",
                                    started_at=100.0, ended_at=150.0)
                    stderr = self._load(la, state_dir)
                    self.assertNotIn(job_id, la.JOBS)
                    self.assertEqual(sorted(la.JOBS), ["healthy"])
                    self.assertTrue(bad.exists())
                    lines = [ln for ln in stderr.splitlines() if ln.strip()]
                    self.assertEqual(len(lines), 1, stderr)
                    self.assertIn(str(bad), lines[0])
                    self.assertIn("state", lines[0])

    def test_missing_null_state_defaults_done_and_non_bool_interrupted_skipped(self):
        # Issue #1556 acceptance: missing/null state keeps the
        # established "done" default, while a non-boolean interrupted
        # follows the loader's malformed-field handling (skip whole).
        la = self._import_agent()
        with tempfile.TemporaryDirectory() as state_dir:
            missing = self._write_job(state_dir, "missing-state",
                                      started_at=100.0, ended_at=150.0)
            meta = json.loads((missing / "meta.json").read_text())
            del meta["state"]
            (missing / "meta.json").write_text(json.dumps(meta))
            self._write_job(state_dir, "null-state", state=None,
                            started_at=100.0, ended_at=150.0)
            bad = self._write_job(state_dir, "string-interrupted",
                                  interrupted="yes",
                                  started_at=100.0, ended_at=150.0)
            self._write_job(state_dir, "healthy",
                            started_at=100.0, ended_at=150.0)
            stderr = self._load(la, state_dir)
            self.assertEqual(la.JOBS["missing-state"]["state"], "done")
            self.assertEqual(la.JOBS["null-state"]["state"], "done")
            self.assertNotIn("string-interrupted", la.JOBS)
            self.assertTrue(bad.exists())
            lines = [ln for ln in stderr.splitlines() if ln.strip()]
            self.assertEqual(len(lines), 1, stderr)
            self.assertIn(str(bad), lines[0])
            self.assertIn("interrupted", lines[0])

class ReaperStartupTest(unittest.TestCase):
    """#1504: past-TTL finished jobs are reaped at startup, with no
    new POST /jobs — the daemon is only ever health-polled here.

    Oracle: a persisted done job whose ended_at is past the 24h TTL
    loses both its index entry and its spool dir at startup, while a
    within-TTL done job and a restored (interrupted) job survive.
    """

    def _write_job(self, state_dir, job_id, **meta):
        spool = Path(state_dir) / "jobs" / job_id
        spool.mkdir(parents=True)
        meta.setdefault("id", job_id)
        meta.setdefault("cwd", "/")
        meta.setdefault("cmd", "test")
        meta.setdefault("exit", 0)
        meta.setdefault("state", "done")
        (spool / "meta.json").write_text(json.dumps(meta))
        (spool / "stdout").write_bytes(b"")
        (spool / "stderr").write_bytes(b"")
        return spool

    def test_past_ttl_restored_job_reaped_at_startup(self):
        now = time.time()
        with tempfile.TemporaryDirectory() as state_dir:
            old = self._write_job(state_dir, "old-done",
                                  started_at=now - 26 * 3600,
                                  ended_at=now - 25 * 3600)
            fresh = self._write_job(state_dir, "fresh-done",
                                    started_at=now - 7200,
                                    ended_at=now - 3600)
            running = self._write_job(state_dir, "was-running",
                                      state="running", exit=None,
                                      started_at=now - 26 * 3600,
                                      ended_at=None)
            with tempfile.NamedTemporaryFile("w", delete=False) as f:
                f.write("reaper-test-token\n")
                token_path = f.name
            port = free_port()
            env = dict(os.environ,
                       LAPTOP_AGENT_TOKEN_FILE=token_path,
                       LAPTOP_AGENT_HOST="127.0.0.1",
                       LAPTOP_AGENT_PORT=str(port),
                       LAPTOP_AGENT_STATE=state_dir)
            proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                    stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE)
            try:
                req = urllib.request.Request(
                    f"http://127.0.0.1:{port}/jobs",
                    headers={"X-Agent-Token": "reaper-test-token"})
                deadline = time.time() + 15
                jobs = None
                while time.time() < deadline:
                    try:
                        with urllib.request.urlopen(req, timeout=2) as r:
                            jobs = json.loads(r.read())["jobs"]
                        break
                    except (urllib.error.URLError, ConnectionError):
                        time.sleep(0.1)
                else:
                    self.fail("agent did not start")
                ids = {j["job_id"] for j in jobs}
                self.assertEqual(ids, {"fresh-done", "was-running"})
                self.assertFalse(old.exists())
                self.assertTrue(fresh.exists())
                self.assertTrue(running.exists())
            finally:
                proc.terminate()
                proc.wait(timeout=10)
                os.unlink(token_path)

    def test_unsafe_persisted_ids_do_not_escape_jobs_at_startup(self):
        now = time.time()
        with tempfile.TemporaryDirectory() as state_dir:
            state = Path(state_dir)
            bad_parent = self._write_job(
                state_dir, "bad-parent", id="..", started_at=now - 26 * 3600,
                ended_at=now - 25 * 3600)
            bad_separator = self._write_job(
                state_dir, "bad-separator", id="a/b",
                started_at=now - 26 * 3600, ended_at=now - 25 * 3600)
            cross_job = self._write_job(
                state_dir, "cross-job", id="healthy",
                started_at=now - 26 * 3600, ended_at=now - 25 * 3600)
            healthy = self._write_job(
                state_dir, "healthy", started_at=now - 3600,
                ended_at=now - 1800)
            sentinel = state / "keep-me"
            sentinel.write_text("state outside jobs must survive")

            with tempfile.NamedTemporaryFile("w", delete=False) as f:
                f.write("reaper-test-token\n")
                token_path = f.name
            port = free_port()
            env = dict(os.environ,
                       LAPTOP_AGENT_TOKEN_FILE=token_path,
                       LAPTOP_AGENT_HOST="127.0.0.1",
                       LAPTOP_AGENT_PORT=str(port),
                       LAPTOP_AGENT_STATE=state_dir)
            proc = subprocess.Popen([sys.executable, str(AGENT)], env=env,
                                    stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL)
            try:
                req = urllib.request.Request(
                    f"http://127.0.0.1:{port}/jobs",
                    headers={"X-Agent-Token": "reaper-test-token"})
                deadline = time.time() + 15
                jobs = None
                while time.time() < deadline:
                    try:
                        with urllib.request.urlopen(req, timeout=2) as r:
                            jobs = json.loads(r.read())["jobs"]
                        break
                    except (urllib.error.URLError, ConnectionError):
                        time.sleep(0.1)
                else:
                    self.fail("agent did not start")

                self.assertEqual({j["job_id"] for j in jobs}, {"healthy"})
                self.assertEqual(jobs[0]["cmd"], "test")
                self.assertTrue(sentinel.exists())
                self.assertTrue(healthy.exists())
                self.assertTrue(bad_parent.exists())
                self.assertTrue(bad_separator.exists())
                self.assertTrue(cross_job.exists())
            finally:
                proc.terminate()
                proc.wait(timeout=10)
                os.unlink(token_path)


if __name__ == "__main__":
    unittest.main()
