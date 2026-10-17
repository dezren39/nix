## 2026-07-28 - Lazy Evaluation for Candidate List Generation in Search Functions
**Learning:** Eagerly sorting full datasets (e.g. `list.slice().sort(byRecent)`) at the entry of a search/lookup utility incurs unnecessary $O(N \log N)$ compute overhead when an exact match or unique title hit occurs early.
**Action:** Always defer candidate generation/sorting until a fallback/candidate list is explicitly needed or when multiple ambiguous matches require sorting.
