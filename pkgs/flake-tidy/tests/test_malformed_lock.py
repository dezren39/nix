"""Malformed flake.lock handling (#1234, #1237, #1243, #1251).

load_lock() must validate the lock shape and raise LockLoadError (mapped
by main() to a clean exit-2 error naming flake.lock) instead of letting
KeyError/TypeError/JSONDecodeError tracebacks escape at exit 1.
"""

import json
import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


def _write_flake_dir(tmp_path, lock_text):
    (tmp_path / "flake.nix").write_text('{ inputs = {}; outputs = _: {}; }\n')
    (tmp_path / "flake.lock").write_text(lock_text)
    cfg = tmp_path / "cfg.json"
    cfg.write_text(json.dumps({}))
    return str(tmp_path), str(cfg)


def _run_main(monkeypatch, capsys, flake_dir, cfg, extra_args):
    """Invoke main() with the given CLI args; return (exit_code, out, err)."""
    from unittest.mock import patch

    argv = ["flake-tidy", *extra_args, "--config", cfg, "--flake-dir", flake_dir]
    monkeypatch.setattr(sys, "argv", argv)
    with patch.object(flake_tidy, "run_nixfmt", return_value=None):
        with pytest.raises(SystemExit) as excinfo:
            flake_tidy.main()
    out, err = capsys.readouterr()
    return excinfo.value.code, out, err


class TestLoadLockValidation:
    def test_invalid_json_raises(self, tmp_path):
        d = tmp_path / "bad"
        d.mkdir()
        (d / "flake.lock").write_text("not json{{{")
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*not valid JSON"):
            flake_tidy.load_lock(str(d))

    def test_non_utf8_raises(self, tmp_path):
        d = tmp_path / "nonutf8"
        d.mkdir()
        (d / "flake.lock").write_bytes(b'{"nodes": {}}' + b"\xff\xfe")
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*not valid UTF-8"):
            flake_tidy.load_lock(str(d))

    def test_missing_nodes_raises(self, tmp_path):
        d = tmp_path / "missing"
        d.mkdir()
        (d / "flake.lock").write_text(json.dumps({}))
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*missing.*'nodes'"):
            flake_tidy.load_lock(str(d))

    @pytest.mark.parametrize("shape", ["[]", '"str"', "42", "null"])
    def test_non_object_top_level_raises(self, tmp_path, shape):
        d = tmp_path / "nonobj"
        d.mkdir()
        (d / "flake.lock").write_text(shape)
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*must contain a JSON object"):
            flake_tidy.load_lock(str(d))

    @pytest.mark.parametrize("nodes", [["a"], "str", 42, None])
    def test_non_object_nodes_raises(self, tmp_path, nodes):
        d = tmp_path / "badnodes"
        d.mkdir()
        (d / "flake.lock").write_text(json.dumps({"nodes": nodes}))
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*'nodes'"):
            flake_tidy.load_lock(str(d))

    @pytest.mark.parametrize("root", [42, ["root"], {"name": "root"}])
    def test_non_string_root_raises(self, tmp_path, root):
        d = tmp_path / "badroot"
        d.mkdir()
        (d / "flake.lock").write_text(json.dumps({"nodes": {}, "root": root}))
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*'root'"):
            flake_tidy.load_lock(str(d))

    def test_well_formed_lock_loads(self, tmp_path):
        d = tmp_path / "good"
        d.mkdir()
        lock = {"nodes": {"root": {"inputs": {}}}, "root": "root", "version": 7}
        (d / "flake.lock").write_text(json.dumps(lock))
        assert flake_tidy.load_lock(str(d)) == lock


class TestLoadLockNodeValidation:
    @pytest.mark.parametrize("node", ["oops", 42, ["x"], None])
    def test_non_object_node_value_raises(self, tmp_path, node):
        d = tmp_path / "badnode"
        d.mkdir()
        lock = {"nodes": {"root": node}, "root": "root"}
        (d / "flake.lock").write_text(json.dumps(lock))
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*node .root."):
            flake_tidy.load_lock(str(d))

    @pytest.mark.parametrize("key", ["inputs", "locked", "original"])
    @pytest.mark.parametrize("value", ["oops", ["x"], 42])
    def test_non_object_sub_value_raises(self, tmp_path, key, value):
        d = tmp_path / "badsub"
        d.mkdir()
        lock = {"nodes": {"root": {key: value}}, "root": "root"}
        (d / "flake.lock").write_text(json.dumps(lock))
        with pytest.raises(flake_tidy.LockLoadError, match=f"flake.lock.*{key}.*node .root."):
            flake_tidy.load_lock(str(d))

    def test_missing_root_node_raises(self, tmp_path):
        d = tmp_path / "missingroot"
        d.mkdir()
        lock = {"nodes": {"other": {"inputs": {}}}, "root": "root"}
        (d / "flake.lock").write_text(json.dumps(lock))
        with pytest.raises(flake_tidy.LockLoadError, match="flake.lock.*root.*not present"):
            flake_tidy.load_lock(str(d))

    def test_well_formed_input_references_load(self, tmp_path):
        d = tmp_path / "goodrefs"
        d.mkdir()
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a", "b": ["a", "b"]}},
                "a": {"locked": {}, "original": {}},
            },
            "root": "root",
            "version": 7,
        }
        (d / "flake.lock").write_text(json.dumps(lock))
        assert flake_tidy.load_lock(str(d)) == lock


