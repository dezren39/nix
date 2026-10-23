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

    def test_get_input_file_order_nested_dotted_override_not_root(self):
        """A nested `inputs.bar.follows` line inside an input's block is an
        override on that input, not a root declaration of `bar` (#652)."""
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    foo = {
      url = "github:example/foo";
      inputs.bar.follows = "nixpkgs";
    };
    baz.url = "github:example/baz";
  };
}
"""
        order = flake_tidy.get_input_file_order(content)
        assert order == ["nixpkgs", "foo", "baz"]
        assert not flake_tidy.root_input_exists_in_content(content, "bar")

    def test_get_input_file_order_nested_dotted_block_keeps_braces(self):
        """A nested one-line `inputs.bar = { ... };` block inside a parent
        block must not be recorded as a root input, and its braces must
        still be counted so later root inputs appear in order (#652)."""
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    foo = {
      url = "github:example/foo";
      inputs.bar = { url = "github:example/bar"; };
    };
    baz.url = "github:example/baz";
  };
  outputs = { self }: { };
}
"""
        order = flake_tidy.get_input_file_order(content)
        assert order == ["nixpkgs", "foo", "baz"]


class TestParseResultIsolation:
    """Callers must not be able to poison later parse results by mutating
    a returned list.

    Regression guard for #454: a memoization of these helpers that hands
    out one shared list (e.g. a bare ``@lru_cache`` on the function)
    makes one caller's mutation corrupt every later call. These tests
    pin the isolation invariant on main so any future caching of
    ``get_input_file_order`` / ``_block_context_for_lines`` must return
    a fresh copy (or an immutable value) per call.
    """

    def test_get_input_file_order_returns_independent_lists(
        self, sample_flake_nix
    ):
        first = flake_tidy.get_input_file_order(sample_flake_nix)
        second = flake_tidy.get_input_file_order(sample_flake_nix)
        assert first == second
        assert first is not second

    def test_get_input_file_order_mutation_does_not_poison_later_calls(
        self, sample_flake_nix
    ):
        expected = list(flake_tidy.get_input_file_order(sample_flake_nix))
        returned = flake_tidy.get_input_file_order(sample_flake_nix)
        returned.append("poisoned")
        returned.reverse()
        returned.clear()
        assert flake_tidy.get_input_file_order(sample_flake_nix) == expected

    def test_block_context_for_lines_returns_independent_lists(
        self, sample_flake_nix
    ):
        first = flake_tidy._block_context_for_lines(sample_flake_nix)
        second = flake_tidy._block_context_for_lines(sample_flake_nix)
        assert first == second
        assert first is not second
        # The fixture has named input blocks, so the result is not all None.
        assert "nixpkgs-unstable" in first

    def test_block_context_mutation_does_not_poison_later_calls(
        self, sample_flake_nix
    ):
        expected = list(flake_tidy._block_context_for_lines(sample_flake_nix))
        returned = flake_tidy._block_context_for_lines(sample_flake_nix)
        for i in range(len(returned)):
            returned[i] = "poisoned"
        assert flake_tidy._block_context_for_lines(sample_flake_nix) == expected


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


class TestNestedInputsBlockFollowsExists:
    """Regression tests for #647: a follows declared in the nested-block
    override style (``foo = { inputs = { nixpkgs.follows = ...; }; }``)
    was invisible to both existence checkers, so the apply paths that
    guard on them would insert a duplicate declaration of the same
    attribute path — a Nix duplicate-attribute error."""

    NESTED = """{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    foo = {
      url = "github:example/foo";
      inputs = {
        nixpkgs.follows = "nixpkgs";
      };
    };
  };
}
"""

    def test_exists_in_nested_block(self):
        assert (
            flake_tidy.follows_exists_in_content(
                self.NESTED, ["foo", "nixpkgs"], "nixpkgs"
            )
            is True
        )

    def test_any_target_in_nested_block(self):
        assert (
            flake_tidy.follows_path_has_any_target(
                self.NESTED, ["foo", "nixpkgs"]
            )
            is True
        )

    def test_wrong_target_does_not_exist_but_path_does(self):
        assert (
            flake_tidy.follows_exists_in_content(
                self.NESTED, ["foo", "nixpkgs"], "other"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(
                self.NESTED, ["foo", "nixpkgs"]
            )
            is True
        )

    def test_nested_block_of_different_input_does_not_count(self):
        content = self.NESTED.replace("foo = {", "bar = {")
        assert "bar = {" in content
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is False
        )

    def test_nested_block_for_different_child_does_not_count(self):
        content = self.NESTED.replace(
            'nixpkgs.follows = "nixpkgs";', 'other.follows = "nixpkgs";'
        )
        assert "other.follows" in content
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is False
        )

    def test_bare_relative_line_directly_in_block_does_not_count(self):
        """``nixpkgs.follows`` directly in foo's block (not inside its
        nested ``inputs = {``) declares foo.nixpkgs.follows — a
        different attribute — and must not count."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      nixpkgs.follows = "nixpkgs";
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is False
        )

    def test_shifted_form_inside_nested_block_does_not_count(self):
        """``inputs.nixpkgs.follows`` INSIDE the nested block declares
        foo.inputs.inputs.nixpkgs.follows, not foo.inputs.nixpkgs —
        the nested scope is judged only on its own relative form."""
        content = self.NESTED.replace(
            'nixpkgs.follows = "nixpkgs";',
            'inputs.nixpkgs.follows = "nixpkgs";',
        )
        assert "inputs.nixpkgs.follows" in content
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )

    def test_one_line_nested_block(self):
        content = self.NESTED.replace(
            """      inputs = {
        nixpkgs.follows = "nixpkgs";
      };""",
            '      inputs = { nixpkgs.follows = "nixpkgs"; };',
        )
        assert 'inputs = { nixpkgs.follows = "nixpkgs"; };' in content
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is True
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is True
        )

    def test_one_line_nested_block_multiple_declarations(self):
        """#649: a one-line nested block with two declarations must be
        judged per-declaration — every position counts, for both
        checkers (previously equality missed both, prefix missed all
        but the first)."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      inputs = { other.follows = "other"; nixpkgs.follows = "nixpkgs"; };
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "other"], "other"
            )
            is True
        )
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is True
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "other"])
            is True
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is True
        )
        # swapped order: the first position counts too
        swapped = content.replace(
            'other.follows = "other"; nixpkgs.follows = "nixpkgs";',
            'nixpkgs.follows = "nixpkgs"; other.follows = "other";',
        )
        assert "nixpkgs.follows" in swapped
        assert (
            flake_tidy.follows_exists_in_content(
                swapped, ["foo", "nixpkgs"], "nixpkgs"
            )
            is True
        )
        assert (
            flake_tidy.follows_path_has_any_target(swapped, ["foo", "nixpkgs"])
            is True
        )

    def test_one_line_multi_decl_wrong_child_and_shifted_do_not_count(self):
        """#649 guards: a different child in a multi-declaration
        one-line block does not count, and the shifted
        ``inputs.nixpkgs.follows`` form inside it still does not
        count (the #647 guard, in the multi-declaration shape)."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      inputs = { other.follows = "other"; inputs.nixpkgs.follows = "nixpkgs"; };
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "nixpkgs"])
            is False
        )
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "missing"], "missing"
            )
            is False
        )
        assert (
            flake_tidy.follows_path_has_any_target(content, ["foo", "missing"])
            is False
        )

    def test_one_line_multi_decl_semicolon_in_target(self):
        """#649: a ``;`` inside a quoted target must not split the
        declaration."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      inputs = { other.follows = "a;b"; nixpkgs.follows = "nixpkgs"; };
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(content, ["foo", "other"], "a;b")
            is True
        )
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is True
        )

    def test_deep_path_spelled_in_nested_block(self):
        """foo's nested block can spell a deeper path relative to foo:
        ``child.inputs.systems.follows`` = foo/child/systems."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      inputs = {
        child.inputs.systems.follows = "systems";
      };
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "child", "systems"], "systems"
            )
            is True
        )
        assert (
            flake_tidy.follows_path_has_any_target(
                content, ["foo", "child", "systems"]
            )
            is True
        )

    def test_second_level_nested_block(self):
        """A named block inside foo's nested block can carry its own
        nested ``inputs = {``: its scope prefix is [foo, bar]."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
      inputs = {
        bar = {
          inputs = {
            nixpkgs.follows = "nixpkgs";
          };
        };
      };
    };
  };
}
"""
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "bar", "nixpkgs"], "nixpkgs"
            )
            is True
        )
        # ... and it is not a declaration about foo/nixpkgs directly
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["foo", "nixpkgs"], "nixpkgs"
            )
            is False
        )


