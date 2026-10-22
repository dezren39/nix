## 2026-10-22 - Lazy Candidate Sorting in Plugin Session Resolution
**Learning:** In session management plugins (like `warp-import.js`), eagerly sorting and slicing candidate arrays before checking exact matches causes unnecessary O(N log N) sorts and array allocations on every happy-path lookup.
**Action:** Always evaluate error-fallback lists and candidate candidate sorting lazily when resolving session IDs or titles.
