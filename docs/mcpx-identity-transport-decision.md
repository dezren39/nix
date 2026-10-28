# mcpx identity transport: comparison and recommendation

Decision doc for dezren39/nix#297 ("reach a daemon by identity, not by
address"). Written 2026-10-28 for queue task 2654 (follow-up of triage
task 2324). This doc is the comparison #297's acceptance criteria ask
for; it implements nothing.

## Where mcpx stands today (verified at HEAD)

mcpx (developing-today/code `pkgs/mcpx`, `origin/main`
`e91098f1193ac50ccd049d0876605ea27d8673c8`):

- `daemon.endpoint` is a plain string setting
  (`internal/settings/registry.go:429`): empty means the local unix
  socket, otherwise a URL or `unix:///path`. There is no identity
  scheme in it.
- `Server.Listen` (`internal/daemon/server.go:253-280`) binds exactly
  two listeners: a unix socket (`net.Listen("unix", …)`, :258) and a
  TCP listener (`net.Listen("tcp", …)`, :273).
- A grep of `internal/`, `cmd/` and `go.mod` finds **no iroh, no
  tsnet, no libp2p**. No identity-based transport exists in any form.
- The build is pure Go: `nix build` cross-compiles the binary without
  a second toolchain. That property is load-bearing — several scores
  below are just "does this option keep it".

Authorisation (who may call, once reached) is #253/#298/#300 and is
explicitly **not** a dependency of this decision, per #297 itself.

## The requirement, restated

From #297, the properties a transport must have:

1. A daemon reachable by **stable cryptographic identity**, not by
   address.
2. **NAT traversal** without port forwarding, with a **relay
   fallback** when hole punching fails.
3. **End-to-end encryption and mutual authentication** derived from
   that identity.

