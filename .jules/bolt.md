## 2026-10-23 - Memoize File Structure Scans and Lift Normalizations in Iterative Checks

**Learning:** `flake-tidy` repeatedly called `_block_context_for_lines` and `_nested_inputs_prefix_for_lines` to parse `flake.nix` line-by-line during `follows_exists_in_content` checks across hundreds of candidate proposals. Additionally, candidate string whitespace was normalized using `re.sub` inside the line iteration loop. Caching structural line context parsing with `@functools.lru_cache` and pre-normalizing candidate strings outside the loop reduced total analysis iteration runtime by over 80%.

**Action:** When validating items in a loop against a fixed document/structure, cache the document structure analysis with `functools.lru_cache` (returning fresh copies if isolation is required by tests/callers) and pre-normalize candidate search patterns before entering line-by-line loop iterations.
