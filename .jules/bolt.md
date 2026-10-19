## 2025-02-21 - Memoizing line-by-line block context parsing in flake-tidy

**Learning:** In string-parsing analysis loops where candidate proposals check line-by-line block scopes (e.g. `follows_exists_in_content`), re-parsing file block contexts and running `re.sub` per line for every proposal dominates runtime (~71% total CPU time). Caching the line-by-line block context with `@functools.lru_cache(maxsize=16)` and pre-normalizing search strings into tuples for `str.startswith(tuple)` cuts analysis execution time by ~66%.

**Action:** When optimizing analysis routines that evaluate candidate matches against a file's AST or line structure, cache file-structure mappings and pre-normalize search strings outside line iteration loops.
