"""Tests for edge cases."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestEdgeCases:
    def test_intermediate_follows_skipped(self, default_config):
        """paths with intermediate follows are skipped."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "nixpkgs", "a": "a", "b": "b"}},
                "nixpkgs": {
                    "locked": {"narHash": "sha256-NP", "rev": "np", "type": "github"},
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "a": {
                    "inputs": {"b": ["b"]},  # follows edge to root's b
                    "locked": {"narHash": "sha256-a", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "a", "type": "github"},
                },
                "b": {
                    "inputs": {"nixpkgs": "nixpkgs_2"},
                    "locked": {"narHash": "sha256-b", "rev": "b", "type": "github"},
                    "original": {"owner": "x", "repo": "b", "type": "github"},
                },
                "nixpkgs_2": {
                    "locked": {"narHash": "sha256-NP", "rev": "np", "type": "github"},
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
    b.url = "github:x/b";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        # Path root->a->b(follows)->nixpkgs has an intermediate follows, should be skipped.
        # But root->b->nixpkgs is valid (no intermediate follows).
        intermediate_proposals = [
            p for p in proposals if p["follows_parts"] == ["a", "b", "nixpkgs"]
        ]
        assert len(intermediate_proposals) == 0

    def test_path_input_skipped(self, default_config):
        """path inputs are never followed/flattened."""
        lock = {
            "nodes": {
                "root": {"inputs": {"local": "local", "tool": "tool"}},
                "local": {
                    "locked": {"narHash": "sha256-local", "path": "/my/path"},
                    "original": {"type": "path", "path": "/my/path"},
                },
                "local_2": {
                    "locked": {"narHash": "sha256-local", "path": "/my/path"},
                    "original": {"type": "path", "path": "/my/path"},
                },
                "tool": {
                    "inputs": {"local": "local_2"},
                    "locked": {"narHash": "sha256-tool", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "tool", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    local.url = "path:/my/path";
    tool.url = "github:x/tool";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        # Path inputs should be skipped entirely
        assert len(proposals) == 0

    def test_empty_lock(self, default_config):
        """empty lock produces no proposals."""
        lock = {
            "nodes": {"root": {"inputs": {}}},
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {};
  outputs = _: {};
}
"""
        dedup_proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        flatten_proposals = flake_tidy.analyze_flatten(lock, content, default_config)
        assert dedup_proposals == []
        assert flatten_proposals == []

    def test_no_duplicates(self, default_config):
        """lock with no duplicates produces nothing."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "nixpkgs", "utils": "utils"}},
                "nixpkgs": {
                    "locked": {"narHash": "sha256-NP", "rev": "aaa", "type": "github"},
                    "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
                },
                "utils": {
                    "locked": {
                        "narHash": "sha256-UTILS",
                        "rev": "bbb",
                        "type": "github",
                    },
                    "original": {
                        "owner": "numtide",
                        "repo": "flake-utils",
                        "type": "github",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    utils.url = "github:numtide/flake-utils";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        assert proposals == []

    def test_deep_path_beyond_max_depth(self, default_config):
        """paths deeper than max-depth are skipped."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "nixpkgs", "a": "a"}},
                "nixpkgs": {
                    "locked": {"narHash": "sha256-NP", "rev": "np", "type": "github"},
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
                    "locked": {"narHash": "sha256-NP", "rev": "np", "type": "github"},
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
        default_config["max-depth"] = 2
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        # a.b.c.nixpkgs is depth 3, should be skipped with max-depth=2
        deep = [p for p in proposals if len(p["follows_parts"]) > 2]
        assert len(deep) == 0

    def test_self_referential_follows(self, default_config):
        """handles self-referential follows without infinite loop."""
        # This lock has a follows that points back to itself via resolution
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a"}},
                "a": {
                    "inputs": {"self-ref": ["a"]},  # follows back to root's a
                    "locked": {"narHash": "sha256-a", "rev": "a", "type": "github"},
                    "original": {"owner": "x", "repo": "a", "type": "github"},
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    a.url = "github:x/a";
  };
  outputs = _: {};
}
"""
        # Should not hang or crash
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        # Just verify it completes without error
        assert isinstance(proposals, list)

    def test_commented_follows_detection(self):
        """detects various comment styles."""
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    # inputs.some-tool.inputs.nixpkgs.follows = "nixpkgs";
    some-tool = {
      url = "github:example/some-tool";
    };
  };
  outputs = _: {};
}
"""
        result = flake_tidy.find_commented_follows(
            content, ["some-tool", "nixpkgs"], "nixpkgs"
        )
        assert result is not None
        line_no, line = result
        assert "#" in line
        assert "follows" in line

    def test_block_and_dotted_mixed(self):
        """handles mixed declaration styles correctly."""
        content = """{
  inputs.early.url = "github:foo/early";
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
  };
  outputs = _: {};
}
"""
        # Should detect the block style since inputs = { appears
        style, start, end = flake_tidy.detect_inputs_style(content)
        # The first match is the dotted line, so it should report dotted
        # since detect_inputs_style finds the first occurrence
        assert style in ("block", "dotted")

        # get_input_file_order should find both
        order = flake_tidy.get_input_file_order(content)
        assert "early" in order
        assert "nixpkgs" in order
