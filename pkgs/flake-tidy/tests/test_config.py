"""Tests for config validation, loading, merging, and CLI override."""

import argparse
import os
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


class TestMergeCLI:
    def _make_args(self, **kwargs):
        """Create a Namespace with all the expected attributes."""
        defaults = {
            "max_depth": None,
            "include": None,
            "include_dedup": None,
            "include_flatten": None,
            "exclude_input": None,
            "exclude_dedup": None,
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

    def test_merge_cli_excludes_append(self, default_config):
        """CLI --exclude-input appends to config."""
        # Pre-seed an exclusion in config
        default_config["exclude"]["input"] = ["self"]
        args = self._make_args(exclude_input=["nixpkgs-unstable"])
        result = flake_tidy.merge_cli_into_config(default_config, args)
        assert "self" in result["exclude"]["input"]
        assert "nixpkgs-unstable" in result["exclude"]["input"]
        assert len(result["exclude"]["input"]) == 2
