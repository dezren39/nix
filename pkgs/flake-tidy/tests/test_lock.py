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


class TestSourceKeyHg:
    """#539: hg-type inputs fell through to source_key's JSON-dump
    fallback like git did before #519 — the rev value was embedded,
    so two pins of one hg repo at different revs never grouped."""

    @staticmethod
    def _node(**orig):
        base = {"type": "hg", "url": "https://example.com/repo"}
        base.update(orig)
        return {"original": base}

    def test_rev_not_part_of_source_key(self):
        """Two hg pins of one URL at different revs share a key."""
        assert flake_tidy.source_key(self._node(rev="aaaa")) == (
            flake_tidy.source_key(self._node(rev="ffff"))
        )
        assert flake_tidy.source_key(self._node(rev="aaaa")) == (
            "hg/https://example.com/repo/:rev-pinned"
        )

    def test_rev_pinned_key_differs_from_unpinned(self):
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(self._node())
        )

    def test_rev_pinned_key_differs_from_ref_only(self):
        assert flake_tidy.source_key(
            self._node(rev="aaaa", ref="default")
        ) != flake_tidy.source_key(self._node(ref="default"))

    def test_url_ref_and_dir_still_distinguish(self):
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(
                self._node(rev="aaaa", url="https://example.com/other")
            )
        )
        assert flake_tidy.source_key(self._node(ref="default")) != (
            flake_tidy.source_key(self._node(ref="stable"))
        )
        assert flake_tidy.source_key(self._node(rev="aaaa")) != (
            flake_tidy.source_key(self._node(rev="aaaa", dir="sub"))
        )

    def test_node_url_hg_is_the_url_not_a_json_dump(self):
        assert (
            flake_tidy.node_url(self._node()) == "https://example.com/repo"
        )
        assert (
            flake_tidy.node_url(self._node(rev="aaaa111122223333"))
            == "https://example.com/repo/aaaa11112222"
        )
        assert (
            flake_tidy.node_url(self._node(ref="default"))
            == "https://example.com/repo/default"
        )
        assert (
            flake_tidy.node_url(self._node(ref="default", dir="sub"))
            == "https://example.com/repo/default?dir=sub"
        )


