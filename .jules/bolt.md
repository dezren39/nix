## 2026-10-22 - Lazy Candidate Sorting in Plugin Session Resolution
**Learning:** In session management plugins (like `warp-import.js`), eagerly sorting and slicing candidate arrays before checking exact matches causes unnecessary O(N log N) sorts and array allocations on every happy-path lookup.
**Action:** Always evaluate error-fallback lists and candidate candidate sorting lazily when resolving session IDs or titles.

## 2026-10-23 - Memoize File Structure Scans and Lift Normalizations in Iterative Checks

**Learning:** `flake-tidy` repeatedly called `_block_context_for_lines` and `_nested_inputs_prefix_for_lines` to parse `flake.nix` line-by-line during `follows_exists_in_content` checks across hundreds of candidate proposals. Additionally, candidate string whitespace was normalized using `re.sub` inside the line iteration loop. Caching structural line context parsing with `@functools.lru_cache` and pre-normalizing candidate strings outside the loop reduced total analysis iteration runtime by over 80%.

**Action:** When validating items in a loop against a fixed document/structure, cache the document structure analysis with `functools.lru_cache` (returning fresh copies if isolation is required by tests/callers) and pre-normalize candidate search patterns before entering line-by-line loop iterations.

## 2026-10-24 - Memoize Input Style Detection, File Order Parsing, and String Canonicalization

**Learning:** Pure parsing helpers in `flake-tidy` (`detect_inputs_style`, `_brace_counts`, `_normalize_vcs_url`, `_name_ref`, `_unquote_name`, and `get_input_file_order`) were repeatedly re-parsing document lines and executing regex replacements across different linting and analysis passes on the same document content. Adding `@functools.lru_cache` to immutable helpers and wrapping cached tuple functions with list constructors (`list(_get_input_file_order_cached(content))`) preserves complete caller isolation while yielding over 20x speedup on repeated lookups.

**Action:** Memoize pure line-parsing and string-canonicalization helpers using `@functools.lru_cache`. When callers expect mutable collections (`list`), separate the cached calculation into an inner function returning a `tuple` and wrap it in `list()` to guarantee isolation without sacrificing speed.
