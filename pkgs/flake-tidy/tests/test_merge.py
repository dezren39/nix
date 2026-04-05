"""Tests for merge logic: hoisting deep-followed transitive inputs to root."""

import json
import os
import sys
import tempfile
import shutil
from unittest.mock import patch

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


# -- Fixtures --


@pytest.fixture
def default_config():
    return flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)


@pytest.fixture
def deep_follows_flake_nix():
    """flake.nix with deep follows that trigger the merge scenario."""
    return """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    systems.url = "github:nix-systems/default";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.cl-nix-lite.inputs.systems.follows = "systems";
      inputs.cl-nix-lite.inputs.treefmt-nix.follows = "treefmt-nix";
    };
  };
  outputs = _: {};
}
"""


@pytest.fixture
def deep_follows_lock():
    """Lock file with the transitive cl-nix-lite input under mac-app-util."""
    return {
        "nodes": {
            "root": {
                "inputs": {
                    "nixpkgs": "nixpkgs",
                    "systems": "systems",
                    "treefmt-nix": "treefmt-nix",
                    "mac-app-util": "mac-app-util",
                }
            },
            "nixpkgs": {
                "locked": {"narHash": "sha256-np", "type": "github"},
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "systems": {
                "locked": {"narHash": "sha256-sys", "type": "github"},
                "original": {
                    "owner": "nix-systems",
                    "repo": "default",
                    "type": "github",
                },
            },
            "treefmt-nix": {
                "locked": {"narHash": "sha256-tf", "type": "github"},
                "original": {
                    "owner": "numtide",
                    "repo": "treefmt-nix",
                    "type": "github",
                },
            },
            "mac-app-util": {
                "inputs": {
                    "nixpkgs": "nixpkgs_2",
                    "cl-nix-lite": "cl-nix-lite",
                    "treefmt-nix": ["treefmt-nix"],
                },
                "locked": {"narHash": "sha256-mau", "type": "github"},
                "original": {
                    "owner": "hraban",
                    "repo": "mac-app-util",
                    "type": "github",
                },
            },
            "nixpkgs_2": {
                "locked": {"narHash": "sha256-np2", "type": "github"},
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "cl-nix-lite": {
                "inputs": {
                    "nixpkgs": "nixpkgs_3",
                    "systems": "systems_2",
                    "treefmt-nix": "treefmt-nix_2",
                },
                "locked": {"narHash": "sha256-cl", "type": "github"},
                "original": {
                    "owner": "hraban",
                    "repo": "cl-nix-lite",
                    "type": "github",
                },
            },
            "nixpkgs_3": {
                "locked": {"narHash": "sha256-np3", "type": "github"},
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "systems_2": {
                "locked": {"narHash": "sha256-sys2", "type": "github"},
                "original": {
                    "owner": "nix-systems",
                    "repo": "default",
                    "type": "github",
                },
            },
            "treefmt-nix_2": {
                "locked": {"narHash": "sha256-tf2", "type": "github"},
                "original": {
                    "owner": "numtide",
                    "repo": "treefmt-nix",
                    "type": "github",
                },
            },
        },
        "root": "root",
        "version": 7,
    }


# -- Tests for _parse_deep_follows --


class TestParseDeepFollows:
    def test_parses_block_style(self, deep_follows_flake_nix):
        results = flake_tidy._parse_deep_follows(deep_follows_flake_nix)
        assert len(results) == 2
        parents = {r["parent"] for r in results}
        children = {r["child"] for r in results}
        sub_inputs = {r["sub_input"] for r in results}
        assert parents == {"mac-app-util"}
        assert children == {"cl-nix-lite"}
        assert sub_inputs == {"systems", "treefmt-nix"}

    def test_parses_dotted_style(self):
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.systems.url = "github:nix-systems/default";
  inputs.mac-app-util.url = "github:hraban/mac-app-util";
  inputs.mac-app-util.inputs.cl-nix-lite.inputs.systems.follows = "systems";
  outputs = _: {};
}
"""
        results = flake_tidy._parse_deep_follows(content)
        assert len(results) == 1
        assert results[0]["parent"] == "mac-app-util"
        assert results[0]["child"] == "cl-nix-lite"
        assert results[0]["sub_input"] == "systems"
        assert results[0]["target"] == "systems"

    def test_no_deep_follows(self):
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs = _: {};
}
"""
        results = flake_tidy._parse_deep_follows(content)
        assert results == []

    def test_ignores_comments(self):
        content = """{
  inputs = {
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      # inputs.cl-nix-lite.inputs.systems.follows = "systems";
    };
  };
  outputs = _: {};
}
"""
        results = flake_tidy._parse_deep_follows(content)
        assert results == []


# -- Tests for _resolve_transitive_url --


class TestResolveTransitiveUrl:
    def test_resolves_from_lock(self, deep_follows_lock):
        url = flake_tidy._resolve_transitive_url(
            deep_follows_lock, "mac-app-util", "cl-nix-lite"
        )
        assert url == "github:hraban/cl-nix-lite"

    def test_returns_none_for_missing_parent(self, deep_follows_lock):
        url = flake_tidy._resolve_transitive_url(
            deep_follows_lock, "nonexistent", "cl-nix-lite"
        )
        assert url is None

    def test_returns_none_for_missing_child(self, deep_follows_lock):
        url = flake_tidy._resolve_transitive_url(
            deep_follows_lock, "mac-app-util", "nonexistent"
        )
        assert url is None


# -- Tests for analyze_merge --


class TestAnalyzeMerge:
    def test_finds_deep_follows(
        self, deep_follows_lock, deep_follows_flake_nix, default_config
    ):
        proposals = flake_tidy.analyze_merge(
            deep_follows_lock, deep_follows_flake_nix, default_config
        )
        assert len(proposals) == 1
        p = proposals[0]
        assert p["parent"] == "mac-app-util"
        assert p["child"] == "cl-nix-lite"
        assert p["url"] == "github:hraban/cl-nix-lite"
        sub_inputs = {s[0] for s in p["sub_follows"]}
        assert sub_inputs == {"systems", "treefmt-nix"}

    def test_skips_when_child_already_root(self, deep_follows_lock, default_config):
        """If cl-nix-lite is already a root input, skip merge."""
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    systems.url = "github:nix-systems/default";
    treefmt-nix.url = "github:numtide/treefmt-nix";
    cl-nix-lite.url = "github:hraban/cl-nix-lite";
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.cl-nix-lite.inputs.systems.follows = "systems";
      inputs.cl-nix-lite.inputs.treefmt-nix.follows = "treefmt-nix";
    };
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_merge(deep_follows_lock, content, default_config)
        assert len(proposals) == 0

    def test_respects_exclude(self, deep_follows_lock, deep_follows_flake_nix):
        config = flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)
        config["exclude"]["merge"] = ["mac-app-util"]
        proposals = flake_tidy.analyze_merge(
            deep_follows_lock, deep_follows_flake_nix, config
        )
        assert len(proposals) == 0

    def test_respects_include(self, deep_follows_lock, deep_follows_flake_nix):
        config = flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)
        config["include"]["merge"] = ["some-other-input"]
        proposals = flake_tidy.analyze_merge(
            deep_follows_lock, deep_follows_flake_nix, config
        )
        assert len(proposals) == 0

    def test_no_proposals_for_clean_flake(self, deep_follows_lock, default_config):
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_merge(deep_follows_lock, content, default_config)
        assert len(proposals) == 0


