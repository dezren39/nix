"""Tests for CLI argument parsing."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestCLI:
    @pytest.fixture(autouse=True)
    def setup_parser(self):
        self.parser = flake_tidy.build_parser()

    def test_default_action(self):
        """default action is 'all'."""
        args = self.parser.parse_args([])
        assert args.action == "all"

    def test_dedup_action(self):
        """'dedup' action parsed correctly."""
        args = self.parser.parse_args(["dedup"])
        assert args.action == "dedup"

    def test_flatten_action(self):
        """'flatten' action parsed correctly."""
        args = self.parser.parse_args(["flatten"])
        assert args.action == "flatten"

    def test_merge_action(self):
        """'merge' action parsed correctly."""
        args = self.parser.parse_args(["merge"])
        assert args.action == "merge"

    def test_dry_run_flag(self):
        """--dry-run flag."""
        args = self.parser.parse_args(["--dry-run"])
        assert args.dry_run is True

        args_without = self.parser.parse_args([])
        assert args_without.dry_run is False

    def test_check_flag(self):
        """--check flag."""
        args = self.parser.parse_args(["--check"])
        assert args.check is True

        args_without = self.parser.parse_args([])
        assert args_without.check is False

    def test_verbose_flag(self):
        """--verbose and -v flags."""
        args_long = self.parser.parse_args(["--verbose"])
        assert args_long.verbose is True

        args_short = self.parser.parse_args(["-v"])
        assert args_short.verbose is True

        args_without = self.parser.parse_args([])
        assert args_without.verbose is False

    def test_config_file_arg(self):
        """--config FILE (#476: config supplied as a file for sandboxes)."""
        args = self.parser.parse_args(["all", "--check", "--config", "cfg.json"])
        assert args.config_file == "cfg.json"

        args_without = self.parser.parse_args([])
        assert args_without.config_file is None

    def test_max_depth_override(self):
        """--max-depth N."""
        args = self.parser.parse_args(["--max-depth", "10"])
        assert args.max_depth == 10

        args_default = self.parser.parse_args([])
        assert args_default.max_depth is None

    def test_max_depth_negative_rejected(self, capsys):
        """--max-depth -1 is a parse error (#1224).

        The same value in a config file is rejected by validate_config
        ("max-depth must be >= 0", exit 2); the CLI spelling used to
        bypass the validator (it runs only in load_config, before
        merge_cli_into_config overwrites the value) and silently
        disabled all traversal while exiting 0.
        """
        with pytest.raises(SystemExit) as excinfo:
            self.parser.parse_args(["--max-depth", "-1"])
        assert excinfo.value.code == 2
        assert "max-depth must be >= 0" in capsys.readouterr().err

        # 0 (unlimited) and positive values behave exactly as before.
        assert self.parser.parse_args(["--max-depth", "0"]).max_depth == 0
        assert self.parser.parse_args(["--max-depth", "6"]).max_depth == 6

    def test_flake_dir_empty_rejected(self, capsys):
        """--flake-dir '' is a parse error (#1229).

        os.path.abspath("") is the caller's current directory, so an
        explicitly empty value used to silently tidy whatever directory
        the caller happened to be standing in -- and in non-check mode
        format and edit its flake.nix -- instead of being rejected.
        """
        with pytest.raises(SystemExit) as excinfo:
            self.parser.parse_args(["--flake-dir", ""])
        assert excinfo.value.code == 2
        assert "--flake-dir" in capsys.readouterr().err

        # The default (option omitted) and non-empty values are unchanged.
        assert self.parser.parse_args([]).flake_dir == "."
        assert self.parser.parse_args(["--flake-dir", "/tmp/x"]).flake_dir == "/tmp/x"

    def test_config_empty_rejected(self, capsys):
        """--config '' is a parse error (#1233).

        load_config picks the explicit source with a falsy chain
        (config_file or $FLAKE_TIDY_CONFIG), so an explicitly empty
        value used to be silently discarded and the run loaded a config
        from the env var / nix eval / import probe -- a config the
        caller did not ask for -- instead of being rejected.
        """
        with pytest.raises(SystemExit) as excinfo:
            self.parser.parse_args(["--config", ""])
        assert excinfo.value.code == 2
        assert "--config" in capsys.readouterr().err

        # Omitting --config and non-empty values are unchanged.
        assert self.parser.parse_args([]).config_file is None
        assert self.parser.parse_args(["--config", "/tmp/x.json"]).config_file == "/tmp/x.json"

    def test_include_exclude_empty_name_rejected(self, capsys):
        """--include*/--exclude* '' is a parse error naming the option (#1239).

        An empty include name matches nothing, so --check used to pass
        vacuously (exit 0) on a flake needing changes; an empty exclude
        name silently dropped real proposals. Every one of the eight
        list options rejects an empty name at parse time.
        """
        options = [
            "--include",
            "--include-dedup",
            "--include-merge",
            "--include-flatten",
            "--exclude-input",
            "--exclude-dedup",
            "--exclude-merge",
            "--exclude-flatten",
        ]
        for option in options:
            with pytest.raises(SystemExit) as excinfo:
                self.parser.parse_args([option, ""])
            assert excinfo.value.code == 2
            assert option in capsys.readouterr().err

    def test_include_mixed_list_with_empty_name_rejected(self, capsys):
        """--include nixpkgs '' is a parse error too (#1239).

        The guard is per element, not per list: one empty name in an
        otherwise well-formed list still vacuums/drops part of the run.
        """
        with pytest.raises(SystemExit) as excinfo:
            self.parser.parse_args(["--include", "nixpkgs", ""])
        assert excinfo.value.code == 2
        assert "--include" in capsys.readouterr().err

        with pytest.raises(SystemExit) as excinfo:
            self.parser.parse_args(["--exclude-input", "nixpkgs", ""])
        assert excinfo.value.code == 2
        assert "--exclude-input" in capsys.readouterr().err

        # Well-formed names are unchanged.
        assert self.parser.parse_args(["--include", "nixpkgs"]).include == [
            "nixpkgs"
        ]

    def test_include_args(self):
        """--include foo bar."""
        args = self.parser.parse_args(["--include", "foo", "bar"])
        assert args.include == ["foo", "bar"]

    def test_include_dedup_args(self):
        """--include-dedup foo."""
        args = self.parser.parse_args(["--include-dedup", "foo"])
        assert args.include_dedup == ["foo"]

    def test_include_flatten_args(self):
        """--include-flatten foo."""
        args = self.parser.parse_args(["--include-flatten", "foo"])
        assert args.include_flatten == ["foo"]

    def test_include_merge_args(self):
        """--include-merge foo."""
        args = self.parser.parse_args(["--include-merge", "foo"])
        assert args.include_merge == ["foo"]

    def test_exclude_args(self):
        """--exclude-input foo bar."""
        args = self.parser.parse_args(["--exclude-input", "foo", "bar"])
        assert args.exclude_input == ["foo", "bar"]

    def test_exclude_dedup_args(self):
        """--exclude-dedup foo."""
        args = self.parser.parse_args(["--exclude-dedup", "foo"])
        assert args.exclude_dedup == ["foo"]

    def test_exclude_merge_args(self):
        """--exclude-merge foo."""
        args = self.parser.parse_args(["--exclude-merge", "foo"])
        assert args.exclude_merge == ["foo"]

    def test_exclude_flatten_args(self):
        """--exclude-flatten foo."""
        args = self.parser.parse_args(["--exclude-flatten", "foo"])
        assert args.exclude_flatten == ["foo"]


class TestMissingFlakeFiles:
    """main() missing flake.lock / flake.nix is exit 2, not 1 (#1250).

    Exit 1 is the --check verdict ("changes needed") on a well-formed
    flake; a directory with no flake at all is an invocation error,
    like ConfigLoadError / LockLoadError in the same main() and the
    missing-file guards in check-lock-sync / check-pin-sync.
    """

    def _run_main(self, monkeypatch, capsys, tmp_path, files, extra_args):
        from unittest.mock import patch

        for name in files:
            (tmp_path / name).write_text("{}\n" if name.endswith(".lock") else "{ inputs = {}; }\n")
        cfg = tmp_path / "cfg.json"
        cfg.write_text("{}\n")
        argv = ["flake-tidy", *extra_args, "--config", str(cfg), "--flake-dir", str(tmp_path)]
        monkeypatch.setattr(sys, "argv", argv)
        with patch.object(flake_tidy, "run_nixfmt", return_value=None):
            with pytest.raises(SystemExit) as excinfo:
                flake_tidy.main()
        out, err = capsys.readouterr()
        return excinfo.value.code, out, err

    @pytest.mark.parametrize("extra_args", [["--check"], []], ids=["check", "non-check"])
    def test_missing_flake_lock_exit_2(self, monkeypatch, capsys, tmp_path, extra_args):
        code, out, err = self._run_main(monkeypatch, capsys, tmp_path, ["flake.nix"], extra_args)
        assert code == 2
        assert "no flake.lock" in err
        assert "Traceback" not in out + err

    @pytest.mark.parametrize("extra_args", [["--check"], []], ids=["check", "non-check"])
    def test_missing_flake_nix_exit_2(self, monkeypatch, capsys, tmp_path, extra_args):
        code, out, err = self._run_main(monkeypatch, capsys, tmp_path, ["flake.lock"], extra_args)
        assert code == 2
        assert "no flake.nix" in err
        assert "Traceback" not in out + err

    def test_missing_both_exit_2(self, monkeypatch, capsys, tmp_path):
        code, out, err = self._run_main(monkeypatch, capsys, tmp_path, [], ["--check"])
        assert code == 2
        assert "no flake.lock" in err

    def test_verdict_control_still_exit_1(self, monkeypatch, capsys, tmp_path):
        """A well-formed flake needing changes still exits 1 in --check."""
        import shutil

        fixture = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixtures", "sample-flake")
        work = tmp_path / "sample"
        shutil.copytree(fixture, work)
        cfg = tmp_path / "cfg.json"
        cfg.write_text("{}\n")
        monkeypatch.setattr(sys, "argv", ["flake-tidy", "--check", "--config", str(cfg), "--flake-dir", str(work)])
        from unittest.mock import patch

        with patch.object(flake_tidy, "run_nixfmt", return_value=None):
            with pytest.raises(SystemExit) as excinfo:
                flake_tidy.main()
        assert excinfo.value.code == 1
        assert "check failed" in capsys.readouterr().out