class TestSourceKeyUrlSpelling:
    """#555: the same repo written in different URL spellings never
    grouped — source_key embedded the raw URL and ref verbatim, so a
    trailing slash, a ".git" suffix, host case, or a fully-qualified
    branch ref each split the dedup group."""

    @staticmethod
    def _key(url, t="git", **orig):
        base = {"type": t, "url": url}
        base.update(orig)
        return flake_tidy.source_key({"original": base})

    def test_trailing_slash_groups(self):
        assert self._key("https://example.com/tool") == self._key(
            "https://example.com/tool/"
        )

    def test_dot_git_suffix_groups(self):
        assert self._key("https://example.com/tool") == self._key(
            "https://example.com/tool.git"
        )
        assert self._key("https://example.com/tool") == self._key(
            "https://example.com/tool.git/"
        )

    def test_ref_format_groups(self):
        """refs/heads/<name> and <name> name the same branch."""
        assert self._key(
            "https://example.com/tool", ref="main"
        ) == self._key("https://example.com/tool", ref="refs/heads/main")

    def test_forge_ref_format_groups(self):
        """The forge branch canonicalizes the ref the same way."""
        plain = {
            "original": {
                "type": "github",
                "owner": "o",
                "repo": "r",
                "ref": "main",
            }
        }
        qualified = {
            "original": {
                "type": "github",
                "owner": "o",
                "repo": "r",
                "ref": "refs/heads/main",
            }
        }
        assert flake_tidy.source_key(plain) == flake_tidy.source_key(
            qualified
        )

    def test_host_and_scheme_case_group(self):
        assert self._key("https://example.com/tool") == self._key(
            "HTTPS://EXAMPLE.COM/tool"
        )

    def test_case_insensitive_forge_path_groups(self):
        """github.com/gitlab.com route paths case-insensitively, like
        the forge branch's owner/repo lowercasing."""
        assert self._key("https://github.com/Owner/Repo") == self._key(
            "https://github.com/owner/repo"
        )

    def test_other_host_path_case_still_distinguishes(self):
        assert self._key("https://example.com/Owner/Repo") != self._key(
            "https://example.com/owner/repo"
        )

    def test_scp_like_spelling_normalizes_within_itself(self):
        assert self._key("git@github.com:o/r") == self._key(
            "git@GitHub.com:o/r.git"
        )

    def test_scp_like_path_case_groups_on_forge_hosts(self):
        """#562: the scp-like branch skipped the case-insensitive
        path rule, so git@github.com:Owner/Repo split from
        git@github.com:owner/repo while the ssh:// and https://
        spellings of the same repo grouped."""
        assert self._key("git@github.com:Owner/Repo") == self._key(
            "git@github.com:owner/repo"
        )
        assert self._key("git@gitlab.com:Owner/Repo") == self._key(
            "git@gitlab.com:owner/repo"
        )

    def test_scp_like_path_case_still_distinguishes_elsewhere(self):
        """#562 near-miss: on a case-sensitive host the scp path
        case still distinguishes, exactly as the :// branch does."""
        assert self._key("git@example.com:Owner/Repo") != self._key(
            "git@example.com:owner/repo"
        )

    def test_uppercase_dot_git_groups_on_forge_hosts(self):
        """#562: the suffix strip was case-sensitive and ran before
        path lowercasing, so repo.GIT survived (then lowercased into
        the key still carrying the suffix)."""
        assert self._key("https://github.com/owner/repo") == self._key(
            "https://github.com/owner/repo.GIT"
        )
        assert self._key("https://github.com/owner/repo") == self._key(
            "https://github.com/owner/repo.Git/"
        )
        assert self._key("git@github.com:owner/repo") == self._key(
            "git@github.com:owner/repo.GIT"
        )

    def test_stacked_dot_git_strips_at_most_one(self):
        """#575: PR #572's post-lowercase strip stacked with the
        early strip, so repo.git.git lost BOTH suffixes on forge
        hosts and grouped with the different repository repo.
        One strip leaves a repository actually named repo.git,
        whose key ends in /repo.git/ — all stacked spellings share
        it, and it differs from the plain repo key."""
        stacked = self._key("https://github.com/owner/repo.git.git")
        assert stacked.endswith("/repo.git/")
        assert self._key("https://github.com/owner/repo.GIT.git") == stacked
        assert self._key("https://github.com/owner/repo.git.GIT") == stacked
        assert self._key("https://github.com/owner/repo.GIT.GIT") == stacked
        assert stacked != self._key("https://github.com/owner/repo")
        assert stacked != self._key("https://github.com/owner/repo.git")
        # The case-sensitive-host result is the reference (#575
        # acceptance): exactly one strip there too, same shape.
        assert self._key(
            "https://example.com/owner/repo.git.git"
        ).endswith("/repo.git/")
        # hg nodes share the git/hg branch.
        assert self._key(
            "https://github.com/owner/repo.git.git", t="hg"
        ).endswith("/repo.git/")

    def test_stacked_dot_git_scp_like_strips_at_most_one(self):
        """#575: the scp-like branch had the same stacked strip."""
        stacked = self._key("git@github.com:owner/repo.git.git")
        assert stacked.endswith(":owner/repo.git/")
        assert self._key("git@github.com:owner/repo.GIT.git") == stacked
        assert self._key("git@github.com:owner/repo.git.GIT") == stacked
        assert stacked != self._key("git@github.com:owner/repo")
        assert stacked != self._key("git@github.com:owner/repo.git")

    def test_uppercase_dot_git_kept_on_case_sensitive_hosts(self):
        """#562 documented choice: on a case-sensitive host repo.GIT
        can be a different repository, so the suffix is kept there."""
        assert self._key("https://example.com/owner/repo") != self._key(
            "https://example.com/owner/repo.GIT"
        )

    def test_default_port_groups(self):
        """#579: an explicit default port names the same endpoint as
        no port, so it must not split the dedup group."""
        assert self._key("https://github.com/o/r") == self._key(
            "https://github.com:443/o/r"
        )
        assert self._key("http://example.com/r") == self._key(
            "http://example.com:80/r"
        )
        assert self._key("ssh://git@example.com/r") == self._key(
            "ssh://git@example.com:22/r"
        )
        assert self._key("git://example.com/r") == self._key(
            "git://example.com:9418/r"
        )
        # hg nodes share the git/hg branch.
        assert self._key("https://example.com/r", t="hg") == self._key(
            "https://example.com:443/r", t="hg"
        )
        # Composes with the suffix/query rules (#560/#575).
        assert self._key("https://github.com/o/r?dir=Sub") == self._key(
            "https://github.com:443/o/r.git?dir=Sub"
        )

    def test_nondefault_port_stays_separate(self):
        """#579 conservatism: a non-default port can be a different
        server, and a port that is default for one scheme is not
        default for another."""
        assert self._key("https://example.com/r") != self._key(
            "https://example.com:8443/r"
        )
        assert self._key("https://example.com/r") != self._key(
            "https://example.com:80/r"
        )
        # Unknown scheme: no default defined, port kept verbatim.
        assert self._key("foo://example.com/r") != self._key(
            "foo://example.com:443/r"
        )

    def test_compound_scheme_default_port_groups(self):
        """#595: Nix prefixes git/hg flake URLs with a compound scheme
        (git+https, git+ssh, hg+https, ...) — the #579 default-port
        lookup keyed on the raw scheme missed them all, so an explicit
        default port split the group on the most common URL family."""
        assert self._key("git+https://github.com/o/r") == self._key(
            "git+https://github.com:443/o/r"
        )
        assert self._key("git+http://example.com/r") == self._key(
            "git+http://example.com:80/r"
        )
        assert self._key("git+ssh://git@example.com/r") == self._key(
            "git+ssh://git@example.com:22/r"
        )
        # hg nodes share the git/hg branch and the hg+ prefix.
        assert self._key("hg+https://example.com/r", t="hg") == self._key(
            "hg+https://example.com:443/r", t="hg"
        )
        # A leading-zero default port still drops (#579/#583 compose).
        assert self._key("git+https://github.com/o/r") == self._key(
            "git+https://github.com:0443/o/r"
        )
        # Composes with the suffix rules: .git strip + path lowering.
        assert self._key("git+https://github.com/O/R") == self._key(
            "git+https://github.com:443/o/r.git"
        )

    def test_compound_scheme_nondefault_port_stays_separate(self):
        """#595 conservatism, same as #579: only the TRANSPORT's
        default drops — a port that is default for another transport
        (or for no transport) can be a genuinely different server."""
        assert self._key("git+https://example.com/r") != self._key(
            "git+https://example.com:8443/r"
        )
        assert self._key("git+https://example.com/r") != self._key(
            "git+https://example.com:22/r"
        )
        assert self._key("git+ssh://git@example.com/r") != self._key(
            "git+ssh://git@example.com:443/r"
        )

    def test_ipv6_host_case_groups(self):
        """#581: the IPv6-literal branch (added for #579's port
        parsing) reassembled the host verbatim, skipping the
        lowercasing the plain-host branch applies — but IPv6 hex
        digits are case-insensitive, so the same server written
        with uppercase hex split the group."""
        assert self._key("http://[2001:db8::1]/repo") == self._key(
            "http://[2001:DB8::1]/repo"
        )
        # With userinfo and an explicit default port (#579 composes).
        assert self._key("ssh://git@[2001:db8::1]/repo") == self._key(
            "ssh://git@[2001:DB8::1]:22/repo"
        )
        # A kept non-default port stays, with the host case folded.
        assert self._key("https://[2001:db8::1]:8443/repo") == self._key(
            "https://[2001:DB8::1]:8443/repo"
        )
        # hg nodes share the git/hg branch.
        assert self._key(
            "https://[2001:db8::1]/repo", t="hg"
        ) == self._key("https://[2001:DB8::1]/repo", t="hg")

    def test_ipv6_near_misses_stay_separate(self):
        """#581 conservatism: a different IPv6 address is a different
        server, and an IPv6 host is not in
        _CASE_INSENSITIVE_PATH_HOSTS, so path case still
        distinguishes exactly as for other case-sensitive hosts."""
        assert self._key("http://[2001:db8::1]/repo") != self._key(
            "http://[2001:db8::2]/repo"
        )
        assert self._key("http://[2001:db8::1]/Repo") != self._key(
            "http://[2001:DB8::1]/repo"
        )

    def test_ipv6_numeric_spellings_group(self):
        """#583: #582 folded the literal's hex case but kept the
        address verbatim, so equivalent numeric spellings of one
        IPv6 address — leading zeros, expanded vs "::"-compressed,
        IPv4-embedded vs hex — still split the group."""
        assert self._key("http://[2001:db8::1]/repo") == self._key(
            "http://[2001:0db8::1]/repo"
        )
        assert self._key("http://[2001:db8::1]/repo") == self._key(
            "http://[2001:db8:0:0:0:0:0:1]/repo"
        )
        assert self._key("http://[::1]/repo") == self._key(
            "http://[0:0:0:0:0:0:0:1]/repo"
        )
        assert self._key("http://[::ffff:192.0.2.1]/repo") == self._key(
            "http://[::ffff:c000:201]/repo"
        )
        # Composes with case folding, userinfo, and the port rules.
        assert self._key("ssh://git@[2001:DB8::1]/repo") == self._key(
            "ssh://git@[2001:0db8::1]:022/repo"
        )
        # hg nodes share the git/hg branch.
        assert self._key(
            "https://[2001:db8::1]/repo", t="hg"
        ) == self._key("https://[2001:0DB8::1]/repo", t="hg")

    def test_ipv6_numeric_near_misses_stay_separate(self):
        """#583 conservatism: a genuinely different address is a
        different server, and path case on an IPv6 host still
        distinguishes (the host is not in
        _CASE_INSENSITIVE_PATH_HOSTS)."""
        assert self._key("http://[2001:db8::1]/repo") != self._key(
            "http://[2001:db8::2]/repo"
        )
        assert self._key("http://[2001:0db8::1]/Repo") != self._key(
            "http://[2001:db8::1]/repo"
        )

    def test_ipv6_malformed_literal_falls_back(self):
        """#583: bracketed contents that do not parse as an IPv6
        address keep the #581 lowercased-verbatim key and never
        raise out of source_key."""
        assert self._key("http://[notanip]/repo") == self._key(
            "http://[NOTANIP]/repo"
        )
        assert self._key("http://[2001:db8::1%25eth0]/repo") == self._key(
            "http://[2001:0DB8::1%25eth0]/repo"
        )
        # A different zone is a different interface — stays separate.
        assert self._key("http://[fe80::1%eth0]/repo") != self._key(
            "http://[fe80::1%eth1]/repo"
        )

    def test_port_numeric_spellings_group(self):
        """#583: #580 compared the port numerically for the
        default-port drop but kept the original string for
        non-default ports, so ":08443" split from ":8443"; an empty
        port ("host:") split from no port at all."""
        assert self._key("https://example.com:8443/r") == self._key(
            "https://example.com:08443/r"
        )
        assert self._key("https://example.com/r") == self._key(
            "https://example.com:/r"
        )
        # A leading-zero default port still drops (#579 composes).
        assert self._key("https://github.com/o/r") == self._key(
            "https://github.com:0443/o/r"
        )

    def test_port_numeric_near_misses_stay_separate(self):
        """#583 conservatism: a genuinely different port is a
        different server, and non-numeric ports stay verbatim."""
        assert self._key("https://example.com:8443/r") != self._key(
            "https://example.com:8444/r"
        )
        assert self._key("https://example.com:8443/r") != self._key(
            "https://example.com:08444/r"
        )
        assert self._key("https://example.com:abc/r") != self._key(
            "https://example.com:abd/r"
        )

    def test_port_unconvertible_digit_spellings_key_verbatim(self):
        """#589: isdigit() is True for Unicode digits int() rejects
        (superscripts, circled digits) and int() refuses digit
        strings over the conversion limit — both raised ValueError
        out of source_key. They are effectively non-numeric: key
        verbatim, never raise, and stay distinct from each other
        and from numeric ports."""
        assert self._key("https://example.com:²/r") == (
            "git/https://example.com:²/r/"
        )
        assert self._key("https://example.com:①/r") != self._key(
            "https://example.com:²/r"
        )
        long_port = "9" * 5000
        assert self._key(f"https://example.com:{long_port}/r") == (
            f"git/https://example.com:{long_port}/r/"
        )
        assert self._key(f"https://example.com:{long_port}/r") != self._key(
            "https://example.com:8443/r"
        )
        # Existing behaviour alongside: default still drops.
        assert self._key("https://example.com:443/r") == self._key(
            "https://example.com/r"
        )

    def test_ssh_and_https_stay_separate(self):
        """#555 decision, documented in _normalize_vcs_url: ssh and
        https are different transports and never merge."""
        assert self._key("https://github.com/o/r") != self._key(
            "git@github.com:o/r.git"
        )

    def test_near_misses_stay_separate(self):
        base = self._key("https://example.com/tool")
        assert base != self._key("https://other.example.com/tool")
        assert base != self._key("https://example.com/tool2")
        assert base != self._key("https://example.com/tool", dir="sub")
        assert base != self._key(
            "https://example.com/tool", submodules=True
        )
        assert base != self._key("https://example.com/tool", t="hg")

    def test_tag_refs_are_not_unqualified(self):
        """refs/tags/<name> is left alone: a bare <name> could be a
        branch, so unqualifying a tag ref could merge two sources."""
        assert self._key(
            "https://example.com/tool", ref="v1"
        ) != self._key("https://example.com/tool", ref="refs/tags/v1")

    def test_rev_pinned_still_separate_after_normalization(self):
        """#512's pin-ness marker survives spelling normalization, and
        two pins in different spellings still group with each other."""
        assert self._key(
            "https://example.com/tool.git", rev="aaaa"
        ) != self._key("https://example.com/tool")
        assert self._key(
            "https://example.com/tool.git", rev="aaaa"
        ) == self._key("https://example.com/tool/", rev="bbbb")

    def test_hg_spellings_group(self):
        assert self._key(
            "https://example.com/repo", t="hg"
        ) == self._key("https://example.com/repo/", t="hg")

    def test_plain_key_shape_unchanged(self):
        """A URL with nothing to normalize keys exactly as before."""
        assert (
            self._key("https://example.com/tool", ref="main")
            == "git/https://example.com/tool/main"
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

    def test_hg_plain(self):
        """#539: hg original URL is hg+<url>, not the JSON blob."""
        assert flake_tidy.node_original_url(
            self._node(type="hg", url="https://example.com/repo")
        ) == "hg+https://example.com/repo"

    def test_hg_ref_and_rev(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="hg",
                url="https://example.com/repo",
                ref="default",
                rev="aaaa",
            )
        ) == "hg+https://example.com/repo?ref=default&rev=aaaa"

    def test_hg_dir_kept_but_git_booleans_not_invented(self):
        assert flake_tidy.node_original_url(
            self._node(type="hg", url="https://example.com/repo", dir="sub")
        ) == "hg+https://example.com/repo?dir=sub"

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