class TestBlockContextClosedBlocks:
    """Regression tests for #628: a block that does not close on a
    line of its own must still end.

    ``_block_context_for_lines`` used to pop its stack only for a
    line that is exactly ``};``, so a one-line block
    (``foo = { url = "..."; };``) — or a block whose ``};`` shares a
    line with the last attribute — stayed "open" forever and every
    later line in the inputs section was attributed to it. Existing
    follows declarations after such a block were then invisible to
    ``follows_exists_in_content`` — the guard the dedup/flatten
    proposal filters rely on — so they were re-proposed and would be
    duplicated on apply.
    """

    ONE_LINE = """{
  inputs = {
    foo = { url = "github:a/foo"; };
    bar.url = "github:a/bar";
    bar.inputs.nixpkgs.follows = "nixpkgs";
  };
}
"""

    SAME_LINE_CLOSE = """{
  inputs = {
    foo = {
      url = "github:a/foo"; };
    bar.inputs.nixpkgs.follows = "nixpkgs";
  };
}
"""

    def test_one_line_block_line_itself_is_in_block(self):
        ctx = flake_tidy._block_context_for_lines(self.ONE_LINE)
        assert ctx[2] == "foo"

    def test_lines_after_one_line_block_are_root_scope(self):
        ctx = flake_tidy._block_context_for_lines(self.ONE_LINE)
        assert ctx[3] is None
        assert ctx[4] is None

    def test_lines_after_same_line_close_are_root_scope(self):
        ctx = flake_tidy._block_context_for_lines(self.SAME_LINE_CLOSE)
        assert ctx[3] == "foo"
        assert ctx[4] is None

    def test_follows_exists_after_one_line_block(self):
        assert (
            flake_tidy.follows_exists_in_content(
                self.ONE_LINE, ["bar", "nixpkgs"], "nixpkgs"
            )
            is True
        )

    def test_follows_path_has_any_target_after_one_line_block(self):
        assert (
            flake_tidy.follows_path_has_any_target(
                self.ONE_LINE, ["bar", "nixpkgs"]
            )
            is True
        )

    def test_block_after_one_line_block_still_tracked(self):
        content = """{
  inputs = {
    foo = { url = "github:a/foo"; };
    bar = {
      url = "github:a/bar";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
}
"""
        ctx = flake_tidy._block_context_for_lines(content)
        assert ctx[4] == "bar"
        assert ctx[5] == "bar"
        assert (
            flake_tidy.follows_exists_in_content(
                content, ["bar", "nixpkgs"], "nixpkgs"
            )
            is True
        )


