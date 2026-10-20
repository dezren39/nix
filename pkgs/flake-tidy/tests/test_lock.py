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

    def test_node_url_path(self):
        """#524: readable URL for path — not a JSON blob, so exclusion
        patterns can match it."""
        node = {
            "original": {"path": "./vendor/foo", "type": "path"},
        }
        url = flake_tidy.node_url(node)
        assert url == "path:./vendor/foo"


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


class TestNodeOriginalUrl:
    """#521: node_original_url must return a nix-resolvable URL for every
    type source_key/node_url recognise — its git and sourcehut nodes fell
    through to a json.dumps blob that callers pass to --override-input
    and write into flake.nix as a hoisted root input's url."""

    @staticmethod
    def _node(**orig):
        return {"original": orig}

    def test_git_plain(self):
        assert flake_tidy.node_original_url(
            self._node(type="git", url="https://example.com/tool.git")
        ) == "git+https://example.com/tool.git"

    def test_git_ref(self):
        assert flake_tidy.node_original_url(
            self._node(type="git", url="https://example.com/tool.git", ref="main")
        ) == "git+https://example.com/tool.git?ref=main"

    def test_git_rev(self):
        assert flake_tidy.node_original_url(
            self._node(type="git", url="https://example.com/tool.git", rev="aaaa")
        ) == "git+https://example.com/tool.git?rev=aaaa"

    def test_git_ref_and_rev(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="git", url="https://example.com/tool.git", ref="main", rev="aaaa"
            )
        ) == "git+https://example.com/tool.git?ref=main&rev=aaaa"

    def test_sourcehut_ref(self):
        assert flake_tidy.node_original_url(
            self._node(type="sourcehut", owner="~user", repo="proj", ref="main")
        ) == "sourcehut:~user/proj/main"

    def test_sourcehut_rev_kept_when_no_ref(self):
        assert flake_tidy.node_original_url(
            self._node(type="sourcehut", owner="~user", repo="proj", rev="bbbb")
        ) == "sourcehut:~user/proj/bbbb"

    def test_github_rev_kept_when_no_ref(self):
        assert flake_tidy.node_original_url(
            self._node(type="github", owner="nixos", repo="nixpkgs", rev="cccc")
        ) == "github:nixos/nixpkgs/cccc"

    def test_path(self):
        """#524: a path node's original URL is the path flake reference,
        not the JSON blob --override-input would reject."""
        assert flake_tidy.node_original_url(
            self._node(type="path", path="./vendor/foo")
        ) == "path:./vendor/foo"

    def test_no_json_blob_for_recognised_types(self):
        for orig in (
            {"type": "git", "url": "https://example.com/tool.git", "ref": "main"},
            {"type": "sourcehut", "owner": "~user", "repo": "proj"},
            {"type": "github", "owner": "nixos", "repo": "nixpkgs", "rev": "cccc"},
            {"type": "path", "path": "./vendor/foo"},
        ):
            assert not flake_tidy.node_original_url(
                self._node(**orig)
            ).startswith("{")


