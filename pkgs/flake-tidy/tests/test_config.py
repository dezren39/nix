"""Tests for config validation, loading, merging, and CLI override."""

import argparse
import json
import os
import subprocess
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestValidateConfig:
    def test_default_config_valid(self):
        """validate DEFAULT_CONFIG passes validation."""
        errors, warnings = flake_tidy.validate_config(flake_tidy.DEFAULT_CONFIG)
        assert errors == []
        assert warnings == []

    def test_validate_config_max_depth_string(self):
        """error when max-depth is string."""
        cfg = {"max-depth": "six"}
        errors, warnings = flake_tidy.validate_config(cfg)
        assert len(errors) == 1
        assert "max-depth" in errors[0]
        assert "str" in errors[0]

    def test_validate_config_unknown_key(self):
        """warning on unknown key."""
        cfg = {"unknown-option": True}
        errors, warnings = flake_tidy.validate_config(cfg)
        assert errors == []
        assert len(warnings) == 1
        assert "unknown config key" in warnings[0]
        assert "unknown-option" in warnings[0]

    def test_validate_config_bad_include_type(self):
        """error when include.input is not list."""
        cfg = {"include": {"input": "nixpkgs"}}
        errors, warnings = flake_tidy.validate_config(cfg)
        assert len(errors) == 1
        assert "include.input" in errors[0]
        assert "list" in errors[0]


class TestMergeConfig:
    def test_merge_config_override_max_depth(self, default_config):
        """override max-depth."""
        result = flake_tidy.merge_config(default_config, {"max-depth": 10})
        assert result["max-depth"] == 10
        # Other values should remain unchanged
        assert result["include"]["input"] == ["*"]
        assert result["exclude"]["input"] == []

    def test_merge_config_override_includes(self, default_config):
        """override includes replaces."""
        override = {"include": {"input": ["nixpkgs", "systems"]}}
        result = flake_tidy.merge_config(default_config, override)
        assert result["include"]["input"] == ["nixpkgs", "systems"]
        # Other include keys should remain since it's a deep merge
        assert result["include"]["dedup"] == ["*"]
        assert result["include"]["flatten"] == ["*"]

    def test_merge_config_deep_merge_excludes(self, default_config):
        """deep merge excludes."""
        override = {"exclude": {"input": ["self"]}}
        result = flake_tidy.merge_config(default_config, override)
        # Override replaces the specific key
        assert result["exclude"]["input"] == ["self"]
        # Other exclude keys should remain since it's a deep merge
        assert result["exclude"]["follows"] == []
        assert result["exclude"]["dedup"] == []


def _fake_run_failing(cmd, **kwargs):
    """A nix subprocess that always fails (sandbox / no daemon)."""
    return subprocess.CompletedProcess(cmd, 1, stdout="", stderr="boom")


def _fake_run_probe_only(cmd, **kwargs):
    """Try 1 (full flake eval) fails; the --impure import probe succeeds."""
    if "--impure" in cmd:
        payload = {"exclude": {"follows": ["opencode.nixpkgs"]}}
        return subprocess.CompletedProcess(
            cmd, 0, stdout=json.dumps(payload), stderr=""
        )
    return subprocess.CompletedProcess(cmd, 1, stdout="", stderr="boom")


class TestLoadConfigFile:
    def test_config_file_supplies_excludes(self, tmp_path, monkeypatch):
        """--config file is loaded without any nix subprocess (#476)."""
        path = tmp_path / "cfg.json"
        path.write_text(json.dumps({"exclude": {"follows": ["a.nixpkgs"]}}))
        monkeypatch.setattr(
            flake_tidy.subprocess, "run", _fake_run_failing
        )
        result = flake_tidy.load_config(
            str(tmp_path), config_file=str(path), strict=True
        )
        assert result["exclude"]["follows"] == ["a.nixpkgs"]

    def test_env_var_supplies_config_file(self, tmp_path, monkeypatch):
        """$FLAKE_TIDY_CONFIG is honoured when --config is absent."""
        path = tmp_path / "cfg.json"
        path.write_text(json.dumps({"max-depth": 3}))
        monkeypatch.setenv("FLAKE_TIDY_CONFIG", str(path))
        monkeypatch.setattr(flake_tidy.subprocess, "run", _fake_run_failing)
        result = flake_tidy.load_config(str(tmp_path), strict=True)
        assert result["max-depth"] == 3

    def test_missing_config_file_is_fatal(self, tmp_path):
        """An explicitly named config file that cannot be read errors
        loudly instead of silently falling back to defaults (#476)."""
        with pytest.raises(flake_tidy.ConfigLoadError):
            flake_tidy.load_config(
                str(tmp_path), config_file=str(tmp_path / "nope.json")
            )

    def test_invalid_config_file_is_fatal(self, tmp_path):
        path = tmp_path / "cfg.json"
        path.write_text("{not json")
        with pytest.raises(flake_tidy.ConfigLoadError):
            flake_tidy.load_config(str(tmp_path), config_file=str(path))