class TestDottedParentBlockRootScope:
    """Regression tests for #655: declarations inside a dotted-style
    parent block (``inputs.parent = { ... };``) are that parent's
    attributes and nested overrides, not root declarations.

    The root-scope checkers used to treat them as root: the dotted
    branch of ``root_input_exists_in_content`` scanned every line
    with no block context at all, and ``_block_context_for_lines``
    could not see the dotted block open (its name group stops at the
    first dot), so ``find_input_url_line`` / ``find_input_dotted_line``
    also reported the nested lines as root scope.
    """

    DOTTED_PARENT = """{
  inputs.parent = {
    url = "github:example/parent";
    inputs.child.url = "github:example/child";
    inputs.child.follows = "parent";
  };
  inputs.real.url = "github:example/real";
}
"""

    def test_block_context_attributes_lines_to_parent(self):
        ctx = flake_tidy._block_context_for_lines(self.DOTTED_PARENT)
        assert ctx[2] == "parent"
        assert ctx[3] == "parent"
        assert ctx[4] == "parent"
        assert ctx[6] is None

    def test_nested_child_is_not_a_root_input(self):
        assert (
            flake_tidy.root_input_exists_in_content(self.DOTTED_PARENT, "child")
            is False
        )

    def test_parent_and_sibling_roots_still_exist(self):
        assert (
            flake_tidy.root_input_exists_in_content(self.DOTTED_PARENT, "parent")
            is True
        )
        assert (
            flake_tidy.root_input_exists_in_content(self.DOTTED_PARENT, "real")
            is True
        )
        assert (
            flake_tidy.root_input_exists_in_content(self.DOTTED_PARENT, "missing")
            is False
        )

    def test_genuine_top_level_dotted_child_still_found(self):
        content = '{\n  inputs.child.url = "github:example/child";\n}\n'
        assert flake_tidy.root_input_exists_in_content(content, "child") is True

    def test_find_url_line_ignores_nested_child(self):
        assert flake_tidy.find_input_url_line(self.DOTTED_PARENT, "child") is None
        assert flake_tidy.find_input_url_line(self.DOTTED_PARENT, "real") == 6

    def test_find_dotted_line_ignores_nested_child(self):
        assert flake_tidy.find_input_dotted_line(self.DOTTED_PARENT, "child") == (
            None,
            False,
        )
        assert flake_tidy.find_input_dotted_line(self.DOTTED_PARENT, "real") == (
            6,
            False,
        )

    def test_remove_url_line_leaves_nested_override_alone(self):
        new_content, removed = flake_tidy.remove_input_url_line(
            self.DOTTED_PARENT, "child"
        )
        assert removed is False
        assert new_content == self.DOTTED_PARENT

    def test_nested_overrides_are_the_parents_follows(self):
        """guard: the nested lines still count as parent's overrides."""
        assert (
            flake_tidy.follows_exists_in_content(
                self.DOTTED_PARENT, ["parent", "child"], "parent"
            )
            is True
        )
        assert (
            flake_tidy.follows_exists_in_content(
                self.DOTTED_PARENT, ["child"], "parent"
            )
            is False
        )

    def test_file_order_ignores_nested_child_in_dotted_parent(self):
        """get_input_file_order must not list the dotted parent's nested
        override child as a root input (#652 remainder: PR #653's
        in_inputs_block guard cannot fire here — this content has no
        `inputs = {` block)."""
        assert flake_tidy.get_input_file_order(self.DOTTED_PARENT) == [
            "parent",
            "real",
        ]

    def test_file_order_genuine_top_level_dotted_child(self):
        """guard: a genuine top-level dotted declaration is still a root
        input in file order."""
        content = '{\n  inputs.child.url = "github:example/child";\n}\n'
        assert flake_tidy.get_input_file_order(content) == ["child"]

    def test_file_order_sibling_dotted_parent_blocks(self):
        """guard: each dotted parent block's own opening line is a root
        declaration, and a later sibling's nested child is not."""
        content = """{
  inputs.a = {
    url = "github:example/a";
  };
  inputs.b = {
    inputs.c.url = "github:example/c";
  };
  inputs.d.url = "github:example/d";
}
"""
        assert flake_tidy.get_input_file_order(content) == ["a", "b", "d"]

    def test_one_line_dotted_parent_block(self):
        content = """{
  inputs.parent = { url = "github:example/parent"; inputs.child.url = "github:example/child"; };
  inputs.real.url = "github:example/real";
}
"""
        assert flake_tidy.root_input_exists_in_content(content, "child") is False
        assert flake_tidy.root_input_exists_in_content(content, "parent") is True
        assert flake_tidy.root_input_exists_in_content(content, "real") is True
        assert flake_tidy.find_input_url_line(content, "child") is None

    def test_sibling_dotted_parent_blocks(self):
        content = """{
  inputs.a = {
    url = "github:example/a";
  };
  inputs.b = {
    inputs.c.url = "github:example/c";
  };
  inputs.d.url = "github:example/d";
}
"""
        assert flake_tidy.root_input_exists_in_content(content, "a") is True
        assert flake_tidy.root_input_exists_in_content(content, "b") is True
        assert flake_tidy.root_input_exists_in_content(content, "c") is False
        assert flake_tidy.root_input_exists_in_content(content, "d") is True


