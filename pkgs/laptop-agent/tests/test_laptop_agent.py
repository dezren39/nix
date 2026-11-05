#!/usr/bin/env python3
"""Tests for laptop-agent startup auth and bounded output capture.

Run with: python3 -m unittest discover -s pkgs/laptop-agent/tests
Wired as the flake check `laptop-agent-tests` (Linux).
"""
import http.client
import json
import os
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

    def test_exec_timeout_not_an_integer(self):
        self._assert_400_then_health(
            "POST", "/exec",
            body={"cmds": [{"argv": [sys.executable, "-c", "pass"]}],
                  "timeout_s": "abc"})

    def test_exec_negative_timeout_is_clamped(self):
        status, body = self._call(
            "POST", "/exec",
            body={"cmds": [{"argv": [sys.executable, "-c", "pass"]}],
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
            body={"argv": [sys.executable, "-c", "pass"]})
        self.assertEqual(status, 200, body)
        job_id = body["job_id"]
        self._assert_400_then_health(
            "GET", f"/jobs/{job_id}?out_off=abc")
        self._assert_400_then_health(
            "GET", f"/jobs/{job_id}?err_off=-1")


if __name__ == "__main__":
    unittest.main()
