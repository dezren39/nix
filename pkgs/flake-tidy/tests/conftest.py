"""Shared test fixtures for flake-tidy tests."""

import json
import os
import sys
import tempfile
import shutil

import pytest

# Add the parent directory to sys.path so we can import flake_tidy
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import flake_tidy


@pytest.fixture
def sample_lock():
    """A sample flake.lock with duplicates for testing."""
    return {
        "nodes": {
            "root": {
                "inputs": {
                    "nixpkgs": "nixpkgs",
                    "nixpkgs-unstable": "nixpkgs-unstable",
                    "systems": "systems",
                    "flake-utils": "flake-utils",
                    "some-tool": "some-tool",
                }
            },
            "nixpkgs": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-SAME-HASH",
                    "owner": "nixos",
                    "repo": "nixpkgs",
                    "rev": "bbbb",
                    "type": "github",
                },
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "nixpkgs-unstable": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-SAME-HASH",
                    "owner": "nixos",
                    "repo": "nixpkgs",
                    "rev": "bbbb",
                    "type": "github",
                },
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "systems": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-systems-hash",
                    "owner": "nix-systems",
                    "repo": "default",
                    "rev": "eeee",
                    "type": "github",
                },
                "original": {
                    "owner": "nix-systems",
                    "repo": "default",
                    "type": "github",
                },
            },
            "systems_2": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-systems-hash",
                    "owner": "nix-systems",
                    "repo": "default",
                    "rev": "eeee",
                    "type": "github",
                },
                "original": {
                    "owner": "nix-systems",
                    "repo": "default",
                    "type": "github",
                },
            },
            "flake-utils": {
                "inputs": {"systems": "systems_2"},
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-flake-utils-hash",
                    "owner": "numtide",
                    "repo": "flake-utils",
                    "rev": "aaaa",
                    "type": "github",
                },
                "original": {
                    "owner": "numtide",
                    "repo": "flake-utils",
                    "type": "github",
                },
            },
            "nixpkgs_2": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-SAME-HASH",
                    "owner": "nixos",
                    "repo": "nixpkgs",
                    "rev": "bbbb",
                    "type": "github",
                },
                "original": {"owner": "nixos", "repo": "nixpkgs", "type": "github"},
            },
            "some-lib": {
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-some-lib-hash",
                    "owner": "example",
                    "repo": "some-lib",
                    "rev": "cccc",
                    "type": "github",
                },
                "original": {"owner": "example", "repo": "some-lib", "type": "github"},
            },
            "some-tool": {
                "inputs": {"nixpkgs": "nixpkgs_2", "some-lib": "some-lib"},
                "locked": {
                    "lastModified": 1710146030,
                    "narHash": "sha256-some-tool-hash",
                    "owner": "example",
                    "repo": "some-tool",
                    "rev": "dddd",
                    "type": "github",
                },
                "original": {"owner": "example", "repo": "some-tool", "type": "github"},
            },
        },
        "root": "root",
        "version": 7,
    }


@pytest.fixture
def sample_flake_nix():
    """A sample flake.nix content for testing."""
    return """{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    nixpkgs-unstable = {
      url = "github:nixos/nixpkgs";
    };
    systems.url = "github:nix-systems/default";
    flake-utils = {
      url = "github:numtide/flake-utils";
    };
    some-tool = {
      url = "github:example/some-tool";
    };
  };
  outputs = _: {};
}
"""


@pytest.fixture
def default_config():
    """Default config for testing."""
    return flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)


@pytest.fixture
def temp_flake_dir(sample_flake_nix, sample_lock):
    """Create a temporary directory with a sample flake.nix and flake.lock."""
    tmpdir = tempfile.mkdtemp(prefix="flake-tidy-test-")
    with open(os.path.join(tmpdir, "flake.nix"), "w") as f:
        f.write(sample_flake_nix)
    with open(os.path.join(tmpdir, "flake.lock"), "w") as f:
        json.dump(sample_lock, f, indent=2)
    yield tmpdir
    shutil.rmtree(tmpdir, ignore_errors=True)