class TestPrimedInputNames:
    """Regression tests for #683: primed input names (``nixpkgs'``) were
    invisible to the block-context and nested-prefix scanners — their
    block-open patterns used ``(\\w[\\w-]*)`` while every other name
    pattern in the file accepts the apostrophe — so a follows declared
    inside a primed input's blocks was not seen."""

    PRIMED_BLOCK = """{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    nixpkgs' = {
      url = "github:NixOS/nixpkgs/pinned";
      inputs.child = {
        url = "github:example/child";
      };
    };
    real.url = "github:example/real";
  };
}
"""

    PRIMED_NESTED = """{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    nixpkgs' = {
      url = "github:NixOS/nixpkgs/pinned";
      inputs = {
        child.follows = "nixpkgs";
      };
    };
  };
}
"""

    def test_block_context_attributes_lines_to_primed_parent(self):
        ctx = flake_tidy._block_context_for_lines(self.PRIMED_BLOCK)
        lines = self.PRIMED_BLOCK.split("\n")
        url_line = lines.index('      url = "github:NixOS/nixpkgs/pinned";')
        assert ctx[url_line] == "nixpkgs'"
        nested_line = lines.index("      inputs.child = {")
        assert ctx[nested_line] == "nixpkgs'"

    def test_nested_prefix_for_primed_parent(self):
        prefixes = flake_tidy._nested_inputs_prefix_for_lines(self.PRIMED_NESTED)
        assert ["nixpkgs'"] in prefixes

    def test_follows_exists_in_primed_parents_nested_block(self):
        assert (
            flake_tidy.follows_exists_in_content(
                self.PRIMED_NESTED, ["nixpkgs'", "child"], "nixpkgs"
            )
            is True
        )

    def test_primed_dotted_url_follows_conflict_detected(self):
        content = """{
  inputs.nixpkgs'.url = "github:NixOS/nixpkgs/pinned";
  inputs.nixpkgs'.follows = "nixpkgs";
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["nixpkgs'"]


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


class TestInsertRootInputDottedParentScope:
    """#696: the dotted anchor scan must not land inside a dotted parent block."""

    DOTTED_PARENT_LAST = """{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs";
  inputs.parent = {
    url = "github:example/parent";
    inputs.child.follows = "nixpkgs";
  };
}
"""

    def _parent_block_lines(self, content):
        lines = content.split("\n")
        start = next(i for i, l in enumerate(lines) if "inputs.parent = {" in l)
        end = next(i for i in range(start + 1, len(lines)) if lines[i].strip() == "};")
        return lines[start : end + 1]

    def test_insert_root_input_after_dotted_parent_block(self):
        result = flake_tidy.insert_root_input(
            self.DOTTED_PARENT_LAST, "newinput", "github:example/new"
        )
        assert flake_tidy.root_input_exists_in_content(result, "newinput") is True
        assert not any("newinput" in l for l in self._parent_block_lines(result))
        url_line = flake_tidy.find_input_url_line(result, "newinput")
        assert url_line is not None
        ctx = flake_tidy._block_context_for_lines(result)
        assert ctx[url_line] is None

    def test_insert_root_input_block_after_dotted_parent_block(self):
        result = flake_tidy.insert_root_input_block(
            self.DOTTED_PARENT_LAST,
            "newinput",
            "github:example/new",
            [("sub", "nixpkgs")],
        )
        assert flake_tidy.root_input_exists_in_content(result, "newinput") is True
        assert not any("newinput" in l for l in self._parent_block_lines(result))
        assert flake_tidy.follows_exists_in_content(
            result, ["newinput", "sub"], "nixpkgs"
        )

    def test_commented_inputs_line_inside_parent_is_not_anchor(self):
        content = """{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs";
  inputs.parent = {
    url = "github:example/parent";
    # inputs.ghost.url = "github:example/ghost";
  };
}
"""
        result = flake_tidy.insert_root_input(content, "newinput", "github:example/new")
        assert flake_tidy.root_input_exists_in_content(result, "newinput") is True
        assert not any("newinput" in l for l in self._parent_block_lines(result))

    def test_plain_dotted_anchor_unchanged(self):
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  outputs = _: {};
}
"""
        result = flake_tidy.insert_root_input(content, "new-input", "github:foo/bar")
        lines = result.split("\n")
        anchor = lines.index('  inputs.nixpkgs.url = "github:nixos/nixpkgs";')
        assert lines[anchor + 1] == '  inputs.new-input.url = "github:foo/bar";'


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


class TestFindInputUrlLine:
    def test_find_url_block_style(self, sample_flake_nix):
        """finds url inside a block-style input."""
        line = flake_tidy.find_input_url_line(sample_flake_nix, "nixpkgs-unstable")
        assert line is not None
        lines = sample_flake_nix.split("\n")
        assert "url" in lines[line]
        assert "nixos/nixpkgs" in lines[line]

    def test_find_url_dotted_style(self):
        """finds url in dotted style."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.foo.url = "github:foo/bar";
  outputs = _: {};
}
"""
        line = flake_tidy.find_input_url_line(content, "foo")
        assert line is not None
        assert "foo/bar" in content.split("\n")[line]

    def test_find_url_block_inline_dotted(self, sample_flake_nix):
        """finds dotted url at block level."""
        # nixpkgs uses ``nixpkgs.url = ...`` inside the inputs block
        line = flake_tidy.find_input_url_line(sample_flake_nix, "nixpkgs")
        assert line is not None
        assert "nixos/nixpkgs" in sample_flake_nix.split("\n")[line]

    def test_find_url_nonexistent(self, sample_flake_nix):
        """returns None for input without url."""
        assert flake_tidy.find_input_url_line(sample_flake_nix, "nonexistent") is None


class TestRemoveInputUrlLine:
    def test_remove_url_block(self, sample_flake_nix):
        """removes url from block-style input."""
        new_content, removed = flake_tidy.remove_input_url_line(
            sample_flake_nix, "nixpkgs-unstable"
        )
        assert removed is True
        assert 'url = "github:nixos/nixpkgs"' not in new_content or new_content.count(
            'url = "github:nixos/nixpkgs"'
        ) < sample_flake_nix.count('url = "github:nixos/nixpkgs"')

    def test_remove_url_dotted(self):
        """removes url from dotted-style input."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.foo.url = "github:foo/bar";
  outputs = _: {};
}
"""
        new_content, removed = flake_tidy.remove_input_url_line(content, "foo")
        assert removed is True
        assert "foo/bar" not in new_content
        # nixpkgs url should still be there
        assert "nixos/nixpkgs" in new_content

    def test_remove_url_nonexistent(self, sample_flake_nix):
        """no-op for input without url."""
        new_content, removed = flake_tidy.remove_input_url_line(
            sample_flake_nix, "nonexistent"
        )
        assert removed is False
        assert new_content == sample_flake_nix


class TestFollowsTargetForInput:
    def test_block_style(self):
        """finds follows target in block style."""
        content = """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    stable = {
      url = "github:nixos/nixpkgs";
      follows = "nixpkgs";
    };
  };
  outputs = _: {};
}
"""
        target = flake_tidy._follows_target_for_input(content, "stable")
        assert target == "nixpkgs"

    def test_dotted_style(self):
        """finds follows target in dotted style."""
        content = """{
  inputs.nixpkgs.url = "github:nixos/nixpkgs";
  inputs.stable.url = "github:nixos/nixpkgs";
  inputs.stable.follows = "nixpkgs";
  outputs = _: {};
}
"""
        target = flake_tidy._follows_target_for_input(content, "stable")
        assert target == "nixpkgs"

    def test_no_follows(self, sample_flake_nix):
        """returns None when input has no follows."""
        target = flake_tidy._follows_target_for_input(
            sample_flake_nix, "nixpkgs-unstable"
        )
        assert target is None


class TestNestedBlockDoesNotShadowRootInput:
    """Regression tests for #634: a same-name block nested inside another
    input's ``inputs = { ... }`` override is a different input and must not
    shadow the root input in the block-style editor scanners."""

    NESTED_FLAKE = """{
  inputs = {
    bar = {
      url = "github:example/bar";
      inputs = {
        foo = {
          follows = "bar/nixpkgs";
        };
      };
    };
    foo = {
      url = "github:example/foo";
      follows = "nixpkgs";
    };
    nixpkgs.url = "github:NixOS/nixpkgs";
  };
}
"""

    def test_url_line_finds_root_input(self):
        """the nested foo block (no url) does not hide the root foo url."""
        line = flake_tidy.find_input_url_line(self.NESTED_FLAKE, "foo")
        assert line is not None
        assert "example/foo" in self.NESTED_FLAKE.split("\n")[line]

    def test_remove_url_line_removes_root_input(self):
        """remove_input_url_line removes the root foo url, not nothing."""
        new_content, removed = flake_tidy.remove_input_url_line(
            self.NESTED_FLAKE, "foo"
        )
        assert removed is True
        assert "example/foo" not in new_content
        assert "example/bar" in new_content

    def test_follows_target_is_root_inputs(self):
        """the root foo follows target wins over the nested override's."""
        target = flake_tidy._follows_target_for_input(self.NESTED_FLAKE, "foo")
        assert target == "nixpkgs"

    def test_block_end_is_root_inputs(self):
        """find_input_block_end returns the root foo block's closing line."""
        result = flake_tidy.find_input_block_end(self.NESTED_FLAKE, "foo")
        assert result is not None
        end_line, _indent = result
        lines = self.NESTED_FLAKE.split("\n")
        # the root foo block closes right before the nixpkgs.url line
        assert lines[end_line + 1].strip().startswith("nixpkgs.url")

    def test_dotted_nested_override_is_not_root_follows(self):
        """a dotted inputs.foo.follows inside bar's block is bar's override."""
        content = """{
  inputs = {
    bar = {
      url = "github:example/bar";
      inputs.foo.follows = "bar/nixpkgs";
    };
    foo = {
      url = "github:example/foo";
    };
    nixpkgs.url = "github:NixOS/nixpkgs";
  };
}
"""
        assert flake_tidy._follows_target_for_input(content, "foo") is None
        line = flake_tidy.find_input_url_line(content, "foo")
        assert line is not None
        assert "example/foo" in content.split("\n")[line]

    def test_sibling_block_still_found(self):
        """guard: the enclosing input itself still resolves normally."""
        line = flake_tidy.find_input_url_line(self.NESTED_FLAKE, "bar")
        assert line is not None
        assert "example/bar" in self.NESTED_FLAKE.split("\n")[line]
        result = flake_tidy.find_input_block_end(self.NESTED_FLAKE, "bar")
        assert result is not None


