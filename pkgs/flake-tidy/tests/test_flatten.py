"""Tests for flatten analysis."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestAnalyzeFlatten:
    def test_analyze_flatten_finds_transitive_only(self, default_config):
        """finds inputs not at root."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool": "tool"}},
                "tool": {
                    "inputs": {"dep": "dep"},
                    "locked": {
                        "narHash": "sha256-tool",
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": {"owner": "ex", "repo": "tool", "type": "github"},
                },
                "dep": {
                    "locked": {
                        "narHash": "sha256-dep",
                        "rev": "bbbb",
                        "type": "github",
                    },
                    "original": {"owner": "ex", "repo": "dep", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    tool.url = "github:ex/tool";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        assert len(proposals) >= 1
        assert any(p["new_input_name"] == "dep" for p in proposals)

    def test_analyze_flatten_skips_with_root_equivalent(
        self, sample_lock, sample_flake_nix, default_config
    ):
        """skips if root equivalent exists."""
        # nixpkgs_2 has same source as root nixpkgs, so flatten should skip it
        proposals = flake_tidy.analyze_flatten(
            sample_lock, sample_flake_nix, default_config
        )
        hoisted_nixpkgs = [
            p for p in proposals if "nixpkgs" in p.get("new_input_name", "")
        ]
        # Should not try to hoist nixpkgs_2 since root nixpkgs already exists
        assert len(hoisted_nixpkgs) == 0

    def test_analyze_flatten_picks_best_name(self, default_config):
        """picks most common name."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool1": "tool1", "tool2": "tool2"}},
                "tool1": {
                    "inputs": {"shared-dep": "shared_1"},
                    "locked": {"narHash": "sha256-t1", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "tool1", "type": "github"},
                },
                "tool2": {
                    "inputs": {"shared-dep": "shared_2"},
                    "locked": {"narHash": "sha256-t2", "rev": "b", "type": "github"},
                    "original": {"owner": "x", "repo": "tool2", "type": "github"},
                },
                "shared_1": {
                    "locked": {
                        "narHash": "sha256-shared",
                        "rev": "ccc",
                        "type": "github",
                    },
                    "original": {"owner": "y", "repo": "shared", "type": "github"},
                },
                "shared_2": {
                    "locked": {
                        "narHash": "sha256-shared",
                        "rev": "ccc",
                        "type": "github",
                    },
                    "original": {"owner": "y", "repo": "shared", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    tool1.url = "github:x/tool1";
    tool2.url = "github:x/tool2";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        assert len(proposals) >= 1
        # Both reference "shared-dep", so that should be the name
        assert proposals[0]["new_input_name"] == "shared-dep"

    def test_analyze_flatten_avoids_name_collision(self, default_config):
        """avoids collision with existing root inputs."""
        lock = {
            "nodes": {
                "root": {"inputs": {"dep": "dep-root", "tool": "tool"}},
                "dep-root": {
                    "locked": {
                        "narHash": "sha256-dep-root",
                        "rev": "aaa",
                        "type": "github",
                    },
                    "original": {"owner": "a", "repo": "dep-root", "type": "github"},
                },
                "tool": {
                    "inputs": {"dep": "dep-nested"},
                    "locked": {
                        "narHash": "sha256-tool",
                        "rev": "bbb",
                        "type": "github",
                    },
                    "original": {"owner": "x", "repo": "tool", "type": "github"},
                },
                "dep-nested": {
                    "locked": {
                        "narHash": "sha256-dep-nested",
                        "rev": "ccc",
                        "type": "github",
                    },
                    "original": {"owner": "y", "repo": "dep-other", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    dep.url = "github:a/dep-root";
    tool.url = "github:x/tool";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        if proposals:
            # Name "dep" collides with existing root input, so it should pick an alternative
            for p in proposals:
                assert p["new_input_name"] != "dep" or "-hoisted" in p["new_input_name"]

    def test_analyze_flatten_respects_includes(self, default_config):
        """only processes included inputs."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool": "tool"}},
                "tool": {
                    "inputs": {"dep": "dep"},
                    "locked": {"narHash": "sha256-tool", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "tool", "type": "github"},
                },
                "dep": {
                    "locked": {"narHash": "sha256-dep", "rev": "b", "type": "github"},
                    "original": {"owner": "y", "repo": "dep", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    tool.url = "github:x/tool";
  };
  outputs = _: {};
}
"""
        # Only include "systems" in flatten, so "dep" should be skipped
        default_config["include"]["flatten"] = ["systems"]
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        assert len(proposals) == 0

    def test_analyze_flatten_respects_excludes(self, default_config):
        """respects flatten excludes."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool": "tool"}},
                "tool": {
                    "inputs": {"dep": "dep"},
                    "locked": {"narHash": "sha256-tool", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "tool", "type": "github"},
                },
                "dep": {
                    "locked": {"narHash": "sha256-dep", "rev": "b", "type": "github"},
                    "original": {"owner": "y", "repo": "dep", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    tool.url = "github:x/tool";
  };
  outputs = _: {};
}
"""
        default_config["exclude"]["flatten"] = ["dep"]
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        assert len(proposals) == 0

    def test_analyze_flatten_groups_same_source(self, default_config):
        """groups same-source into one proposal."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool1": "tool1", "tool2": "tool2"}},
                "tool1": {
                    "inputs": {"lib": "lib_1"},
                    "locked": {"narHash": "sha256-t1", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "tool1", "type": "github"},
                },
                "tool2": {
                    "inputs": {"lib": "lib_2"},
                    "locked": {"narHash": "sha256-t2", "rev": "b", "type": "github"},
                    "original": {"owner": "x", "repo": "tool2", "type": "github"},
                },
                "lib_1": {
                    "locked": {"narHash": "sha256-lib", "rev": "ccc", "type": "github"},
                    "original": {"owner": "y", "repo": "lib", "type": "github"},
                },
                "lib_2": {
                    "locked": {"narHash": "sha256-lib", "rev": "ccc", "type": "github"},
                    "original": {"owner": "y", "repo": "lib", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    tool1.url = "github:x/tool1";
    tool2.url = "github:x/tool2";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        # Both lib_1 and lib_2 have same source, should be grouped into one proposal
        assert len(proposals) == 1
        # Should have follows for both paths
        assert len(proposals[0]["follows"]) == 2