class TestLockedUrlFallbacks:
    """Follow-up to #521: PR #522 fixed node_original_url and taught two
    locked fallbacks sourcehut, but a git-locked node behind an indirect
    original still produced no override in any of the three fallbacks
    (they enumerated only the forge types)."""

    @staticmethod
    def _indirect_lock(locked):
        return {
            "nodes": {
                "root": {"inputs": {"tool": "tool"}},
                "tool": {
                    "original": {"type": "indirect", "id": "mytool"},
                    "locked": locked,
                },
            },
            "root": "root",
            "version": 7,
        }

    _GIT_LOCKED = {
        "type": "git",
        "url": "https://example.com/tool.git",
        "rev": "aaaa1111",
    }
    _GIT_URL = "git+https://example.com/tool.git?rev=aaaa1111"

    def test_build_override_from_flake_name_git_locked(self):
        lock = self._indirect_lock(self._GIT_LOCKED)
        assert flake_tidy.build_override_from_flake_name(
            lock, "tool", "mytool"
        ) == {"tool": self._GIT_URL}

    def test_compute_indirect_overrides_git_locked(self):
        lock = self._indirect_lock(self._GIT_LOCKED)
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": self._GIT_URL
        }

    def test_resolve_transitive_url_git_locked(self):
        lock = {
            "nodes": {
                "root": {"inputs": {"parent": "parent"}},
                "parent": {"inputs": {"child": "child"}},
                "child": {
                    "original": {"type": "indirect", "id": "child"},
                    "locked": {
                        "type": "git",
                        "url": "https://example.com/child.git",
                        "rev": "cccc3333",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        assert flake_tidy._resolve_transitive_url(lock, "parent", "child") == (
            "git+https://example.com/child.git?rev=cccc3333"
        )

    def test_compute_indirect_overrides_sourcehut_locked(self):
        """The compute_indirect_overrides fallback only knew
        github/gitlab even after #522 — sourcehut was missing there."""
        lock = self._indirect_lock(
            {"type": "sourcehut", "owner": "~user", "repo": "proj", "rev": "bbbb"}
        )
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": "sourcehut:~user/proj"
        }

    def test_compute_indirect_overrides_tarball_locked(self):
        """#533: a tarball-locked node behind an indirect original
        produced no override — _locked_url only knew forge+git."""
        lock = self._indirect_lock(
            {
                "type": "tarball",
                "url": "https://example.com/tool.tar.gz",
                "narHash": "sha256-eeee",
            }
        )
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": "https://example.com/tool.tar.gz"
        }
        assert flake_tidy.build_override_from_flake_name(
            lock, "tool", "mytool"
        ) == {"tool": "https://example.com/tool.tar.gz"}

    def test_compute_indirect_overrides_file_locked(self):
        """#533: file-locked nodes get the file+ prefix for http URLs,
        mirroring node_original_url's file branch."""
        lock = self._indirect_lock(
            {
                "type": "file",
                "url": "https://example.com/tool",
                "narHash": "sha256-ffff",
            }
        )
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": "file+https://example.com/tool"
        }

    def test_locked_url_path_and_empty(self):
        """#533: path locked yields path:<path>; empty url/path -> None."""
        assert flake_tidy._locked_url(
            {"type": "path", "path": "./vendor/tool"}
        ) == "path:./vendor/tool"
        assert flake_tidy._locked_url({"type": "tarball"}) is None
        assert flake_tidy._locked_url({"type": "file"}) is None
        assert flake_tidy._locked_url({"type": "path"}) is None

    def test_locked_forge_shape_unchanged(self):
        """Guard: the forge locked shape stays type:owner/repo."""
        lock = self._indirect_lock(
            {"type": "github", "owner": "nixos", "repo": "nixpkgs", "rev": "dddd"}
        )
        assert flake_tidy.build_override_from_flake_name(
            lock, "tool", "mytool"
        ) == {"tool": "github:nixos/nixpkgs"}


class TestForgeHostAndDir:
    """#535: forge host/dir are source-distinguishing — host names a
    different server (GitHub Enterprise, self-hosted GitLab, hg.sr.ht)
    and dir a different sub-flake — but source_key grouped them away
    and every rebuilt URL dropped them."""

    @staticmethod
    def _node(**orig):
        return {"original": orig}

    def test_source_key_distinguishes_host(self):
        plain = self._node(type="gitlab", owner="g", repo="p")
        hosted = self._node(
            type="gitlab", owner="g", repo="p", host="gitlab.example.com"
        )
        assert flake_tidy.source_key(plain) != flake_tidy.source_key(hosted)

    def test_source_key_distinguishes_dir(self):
        a = self._node(type="github", owner="o", repo="r", dir="a")
        b = self._node(type="github", owner="o", repo="r", dir="b")
        plain = self._node(type="github", owner="o", repo="r")
        assert flake_tidy.source_key(a) != flake_tidy.source_key(b)
        assert flake_tidy.source_key(a) != flake_tidy.source_key(plain)

    def test_source_key_plain_forge_shape_unchanged(self):
        """Guard: nodes without extra fields keep the pre-#535 key."""
        assert (
            flake_tidy.source_key(self._node(type="github", owner="o", repo="r"))
            == "github/o/r/"
        )
        assert flake_tidy.source_key(
            self._node(type="gitlab", owner="g", repo="p", rev="aaaa")
        ).startswith("gitlab/g/p/:rev-pinned")

    def test_original_url_keeps_host(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="gitlab",
                owner="openldap",
                repo="openldap",
                host="git.openldap.org",
            )
        ) == "gitlab:openldap/openldap?host=git.openldap.org"

    def test_original_url_keeps_dir_and_host_with_ref(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="github",
                owner="internal",
                repo="project",
                ref="main",
                host="company-github.example.org",
                dir="sub",
            )
        ) == "github:internal/project/main?host=company-github.example.org&dir=sub"

    def test_original_url_keeps_sourcehut_host_with_rev(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="sourcehut",
                owner="~misterio",
                repo="nix-colors",
                host="hg.sr.ht",
                rev="21c1a380",
            )
        ) == "sourcehut:~misterio/nix-colors/21c1a380?host=hg.sr.ht"

    def test_locked_url_keeps_host(self):
        assert flake_tidy._locked_url(
            {
                "type": "gitlab",
                "owner": "openldap",
                "repo": "openldap",
                "host": "git.openldap.org",
                "rev": "aaaa1111",
            }
        ) == "gitlab:openldap/openldap?host=git.openldap.org"

    def test_node_url_surfaces_host_and_dir(self):
        s = flake_tidy.node_url(
            self._node(
                type="github", owner="o", repo="r", host="gh.example.com", dir="sub"
            )
        )
        assert "host=gh.example.com" in s
        assert "dir=sub" in s