# -- Tests for apply_merge --


class TestApplyMerge:
    @patch("flake_tidy.run_nix_flake_lock_robust", return_value=(True, ""))
    @patch("flake_tidy.run_nixfmt")
    def test_applies_merge_correctly(
        self,
        mock_fmt,
        mock_lock,
        deep_follows_lock,
        deep_follows_flake_nix,
        default_config,
    ):
        tmpdir = tempfile.mkdtemp(prefix="flake-tidy-merge-")
        try:
            with open(os.path.join(tmpdir, "flake.nix"), "w") as f:
                f.write(deep_follows_flake_nix)
            with open(os.path.join(tmpdir, "flake.lock"), "w") as f:
                json.dump(deep_follows_lock, f)

            proposals = flake_tidy.analyze_merge(
                deep_follows_lock, deep_follows_flake_nix, default_config
            )
            assert len(proposals) == 1

            applied, failed = flake_tidy.apply_merge(tmpdir, proposals)
            assert applied == 1
            assert failed == []

            content = flake_tidy.read_flake_nix(tmpdir)
            # Old deep follows should be gone
            assert "inputs.cl-nix-lite.inputs.systems.follows" not in content
            assert "inputs.cl-nix-lite.inputs.treefmt-nix.follows" not in content
            # New root input should exist
            assert "cl-nix-lite" in content
            assert "github:hraban/cl-nix-lite" in content
            # Parent should follow root
            assert 'follows = "cl-nix-lite"' in content
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)

    @patch("flake_tidy.run_nix_flake_lock_robust", return_value=(False, "error"))
    @patch("flake_tidy.run_nixfmt")
    def test_backs_out_on_lock_failure(
        self,
        mock_fmt,
        mock_lock,
        deep_follows_lock,
        deep_follows_flake_nix,
        default_config,
    ):
        tmpdir = tempfile.mkdtemp(prefix="flake-tidy-merge-")
        try:
            with open(os.path.join(tmpdir, "flake.nix"), "w") as f:
                f.write(deep_follows_flake_nix)
            with open(os.path.join(tmpdir, "flake.lock"), "w") as f:
                json.dump(deep_follows_lock, f)

            proposals = flake_tidy.analyze_merge(
                deep_follows_lock, deep_follows_flake_nix, default_config
            )
            applied, failed = flake_tidy.apply_merge(tmpdir, proposals)
            assert applied == 0
            assert len(failed) == 1

            # Content should be restored
            content = flake_tidy.read_flake_nix(tmpdir)
            assert "inputs.cl-nix-lite.inputs.systems.follows" in content
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)


