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


class TestCrossTypeDedup:
    """Tests for cross-type hash-based dedup (e.g. indirect vs github)."""

    def _make_cross_type_lock(
        self,
        root_type="github",
        trans_type="indirect",
        same_hash=True,
    ):
        """Build a lock where a root node and a transitive node differ in
        original type but may have the same locked hash."""
        root_hash = "sha256-UTILS-HASH"
        trans_hash = root_hash if same_hash else "sha256-DIFFERENT"
        root_original = (
            {"owner": "numtide", "repo": "flake-utils", "type": root_type}
            if root_type in ("github", "gitlab")
            else {"url": "https://github.com/numtide/flake-utils.git", "type": root_type}
            if root_type in ("git", "hg")
            else {"id": "flake-utils", "type": root_type}
        )
        trans_original = (
            {"id": "flake-utils", "type": trans_type}
            if trans_type == "indirect"
            else {"url": "https://github.com/numtide/flake-utils.git", "type": trans_type}
            if trans_type in ("git", "hg")
            else {"owner": "numtide", "repo": "flake-utils", "type": trans_type}
        )
        return {
            "nodes": {
                "root": {
                    "inputs": {
                        "flake-utils": "flake-utils",
                        "mac-app-util": "mac-app-util",
                    }
                },
                "flake-utils": {
                    "locked": {
                        "narHash": root_hash,
                        "rev": "aaaa",
                        "type": "github",
                    },
                    "original": root_original,
                },
                "flake-utils_2": {
                    "locked": {
                        "narHash": trans_hash,
                        "rev": "aaaa" if same_hash else "bbbb",
                        "type": "github",
                    },
                    "original": trans_original,
                },
                "mac-app-util": {
                    "inputs": {"flake-utils": "flake-utils_2"},
                    "locked": {
                        "narHash": "sha256-MAU",
                        "rev": "cccc",
                        "type": "github",
                    },
                    "original": {
                        "owner": "hraban",
                        "repo": "mac-app-util",
                        "type": "github",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }

    CONTENT = """{
  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""

    def test_cross_type_indirect_follows_github(self, default_config):
        """indirect transitive node follows github root with same hash."""
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=True
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and p["target"] == "flake-utils"
        ]
        assert len(cross) == 1, f"Expected cross-type dedup proposal, got: {proposals}"
        assert "cross-type" in cross[0].get("source_key", "")

    def test_cross_type_skips_different_hash(self, default_config):
        """no proposal when hashes differ."""
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=False
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
        ]
        assert len(cross) == 0

    def test_cross_type_prefers_github_over_indirect_at_root(self, default_config):
        """when root is github, indirect transitive follows it (not the reverse)."""
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=True
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
        ]
        assert len(cross) == 1
        assert cross[0]["target"] == "flake-utils"

    def test_cross_type_skips_if_transitive_more_explicit(self, default_config):
        """skip if transitive node is more explicit (github) than root (indirect)."""
        lock = self._make_cross_type_lock(
            root_type="indirect", trans_type="github", same_hash=True
        )
        # Adjust content to match indirect root
        content = """{
  inputs = {
    flake-utils.url = "indirect:flake-utils";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and "cross-type" in p.get("source_key", "")
        ]
        assert len(cross) == 0

    def test_cross_type_indirect_follows_hg(self, default_config):
        """#539: hg was unlisted in _TYPE_PREF (default 99, below
        indirect), so an explicit hg root was never chosen."""
        lock = self._make_cross_type_lock(
            root_type="hg", trans_type="indirect", same_hash=True
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and p["target"] == "flake-utils"
        ]
        assert len(cross) == 1, f"Expected cross-type dedup proposal, got: {proposals}"

    def test_cross_type_skips_if_transitive_hg_more_explicit(
        self, default_config
    ):
        """#539: an hg transitive node must not be folded into an
        indirect root — hg ranks above indirect."""
        lock = self._make_cross_type_lock(
            root_type="indirect", trans_type="hg", same_hash=True
        )
        content = """{
  inputs = {
    flake-utils.url = "indirect:flake-utils";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and "cross-type" in p.get("source_key", "")
        ]
        assert len(cross) == 0

    def test_cross_type_respects_excludes(self, default_config):
        """excluded inputs are not cross-type deduped."""
        default_config["exclude"]["dedup"] = ["mac-app-util"]
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=True
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
        ]
        assert len(cross) == 0

    def test_cross_type_respects_includes(self, default_config):
        """only included parents are cross-type deduped."""
        default_config["include"]["dedup"] = ["some-other-input"]
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=True
        )
        proposals = flake_tidy.analyze_dedup(lock, self.CONTENT, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
        ]
        assert len(cross) == 0

    def test_cross_type_skips_existing_follows(self, default_config):
        """no proposal if follows already declared in flake.nix."""
        lock = self._make_cross_type_lock(
            root_type="github", trans_type="indirect", same_hash=True
        )
        content = """{
  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    mac-app-util = {
      url = "github:hraban/mac-app-util";
      inputs.flake-utils.follows = "flake-utils";
    };
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
        ]
        assert len(cross) == 0

    def test_cross_type_git_over_indirect(self, default_config):
        """git root is preferred over indirect transitive (#526).

        Regression: _TYPE_PREF had no git entry, so git defaulted to 99 —
        below indirect (6) — and this proposal was never made.
        """
        lock = self._make_cross_type_lock(
            root_type="git", trans_type="indirect", same_hash=True
        )
        content = """{
  inputs = {
    flake-utils.url = "git+https://github.com/numtide/flake-utils.git";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and p["target"] == "flake-utils"
            and "cross-type" in p.get("source_key", "")
        ]
        assert len(cross) == 1, f"Expected cross-type dedup proposal, got: {proposals}"

    def test_cross_type_skips_if_transitive_git_more_explicit(self, default_config):
        """skip if transitive node is git and root is indirect (#526).

        Regression: with git at the default rank 99, an explicit git
        transitive input was proposed as a follows to an indirect root,
        discarding the explicit URL.
        """
        lock = self._make_cross_type_lock(
            root_type="indirect", trans_type="git", same_hash=True
        )
        content = """{
  inputs = {
    flake-utils.url = "indirect:flake-utils";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and "cross-type" in p.get("source_key", "")
        ]
        assert len(cross) == 0, f"Expected no cross-type proposal, got: {proposals}"

    def test_cross_type_gitlab_over_indirect(self, default_config):
        """gitlab root also preferred over indirect transitive."""
        lock = self._make_cross_type_lock(
            root_type="gitlab", trans_type="indirect", same_hash=True
        )
        content = """{
  inputs = {
    flake-utils.url = "gitlab:numtide/flake-utils";
    mac-app-util.url = "github:hraban/mac-app-util";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        cross = [
            p
            for p in proposals
            if p["follows_parts"] == ["mac-app-util", "flake-utils"]
            and p["target"] == "flake-utils"
        ]
        assert len(cross) == 1


class TestAnalyzeDedupRevDivergence:
    """#509: same repo pinned at two different revs must dedup to the
    newest locked lastModified instead of never being grouped."""

    @staticmethod
    def _node(rev, last_modified, nar_hash, pin=True):
        original = {"owner": "o", "repo": "r", "type": "github"}
        if pin:
            original["rev"] = rev
        return {
            "locked": {
                "lastModified": last_modified,
                "narHash": nar_hash,
                "owner": "o",
                "repo": "r",
                "rev": rev,
                "type": "github",
            },
            "original": original,
        }

    def _two_root_lock(self, old_lm=100, new_lm=200):
        return {
            "nodes": {
                "root": {"inputs": {"old": "old", "new": "new"}},
                "old": self._node("aaaa", old_lm, "sha256-A"),
                "new": self._node("bbbb", new_lm, "sha256-B"),
            },
            "root": "root",
            "version": 7,
        }

    _TWO_ROOT_CONTENT = """{
  inputs = {
    old.url = "github:o/r/aaaa";
    new.url = "github:o/r/bbbb";
  };
  outputs = _: {};
}
"""

    def test_dedup_to_newest_rev(self, default_config):
        """The older pin follows the newest, not the first in file order
        (old is declared first; new must still win on lastModified)."""
        proposals = flake_tidy.analyze_dedup(
            self._two_root_lock(), self._TWO_ROOT_CONTENT, default_config
        )
        root_dedup = [p for p in proposals if p["follows_parts"] == ["old"]]
        assert len(root_dedup) == 1, f"proposals: {proposals}"
        p = root_dedup[0]
        assert p["target"] == "new"
        assert p.get("rev_dedup") is True
        assert p["kept_rev"] == "bbbb"
        assert p["dropped_rev"] == "aaaa"
        assert "bbbb" in p["desc"] and "aaaa" in p["desc"]

    def test_equal_last_modified_is_deterministic(self, default_config):
        """Equal lastModified: the rev string breaks the tie, whichever
        input is declared first."""
        proposals = flake_tidy.analyze_dedup(
            self._two_root_lock(old_lm=150, new_lm=150),
            self._TWO_ROOT_CONTENT,
            default_config,
        )
        root_dedup = [p for p in proposals if p["follows_parts"] == ["old"]]
        assert len(root_dedup) == 1
        assert root_dedup[0]["target"] == "new"
        assert root_dedup[0]["kept_rev"] == "bbbb"

    def test_rev_divergent_pair_excluded_is_left_alone(self, default_config):
        """A pair where the stale side is excluded by config is untouched."""
        default_config["exclude"]["dedup"] = ["old"]
        proposals = flake_tidy.analyze_dedup(
            self._two_root_lock(), self._TWO_ROOT_CONTENT, default_config
        )
        assert [p for p in proposals if p["follows_parts"] == ["old"]] == []

    def test_transitive_dedup_to_newest_root(self, default_config):
        """A dependency's older pin of a root source follows the root."""
        lock = {
            "nodes": {
                "root": {"inputs": {"lib": "lib", "tool": "tool"}},
                "lib": self._node("bbbb", 200, "sha256-B"),
                "lib_2": self._node("aaaa", 100, "sha256-A"),
                "tool": {
                    "inputs": {"lib": "lib_2"},
                    "locked": {
                        "lastModified": 100,
                        "narHash": "sha256-T",
                        "owner": "example",
                        "repo": "tool",
                        "rev": "tttt",
                        "type": "github",
                    },
                    "original": {
                        "owner": "example",
                        "repo": "tool",
                        "type": "github",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    lib.url = "github:o/r/bbbb";
    tool.url = "github:example/tool";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        trans = [
            p
            for p in proposals
            if p["follows_parts"] == ["tool", "lib"] and p["target"] == "lib"
        ]
        assert len(trans) == 1, f"proposals: {proposals}"
        assert trans[0].get("rev_dedup") is True
        assert trans[0]["kept_rev"] == "bbbb"
        assert trans[0]["dropped_rev"] == "aaaa"


class TestAnalyzeDedupPinnedVsUnpinned:
    """#512: a rev-pinned input must never be deduped against an
    unpinned (no rev, no ref) input of the same repo — in either
    direction. #509's pin-vs-pin dedup is unaffected (covered by
    TestAnalyzeDedupRevDivergence)."""

    _node = staticmethod(TestAnalyzeDedupRevDivergence._node)

    def _lock(self, pinned_lm, plain_lm):
        return {
            "nodes": {
                "root": {"inputs": {"pinned": "pinned", "plain": "plain"}},
                "pinned": self._node("aaaa", pinned_lm, "sha256-A", pin=True),
                "plain": self._node("bbbb", plain_lm, "sha256-B", pin=False),
            },
            "root": "root",
            "version": 7,
        }

    _CONTENT = """{
  inputs = {
    pinned.url = "github:o/r/aaaa";
    plain.url = "github:o/r";
  };
  outputs = _: {};
}
"""

    def test_pinned_older_than_unpinned_no_proposal(self, default_config):
        """The deliberate (older) pin is not consolidated away into the
        newer unpinned input."""
        proposals = flake_tidy.analyze_dedup(
            self._lock(pinned_lm=100, plain_lm=200),
            self._CONTENT,
            default_config,
        )
        assert proposals == [], f"proposals: {proposals}"

    def test_pinned_newer_than_unpinned_no_proposal(self, default_config):
        """The unpinned input is not frozen onto the newer pin."""
        # Pinned locked rev must be the newest for this direction.
        lock = self._lock(pinned_lm=300, plain_lm=200)
        lock["nodes"]["pinned"] = self._node("cccc", 300, "sha256-C", pin=True)
        proposals = flake_tidy.analyze_dedup(lock, self._CONTENT, default_config)
        assert proposals == [], f"proposals: {proposals}"

    def test_ref_tracking_control_stays_ungrouped(self, default_config):
        """Control: an unpinned node WITH a ref already kept a distinct
        key — that must stay non-grouped too."""
        lock = self._lock(pinned_lm=100, plain_lm=200)
        lock["nodes"]["plain"]["original"]["ref"] = "main"
        proposals = flake_tidy.analyze_dedup(lock, self._CONTENT, default_config)
        assert proposals == [], f"proposals: {proposals}"

    def test_transitive_unpinned_not_folded_into_pinned_root(
        self, default_config
    ):
        """A dependency's unpinned copy of a pinned root source is not
        pointed at the pin via transitive dedup."""
        lock = {
            "nodes": {
                "root": {"inputs": {"lib": "lib", "tool": "tool"}},
                "lib": self._node("bbbb", 200, "sha256-B", pin=True),
                "lib_2": self._node("aaaa", 100, "sha256-A", pin=False),
                "tool": {
                    "inputs": {"lib": "lib_2"},
                    "locked": {
                        "lastModified": 100,
                        "narHash": "sha256-T",
                        "owner": "example",
                        "repo": "tool",
                        "rev": "tttt",
                        "type": "github",
                    },
                    "original": {
                        "owner": "example",
                        "repo": "tool",
                        "type": "github",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    lib.url = "github:o/r/bbbb";
    tool.url = "github:example/tool";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        assert [
            p for p in proposals if p["follows_parts"] == ["tool", "lib"]
        ] == [], f"proposals: {proposals}"


class TestAnalyzeDedupGitRevDivergence:
    """#519: same git URL pinned at two different revs must dedup to the
    newest locked lastModified — the #509 consolidation, for git-type
    inputs (which source_key's fallback never grouped)."""

    _URL = "https://example.com/tool.git"

    @classmethod
    def _node(cls, rev, last_modified, nar_hash, pin=True):
        original = {"type": "git", "url": cls._URL}
        if pin:
            original["rev"] = rev
        return {
            "locked": {
                "lastModified": last_modified,
                "narHash": nar_hash,
                "rev": rev,
                "type": "git",
                "url": cls._URL,
            },
            "original": original,
        }

    def _two_root_lock(self, pinned_old=True, pinned_new=True):
        return {
            "nodes": {
                "root": {"inputs": {"old": "old", "new": "new"}},
                "old": self._node("aaaa", 100, "sha256-A", pin=pinned_old),
                "new": self._node("bbbb", 200, "sha256-B", pin=pinned_new),
            },
            "root": "root",
            "version": 7,
        }

    _TWO_ROOT_CONTENT = """{
  inputs = {
    old.url = "git+https://example.com/tool.git?rev=aaaa";
    new.url = "git+https://example.com/tool.git?rev=bbbb";
  };
  outputs = _: {};
}
"""

    def test_git_dedup_to_newest_rev(self, default_config):
        proposals = flake_tidy.analyze_dedup(
            self._two_root_lock(), self._TWO_ROOT_CONTENT, default_config
        )
        root_dedup = [p for p in proposals if p["follows_parts"] == ["old"]]
        assert len(root_dedup) == 1, f"proposals: {proposals}"
        p = root_dedup[0]
        assert p["target"] == "new"
        assert p.get("rev_dedup") is True
        assert p["kept_rev"] == "bbbb"
        assert p["dropped_rev"] == "aaaa"

    def test_git_pinned_vs_unpinned_no_proposal(self, default_config):
        """#512 for git: a pinned input and an unpinned one of the same
        URL propose nothing, in either pin position."""
        for lock in (
            self._two_root_lock(pinned_old=True, pinned_new=False),
            self._two_root_lock(pinned_old=False, pinned_new=True),
        ):
            proposals = flake_tidy.analyze_dedup(
                lock, self._TWO_ROOT_CONTENT, default_config
            )
            assert proposals == [], f"proposals: {proposals}"


class TestAnalyzeDedupUrlSpelling:
    """#555: two root inputs naming the same git repo in different URL
    spellings (".git" suffix vs trailing slash) lock to identical
    content but never grouped before spelling normalization."""

    @staticmethod
    def _node(url):
        return {
            "locked": {
                "lastModified": 100,
                "narHash": "sha256-A",
                "rev": "aaaa",
                "type": "git",
                "url": url,
            },
            "original": {"type": "git", "url": url, "rev": "aaaa"},
        }

    def test_spelling_variants_dedup(self, default_config):
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a", "b": "b"}},
                "a": self._node("https://example.com/tool.git"),
                "b": self._node("https://example.com/tool/"),
            },
            "root": "root",
            "version": 7,
        }
        content = """{
  inputs = {
    a.url = "git+https://example.com/tool.git?rev=aaaa";
    b.url = "git+https://example.com/tool/?rev=aaaa";
  };
  outputs = _: {};
}
"""
        proposals = flake_tidy.analyze_dedup(lock, content, default_config)
        assert len(proposals) == 1, f"proposals: {proposals}"
        assert proposals[0]["follows_parts"] in (["a"], ["b"])


class TestNormalizeVcsUrlQuery:
    """#560: a raw git URL with an embedded query (?shallow=1, #528)
    defeated the #555 suffix rules — ".git" is not at the end of
    "repo.git?shallow=1" — and its query values were lowercased on
    github/gitlab hosts. The query is split off first and reattached
    verbatim."""

    def test_dotgit_before_query_groups(self):
        a = flake_tidy._normalize_vcs_url("https://github.com/Foo/Bar.git?shallow=1")
        b = flake_tidy._normalize_vcs_url("https://github.com/foo/bar?shallow=1")
        assert a == b == "https://github.com/foo/bar?shallow=1"

    def test_trailing_slash_before_query_groups(self):
        a = flake_tidy._normalize_vcs_url("https://github.com/foo/bar/?shallow=1")
        b = flake_tidy._normalize_vcs_url("https://github.com/foo/bar?shallow=1")
        assert a == b

    def test_query_reattached_verbatim(self):
        url = "https://github.com/foo/bar?dir=Sub/Flake&ref=FeatureBranch"
        assert flake_tidy._normalize_vcs_url(url) == url

    def test_source_key_groups_dotgit_before_query(self):
        def node(url):
            return {"original": {"type": "git", "url": url}}

        assert flake_tidy.source_key(
            node("https://example.com/tool.git?shallow=1")
        ) == flake_tidy.source_key(node("https://example.com/tool?shallow=1"))

    def test_source_key_keeps_query_case_distinct(self):
        def node(url):
            return {"original": {"type": "git", "url": url}}

        assert flake_tidy.source_key(
            node("https://github.com/foo/bar?dir=Sub/Flake")
        ) != flake_tidy.source_key(
            node("https://github.com/foo/bar?dir=sub/flake")
        )


class TestNormalizeVcsUrlTrailingDotHost:
    """#584: a trailing dot on the host is the DNS root label —
    "github.com." is the same host as "github.com" — but the host was
    kept verbatim, so the spelling split the dedup group in both the
    "://" and scp-like branches; on forge hosts the dot also defeated
    the _CASE_INSENSITIVE_PATH_HOSTS membership test, so the path case
    rule silently stopped applying to that spelling."""

    def test_trailing_dot_groups_url_branch(self):
        a = flake_tidy._normalize_vcs_url("https://github.com./owner/repo")
        b = flake_tidy._normalize_vcs_url("https://github.com/owner/repo")
        assert a == b == "https://github.com/owner/repo"

    def test_trailing_dot_groups_scp_branch(self):
        a = flake_tidy._normalize_vcs_url("git@github.com.:owner/repo")
        b = flake_tidy._normalize_vcs_url("git@github.com:owner/repo")
        assert a == b == "git@github.com:owner/repo"

    def test_trailing_dot_keeps_path_case_rule(self):
        a = flake_tidy._normalize_vcs_url("https://github.com./Owner/Repo")
        b = flake_tidy._normalize_vcs_url("https://github.com/owner/repo")
        assert a == b

    def test_trailing_dot_case_sensitive_host_keeps_path_case(self):
        a = flake_tidy._normalize_vcs_url("https://example.com./Repo")
        b = flake_tidy._normalize_vcs_url("https://example.com/Repo")
        assert a == b == "https://example.com/Repo"

    def test_only_one_trailing_dot_stripped(self):
        assert flake_tidy._normalize_vcs_url(
            "https://example.com../Repo"
        ) == "https://example.com./Repo"

    def test_trailing_dot_with_default_port_groups(self):
        a = flake_tidy._normalize_vcs_url("https://github.com.:443/owner/repo")
        b = flake_tidy._normalize_vcs_url("https://github.com/owner/repo")
        assert a == b


class TestForgeSourceKeyHostSpelling:
    """#599: for forge (github/gitlab/sourcehut) inputs the host extra
    was copied verbatim into source_key while owner/repo were
    lowercased — so one enterprise/self-hosted server written with a
    different host case, or with the DNS root-label trailing dot,
    split the dedup group."""

    @staticmethod
    def _node(host, **extra):
        original = {
            "type": "github",
            "owner": "foo",
            "repo": "bar",
            "host": host,
        }
        original.update(extra)
        return {"original": original}

    def test_host_case_variants_group(self):
        a = flake_tidy.source_key(self._node("GitHub.Example.com"))
        b = flake_tidy.source_key(self._node("github.example.com"))
        assert a == b

    def test_host_trailing_dot_groups(self):
        a = flake_tidy.source_key(self._node("github.example.com."))
        b = flake_tidy.source_key(self._node("github.example.com"))
        assert a == b

    def test_different_hosts_still_split(self):
        a = flake_tidy.source_key(self._node("github.example.com"))
        b = flake_tidy.source_key(self._node("gitlab.example.com"))
        assert a != b

    def test_dir_case_still_splits(self):
        a = flake_tidy.source_key(self._node("github.example.com", dir="Sub"))
        b = flake_tidy.source_key(self._node("github.example.com", dir="sub"))
        assert a != b


class TestAnalyzeDedupHgRevDivergence:
    """#539: same hg URL pinned at two different revs must dedup to
    the newest locked lastModified — the #509 consolidation, for
    hg-type inputs (which source_key's fallback never grouped)."""

    _URL = "https://example.com/repo"

    @classmethod
    def _node(cls, rev, last_modified, nar_hash, pin=True):
        original = {"type": "hg", "url": cls._URL}
        if pin:
            original["rev"] = rev
        return {
            "locked": {
                "lastModified": last_modified,
                "narHash": nar_hash,
                "rev": rev,
                "type": "hg",
                "url": cls._URL,
            },
            "original": original,
        }

    def _two_root_lock(self, pinned_old=True, pinned_new=True):
        return {
            "nodes": {
                "root": {"inputs": {"old": "old", "new": "new"}},
                "old": self._node("aaaa", 100, "sha256-A", pin=pinned_old),
                "new": self._node("bbbb", 200, "sha256-B", pin=pinned_new),
            },
            "root": "root",
            "version": 7,
        }

    _TWO_ROOT_CONTENT = """{
  inputs = {
    old.url = "hg+https://example.com/repo?rev=aaaa";
    new.url = "hg+https://example.com/repo?rev=bbbb";
  };
  outputs = _: {};
}
"""

    def test_hg_dedup_to_newest_rev(self, default_config):
        proposals = flake_tidy.analyze_dedup(
            self._two_root_lock(), self._TWO_ROOT_CONTENT, default_config
        )
        root_dedup = [p for p in proposals if p["follows_parts"] == ["old"]]
        assert len(root_dedup) == 1, f"proposals: {proposals}"
        p = root_dedup[0]
        assert p["target"] == "new"
        assert p.get("rev_dedup") is True
        assert p["kept_rev"] == "bbbb"
        assert p["dropped_rev"] == "aaaa"

    def test_hg_pinned_vs_unpinned_no_proposal(self, default_config):
        for lock in (
            self._two_root_lock(pinned_old=True, pinned_new=False),
            self._two_root_lock(pinned_old=False, pinned_new=True),
        ):
            proposals = flake_tidy.analyze_dedup(
                lock, self._TWO_ROOT_CONTENT, default_config
            )
            assert proposals == [], f"proposals: {proposals}"
