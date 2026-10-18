# Bolt's Journal

## 2025-05-18 - Memoize IPC socket candidate paths in plugin event loop
**Learning:** In event listener plugins that handle high-frequency event streams (like `sidepulse.js`), calling helper functions that perform environment variable lookups and string path concatenation on every event adds unnecessary CPU overhead.
**Action:** Module-level pre-computation / caching of static path configurations prevents redundant work during event processing.
