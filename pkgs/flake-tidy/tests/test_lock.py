"""Tests for lock file analysis functions."""

import os
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy


class TestSourceKey:
    def test_source_key_github(self):
        """github owner/repo key."""
        node = {
            "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
        }
        key = flake_tidy.source_key(node)
        assert key == "github/nixos/nixpkgs/"
        # No ref means empty ref component

    def test_source_key_github_with_ref(self):
        """github with ref."""
        node = {
            "original": {
                "owner": "nixos",
                "repo": "nixpkgs",
                "ref": "nixos-24.05",
                "type": "github",
            },
        }
        key = flake_tidy.source_key(node)
        assert key == "github/nixos/nixpkgs/nixos-24.05"

    def test_source_key_indirect(self):
        """indirect type."""
        node = {
            "original": {"id": "nixpkgs", "type": "indirect"},
        }
        key = flake_tidy.source_key(node)
        assert key == "indirect/nixpkgs"

    def test_source_key_path(self):
        """path type."""
        node = {
            "original": {"path": "/home/user/myflake", "type": "path"},
        }
        key = flake_tidy.source_key(node)
        assert key == "path//home/user/myflake"


class TestIsPathInput:
    def test_is_path_input_true(self):
        """path input detected."""
        node = {"original": {"type": "path", "path": "/some/path"}}
        assert flake_tidy.is_path_input(node) is True

    def test_is_path_input_false(self):
        """non-path input."""
        node = {"original": {"type": "github", "owner": "nixos", "repo": "nixpkgs"}}
        assert flake_tidy.is_path_input(node) is False


class TestNodeUrl:
    def test_node_url_github(self):
        """readable URL for github."""
        node = {
            "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
        }
        url = flake_tidy.node_url(node)
        assert url == "github:nixos/nixpkgs"

    def test_node_url_indirect(self):
        """readable URL for indirect."""
        node = {
            "original": {"id": "nixpkgs", "type": "indirect"},
        }
        url = flake_tidy.node_url(node)
        assert url == "indirect:nixpkgs"


class TestLockedHash:
    def test_locked_hash_narhash(self):
        """returns narHash."""
        node = {
            "locked": {
                "narHash": "sha256-ABCDEF",
                "rev": "deadbeef",
            },
        }
        assert flake_tidy.locked_hash(node) == "sha256-ABCDEF"

    def test_locked_hash_rev_fallback(self):
        """falls back to rev."""
        node = {
            "locked": {
                "rev": "deadbeef",
            },
        }
        assert flake_tidy.locked_hash(node) == "deadbeef"


class TestSourceKeyRevGrouping:
    def test_rev_not_part_of_source_key(self):
        """#509: two pins of one repo at different revs share a key."""
        def node(rev):
            return {
                "original": {
                    "owner": "nixos",
                    "repo": "nixpkgs",
                    "rev": rev,
                    "type": "github",
                }
            }

        assert flake_tidy.source_key(node("aaaa")) == flake_tidy.source_key(
            node("bbbb")
        )

    def test_rev_pinned_key_differs_from_unpinned(self):
        """#512: a rev pin must not group with an unpinned node of the
        same repo (whose key the #509 key collided with exactly)."""
        pinned = {
            "original": {
                "owner": "nixos",
                "repo": "nixpkgs",
                "rev": "aaaa",
                "type": "github",
            }
        }
        unpinned = {
            "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"}
        }
        assert flake_tidy.source_key(pinned) != flake_tidy.source_key(unpinned)

    def test_rev_pinned_key_differs_from_ref_only(self):
        """#512: a rev pin (even alongside a ref) must not group with a
        ref-only tracking node of the same repo and ref."""
        pinned = {
            "original": {
                "owner": "nixos",
                "repo": "nixpkgs",
                "ref": "main",
                "rev": "aaaa",
                "type": "github",
            }
        }
        ref_only = {
            "original": {
                "owner": "nixos",
                "repo": "nixpkgs",
                "ref": "main",
                "type": "github",
            }
        }
        assert flake_tidy.source_key(pinned) != flake_tidy.source_key(ref_only)


class TestSourceKeyGit:
    """#519: git-type inputs fell through to source_key's JSON-dump
    fallback, which embeds the rev value — the #509 bug for git inputs,
    and pin-ness only separated by accident."""

    @staticmethod
    def _node(**orig):
        base = {"type": "git", "url": "https://example.com/tool.git"}
        base.update(orig)
        return {"original": base}

    def test_rev_not_part_of_source_key(self):
        """Two git pins of one URL at different revs share a key."""
        assert flake_tidy.source_key(self._node(rev="aaaa")) == (
            flake_tidy.source_key(self._node(rev="bbbb"))
        )

    def test_rev_pinned_key_differs_from_unpinned(self):
        """A git rev pin must not group with an unpinned git node."""
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(self._node())
        )

    def test_rev_pinned_key_differs_from_ref_only(self):
        """A git rev pin alongside a ref must not group with ref-only."""
        assert flake_tidy.source_key(
            self._node(rev="aaaa", ref="main")
        ) != flake_tidy.source_key(self._node(ref="main"))

    def test_url_and_ref_still_distinguish(self):
        """Different URLs, or different refs, keep separate keys."""
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(
                self._node(rev="aaaa", url="https://example.com/other.git")
            )
        )
        assert flake_tidy.source_key(self._node(ref="main")) != (
            flake_tidy.source_key(self._node(ref="dev"))
        )

    def test_other_original_fields_still_distinguish(self):
        """Fields like submodules describe a different fetch of the same
        URL; only the rev value is dropped from the key."""
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(self._node(rev="aaaa", submodules=True))
        )

    def test_node_url_git_is_the_url_not_a_json_dump(self):
        """node_url for a git node returns the URL (ref/rev readable),
        so exclusion patterns can match it."""
        assert (
            flake_tidy.node_url(self._node())
            == "https://example.com/tool.git"
        )
        assert (
            flake_tidy.node_url(self._node(rev="aaaa111122223333"))
            == "https://example.com/tool.git/aaaa11112222"
        )
        assert (
            flake_tidy.node_url(self._node(ref="main"))
            == "https://example.com/tool.git/main"
        )