class TestLoadConfigStrict:
    def test_strict_raises_when_no_source_loads(self, tmp_path, monkeypatch):
        """Check mode must not silently check against defaults when every
        config source fails (e.g. inside a nix build sandbox) (#476)."""
        monkeypatch.setattr(flake_tidy.subprocess, "run", _fake_run_failing)
        monkeypatch.delenv("FLAKE_TIDY_CONFIG", raising=False)
        with pytest.raises(flake_tidy.ConfigLoadError):
            flake_tidy.load_config(str(tmp_path), strict=True)

    def test_non_strict_falls_back_to_defaults(self, tmp_path, monkeypatch):
        monkeypatch.setattr(flake_tidy.subprocess, "run", _fake_run_failing)
        monkeypatch.delenv("FLAKE_TIDY_CONFIG", raising=False)
        result = flake_tidy.load_config(str(tmp_path))
        assert result["exclude"]["follows"] == []
        assert result["max-depth"] == 6

    def test_import_probe_loads_outputs_config(self, tmp_path, monkeypatch):
        """Regression (#476): flakeTidy lives under outputs, so when full
        `nix eval .#flakeTidy` fails (packaged nix), the import probe must
        still find it — the old probe read the flake.nix top level and
        always came back empty."""
        monkeypatch.setattr(flake_tidy.subprocess, "run", _fake_run_probe_only)
        monkeypatch.delenv("FLAKE_TIDY_CONFIG", raising=False)
        result = flake_tidy.load_config(str(tmp_path), strict=True)
        assert result["exclude"]["follows"] == ["opencode.nixpkgs"]

    def test_import_probe_quotes_space_in_flake_dir(self, tmp_path, monkeypatch):
        """Regression (#487): the probe interpolated flake_dir unquoted
        into the Nix expression, and a Nix path literal cannot contain a
        space — so for a checkout under a spaced path the probe could
        never succeed. The path must be passed as a quoted string via
        builtins.toPath."""
        spaced = tmp_path / "probe test"
        spaced.mkdir()
        captured = {}

        def fake_run(cmd, **kwargs):
            if "--impure" in cmd:
                captured["expr"] = cmd[cmd.index("--expr") + 1]
                payload = {"exclude": {"follows": ["a.b"]}}
                return subprocess.CompletedProcess(
                    cmd, 0, stdout=json.dumps(payload), stderr=""
                )
            return subprocess.CompletedProcess(cmd, 1, stdout="", stderr="boom")

        monkeypatch.setattr(flake_tidy.subprocess, "run", fake_run)
        monkeypatch.delenv("FLAKE_TIDY_CONFIG", raising=False)
        result = flake_tidy.load_config(str(spaced), strict=True)
        assert result["exclude"]["follows"] == ["a.b"]
        # The spaced path appears only inside a quoted Nix string handed
        # to builtins.toPath — never as a bare path literal after import.
        assert f'builtins.toPath "{spaced}/flake.nix"' in captured["expr"]
        assert f"import {spaced}" not in captured["expr"]

    def test_import_probe_escapes_nix_string_characters(self, monkeypatch):
        """Directory names may also contain characters that are special
        inside a Nix string (quote, backslash, the `${` antiquotation
        opener); the generated expression must escape all of them."""
        captured = {}

        def fake_run(cmd, **kwargs):
            if "--impure" in cmd:
                captured["expr"] = cmd[cmd.index("--expr") + 1]
                return subprocess.CompletedProcess(
                    cmd, 0, stdout="{}", stderr=""
                )
            return subprocess.CompletedProcess(cmd, 1, stdout="", stderr="boom")

        monkeypatch.setattr(flake_tidy.subprocess, "run", fake_run)
        monkeypatch.delenv("FLAKE_TIDY_CONFIG", raising=False)
        flake_tidy.load_config('/tmp/we"ird\\${x}', strict=True)
        assert 'builtins.toPath "/tmp/we\\"ird\\\\\\${x}/flake.nix"' in captured["expr"]


class TestMergeCLI:
    def _make_args(self, **kwargs):
        """Create a Namespace with all the expected attributes."""
        defaults = {
            "max_depth": None,
            "include": None,
            "include_dedup": None,
            "include_merge": None,
            "include_flatten": None,
            "exclude_input": None,
            "exclude_dedup": None,
            "exclude_merge": None,
            "exclude_flatten": None,
        }
        defaults.update(kwargs)
        return argparse.Namespace(**defaults)

    def test_merge_cli_includes_replace(self, default_config):
        """CLI --include replaces config."""
        args = self._make_args(include=["nixpkgs", "home-manager"])
        result = flake_tidy.merge_cli_into_config(default_config, args)
        assert result["include"]["input"] == ["nixpkgs", "home-manager"]
        # Dedup/flatten should remain unchanged
        assert result["include"]["dedup"] == ["*"]

    def test_merge_cli_negative_max_depth_rejected(self, default_config):
        """CLI --max-depth < 0 is rejected at merge time (#1224).

        Pins the programmatic path (a hand-built Namespace bypasses the
        parser type) to the same "max-depth must be >= 0" rejection the
        config validator gives the config-file spelling.
        """
        args = self._make_args(max_depth=-1)
        with pytest.raises(ValueError, match="max-depth must be >= 0"):
            flake_tidy.merge_cli_into_config(default_config, args)

    def test_merge_cli_excludes_append(self, default_config):
        """CLI --exclude-input appends to config."""
        # Pre-seed an exclusion in config
        default_config["exclude"]["input"] = ["self"]
        args = self._make_args(exclude_input=["nixpkgs-unstable"])
        result = flake_tidy.merge_cli_into_config(default_config, args)
        assert "self" in result["exclude"]["input"]
        assert "nixpkgs-unstable" in result["exclude"]["input"]
        assert len(result["exclude"]["input"]) == 2
