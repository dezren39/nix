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
            else {"id": "flake-utils", "type": root_type}
        )
        trans_original = (
            {"id": "flake-utils", "type": trans_type}
            if trans_type == "indirect"
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