class TestNestedInputsBlockDoesNotResetScanners:
    """Regression tests for #640: PR #635 guarded the block-start branch in
    three scanners, but a nested ``inputs = {`` line still re-entered the
    block-start branch in four more, resetting each scanner's brace depth
    so everything after the nested block was misread."""

    NESTED_FLAKE = TestNestedBlockDoesNotShadowRootInput.NESTED_FLAKE

    def test_file_order_counts_only_root_inputs(self):
        """nested-only names are not root inputs; trailing roots survive."""
        content = self.NESTED_FLAKE.replace(
            """        foo = {
          follows = "bar/nixpkgs";
        };""",
            """        foo = {
          follows = "bar/nixpkgs";
        };
        onlyNested = {
          follows = "nixpkgs";
        };""",
        )
        assert "onlyNested" in content  # the replace actually applied
        order = flake_tidy.get_input_file_order(content)
        assert order == ["bar", "foo", "nixpkgs"]

    def test_insert_before_inputs_end_uses_root_block_end(self):
        """the new line lands before the ROOT inputs close, not bar's."""
        content = """{
  inputs = {
    bar = {
      inputs = {
        foo = { follows = "nixpkgs"; };
      };
    };
    nixpkgs.url = "github:NixOS/nixpkgs";
  };
  outputs = { self }: { };
}
"""
        out = flake_tidy._insert_before_inputs_end(
            content.split("\n"), '    newinput.url = "github:example/new";', True
        )
        lines = out.split("\n")
        idx = next(i for i, line in enumerate(lines) if "newinput" in line)
        # after the last root input, just before the root block's closing };
        assert "nixpkgs.url" in lines[idx - 1]
        assert lines[idx + 1].strip() == "};"
        assert lines[idx + 1].startswith("  }")

    def test_file_order_plain_block_unchanged(self):
        """guard: a plain block with no nested override is unaffected."""
        content = """{
  inputs = {
    foo = {
      url = "github:example/foo";
    };
    nixpkgs.url = "github:NixOS/nixpkgs";
  };
}
"""
        assert flake_tidy.get_input_file_order(content) == ["foo", "nixpkgs"]