class TestGitUrlFetchAttributes:
    """#528 + #538: the git URL rebuild dropped the boolean fetch
    attributes source_key treats as source-distinguishing —
    submodules (#528), shallow and allRefs (#538) — and
    node_original_url joined its params with a second '?' when the
    raw URL already carried a query."""

    @staticmethod
    def _node(**orig):
        base = {"type": "git", "url": "https://example.com/tool.git"}
        base.update(orig)
        return {"original": base}

    def test_original_url_keeps_submodules(self):
        assert flake_tidy.node_original_url(
            self._node(rev="aaaa1111", submodules=True)
        ) == "git+https://example.com/tool.git?rev=aaaa1111&submodules=1"
        assert flake_tidy.node_original_url(
            self._node(ref="main", submodules=True)
        ) == "git+https://example.com/tool.git?ref=main&submodules=1"
        assert flake_tidy.node_original_url(
            self._node(submodules=True)
        ) == "git+https://example.com/tool.git?submodules=1"

    def test_original_url_keeps_shallow_and_all_refs(self):
        assert flake_tidy.node_original_url(
            self._node(rev="aaaa1111", shallow=True)
        ) == "git+https://example.com/tool.git?rev=aaaa1111&shallow=1"
        assert flake_tidy.node_original_url(
            self._node(rev="aaaa1111", allRefs=True)
        ) == "git+https://example.com/tool.git?rev=aaaa1111&allRefs=1"
        assert flake_tidy.node_original_url(
            self._node(shallow=True)
        ) == "git+https://example.com/tool.git?shallow=1"

    def test_original_url_no_second_question_mark(self):
        """A raw URL already carrying ?shallow=1 joins params with &."""
        assert flake_tidy.node_original_url(
            self._node(
                url="https://example.com/tool.git?shallow=1", ref="main"
            )
        ) == "git+https://example.com/tool.git?shallow=1&ref=main"

    def test_false_attributes_not_emitted(self):
        """Guard: false is the fetch default — plain shape unchanged."""
        assert flake_tidy.node_original_url(
            self._node(rev="aaaa1111", submodules=False, shallow=False)
        ) == "git+https://example.com/tool.git?rev=aaaa1111"

    def test_locked_url_keeps_fetch_attributes(self):
        locked = {
            "type": "git",
            "url": "https://example.com/tool.git",
            "rev": "aaaa1111",
            "submodules": True,
        }
        assert (
            flake_tidy._locked_url(locked)
            == "git+https://example.com/tool.git?rev=aaaa1111&submodules=1"
        )
        assert flake_tidy._locked_url(
            {**locked, "submodules": False, "shallow": True}
        ) == "git+https://example.com/tool.git?rev=aaaa1111&shallow=1"
        assert flake_tidy._locked_url(
            {**locked, "submodules": False, "allRefs": True}
        ) == "git+https://example.com/tool.git?rev=aaaa1111&allRefs=1"

    def test_locked_fallback_keeps_submodules(self):
        """The indirect-override fallbacks surface the locked
        submodules flag via _locked_url."""
        lock = TestLockedUrlFallbacks._indirect_lock(
            {
                "type": "git",
                "url": "https://example.com/tool.git",
                "rev": "aaaa1111",
                "submodules": True,
            }
        )
        expected = {
            "tool": "git+https://example.com/tool.git?rev=aaaa1111&submodules=1"
        }
        assert flake_tidy.compute_indirect_overrides(lock) == expected
        assert (
            flake_tidy.build_override_from_flake_name(lock, "tool", "mytool")
            == expected
        )

    def test_node_url_surfaces_fetch_attributes(self):
        s = flake_tidy.node_url(self._node(rev="aaaa1111", shallow=True))
        assert "shallow=1" in s
        s = flake_tidy.node_url(self._node(allRefs=True, submodules=True))
        assert "allRefs=1" in s
        assert "submodules=1" in s
        # Guard: a plain node keeps its pre-existing readable shape.
        assert (
            flake_tidy.node_url(self._node())
            == "https://example.com/tool.git"
        )