class TestNodeOriginalUrlEncoding:
    """#601: the lock records flake-ref values decoded, but the
    Nix-facing builders interpolated them raw — a value containing
    &, #, % or a space did not parse back to itself (a dir named
    "x&rev=…" forged a rev pin). Values are now percent-encoded the
    way Nix's own serializer encodes them; node_url's readable form
    deliberately stays raw."""

    @staticmethod
    def _node(**orig):
        return {"original": orig}

    def test_git_dir_cannot_inject_rev(self):
        url = flake_tidy.node_original_url(
            self._node(
                type="git",
                url="https://example.com/tool.git",
                ref="main",
                dir="x&rev=deadbeef",
            )
        )
        assert url == (
            "git+https://example.com/tool.git"
            "?ref=main&dir=x%26rev%3Ddeadbeef"
        )
        # Parsing the query back yields exactly the recorded fields.
        from urllib.parse import parse_qs, urlsplit

        assert parse_qs(urlsplit(url).query) == {
            "ref": ["main"],
            "dir": ["x&rev=deadbeef"],
        }

    def test_git_ref_with_ampersand(self):
        assert flake_tidy.node_original_url(
            self._node(
                type="git", url="https://example.com/tool.git", ref="feat&x"
            )
        ) == "git+https://example.com/tool.git?ref=feat%26x"

    def test_hg_dir_with_space(self):
        assert flake_tidy.node_original_url(
            self._node(type="hg", url="https://example.com/repo", dir="my dir")
        ) == "hg+https://example.com/repo?dir=my%20dir"

    def test_forge_ref_with_hash(self):
        """A raw # makes Nix reject the ref ("unexpected fragment")."""
        assert flake_tidy.node_original_url(
            self._node(type="github", owner="o", repo="r", ref="a#b")
        ) == "github:o/r/a%23b"

    def test_forge_ref_with_slash_unchanged(self):
        """Multi-segment refs keep their raw slashes (guard)."""
        assert flake_tidy.node_original_url(
            self._node(type="github", owner="o", repo="r", ref="feature/foo")
        ) == "github:o/r/feature/foo"

    def test_forge_dir_with_ampersand(self):
        assert flake_tidy.node_original_url(
            self._node(type="github", owner="o", repo="r", dir="a&b")
        ) == "github:o/r?dir=a%26b"

    def test_path_with_space(self):
        assert flake_tidy.node_original_url(
            self._node(type="path", path="./my dir/foo")
        ) == "path:./my%20dir/foo"

    def test_tarball_dir_with_literal_percent(self):
        """A literal % in the value is itself encoded (%25), so the
        recorded spelling — not its mis-decoding — survives."""
        assert flake_tidy.node_original_url(
            self._node(
                type="tarball",
                url="https://example.com/t.tar.gz",
                dir="a%26b",
            )
        ) == "https://example.com/t.tar.gz?dir=a%2526b"

    def test_locked_git_dir_cannot_inject_rev(self):
        """_locked_url (the indirect-override fallback) encodes too."""
        lock = {
            "nodes": {
                "root": {"inputs": {"tool": "tool"}},
                "tool": {
                    "original": {"type": "indirect", "id": "mytool"},
                    "locked": {
                        "type": "git",
                        "url": "https://example.com/tool.git",
                        "rev": "aaaa1111",
                        "dir": "x&rev=deadbeef",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": "git+https://example.com/tool.git"
            "?rev=aaaa1111&dir=x%26rev%3Ddeadbeef"
        }

    def test_node_url_stays_readable(self):
        """node_url is the exclusion-pattern form: values stay raw."""
        assert "dir=a&b" in flake_tidy.node_url(
            self._node(
                type="git", url="https://example.com/tool.git", dir="a&b"
            )
        )


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

    _HG_LOCKED = {
        "type": "hg",
        "url": "https://example.com/repo",
        "rev": "aaaa1111",
    }
    _HG_URL = "hg+https://example.com/repo?rev=aaaa1111"

    def test_locked_url_hg(self):
        """#539: an hg locked node yields hg+<url>?rev=, not None."""
        assert flake_tidy._locked_url(self._HG_LOCKED) == self._HG_URL
        assert flake_tidy._locked_url({"type": "hg"}) is None

    def test_build_override_from_flake_name_hg_locked(self):
        lock = self._indirect_lock(self._HG_LOCKED)
        assert flake_tidy.build_override_from_flake_name(
            lock, "tool", "mytool"
        ) == {"tool": self._HG_URL}

    def test_compute_indirect_overrides_hg_locked(self):
        lock = self._indirect_lock(self._HG_LOCKED)
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": self._HG_URL
        }

    def test_compute_indirect_overrides_sourcehut_locked(self):
        """The compute_indirect_overrides fallback only knew
        github/gitlab even after #522 — sourcehut was missing there."""
        lock = self._indirect_lock(
            {"type": "sourcehut", "owner": "~user", "repo": "proj", "rev": "bbbb"}
        )
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "tool": "sourcehut:~user/proj/bbbb"
        }

    def test_compute_indirect_overrides_cycle(self):
        """#563: a cyclic lock graph (A inputs B, B inputs A) must not
        recurse to RecursionError — the walk skips refs to nodes
        already on the current path and returns normally."""
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a"}},
                "a": {
                    "original": {"type": "github", "owner": "o", "repo": "r"},
                    "inputs": {"b": "b"},
                },
                "b": {
                    "original": {"type": "github", "owner": "o", "repo": "r2"},
                    "inputs": {"a": "a"},
                },
            },
            "root": "root",
            "version": 7,
        }
        assert flake_tidy.compute_indirect_overrides(lock) == {}

    def test_compute_indirect_overrides_self_loop_and_diamond(self):
        """#563: a self-loop terminates, and a diamond still walks the
        shared node once per path (guard is per-path, not global) —
        the indirect child under both parents gets both overrides."""
        lock = {
            "nodes": {
                "root": {"inputs": {"x": "x", "y": "y"}},
                "x": {"inputs": {"shared": "shared", "me": "x"}},
                "y": {"inputs": {"shared": "shared"}},
                "shared": {
                    "original": {"type": "indirect", "id": "mytool"},
                    "locked": {
                        "type": "github",
                        "owner": "nixos",
                        "repo": "nixpkgs",
                        "rev": "dddd",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        assert flake_tidy.compute_indirect_overrides(lock) == {
            "x/shared": "github:nixos/nixpkgs/dddd",
            "y/shared": "github:nixos/nixpkgs/dddd",
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
        """Guard: a forge locked node with NO rev/ref keeps the plain
        type:owner/repo shape — the rev segment appears only when the
        lock actually records one (#609)."""
        lock = self._indirect_lock(
            {"type": "github", "owner": "nixos", "repo": "nixpkgs"}
        )
        assert flake_tidy.build_override_from_flake_name(
            lock, "tool", "mytool"
        ) == {"tool": "github:nixos/nixpkgs"}

    def test_locked_url_forge_keeps_rev(self):
        """#609: the forge locked rev is the pin — dropping it rebuilt
        the override at the default branch while git/hg kept theirs."""
        for t in ("github", "gitlab", "sourcehut"):
            assert flake_tidy._locked_url(
                {"type": t, "owner": "o", "repo": "r", "rev": "aaaa1111"}
            ) == f"{t}:o/r/aaaa1111"

    def test_locked_url_forge_ref_beats_rev(self):
        """Mirror node_original_url: a recorded ref wins over the rev."""
        assert flake_tidy._locked_url(
            {
                "type": "github",
                "owner": "o",
                "repo": "r",
                "ref": "release-1",
                "rev": "aaaa1111",
            }
        ) == "github:o/r/release-1"

    def test_resolve_transitive_url_forge_locked(self):
        """#609 end-to-end: the transitive fallback returns the pinned
        forge URL, like the git case above."""
        lock = {
            "nodes": {
                "root": {"inputs": {"parent": "parent"}},
                "parent": {"inputs": {"child": "child"}},
                "child": {
                    "original": {"type": "indirect", "id": "child"},
                    "locked": {
                        "type": "github",
                        "owner": "o",
                        "repo": "r",
                        "rev": "cccc3333",
                    },
                },
            },
            "root": "root",
            "version": 7,
        }
        assert (
            flake_tidy._resolve_transitive_url(lock, "parent", "child")
            == "github:o/r/cccc3333"
        )


class TestBuildOverrideInputsIndirect:
    """#568: build_override_inputs returned the unresolvable
    indirect: ref itself as the --override-input, shadowing the
    locked-URL fallback in run_nix_flake_lock_robust."""

    _LOCK = {
        "nodes": {
            "root": {"inputs": {"a": "a"}},
            "a": {"inputs": {"b": "b"}},
            "b": {
                "original": {"type": "indirect", "id": "y"},
                "locked": {
                    "type": "github",
                    "owner": "o",
                    "repo": "r",
                    "rev": "abc123",
                },
            },
        },
        "root": "root",
        "version": 7,
    }

    def test_indirect_node_yields_no_override(self):
        assert flake_tidy.build_override_inputs(self._LOCK, "a/b") == {}

    def test_fallback_reaches_locked_url(self):
        """The chain run_nix_flake_lock_robust follows: an empty
        path-based result lets the flake-name fallback produce the
        concrete locked URL."""
        assert flake_tidy.build_override_inputs(self._LOCK, "a/b") == {}
        assert flake_tidy.build_override_from_flake_name(
            self._LOCK, "a/b", "y"
        ) == {"a/b": "github:o/r/abc123"}

    def test_direct_node_still_overrides(self):
        """Guard: a non-indirect node at the path is unchanged."""
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "a"}},
                "a": {
                    "original": {
                        "type": "git",
                        "url": "https://example.com/tool.git",
                        "rev": "aaaa1111",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        assert flake_tidy.build_override_inputs(lock, "a") == {
            "a": "git+https://example.com/tool.git?rev=aaaa1111"
        }


class TestExtractFailedInputPrimed:
    """#691: extract_failed_input truncated primed names at the
    apostrophe — Nix wraps the name in single quotes verbatim, so
    nixpkgs' appears as 'nixpkgs'' in stderr. The truncated path
    missed in build_override_inputs and in _match_failure_to_proposal,
    defeating the robust lock retry."""

    def test_primed_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs''"
        ) == ("nixpkgs'", None)

    def test_primed_path_segments(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'parent/child''"
        ) == ("parent/child'", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'parent'/child'"
        ) == ("parent'/child", None)

    def test_unprimed_input_path_unchanged(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs-unstable'"
        ) == ("nixpkgs-unstable", None)

    def test_following_text_not_swallowed(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs'' and then 'other'"
        ) == ("nixpkgs'", None)

    def test_primed_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:cl-nix-lite'' in the registry"
        ) == (None, "cl-nix-lite'")

    def test_unprimed_flake_name_unchanged(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs'"
        ) == (None, "nixpkgs")

    def test_primed_failure_produces_override_and_retry(self, monkeypatch):
        """End-to-end run_nix_flake_lock_robust shape: a primed failed
        input resolves through the lock to an override and the retry
        is attempted with it."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs'": "np"}},
                "np": {
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        seen_overrides = []
        calls = {"n": 0}

        def fake_lock(_flake_dir, overrides=None):
            calls["n"] += 1
            seen_overrides.append(dict(overrides) if overrides else {})
            if calls["n"] == 1:
                return False, "… while updating the flake input 'nixpkgs''"
            return True, ""

        monkeypatch.setattr(flake_tidy, "run_nix_flake_lock", fake_lock)
        ok, err = flake_tidy.run_nix_flake_lock_robust(
            "/nonexistent", lock_data=lock
        )
        assert (ok, err) == (True, "")
        assert seen_overrides[-1] == {"nixpkgs'": "github:example/nixpkgs"}


class TestExtractFailedInputDotted:
    """#710: extract_failed_input returned (None, None) for dotted
    names — a dotted name is a single input (spelled quoted in
    flake.nix, #705), but the parser's segment set had no dot, a
    regression from the pre-#692 [^']+ capture. On a miss the robust
    lock retry gives up and flatten elimination fails every proposal.
    """

    def test_dotted_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my.input'"
        ) == ("my.input", None)

    def test_dotted_nested_segment(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'parent/my.input'"
        ) == ("parent/my.input", None)

    def test_dotted_and_primed_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my.input''"
        ) == ("my.input'", None)

    def test_dotted_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:my.input' in the registry"
        ) == (None, "my.input")

    def test_dotted_following_text_not_swallowed(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my.input' and then 'other'"
        ) == ("my.input", None)


class TestExtractFailedInputGeneralNames:
    """#712: extract_failed_input's character set still rejected every
    other quote-requiring name — my+input, my input, my@input all
    returned (None, None), so the robust lock retry silently gave up
    for exactly the names the content scanners (#705) accept. The
    parser now takes a name structurally (any character except the
    wrapping quote, "/" and newline, with the #691 apostrophe rules),
    not one character at a time."""

    def test_plus_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my+input'"
        ) == ("my+input", None)

    def test_space_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my input'"
        ) == ("my input", None)

    def test_at_input_path(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my@input'"
        ) == ("my@input", None)

    def test_general_nested_segment(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'parent/my+input'"
        ) == ("parent/my+input", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my input/child@x'"
        ) == ("my input/child@x", None)

    def test_general_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:my+input' in the registry"
        ) == (None, "my+input")
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:my@input' in the registry"
        ) == (None, "my@input")

    def test_general_primed_and_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my+input''"
        ) == ("my+input'", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my+input' and then 'other'"
        ) == ("my+input", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'my input' and then 'other'"
        ) == ("my input", None)


class TestExtractFailedInputApostropheContinuation:
    """#714: PR #713's structural parse still truncated a name at an
    apostrophe followed by a character outside its lookahead set
    [A-Za-z0-9_+@./-] (a'!b -> a) and admitted only one trailing
    prime (nixpkgs'' -> nixpkgs'). A truncated prefix is worse than
    the pre-#713 miss: build_override_inputs and
    _match_failure_to_proposal trust it and act on a different input.
    The closing quote is now the first quote that is not part of a
    doubled quote and leaves the rest of the line's quotes paired."""

    def test_apostrophe_before_punctuation(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'!b'"
        ) == ("a'!b", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'%b'"
        ) == ("a'%b", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'~b'"
        ) == ("a'~b", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a':b'"
        ) == ("a':b", None)

    def test_apostrophe_before_space_and_non_ascii(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'foo' bar'"
        ) == ("foo' bar", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'éb'"
        ) == ("a'éb", None)

    def test_apostrophe_continuation_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:a'!b' in the registry"
        ) == (None, "a'!b")
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:a':b' in the registry"
        ) == (None, "a':b")

    def test_double_trailing_prime(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs'''"
        ) == ("nixpkgs''", None)

    def test_doubled_prime_inside_name(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a''b'"
        ) == ("a''b", None)

    def test_continuation_name_with_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'!b' and then 'other'"
        ) == ("a'!b", None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #714 harm, end to end: the extracted path must resolve
        through the lock to the failing input's override, not to the
        unrelated input named by the truncated prefix."""
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "na", "a'!b": "nb"}},
                "na": {
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "a-tool",
                    }
                },
                "nb": {
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "ab-tool",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'!b'"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "a'!b": "github:example/ab-tool"
        }


class TestExtractFailedInputMultiPrimedPaths:
    """#722: PR #720's rest-parity rule still truncated a path primed
    in more than one segment ('a'/b'' -> a): the first segment's
    prime leaves an even number of quotes after it, so it was taken
    as the close. The same parity assumption swallowed prose when
    following text held a possessive (nixpkgs' in the user's flake
    -> nixpkgs' in the user). Quotes that can only continue a name
    or a word are now never the close: a quote before "/" ends a
    primed segment, a quote with a word character on both sides is
    an apostrophe inside a word, and the rest parity counts only
    delimiter quotes; a candidate name must also be a plausible
    path, so an ambiguous line yields no match."""

    def test_primed_nonfinal_and_final_segments(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'/b''"
        ) == ("a'/b'", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'parent'/child''"
        ) == ("parent'/child'", None)

    def test_multi_primed_with_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'/b'' and then 'other'"
        ) == ("a'/b'", None)

    def test_multi_primed_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:a'b'' in the registry"
        ) == (None, "a'b'")

    def test_possessive_in_following_text_not_swallowed(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the user's flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs'' in the user's flake"
        ) == ("nixpkgs'", None)

    def test_unbalanced_following_text_yields_no_match(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'other"
        ) == (None, None)

    def test_extracted_path_overrides_the_right_input(self):
        """The #722 harm, end to end: with both the unrelated input
        a and the real path a'/b' in the lock, the override must
        target the failing path, not its first segment."""
        lock = {
            "nodes": {
                "root": {"inputs": {"a": "na", "a'": "np"}},
                "na": {
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "a-tool",
                    }
                },
                "np": {
                    "inputs": {"b'": "nb"},
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "a-tool",
                    }
                },
                "nb": {
                    "original": {
                        "type": "github",
                        "owner": "example",
                        "repo": "ab-tool",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'/b''"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "a'/b'": "github:example/ab-tool"
        }


class TestExtractFailedInputFinalPossessive:
    """#727: PR #726's possessive rule covered only the medial
    shape (user's): _rest_delimiter_quotes counted a word-final
    (plural) possessive — users', James' — as a delimiter, so the
    genuine closing quote left an odd rest count and was rejected,
    and the possessive itself was accepted as the close, swallowing
    prose into the name (nixpkgs' in the users). A word-final
    possessive in the rest is now discounted when its word is a
    standalone prose word preceded by further prose; a delimiter
    closing a quoted span ('other') or the wrapper itself (the bar
    of 'foo' bar', the b of a'!b) still counts."""

    def test_final_possessive_in_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the users' flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in James' flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the developers' flake"
        ) == ("nixpkgs", None)

    def test_final_possessive_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the users' registry"
        ) == (None, "nixpkgs")
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in James' registry"
        ) == (None, "nixpkgs")

    def test_primed_name_with_final_possessive(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs'' in the users' flake"
        ) == ("nixpkgs'", None)

    def test_final_possessive_with_quoted_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the users' flake"
            " and then 'other'"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'other'"
            " in the users' flake"
        ) == ("nixpkgs", None)

    def test_name_final_word_after_apostrophe_still_counts(self):
        """Guard (#714 shape): the closing quote of 'foo' bar' ends
        the first word after the in-name apostrophe, so it is a
        delimiter, not a possessive — the name is not truncated."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'foo' bar'"
        ) == ("foo' bar", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'a'!b' and then 'other'"
        ) == ("a'!b", None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #727 harm, end to end: the extracted path must
        resolve through the lock to the failing input, not to
        nothing (the swallowed name matches no input)."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "n1"}},
                "n1": {
                    "original": {
                        "type": "github",
                        "owner": "NixOS",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the users' flake"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "nixpkgs": "github:NixOS/nixpkgs"
        }


class TestExtractFailedInputMultiWordSpan:
    """#731: PR #728's _is_final_possessive judged every rest quote
    on its own, so the closing quote of a multi-word quoted span
    ('power users') met its standalone-word test — the span's last
    word is preceded by the space inside the span — was discounted
    as a possessive, and the genuine close was rejected for an odd
    rest count, swallowing the whole span into the name. The same
    helper's word walk stopped at a hyphen, so a hyphenated
    compound's possessive (end-users') was never discounted and
    the name was truncated at it. The rest parity now tracks
    quoted spans (a span's closing quote always counts, whatever
    word it ends) and the possessive walk crosses a hyphen flanked
    by word characters, so the compound's first segment is what
    must be a standalone prose word."""

    def test_multi_word_span_in_following_text(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users'"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'my flake'"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'the users'"
        ) == ("nixpkgs", None)

    def test_multi_word_span_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the 'the registry' thing"
        ) == (None, "nixpkgs")

    def test_final_possessive_with_multi_word_span(self):
        """Both orders: the possessive discount and the span
        closing must each be judged correctly in one line."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the users' flake"
            " and then 'power users'"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users'"
            " in the users' flake"
        ) == ("nixpkgs", None)

    def test_hyphenated_compound_possessive(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the end-users' flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the power-users' flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the end-users' registry"
        ) == (None, "nixpkgs")

    def test_unclosed_multi_word_span_yields_no_match(self):
        """Guard: an opening quote that is never closed still
        strands the parity — no swallowed name."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users"
        ) == (None, None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #731 harm, end to end: the swallowed name matches
        no input, so the override comes back empty and the robust
        lock retry silently targets nothing."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "n1"}},
                "n1": {
                    "original": {
                        "type": "github",
                        "owner": "NixOS",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users'"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "nixpkgs": "github:NixOS/nixpkgs"
        }


class TestExtractFailedInputPrimedSpanEnd:
    """#733: PR #732's span tracker in _rest_delimiter_quotes
    counted a closing run per quote, so a following span quoting
    a prime-ending name ('power users'') closed at its first
    prime and the second prime — preceded by a quote, not a word
    character — opened a phantom span that was never closed.
    The genuine close was left with an odd delimiter count and
    rejected, and a later candidate returned a swallowed name:
    a regression of the multi-word cases #731/#732 fixed. A
    span's closing run now counts as the single delimiter it
    is — the primes before its last quote are name text, as
    _wrapped_name already treats a doubled quote in the name
    itself."""

    def test_multi_word_span_quoting_primed_name(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users''"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'my flake''"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the 'power users'' flake"
        ) == ("nixpkgs", None)

    def test_primed_span_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the 'the registry'' thing"
        ) == (None, "nixpkgs")

    def test_single_word_span_quoting_primed_name(self):
        """The single-word shape also failed (pre-existing, same
        root area): the span's close at the first prime stranded
        the second prime as a phantom opening."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'other''"
        ) == ("nixpkgs", None)

    def test_span_quoting_double_primed_name(self):
        """A run of three: two primes of name text plus the
        span's close is still the one closing delimiter."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'nixpkgs'''"
        ) == ("nixpkgs", None)

    def test_primed_wrapper_name_then_primed_span(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs'' and then 'other''"
        ) == ("nixpkgs'", None)

    def test_primed_span_with_final_possessive(self):
        """Both orders: the possessive discount and the primed
        span's closing run must each be judged correctly in one
        line."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the users' flake"
            " and then 'power users''"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users''"
            " in the users' flake"
        ) == ("nixpkgs", None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #733 harm, end to end: the swallowed name matches
        no input, so the override comes back empty and the robust
        lock retry silently targets nothing."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "n1"}},
                "n1": {
                    "original": {
                        "type": "github",
                        "owner": "NixOS",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users''"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "nixpkgs": "github:NixOS/nixpkgs"
        }


class TestExtractFailedInputAdjacentSpans:
    """#743: PR #740's closing-run collapse is unconditional, so
    it also swallows a run that is two delimiters — the close of
    one span and the opening of the next, written adjacently
    ('a''b').  The second span's opening was eaten as name text,
    its close was judged outside any span, the parity flipped,
    and a later candidate returned a swallowed name.  A run is
    only the #733 single close when what follows it ends the
    quoted phrase; a word character after the run makes its
    last quote the next span's opening."""

    def test_adjacent_spans_after_the_name(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''b'"
        ) == ("nixpkgs", None)

    def test_adjacent_spans_then_a_third_span(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''b' and 'c'"
        ) == ("nixpkgs", None)

    def test_adjacent_spans_before_more_prose(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the 'a''b' flake"
        ) == ("nixpkgs", None)

    def test_adjacent_spans_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the 'a''b' thing"
        ) == (None, "nixpkgs")

    def test_primed_span_adjacent_to_next_span(self):
        """A run of three with a word after it: the primed span's
        prime is name text, then close plus the next opening."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'users'''b'"
        ) == ("nixpkgs", None)

    def test_primed_span_end_still_a_single_close(self):
        """Guard (#733 unchanged): a run followed by a space or
        end-of-line is still the span's one closing delimiter."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users''"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' in the 'power users'' flake"
        ) == ("nixpkgs", None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #743 harm, end to end: the swallowed name matches
        no input, so the override comes back empty and the robust
        lock retry silently targets nothing."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "n1"}},
                "n1": {
                    "original": {
                        "type": "github",
                        "owner": "NixOS",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''b'"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "nixpkgs": "github:NixOS/nixpkgs"
        }


class TestExtractFailedInputClosingRunLookahead:
    """#745: PR #744 judged a closing run by the single character
    after it — a word character meant close plus the next span's
    opening, anything else a single close — and misjudged in
    both directions.  A prose suffix after a primed span
    ('users''s) was taken for a span that never closes, flipping
    the parity and swallowing prose into the name (a regression
    of the #733 reading), while an adjacent span starting with
    punctuation ('a''-b') was collapsed to a single close, its
    own close judged outside any span (the #743 bug for
    punctuation-start names).  A run now counts as close plus
    opening only when a span opened at its last quote actually
    closes later in the line."""

    def test_prose_suffix_after_primed_span(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'users''s flake"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'power users''s flake"
        ) == ("nixpkgs", None)

    def test_prose_suffix_after_primed_span_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the 'users''s thing"
        ) == (None, "nixpkgs")

    def test_prose_suffix_then_an_independent_span(self):
        """The next quote after the run opens a later span; it is
        preceded by whitespace, so it cannot be the close of a
        span the run supposedly opened."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'users''s"
            " flake and then 'other'"
        ) == ("nixpkgs", None)

    def test_punctuation_start_adjacent_spans(self):
        for second in ("-b", "/nix/store/abc", "$HOME/x", ".foo", "@foo", "--rebuild"):
            assert flake_tidy.extract_failed_input(
                f"… while updating the flake input 'nixpkgs' and then 'a''{second}'"
            ) == ("nixpkgs", None), second

    def test_punctuation_start_adjacent_span_flake_name(self):
        assert flake_tidy.extract_failed_input(
            "cannot find flake 'flake:nixpkgs' in the 'a''--rebuild' thing"
        ) == (None, "nixpkgs")

    def test_punctuation_start_adjacent_span_then_a_third(self):
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''-b' and 'c'"
        ) == ("nixpkgs", None)

    def test_ambiguous_guards_still_hold(self):
        """'users''-owned is a primed span plus a hyphen suffix
        (single close, #733) even though '-owned' starts with
        the same character as the '-b' span above: no span
        opened at the run closes later, because none opens."""
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'users''-owned"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''b'"
        ) == ("nixpkgs", None)
        assert flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'users'''b'"
        ) == ("nixpkgs", None)

    def test_extracted_name_overrides_the_right_input(self):
        """The #745 harm, end to end: the swallowed name matches
        no input, so the override comes back empty and the robust
        lock retry silently targets nothing."""
        lock = {
            "nodes": {
                "root": {"inputs": {"nixpkgs": "n1"}},
                "n1": {
                    "original": {
                        "type": "github",
                        "owner": "NixOS",
                        "repo": "nixpkgs",
                    }
                },
            },
            "root": "root",
            "version": 7,
        }
        input_path, _ = flake_tidy.extract_failed_input(
            "… while updating the flake input 'nixpkgs' and then 'a''-b'"
        )
        assert flake_tidy.build_override_inputs(lock, input_path) == {
            "nixpkgs": "github:NixOS/nixpkgs"
        }


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
        ) == "gitlab:openldap/openldap/aaaa1111?host=git.openldap.org"

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


class TestEmbeddedQueryUrls:
    """#544: a raw URL may already embed a query (#528). node_url's
    git/hg branches joined params with a hard-coded "?" — a second,
    malformed "?" — and _append_query appended field params the URL
    already carried, duplicating keys (or conflicting on dir)."""

    @staticmethod
    def _node(**orig):
        return {"original": orig}

    def test_node_url_git_embedded_query_gets_one_question_mark(self):
        s = flake_tidy.node_url(
            self._node(
                type="git",
                url="https://example.com/repo.git?shallow=1",
                dir="sub",
                shallow=True,
            )
        )
        assert s.count("?") == 1
        assert s == "https://example.com/repo.git?shallow=1&dir=sub"

    def test_node_url_hg_embedded_query_gets_one_question_mark(self):
        s = flake_tidy.node_url(
            self._node(
                type="hg",
                url="https://example.com/repo?dir=old",
                dir="new",
            )
        )
        assert s.count("?") == 1
        assert s.count("dir=") == 1

    def test_node_url_git_ref_goes_before_embedded_query(self):
        # #546: the /ref segment is path, not query — appending it
        # after the raw URL corrupted shallow's value to "1/main".
        s = flake_tidy.node_url(
            self._node(
                type="git",
                url="https://example.com/repo.git?shallow=1",
                ref="main",
                dir="sub",
            )
        )
        assert s == "https://example.com/repo.git/main?shallow=1&dir=sub"

    def test_node_url_git_rev_goes_before_embedded_query(self):
        s = flake_tidy.node_url(
            self._node(
                type="git",
                url="https://example.com/repo.git?shallow=1",
                rev="abc1234567890def",
            )
        )
        assert s == "https://example.com/repo.git/abc123456789?shallow=1"

    def test_node_url_hg_ref_goes_before_embedded_query(self):
        s = flake_tidy.node_url(
            self._node(
                type="hg",
                url="https://example.com/repo?foo=1",
                ref="default",
                dir="sub",
            )
        )
        assert s == "https://example.com/repo/default?foo=1&dir=sub"

    def test_node_url_hg_rev_goes_before_embedded_query(self):
        s = flake_tidy.node_url(
            self._node(
                type="hg",
                url="https://example.com/repo?foo=1",
                rev="abc1234567890def",
            )
        )
        assert s == "https://example.com/repo/abc123456789?foo=1"

    def test_node_original_url_dedupes_embedded_params(self):
        s = flake_tidy.node_original_url(
            self._node(
                type="git",
                url="https://example.com/repo.git?shallow=1",
                shallow=True,
                dir="sub",
            )
        )
        assert s == "git+https://example.com/repo.git?shallow=1&dir=sub"

    def test_node_original_url_conflicting_dir_keeps_embedded(self):
        s = flake_tidy.node_original_url(
            self._node(
                type="tarball",
                url="https://example.com/a.tgz?dir=old",
                dir="new",
            )
        )
        assert s == "https://example.com/a.tgz?dir=old"

    def test_locked_url_dedupes_embedded_params(self):
        s = flake_tidy._locked_url(
            {
                "type": "git",
                "url": "https://example.com/repo.git?dir=old",
                "dir": "new",
                "rev": "abc123",
            }
        )
        assert s.count("dir=") == 1
        assert s == "git+https://example.com/repo.git?dir=old&rev=abc123"

    def test_urls_without_embedded_query_unchanged(self):
        assert flake_tidy.node_url(
            self._node(
                type="git", url="https://example.com/r.git", dir="sub"
            )
        ) == "https://example.com/r.git?dir=sub"
        assert flake_tidy.node_original_url(
            self._node(
                type="git",
                url="https://example.com/r.git",
                shallow=True,
            )
        ) == "git+https://example.com/r.git?shallow=1"