class TestBraceCountingIgnoresCommentsAndStrings:
    """Regression tests for #623: braces in comments/strings are not structural."""

    COMMENT_FLAKE = """{
  inputs = {
    foo = {
      url = "github:a/b";
    }; # } stray brace in a comment
    bar = {
      url = "github:c/d";
    };
    baz = {
      url = "github:e/f";
    };
  };
}
"""

    def test_order_survives_comment_brace(self):
        """inputs after a `}; # }` line are still found."""
        order = flake_tidy.get_input_file_order(self.COMMENT_FLAKE)
        assert order == ["foo", "bar", "baz"]

    def test_editor_finds_input_after_comment_brace(self):
        """block end and url line resolve for an input after the comment."""
        assert flake_tidy.find_input_block_end(self.COMMENT_FLAKE, "bar") == (
            7,
            "      ",
        )
        assert flake_tidy.find_input_url_line(self.COMMENT_FLAKE, "bar") == 6

    def test_detect_style_end_survives_comment_brace(self):
        """detect_inputs_style reports the real inputs-block end line."""
        style, start, end = flake_tidy.detect_inputs_style(self.COMMENT_FLAKE)
        assert style == "block"
        assert end == 11

    def test_open_brace_in_comment_does_not_extend_block(self):
        """a `# {` comment must not keep the inputs block open either."""
        content = self.COMMENT_FLAKE.replace("# } stray", "# { stray")
        assert flake_tidy.get_input_file_order(content) == ["foo", "bar", "baz"]

    def test_brace_in_string_is_not_structural(self):
        """a `}` inside a quoted url value does not close the block."""
        content = """{
  inputs = {
    foo = {
      url = "https://example.com/a}b";
    };
    bar.url = "github:c/d";
  };
}
"""
        assert flake_tidy.get_input_file_order(content) == ["foo", "bar"]
        assert flake_tidy.find_input_url_line(content, "bar") == 5

    def test_hash_in_string_does_not_start_comment(self):
        """a `#` inside a quoted value is not a comment start."""
        content = """{
  inputs = {
    foo = {
      url = "https://example.com/a#}b";
    };
    bar.url = "github:c/d";
  };
}
"""
        assert flake_tidy.get_input_file_order(content) == ["foo", "bar"]

    def test_deep_follows_survives_comment_brace(self):
        """_parse_deep_follows still sees blocks after a `}; # }` line."""
        content = """{
  inputs = {
    parent = {
      url = "github:a/b";
    }; # }
    other = {
      inputs.child.inputs.systems.follows = "systems";
    };
  };
}
"""
        results = flake_tidy._parse_deep_follows(content)
        assert len(results) == 1
        assert results[0]["parent"] == "other"
        assert results[0]["child"] == "child"

    def test_brace_counts_helper(self):
        """the shared counter ignores comments and string contents."""
        assert flake_tidy._brace_counts("foo = { # }") == (1, 0)
        assert flake_tidy._brace_counts('url = "a{b}c";') == (0, 0)
        assert flake_tidy._brace_counts("}; # { { {") == (0, 1)
        assert flake_tidy._brace_counts('x = "a\\"}b"; }') == (0, 1)


