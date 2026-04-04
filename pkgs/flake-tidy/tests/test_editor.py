"""Tests for flake.nix editing functions."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestDetectInputsStyle:
    def test_detect_inputs_style_block(self, sample_flake_nix):
        """detects block style."""
        style, start, end = flake_tidy.detect_inputs_style(sample_flake_nix)
        assert style == "block"
        assert start is not None
        assert end is not None
        assert start < end

    def test_detect_inputs_style_dotted(self):
        """detects dotted style."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.flake-utils.url = "github:numtide/flake-utils";
  outputs = _: {};
}
"""
        style, start, end = flake_tidy.detect_inputs_style(content)
        assert style == "dotted"
        assert start is None
        assert end is None


class TestGetInputFileOrder:
    def test_get_input_file_order_block(self, sample_flake_nix):
        """correct order in block style."""
        order = flake_tidy.get_input_file_order(sample_flake_nix)
        assert order == [
            "nixpkgs",
            "nixpkgs-unstable",
            "systems",
            "flake-utils",
            "some-tool",
        ]

    def test_get_input_file_order_mixed(self):
        """correct order with mixed styles."""
        content = """{
  inputs.early.url = "github:foo/early";
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    systems.url = "github:nix-systems/default";
  };
  inputs.late.url = "github:foo/late";
  outputs = _: {};
}
"""
        order = flake_tidy.get_input_file_order(content)
        # "early" comes first (dotted before block), then block entries, then "late"
        assert "early" in order
        assert "nixpkgs" in order
        assert "systems" in order
        assert "late" in order
        assert order.index("early") < order.index("nixpkgs")


class TestFollowsExists:
    def test_follows_exists_absolute(self):
        """detects absolute form."""
        content = """
  inputs.some-tool.inputs.nixpkgs.follows = "nixpkgs";
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["some-tool", "nixpkgs"], "nixpkgs"
            )
            is True
        )

    def test_follows_exists_relative(self):
        """detects relative form."""
        content = """{
  inputs = {
    some-tool = {
      url = "github:example/some-tool";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["some-tool", "nixpkgs"], "nixpkgs"
            )
            is True
        )

    def test_follows_not_exists(self, sample_flake_nix):
        """returns False when not present."""
        assert (
            flake_tidy.follows_exists_in_content(
                sample_flake_nix, ["some-tool", "nixpkgs"], "nixpkgs"
            )
            is False
        )


class TestInsertFollows:
    def test_insert_follows_block_style(self, sample_flake_nix):
        """inserts inside block."""
        result = flake_tidy.insert_follows_in_content(
            sample_flake_nix, ["some-tool", "nixpkgs"], "nixpkgs"
        )
        # The follows declaration should be present
        assert flake_tidy.follows_exists_in_content(
            result, ["some-tool", "nixpkgs"], "nixpkgs"
        )
        # Original content should still be present
        assert "some-tool" in result
        assert "github:example/some-tool" in result

    def test_insert_follows_dotted_style(self):
        """inserts after dotted lines."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.some-tool.url = "github:example/some-tool";
  outputs = _: {};
}
"""
        result = flake_tidy.insert_follows_in_content(
            content, ["some-tool", "nixpkgs"], "nixpkgs"
        )
        assert 'follows = "nixpkgs"' in result

    def test_insert_follows_multilevel(self, sample_flake_nix):
        """handles multi-level follows."""
        result = flake_tidy.insert_follows_in_content(
            sample_flake_nix, ["flake-utils", "systems"], "systems"
        )
        assert flake_tidy.follows_exists_in_content(
            result, ["flake-utils", "systems"], "systems"
        )


class TestInsertRootInput:
    def test_insert_root_input_block(self, sample_flake_nix):
        """adds new input in block style."""
        result = flake_tidy.insert_root_input(
            sample_flake_nix, "new-input", "github:foo/new-input"
        )
        assert "new-input" in result
        assert "github:foo/new-input" in result

    def test_insert_root_input_dotted(self):
        """adds new input in dotted style."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  outputs = _: {};
}
"""
        result = flake_tidy.insert_root_input(content, "new-input", "github:foo/bar")
        assert "new-input" in result
        assert "github:foo/bar" in result

    def test_insert_root_input_no_duplicate(self, sample_flake_nix):
        """doesn't duplicate existing input."""
        result = flake_tidy.insert_root_input(
            sample_flake_nix, "nixpkgs", "github:nixos/nixpkgs"
        )
        # Should be unchanged since nixpkgs already exists
        assert result == sample_flake_nix


class TestRootInputExists:
    def test_root_input_exists(self, sample_flake_nix):
        """detects existing input."""
        assert (
            flake_tidy.root_input_exists_in_content(sample_flake_nix, "nixpkgs") is True
        )
        assert (
            flake_tidy.root_input_exists_in_content(sample_flake_nix, "nonexistent")
            is False
        )


class TestUncommentFollows:
    def test_uncomment_follows(self):
        """uncomments a commented follows line."""
        content = """line0
    # inputs.some-tool.inputs.nixpkgs.follows = "nixpkgs";
line2"""
        result = flake_tidy.uncomment_line(content, 1)
        lines = result.split("\n")
        stripped = lines[1].strip()
        assert not stripped.startswith("#")
        assert 'follows = "nixpkgs"' in stripped