# -- Tests for merge function --


class TestMergeEndToEnd:
    @patch("flake_tidy.run_nix_flake_lock_robust", return_value=(True, ""))
    @patch("flake_tidy.run_nixfmt")
    def test_merge_check_mode(
        self,
        mock_fmt,
        mock_lock,
        deep_follows_lock,
        deep_follows_flake_nix,
        default_config,
    ):
        tmpdir = tempfile.mkdtemp(prefix="flake-tidy-merge-")
        try:
            with open(os.path.join(tmpdir, "flake.nix"), "w") as f:
                f.write(deep_follows_flake_nix)
            with open(os.path.join(tmpdir, "flake.lock"), "w") as f:
                json.dump(deep_follows_lock, f)

            count = flake_tidy.merge(tmpdir, default_config, check=True)
            assert count == 1
            # Lock should not have been called in check mode
            mock_lock.assert_not_called()
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)

    @patch("flake_tidy.run_nix_flake_lock_robust", return_value=(True, ""))
    @patch("flake_tidy.run_nixfmt")
    def test_merge_dry_run(
        self,
        mock_fmt,
        mock_lock,
        deep_follows_lock,
        deep_follows_flake_nix,
        default_config,
    ):
        tmpdir = tempfile.mkdtemp(prefix="flake-tidy-merge-")
        try:
            with open(os.path.join(tmpdir, "flake.nix"), "w") as f:
                f.write(deep_follows_flake_nix)
            with open(os.path.join(tmpdir, "flake.lock"), "w") as f:
                json.dump(deep_follows_lock, f)

            count = flake_tidy.merge(tmpdir, default_config, dry_run=True)
            assert count == 1
            mock_lock.assert_not_called()
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)
