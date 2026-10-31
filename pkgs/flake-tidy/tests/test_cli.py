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