class TestUrlFollowsConflicts:
    """check_url_follows_conflicts: only a root input's OWN url/follows
    count — attributes of a nested ``inputs = { ... }`` override belong
    to the nested input (#642). First tests for this function."""

    def test_nested_override_url_is_not_the_root_inputs(self):
        """root input with follows only + nested bar url -> no conflict."""
        content = """{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    foo = {
      follows = "nixpkgs";
      inputs = {
        bar = { url = "github:x/y"; };
      };
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []

    def test_nested_override_follows_is_not_the_root_inputs(self):
        """root input with url only + nested bar follows -> no conflict."""
        content = """{
  inputs = {
    foo = {
      url = "github:x/y";
      inputs = {
        bar = {
          follows = "nixpkgs";
        };
      };
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []

    def test_dotted_nested_attrs_are_not_the_root_inputs(self):
        """dotted inputs.bar.url inside foo's block is bar's, not foo's."""
        content = """{
  inputs = {
    foo = {
      inputs.bar.url = "github:x/y";
      follows = "nixpkgs";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []

    def test_one_line_block_conflict_detected(self):
        """a genuine conflict written on one line is reported (#642)."""
        content = """{
  inputs = {
    foo = { url = "github:x/y"; follows = "nixpkgs"; };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_multiline_block_conflict_detected(self):
        """guard: the plain multi-line genuine conflict still reports."""
        content = """{
  inputs = {
    foo = {
      url = "github:x/y";
      follows = "nixpkgs";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_opening_line_attributes_conflict_detected(self):
        """guard: url on the opening line + follows below still reports."""
        content = """{
  inputs = {
    foo = { url = "github:x/y";
      follows = "nixpkgs";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_dotted_style_conflict_and_deep_follows_guard(self):
        """guard: dotted url+follows reports; a deep follows does not."""
        content = """{
  inputs.foo.url = "github:x/y";
  inputs.foo.follows = "nixpkgs";
  inputs.bar.url = "github:a/b";
  inputs.bar.inputs.baz.follows = "nixpkgs";
  outputs = _: {};
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_attribute_after_one_line_nested_block_same_line(self):
        """a direct attribute after a nested block that opens and closes
        on the same line sits at the block's own level and counts (#646)."""
        content = """{
  inputs = {
    foo = {
      inputs = { bar = { url = "github:a/b"; }; }; follows = "nixpkgs";
      url = "github:x/y";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_attribute_after_multiline_nested_block_closes_same_line(self):
        """same, when the nested block spans lines but closes mid-line."""
        content = """{
  inputs = {
    foo = {
      inputs = {
        bar = { url = "github:a/b"; };
      }; follows = "nixpkgs";
      url = "github:x/y";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_follows_in_trailing_comment_is_not_an_attribute(self):
        """a ``; follows =`` inside a trailing comment does not count."""
        content = """{
  inputs = {
    foo = {
      url = "github:x/y"; # note; follows = "nixpkgs"
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []

    def test_url_in_trailing_comment_is_not_an_attribute(self):
        """mirror: a ``url =`` inside a trailing comment does not count."""
        content = """{
  inputs = {
    foo = {
      follows = "nixpkgs"; # url = "github:x/y"
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []

    def test_brace_inside_string_value_does_not_truncate(self):
        """a ``{`` inside a quoted url does not cut the judged segment."""
        content = """{
  inputs = {
    foo = { url = "github:x/{y}"; follows = "nixpkgs"; };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == ["foo"]

    def test_follows_inside_string_value_is_not_an_attribute(self):
        """a ``; follows =`` inside a quoted value does not count."""
        content = """{
  inputs = {
    foo = {
      url = "github:x/y; follows = nixpkgs";
    };
  };
}
"""
        assert flake_tidy.check_url_follows_conflicts(content) == []
