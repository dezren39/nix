import json
import os
import shutil
import sys
import tempfile
from unittest.mock import patch

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import flake_tidy

FIXTURE_DIR = os.path.join(os.path.dirname(__file__), "fixtures", "sample-flake")


@pytest.fixture
def work_dir():
    tmpdir = tempfile.mkdtemp(prefix="flake-tidy-integration-")
    shutil.copytree(FIXTURE_DIR, tmpdir, dirs_exist_ok=True)
    yield tmpdir
    shutil.rmtree(tmpdir, ignore_errors=True)


@pytest.fixture
def config():
    return flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)


class TestIntegrationDedup:
    def test_finds_root_same_url(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        root_proposals = [p for p in proposals if len(p["follows_parts"]) == 1]
        root_targets = {p["follows_parts"][0] for p in root_proposals}
        assert "nixpkgs-unstable" in root_targets

    def test_finds_transitive_systems(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        paths = {p["follows_path"] for p in proposals}
        assert "flake-utils.systems" in paths

    def test_finds_transitive_nixpkgs(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        paths = {p["follows_path"] for p in proposals}
        assert "some-tool.nixpkgs" in paths

    def test_apply_adds_follows_to_content(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        for p in proposals:
            content = flake_tidy.insert_follows_in_content(
                content, p["follows_parts"], p["target"]
            )
        assert 'follows = "nixpkgs"' in content
        assert "systems" in content

    def test_exclude_prevents_dedup(self, work_dir):
        config = flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)
        config["exclude"]["input"] = ["nixpkgs-unstable"]
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        root_targets = {
            p["follows_parts"][0] for p in proposals if len(p["follows_parts"]) == 1
        }
        assert "nixpkgs-unstable" not in root_targets

    def test_include_narrows_scope(self, work_dir):
        config = flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)
        config["include"]["dedup"] = ["flake-utils"]
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_dedup(lock, content, config)
        for p in proposals:
            assert p["follows_parts"][0] == "flake-utils"


class TestIntegrationFlatten:
    def test_finds_some_lib(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_flatten(lock, content, config)
        names = {p["new_input_name"] for p in proposals}
        assert "some-lib" in names

    def test_exclude_prevents_flatten(self, work_dir):
        config = flake_tidy._deep_copy_config(flake_tidy.DEFAULT_CONFIG)
        config["exclude"]["flatten"] = ["some-lib"]
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_flatten(lock, content, config)
        names = {p["new_input_name"] for p in proposals}
        assert "some-lib" not in names

    def test_apply_adds_root_input(self, work_dir, config):
        lock = flake_tidy.load_lock(work_dir)
        content = flake_tidy.read_flake_nix(work_dir)
        proposals = flake_tidy.analyze_flatten(lock, content, config)
        for p in proposals:
            content = flake_tidy.insert_root_input(
                content, p["new_input_name"], p["url"]
            )
            for fp in p["follows"]:
                content = flake_tidy.insert_follows_in_content(
                    content, fp, p["new_input_name"]
                )
        assert "some-lib" in content
        assert "github:example/some-lib" in content


class TestIntegrationEndToEnd:
    @patch("flake_tidy.run_nix_flake_lock", return_value=(True, ""))
    @patch("flake_tidy.run_nixfmt")
    def test_dedup_check_mode(self, mock_fmt, mock_lock, work_dir, config):
        count = flake_tidy.dedup(work_dir, config, check=True)
        assert count > 0
        mock_lock.assert_not_called()

    @patch("flake_tidy.run_nix_flake_lock", return_value=(True, ""))
    @patch("flake_tidy.run_nixfmt")
    def test_flatten_check_mode(self, mock_fmt, mock_lock, work_dir, config):
        count = flake_tidy.flatten(work_dir, config, check=True)
        assert count > 0
        mock_lock.assert_not_called()
