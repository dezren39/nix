## 2026-10-22 - Lazy Candidate Sorting in Plugin Session Resolution
**Learning:** In session management plugins (like `warp-import.js`), eagerly sorting and slicing candidate arrays before checking exact matches causes unnecessary O(N log N) sorts and array allocations on every happy-path lookup.
**Action:** Always evaluate error-fallback lists and candidate candidate sorting lazily when resolving session IDs or titles.

## 2026-10-23 - Memoize File Structure Scans and Lift Normalizations in Iterative Checks

**Learning:** `flake-tidy` repeatedly called `_block_context_for_lines` and `_nested_inputs_prefix_for_lines` to parse `flake.nix` line-by-line during `follows_exists_in_content` checks across hundreds of candidate proposals. Additionally, candidate string whitespace was normalized using `re.sub` inside the line iteration loop. Caching structural line context parsing with `@functools.lru_cache` and pre-normalizing candidate strings outside the loop reduced total analysis iteration runtime by over 80%.

**Action:** When validating items in a loop against a fixed document/structure, cache the document structure analysis with `functools.lru_cache` (returning fresh copies if isolation is required by tests/callers) and pre-normalize candidate search patterns before entering line-by-line loop iterations.

## 2026-10-24 - Memoize Input Style Detection, File Order Parsing, and String Canonicalization

**Learning:** Pure parsing helpers in `flake-tidy` (`detect_inputs_style`, `_brace_counts`, `_normalize_vcs_url`, `_name_ref`, `_unquote_name`, and `get_input_file_order`) were repeatedly re-parsing document lines and executing regex replacements across different linting and analysis passes on the same document content. Adding `@functools.lru_cache` to immutable helpers and wrapping cached tuple functions with list constructors (`list(_get_input_file_order_cached(content))`) preserves complete caller isolation while yielding over 20x speedup on repeated lookups.

**Action:** Memoize pure line-parsing and string-canonicalization helpers using `@functools.lru_cache`. When callers expect mutable collections (`list`), separate the cached calculation into an inner function returning a `tuple` and wrap it in `list()` to guarantee isolation without sacrificing speed.

## 2026-10-27 - Fast-Path Guard for Quoted Attribute Name Unquoting

**Learning:** `_unquote_attr_names` in `flake-tidy` runs `re.sub` over every line during follows checks across hundreds of candidate proposals. Adding a fast-path check (`q_idx == -1 or q_idx > last_op`) skips `re.sub` entirely when no double quotes precede an attribute operator (`.` or `=`), yielding a ~60% speedup on unquoted lines without changing output.

**Action:** Before calling `re.sub` on high-frequency line loops, check if necessary syntax characters (like quotes before operators) are present to fast-path non-matching lines without regular expression invocation.

## 2026-10-28 - Pre-compile Dynamic Regexes in Line Loops and Memoize Presence Queries

**Learning:** Pure content-scanning functions like `root_input_exists_in_content` and `_follows_target_for_input` were repeatedly re-parsing `flake.nix` lines and re-compiling string regular expressions (`re.match(rf"...")`) inside `for` loops across multiple deduplication and candidate proposal checks. Memoizing pure query functions with `@functools.lru_cache` and pre-compiling regex objects (`re.compile`) prior to entering line-by-line loops eliminated repeated string formatting and regex compilation overhead, yielding over 25x speedup for line-scanning queries.

**Action:** Pre-compile dynamic regular expressions into `re.Pattern` objects before entering line-iteration loops, and apply `@functools.lru_cache` to pure document query functions that return immutable types (`bool`, `str | None`).

## 2026-10-31 - Module-level Pre-compiled Token Regexes and Unquoting Fast-Path

**Learning:** Flake line parsing in `_get_input_file_order_cached`, `_block_context_for_lines_cached`, and `_nested_inputs_prefix_for_lines_cached` repeatedly called `re.match` using dynamic f-strings built from `_SEG_TOKEN` and `_NAME_TOKEN` across every line of input files. Pre-compiling static token matching regexes at the module level provided a 2.3x speedup. Furthermore, `_unquote_name` unconditionally executed `re.sub(r"\\(.)", r"\1", ...)` on all quoted tokens; adding a fast-path check `if "\\" in inner:` avoids regex substitution on standard quoted identifiers, speeding unquoting by over 2.6x.

**Action:** Pre-compile token patterns into module-level regex constants (`_BLOCK_OPEN_RE`, etc.) when parsing structural documents, and guard escape-character regex replacements (`re.sub`) with substring containment checks (`if "\\" in s:`).

## 2026-11-01 - Early Line-Level Keyword Filtering and Query Memoization for Follows Checks

**Learning:** `follows_exists_in_content` and `follows_path_has_any_target` are called repeatedly during deduplication, merge, and flattening proposal checks. In uncached line loops, processing every non-comment line through whitespace collapsing and attribute unquoting wastes CPU time on irrelevant lines; adding an early check (`"follows" not in stripped`) bypasses all normalization overhead for lines that cannot possibly match. Furthermore, caching the pure query results via `@functools.lru_cache` on inner functions accepting `tuple[str, ...]` yields a ~35x speedup on repeated lookups while preserving list argument compatibility for callers.

**Action:** Add fast substring checks in line-iteration loops to reject irrelevant lines before running costly string normalizations, and memoize pure query functions that accept list parameters by wrapping a tuple-based inner function with `@functools.lru_cache`.
