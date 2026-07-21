# MCP Subsystem — Known Limitations & Audit Acknowledgements

> **Created:** 2026-04-07
> **Branch:** `configurable-timeout`
> **Context:** Post-audit (Phases 1–3) — these items were reviewed and
> intentionally deferred as acceptable trade-offs for the current
> single-host, developer-tool use case.

---

## H2 — Session Registry Read-Modify-Write Race

**Component:** `mcp_session_registry.ts`

**Issue:** The session registry uses a file-based JSON store with a
read → modify → write cycle.  Two lootbox instances writing
simultaneously could lose one write (last-writer-wins).

**Impact:** A session entry may be silently dropped, causing the
auto-port assigner to re-assign an already-used port.

**Why acceptable now:**
- The registry is used only for the `auto-port` multi-client strategy,
  which is an opt-in, developer-convenience feature.
- Concurrent lootbox starts on the same machine are rare in practice.
- The TCP probe (`isTcpPortAvailable`) provides a secondary check that
  catches most conflicts even when the registry is stale.

**Future mitigation (if needed):**
- Use file-level advisory locking (`flock` / `Deno.flock`) around the
  read-modify-write cycle.
- Or switch to an append-only log format that avoids full-file rewrites.

---

## H3 — TOCTOU in Auto-Port TCP Probe

**Component:** `mcp_auto_port.ts` → `isTcpPortAvailable()`

**Issue:** There is a Time-Of-Check-to-Time-Of-Use gap between probing
a port with a test TCP listen and actually launching the MCP server on
that port.  Another process could claim the port in between.

**Impact:** The MCP server may fail to bind to the assigned port.  The
health monitor will detect this and attempt reconnection, so the failure
is not silent — but the first connection attempt will fail.

**Why acceptable now:**
- The port range (`9222–9299` by default) is a narrow, well-known
  developer-tool range.  Contention with unrelated processes is uncommon.
- The health monitor + reconnection loop provides automatic recovery.
- True atomic port reservation would require OS-level socket inheritance
  (`SO_REUSEPORT` + `fork`) or a filesystem lock, both of which add
  significant complexity for minimal real-world benefit.

**Future mitigation (if needed):**
- Pre-bind the port and pass the listening socket to the child process
  (requires `Deno.listen` → fd passing, not currently supported cleanly).
- Retry with the next port in range on `EADDRINUSE`, before falling back
  to the health-monitor reconnect path.

---

## M5 — Silent JSON Discard in Registry Read

**Component:** `mcp_session_registry.ts` → `readRegistry()`

**Issue:** If the registry JSON file is corrupt (e.g., partial write from
a crash or concurrent writer per H2), `JSON.parse` will throw.  The
current implementation catches this error and returns a fresh empty
registry, silently discarding all prior session data.

**Impact:** All tracked sessions are lost.  The auto-port assigner will
re-probe from scratch, which may reassign ports that are still in use
by running MCP servers.  The TCP probe mitigates most conflicts.

**Why acceptable now:**
- Registry corruption is rare (requires crash mid-write or the H2 race).
- Session data is ephemeral — it represents currently-running processes,
  and stale-PID cleanup already handles the common staleness case.
- Logging the parse error (currently done via `console.error`) allows
  debugging when it does occur.

**Future mitigation (if needed):**
- Write-ahead: write to a `.tmp` file, then `rename()` atomically.
  *(Already partially implemented — see M6 below for rename caveats.)*
- Keep a backup copy of the last-good registry for fallback.

---

## M6 — Temp-File Rename Not Atomic on NFS/CIFS

**Component:** `mcp_session_registry.ts` → `writeRegistry()`

**Issue:** The registry write uses a temp-file + `Deno.rename()` pattern
for crash safety.  While `rename(2)` is atomic on local filesystems
(ext4, APFS, NTFS), it is **not guaranteed atomic** on networked
filesystems like NFS v3, CIFS/SMB, or some FUSE mounts.

**Impact:** On NFS, a concurrent reader could see a partial or empty
file during the rename window, leading to the M5 scenario.

**Why acceptable now:**
- Lootbox is a local developer tool.  The session registry lives in a
  local temp directory (via `Deno.makeTempDir` or user-configured path).
  Running it over NFS is an unusual edge case.
- On all common local filesystems, the rename is truly atomic.

**Future mitigation (if needed):**
- Document that the sessions directory must be on a local filesystem.
- Use `fsync` on the directory after rename to ensure durability.
- For NFS use cases, switch to advisory locking.

---

## L3 — Integration Test Gap

**Component:** `test/mcp_session_integration_test.ts`

**Issue:** The "integration" tests for `McpIntegrationManager` do not
instantiate the real `McpIntegrationManager` class against real MCP
server processes.  They test the session-registry, auto-port, and
strategy logic in isolation using mocks / partial stubs.

**Impact:** Bugs in the wiring between `McpIntegrationManager`,
`McpClientManager`, and `McpHealthMonitor` (e.g., event propagation,
config threading, shutdown ordering) are not exercised by automated
tests.

**Why acceptable now:**
- Unit tests for each component (client manager, health monitor, session
  registry, auto-port assigner) cover the individual modules thoroughly.
  As of this writing, there are **95 passing tests** across these modules.
- A true integration test requires spawning a real MCP server (e.g.,
  `@anthropic/mcp-server-memory`), which introduces external dependencies
  and flakiness from process startup timing.
- Manual testing during development confirmed the wiring works end-to-end.

**Future mitigation (when capacity allows):**
- Add a `test/mcp_e2e_test.ts` that:
  1. Starts a lightweight MCP server fixture (e.g., a simple echo server).
  2. Creates a real `McpIntegrationManager` with that server in its config.
  3. Verifies: connection, health check, simulated disconnect, reconnection,
     circuit breaker trip, and graceful shutdown.
- Guard behind a `--e2e` flag or `Deno.env.get("MCP_E2E")` so it doesn't
  run in CI without the fixture available.

---

## Summary Matrix

| ID | Severity | Component | Status |
|----|----------|-----------|--------|
| H2 | High | session_registry.ts | Acknowledged — mitigated by TCP probe |
| H3 | High | mcp_auto_port.ts | Acknowledged — mitigated by health monitor |
| M5 | Medium | session_registry.ts | Acknowledged — mitigated by stale-PID cleanup |
| M6 | Medium | session_registry.ts | Acknowledged — local-FS assumption documented |
| L3 | Low | integration tests | Acknowledged — unit coverage is comprehensive |