class TestLoadLockInputsValueValidation:
    @pytest.mark.parametrize(
        "value",
        [42, {"x": 1}, ["b", 42], [{"x": 1}], None, True],
        ids=["number", "dict", "list-nonstring", "list-dict", "null", "bool"],
    )
    def test_invalid_inputs_value_raises(self, tmp_path, value):
        d = tmp_path / "badinputs"
        d.mkdir()
        lock = {"nodes": {"root": {"inputs": {"a": value}}}, "root": "root"}
        (d / "flake.lock").write_text(json.dumps(lock))
        with pytest.raises(
            flake_tidy.LockLoadError,
            match="flake.lock.*input .a..*node .root.",
        ):
            flake_tidy.load_lock(str(d))

    def test_invalid_inputs_value_in_non_root_node_raises(self, tmp_path):
        d = tmp_path / "badinputs2"
        d.mkdir()
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a"}},
                "a": {"inputs": {"b": 42}},
            },
            "root": "root",
        }
        (d / "flake.lock").write_text(json.dumps(lock))
        with pytest.raises(
            flake_tidy.LockLoadError,
            match="flake.lock.*input .b..*node .a.",
        ):
            flake_tidy.load_lock(str(d))

    @pytest.mark.parametrize(
        "lock",
        [
            {"nodes": {"root": {"inputs": {"a": 42}}}, "root": "root"},
            {"nodes": {"root": {"inputs": {"a": {"x": 1}}}}, "root": "root"},
            {"nodes": {"root": {"inputs": {"a": ["b", 42]}}}, "root": "root"},
        ],
        ids=["number-value", "dict-value", "list-nonstring-element"],
    )
    def test_inputs_value_check_mode_exit_2_no_traceback(
        self, tmp_path, monkeypatch, capsys, lock
    ):
        flake_dir, cfg = _write_flake_dir(tmp_path, json.dumps(lock))
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, ["--check"])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    def test_inputs_value_non_check_mode_exit_2(self, tmp_path, monkeypatch, capsys):
        lock = {"nodes": {"root": {"inputs": {"a": 42}}}, "root": "root"}
        flake_dir, cfg = _write_flake_dir(tmp_path, json.dumps(lock))
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, [])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err


class TestMalformedLockMain:
    @pytest.mark.parametrize(
        "lock_text",
        [
            "not json{{{",
            "{}",
            "[]",
            '"str"',
            json.dumps({"nodes": ["a"]}),
        ],
        ids=["invalid-json", "missing-nodes", "top-level-list", "top-level-str", "nodes-as-list"],
    )
    def test_check_mode_exit_2_no_traceback(self, tmp_path, monkeypatch, capsys, lock_text):
        flake_dir, cfg = _write_flake_dir(tmp_path, lock_text)
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, ["--check"])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    @pytest.mark.parametrize(
        "lock",
        [
            {"nodes": {"root": "oops"}, "root": "root"},
            {"nodes": {"root": 42}, "root": "root"},
            {"nodes": {"root": {"inputs": "oops"}}, "root": "root"},
            {"nodes": {"root": {"inputs": []}}, "root": "root"},
            {"nodes": {"root": {"inputs": {}}, "y": {"locked": "oops", "original": {}}}, "root": "root"},
            {"nodes": {"other": {"inputs": {}}}, "root": "root"},
        ],
        ids=["node-string", "node-number", "inputs-string", "inputs-list", "locked-string", "missing-root-node"],
    )
    def test_node_shape_check_mode_exit_2_no_traceback(self, tmp_path, monkeypatch, capsys, lock):
        flake_dir, cfg = _write_flake_dir(tmp_path, json.dumps(lock))
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, ["--check"])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    def test_node_shape_non_check_mode_exit_2(self, tmp_path, monkeypatch, capsys):
        lock = {"nodes": {"root": "oops"}, "root": "root"}
        flake_dir, cfg = _write_flake_dir(tmp_path, json.dumps(lock))
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, [])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    def test_non_check_mode_exit_2_no_traceback(self, tmp_path, monkeypatch, capsys):
        flake_dir, cfg = _write_flake_dir(tmp_path, "{}")
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, [])
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    @pytest.mark.parametrize("extra_args", [["--check"], []], ids=["check", "non-check"])
    def test_non_utf8_exit_2_no_traceback(self, tmp_path, monkeypatch, capsys, extra_args):
        (tmp_path / "flake.nix").write_text("{ inputs = {}; outputs = _: {}; }\n")
        (tmp_path / "flake.lock").write_bytes(
            b'{"nodes": {"root": {"inputs": {}}}, "root": "root"}' + b"\xff"
        )
        cfg = tmp_path / "cfg.json"
        cfg.write_text(json.dumps({}))
        code, out, err = _run_main(monkeypatch, capsys, str(tmp_path), str(cfg), extra_args)
        assert code == 2
        assert "flake.lock" in err
        assert "Traceback" not in out + err

    def test_well_formed_control_check_passes(self, tmp_path, monkeypatch, capsys):
        lock = {"nodes": {"root": {"inputs": {}}}, "root": "root", "version": 7}
        flake_dir, cfg = _write_flake_dir(tmp_path, json.dumps(lock))
        code, out, err = _run_main(monkeypatch, capsys, flake_dir, cfg, ["--check"])
        assert code == 0
        assert "check passed" in out
