# mcpx Development Strategies & Lessons Learned

## Verification and Diagnostics

- **Never test a system against itself.** Driving mcpx's own `server/discover` against mcpx proved both halves agreed, not that either was right — they shared the same wrong field name. Test against the schema, the spec, or a reference implementation.
- The signature defect is **declared but not delivered** (#177): a flag parsed and dropped, a capability advertised and never sent, a setting read but inert. `consumed_test.go` proves a setting is *read*, not that reading it changes anything.
- The daemon is keyed by its **config file set**. Three separate bugs came from this; environment differences that should produce different daemons do not.
- On macOS `/tmp` is a symlink to `/private/tmp` and the product canonicalises paths, so a test using `os.MkdirTemp("/tmp", …)` passes on Linux CI and fails on every Mac.
- `nix build`'s `checkPhase` has had a hand-maintained package list that drifted from the packages that have tests. CI runs `go test ./...` and does not.
- The official conformance suite ships its own fixture server. Running it against a different server measures nothing. **A falling score can mean the code got more correct** — it has, twice.
- Under parallel agents, load averages above 150 push `internal/e2e` past its default timeout. Raise `-timeout`; do not read it as a failure.