class TestGenericDirAttribute:
    """#537: dir is a generic flake-ref attribute, not forge-only —
    tarball/file/path source_keys collided across dir, and every
    non-forge URL rebuild dropped it, pointing at the root flake."""

    @staticmethod
    def _node(**orig):
        return {"original": orig}

    def test_source_key_distinguishes_dir_for_tarball_file_path(self):
        for base in (
            {"type": "tarball", "url": "https://example.com/t.tar.gz"},
            {"type": "file", "url": "https://example.com/t"},
            {"type": "path", "path": "./vendor"},
        ):
            a = flake_tidy.source_key(self._node(**base, dir="a"))
            b = flake_tidy.source_key(self._node(**base, dir="b"))
            plain = flake_tidy.source_key(self._node(**base))
            assert a != b
            assert a != plain

    def test_source_key_plain_shape_unchanged_without_dir(self):
        """Guard: nodes without dir keep the pre-#537 key shape."""
        assert flake_tidy.source_key(
            self._node(type="tarball", url="https://example.com/t.tar.gz")
        ) == "tarball/https://example.com/t.tar.gz"
        assert flake_tidy.source_key(
            self._node(type="path", path="./vendor")
        ) == "path/./vendor"

    def test_original_url_keeps_dir(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="git", url="https://example.com/r.git", dir="flake1"
            )
        ) == "git+https://example.com/r.git?dir=flake1"
        assert flake_tidy.node_original_url(
            self._node(
                type="tarball",
                url="https://example.com/t.tar.gz",
                dir="sub",
            )
        ) == "https://example.com/t.tar.gz?dir=sub"
        assert flake_tidy.node_original_url(
            self._node(type="file", url="https://example.com/t", dir="sub")
        ) == "file+https://example.com/t?dir=sub"
        assert flake_tidy.node_original_url(
            self._node(type="path", path="./vendor", dir="sub")
        ) == "path:./vendor?dir=sub"

    def test_original_url_dir_joins_existing_query_with_ampersand(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="tarball",
                url="https://example.com/t.tar.gz?x=1",
                dir="sub",
            )
        ) == "https://example.com/t.tar.gz?x=1&dir=sub"

    def test_locked_url_keeps_dir(self):
        assert flake_tidy._locked_url(
            {
                "type": "git",
                "url": "https://example.com/r.git",
                "rev": "aaaa1111",
                "dir": "flake1",
            }
        ) == "git+https://example.com/r.git?rev=aaaa1111&dir=flake1"
        assert flake_tidy._locked_url(
            {
                "type": "tarball",
                "url": "https://example.com/t.tar.gz",
                "dir": "sub",
            }
        ) == "https://example.com/t.tar.gz?dir=sub"
        assert flake_tidy._locked_url(
            {"type": "file", "url": "https://example.com/t", "dir": "sub"}
        ) == "file+https://example.com/t?dir=sub"
        assert flake_tidy._locked_url(
            {"type": "path", "path": "./vendor", "dir": "sub"}
        ) == "path:./vendor?dir=sub"

    def test_node_url_surfaces_dir(self):
        assert "dir=sub" in flake_tidy.node_url(
            self._node(
                type="git", url="https://example.com/r.git", dir="sub"
            )
        )
        assert "dir=sub" in flake_tidy.node_url(
            self._node(
                type="tarball",
                url="https://example.com/t.tar.gz",
                dir="sub",
            )
        )
        assert "dir=sub" in flake_tidy.node_url(
            self._node(type="path", path="./vendor", dir="sub")
        )
        # Guard: plain shapes unchanged.
        assert flake_tidy.node_url(
            self._node(type="path", path="./vendor")
        ) == "path:./vendor"