…scored alongside the costs this fleet actually pays: pure-Go build /
cross-compile / release-matrix cost (#158), coordinator
centralisation, and operational sharp edges.

## The matrix

| Criterion | iroh via cgo (`iroh-ffi`) | iroh as sidecar | `tsnet` (Tailscale) | `go-libp2p` |
|---|---|---|---|---|
| 1. Stable crypto identity | ✅ NodeId = ed25519 key, owned by the node | ✅ same | ⚠️ Identity is a tailnet node identity — stable, but issued and revocable by the coordination server, not a bare keypair mcpx owns | ✅ PeerId from a keypair, owned by the node |
| 2. NAT traversal + relay fallback | ✅ Hole punching + relay network | ✅ same | ✅ Mature hole punching + DERP relays, the most battle-tested of the four | ⚠️ Hole punching (DCUtR) + Circuit Relay v2, but relay capacity is bring-your-own and discovery needs bootstrap/rendezvous in practice |
| 3. E2E encryption + mutual auth from identity | ✅ QUIC/TLS to the NodeId | ✅ same | ✅ WireGuard to the node identity; ACLs are a bonus answer to part of the #298 question | ✅ Noise/TLS to the PeerId |
| Pure-Go build kept | ❌ cgo + a Rust toolchain per platform; cross-compile stops being free; a Rust async runtime inside a Go process | ✅ mcpx stays pure Go; the Rust artefact is a separate, optional binary | ✅ Pure Go library | ✅ Pure Go library |
| Release-matrix cost (#158) | ❌ Multiplies: per-platform native artefacts in `package.nix` | ⚠️ Adds one optional artefact per platform to the pipeline, not to the main binary | ✅ None beyond a Go dependency | ✅ None beyond a Go dependency |
| Coordinator centralisation | ✅ None required (public relays/discovery are replaceable and self-hostable) | ✅ same | ❌ Depends on Tailscale's coordination service unless the fleet self-hosts Headscale — a real, ongoing operational commitment | ✅ None required, though a personal fleet will likely end up running a bootstrap/relay node, which is a softer version of the same thing |
| Operational sharp edges | Runtime interop debugging nobody here has signed up for | Supervision, addressing and versioning of a second process; "mcpx supports iroh" means "with the sidecar installed" | Account enrolment per node; if the tailnet is down for auth, new nodes cannot join (existing connections persist) | Largest dependency of the four; relay reservations, discovery and NAT behaviour need the most operator understanding |
| Extras nobody else has | — | `iroh-blobs`: BLAKE3-verified, resumable, content-addressed transfer (see #187 below) | Already in this project's orbit: #115 proposes a Tailscale server, which would make the dependency arrive anyway | The most composable protocol stack, at the price of the most surface |

## Does #187 want iroh-blobs badly enough to pay for a sidecar?

**No — not on what #187 actually asks for today.** Read at HEAD,
#187's problem is cache *policy* fragmentation on one machine: three
runtime features (#103 prepared dependencies, #104 import graphs,
#107 flake resolutions) each designing a content-addressed cache
alone, with separate layouts, eviction rules and concurrency bugs.
Its "Current state" section is entirely about local pruners
(`pruneWorkDirs`, `prunePrograms`, `pruneEntries`) and the absence of
size/age eviction and pinning. Nothing in #187 asks to *move* cache
entries between machines.

`iroh-blobs` solves verified, resumable **transfer**. That becomes
#187's problem only on the day someone writes the requirement
"fetch this script's dependency closure from whichever of my machines
has it" — the convergence story #297 sketches. That requirement does
not exist yet. Paying a permanent sidecar and a second toolchain for
a hypothetical requirement is the wrong order; and if the requirement
does land, the interface recommended below admits the sidecar then
without rework.

## Recommendation

**Adopt `tsnet` as mcpx's first identity transport, behind a
transport interface that can hold more than one; treat iroh-as-sidecar
as the sanctioned second transport if the decentralisation or
iroh-blobs requirement is ever written down; reject iroh-via-cgo
outright.**

Reasons, in the order they decided it:

1. **It is the only option that meets 1–3 today with zero build
   damage.** tsnet is pure Go, its NAT traversal and DERP relay
   network are the most proven of the four, and it changes the
   release matrix not at all. The runner-up on purity, go-libp2p,
   meets the letter of 1–3 but leaves relay capacity and discovery
   as the operator's problem — for a single-user fleet that is the
   difference between a feature and a hobby.
2. **Its one real cost — coordination centralisation — is a cost
   this fleet is already deciding to pay.** #115 proposes a
   Tailscale server. If that lands, tsnet's dependency is already in
   the building and this transport is nearly free. If #115 is
   rejected *because* of centralisation, that same decision is the
   trigger to promote the iroh sidecar instead — the interface makes
   that a substitution, not a rewrite. (Self-hosting Headscale is
   the middle path and stays available either way.)
3. **cgo is a permanent tax for a reversible decision.** Linking
   `iroh-ffi` spends the pure-Go build — the property that makes
   mcpx cheap to ship — on the first transport tried. The sidecar
   gets the same iroh guarantees while keeping that spend optional
   and removable.
4. **Identity delegated to a tailnet is a fair trade at this
   scale.** A PeerId/NodeId mcpx owns outright is cleaner in
   principle; in practice the fleet's machines already live and die
   by account-backed identity systems, and tsnet's ACLs give part of
   the #298 answer for free. This is the recommendation's weakest
   point and it is named as such: if "identity no third party can
   revoke" becomes a hard requirement, the answer flips to the
   sidecar.

## #297's open questions, answered

- **More than one transport at once, or one at a time?** The daemon
  should hold transports behind an interface and allow several
  *registered*, but ship and enable **one at a time by
  configuration**. Holding several live multiplies the surface each
  has to be correct on; the interface, not simultaneity, is what
  keeps the second transport cheap to add.
- **Does the TUI work acceptably over a relayed connection?**
  Unmeasured — and it should be measured, in the tsnet spike, rather
  than assumed. Expectation: relay RTT (DERP is HTTPS-shaped and
  regional) makes an interactive TUI noticeably laggy but usable;
  hole-punched connections are unaffected. Recommendation: identity
  reach v1 promises RPC/CLI, and the TUI is labelled best-effort on
  relayed paths until the spike says otherwise.
- **Phone / browser in scope?** **No for v1.** None of iroh, tsnet
  or libp2p is reachable from a web page; a browser requirement
  means a hosted relay/gateway, which is a different project and
  should be filed as one if Drew wants it. Native clients (laptop,
  phone apps, other daemons) are the v1 target.
- **`daemon.endpoint` scheme, or a new setting?** **Grow the
  scheme.** `daemon.endpoint` already holds a URL-or-path string
  (`registry.go:429`); `tsnet://<host>` (and later `iroh://<nodeid>`)
  keeps one setting and one mental model, and the empty-string =
  local-socket default is untouched. Address-based endpoints keep
  working unchanged, as #297's acceptance requires.
- **Does accepting a sidecar here accept one for the Roc platform
  host too?** No. It would be *evidence* — the release pipeline
  would already know how to ship one Rust artefact — but each
  sidecar pays its own distribution and supervision cost and is
  decided on its own merits. Flagged here so the precedent is a
  choice, not a drift.

## What this doc does not do

No transport code, no new dependency, no authorisation work
(#253/#298/#300). Failure-mode requirements for the eventual
implementation are #297's own: relay-only paths and identity
mismatches must produce clear errors, never a silent fallback to an
unauthenticated route.
