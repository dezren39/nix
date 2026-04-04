"""Tests for include/exclude logic."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestShouldInclude:
    def test_should_include_wildcard(self, default_config):
        """* includes everything."""
        assert flake_tidy.should_include(default_config, "dedup", "nixpkgs") is True
        assert flake_tidy.should_include(default_config, "dedup", "anything") is True
        assert flake_tidy.should_include(default_config, "flatten", "systems") is True

    def test_should_include_specific(self, default_config):
        """specific list includes only listed."""
        default_config["include"]["input"] = ["nixpkgs", "systems"]
        assert flake_tidy.should_include(default_config, "dedup", "nixpkgs") is True
        assert flake_tidy.should_include(default_config, "dedup", "systems") is True

    def test_should_include_not_listed(self, default_config):
        """not listed returns False."""
        default_config["include"]["input"] = ["nixpkgs"]
        assert (
            flake_tidy.should_include(default_config, "dedup", "flake-utils") is False
        )


class TestShouldExclude:
    def test_should_exclude_global(self, default_config):
        """global exclude works."""
        default_config["exclude"]["input"] = ["self"]
        assert flake_tidy.should_exclude(default_config, "dedup", "self") is True
        assert flake_tidy.should_exclude(default_config, "flatten", "self") is True

    def test_should_exclude_operation_specific(self, default_config):
        """operation-specific exclude."""
        default_config["exclude"]["dedup"] = ["systems"]
        assert flake_tidy.should_exclude(default_config, "dedup", "systems") is True
        # Not excluded from flatten
        assert flake_tidy.should_exclude(default_config, "flatten", "systems") is False

    def test_should_exclude_url(self, default_config):
        """URL-based exclude."""
        default_config["exclude"]["input-url"] = ["github:private/repo"]
        assert (
            flake_tidy.should_exclude(
                default_config, "dedup", "some-input", "github:private/repo"
            )
            is True
        )
        assert (
            flake_tidy.should_exclude(
                default_config, "dedup", "some-input", "github:public/repo"
            )
            is False
        )


class TestIsExcludedFollows:
    def test_is_excluded_follows_path(self, default_config):
        """follows path exclusion."""
        default_config["exclude"]["follows"] = ["home-manager.nixpkgs"]
        assert (
            flake_tidy.is_excluded_follows(default_config, "home-manager.nixpkgs")
            is True
        )
        assert flake_tidy.is_excluded_follows(default_config, "other.nixpkgs") is False


class TestIsExcludedFull:
    def test_is_excluded_full_combined(self, default_config, sample_lock):
        """combined exclusion check."""
        # Exclude the target by name
        default_config["exclude"]["dedup"] = ["nixpkgs"]
        result = flake_tidy.is_excluded_full(
            default_config,
            sample_lock,
            "dedup",
            "some-tool.nixpkgs",
            ["some-tool", "nixpkgs"],
            "nixpkgs",
            "nixpkgs",
        )
        assert result is True

    def test_include_overrides_exclude(self, default_config, sample_lock):
        """include and exclude interaction -- exclusion still wins because include and exclude are independent checks."""
        default_config["exclude"]["input"] = ["some-tool"]
        # Even if "some-tool" is globally excluded, the full check catches it
        result = flake_tidy.is_excluded_full(
            default_config,
            sample_lock,
            "dedup",
            "some-tool.nixpkgs",
            ["some-tool", "nixpkgs"],
            "nixpkgs",
            "nixpkgs",
        )
        assert result is True
