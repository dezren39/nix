"""Tests for dedup analysis."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestAnalyzeDedup:
    def test_analyze_dedup_finds_root_duplicates(
        self, sample_lock, sample_flake_nix, default_config
    ):
        """finds same-URL root inputs."""
        proposals = flake_tidy.analyze_dedup(
            sample_lock, sample_flake_nix, default_config
        )
        # nixpkgs and nixpkgs-unstable have the same source and hash
        root_dedup = [
            p
            for p in proposals
            if p["follows_parts"] == ["nixpkgs-unstable"] and p["target"] == "nixpkgs"
        ]
        assert len(root_dedup) == 1, f"Expected root dedup proposal, got: {proposals}"

    def test_analyze_dedup_finds_transitive(
        self, sample_lock, sample_flake_nix, default_config
    ):
        """finds transitive duplicates."""
        proposals = flake_tidy.analyze_dedup(
            sample_lock, sample_flake_nix, default_config
        )
        # some-tool has nixpkgs_2 which duplicates nixpkgs at root
        transitive = [
            p
            for p in proposals
            if len(p["follows_parts"]) > 1 and "nixpkgs" in p["follows_parts"][-1]
        ]
        # Should find some-tool.nixpkgs -> follows nixpkgs
        assert any(
            p["follows_parts"] == ["some-tool", "nixpkgs"] for p in transitive
        ), f"Expected transitive dedup, got: {proposals}"

    def test_analyze_dedup_skips_excluded(
        self, sample_lock, sample_flake_nix, default_config
    ):
        """respects excludes."""
        default_config["exclude"]["dedup"] = ["nixpkgs-unstable"]
        proposals = flake_tidy.analyze_dedup(
            sample_lock, sample_flake_nix, default_config
        )
        # nixpkgs-unstable should not appear as a follows source
        root_dedup = [
            p for p in proposals if p["follows_parts"] == ["nixpkgs-unstable"]
        ]
        assert len(root_dedup) == 0

    def test_analyze_dedup_respects_includes(
        self, sample_lock, sample_flake_nix, default_config
    ):
        """only processes included inputs."""
        # Only include systems
        default_config["include"]["dedup"] = ["systems"]
        proposals = flake_tidy.analyze_dedup(
            sample_lock, sample_flake_nix, default_config
        )
        # Should not find nixpkgs dedup proposals since nixpkgs isn't included
        nixpkgs_proposals = [p for p in proposals if "nixpkgs" in p["follows_parts"][0]]
        assert len(nixpkgs_proposals) == 0

    def test_analyze_dedup_skips_different_hash(self, default_config):
        """doesn't merge different hashes."""
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a", "b": "b"}},
                "a": {
                    "locked": {
                        "narHash": "sha256-HASH-A",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "b": {
                    "locked": {
                        "narHash": "sha256-HASH-B",
                        "rev": "bbbb",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    a.url = "github:nixos/nixpkgs";
    b.url = "github:nixos/nixpkgs";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        # Different hashes means no dedup
        assert len(proposals) == 0

    def test_analyze_dedup_skips_existing_follows(self, default_config):
        """doesn't re-add existing follows."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "nixpkgs", "tool": "tool"}},
                "nixpkgs": {
                    "locked": {
                        "narHash": "sha256-SAME",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "nixpkgs_2": {
                    "locked": {
                        "narHash": "sha256-SAME",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "tool": {
                    "inputs": {"nixpkgs": "nixpkgs_2"},
                    "locked": {
                        "narHash": "sha256-tool",
                        "rev": "bbbb",
                        "type": "github",
                    },
                    "original": {"owner": "ex", "repo": "tool", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        # Content already has the follows
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    tool = {
      url = "github:ex/tool";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        follows_proposals = [
            p for p in proposals if p["follows_parts"] == ["tool", "nixpkgs"]
        ]
        assert len(follows_proposals) == 0

    def test_analyze_dedup_respects_max_depth(self, default_config):
        """skips deep paths."""
        # Create a deep chain: root -> a -> b -> c -> d (depth 4)
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "nixpkgs", "a": "a"}},
                "nixpkgs": {
                    "locked": {
                        "narHash": "sha256-SAME",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "a": {
                    "inputs": {"b": "b"},
                    "locked": {"narHash": "sha256-a", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "a", "type": "github"},
                },
                "b": {
                    "inputs": {"c": "c"},
                    "locked": {"narHash": "sha256-b", "rev": "b", "type": "github"},
                    "original": {"owner": "x", "repo": "b", "type": "github"},
                },
                "c": {
                    "inputs": {"nixpkgs": "nixpkgs_deep"},
                    "locked": {"narHash": "sha256-c", "rev": "c", "type": "github"},
                    "original": {"owner": "x", "repo": "c", "type": "github"},
                },
                "nixpkgs_deep": {
                    "locked": {
                        "narHash": "sha256-SAME",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    a.url = "github:x/a";
  };
  outputs = _: {};
}
"""
        # max-depth 2 means depth-3 path a.b.c.nixpkgs is skipped
        default_config["max-depth"] = 2
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        deep_proposals = [p for p in proposals if len(p["follows_parts"]) > 2]
        assert len(deep_proposals) == 0
