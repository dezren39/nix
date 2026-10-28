# mcpx identity transport: comparison and recommendation for #297

- Status: decision made 2026-10-28 (queue task 2654, lane A). No code in
  this document's PR — it answers the question #297 exists to force, so an
  implementation issue can be filed against a settled choice.
- Issues: dezren39/nix#297 (this decision), #187 (the iroh-blobs
  question, answered in §4), #85 (ssh remote access — the incumbent),
  #115 (Tailscale proposal — changes the tsnet calculus if it lands),
  #158 (release artefacts), #174 (parent tracker).
- Code citations are to developing-today/code, where the mcpx source
  lives, verified at `528886eba` on 2026-10-28.

## 1. The requirement

From #297, stated as properties rather than as a product name:

1. A daemon reachable by **stable cryptographic identity**, not by address.
2. **NAT traversal with no port forwarding**, and a **relay fallback** when
   hole punching fails.
3. **End-to-end encryption and mutual authentication** derived from that
   identity.

Plus two constraints from how mcpx is actually built, which any candidate
must survive:

4. **Pure-Go build.** mcpx's `package.nix` sets `env.CGO_ENABLED = "0"`
   (`pkgs/mcpx/package.nix:60`, developing-today/code) and the release
   matrix (#158) cross-compiles without ceremony. A transport that breaks
   this is not a dependency, it is a build-system change.
5. **No silent downgrade.** Failure (relay-only path, identity mismatch)
   must error clearly, never fall back to an unauthenticated route (#297
   acceptance).

Who may call is authorisation (#253/#298/#300) and is explicitly **not**
part of this decision.

## 2. Current state, verified

- Two listeners, both address-shaped. `Server.Listen`
  (`pkgs/mcpx/internal/daemon/server.go:253`) binds a unix socket
  (`net.Listen("unix", …)`, :258) and a TCP host/port (`net.Listen("tcp",
  …)`, :273) — nothing else. The serve loop already iterates a slice of
  `net.Listener`s (:313), which matters in §5: a transport that can hand
  back a `net.Listener` needs no surgery in the daemon.
- One client setting, address-shaped. `daemon.endpoint`
  (`pkgs/mcpx/internal/settings/registry.go:429`): empty means the local
  unix socket, a URL points at a daemon on another machine, `unix:///path`
  targets a different local socket. No identity scheme exists.
- No candidate is present. `grep -rn 'iroh|tsnet|libp2p' pkgs/mcpx/internal
  pkgs/mcpx/cmd pkgs/mcpx/go.mod` at `528886eba`: zero hits.

## 3. Criteria matrix

Scored against §1. ● meets it, ◐ partially / with a real asterisk,
○ fails or fights it.

| Criterion | iroh via cgo (`iroh-ffi`) | iroh as sidecar | `tsnet` (Tailscale) | `go-libp2p` |
|---|---|---|---|---|
| 1. Stable crypto identity | ● ed25519 NodeId is the address | ● same, via the sidecar's NodeId | ● tailnet node identity (WireGuard keypair, account-anchored device name); stable across networks/addresses | ● PeerId from the node's keypair |
| 2. NAT traversal + relay fallback | ● hole punching + iroh relays | ● same | ● the most battle-tested of the four: STUN/hole-punching plus the global DERP relay network | ◐ DCUtR hole punching + Circuit Relay v2 exist, but relay capacity is bring-your-own/public-best-effort, and finding *your* peer (DHT/rendezvous) is the genuinely hard part |
| 3. E2E encryption + mutual auth from identity | ● QUIC/TLS, peer is its key | ● same on the tunnel; the last hop (sidecar→unix socket) is local-trust | ● WireGuard end to end; the server learns the peer's tailnet identity (WhoIs), so mutual auth comes from enrolment | ● Noise/TLS with peer-id authentication |
| 4. Pure-Go build / cross-compile / release cost (#158) | ○ **fails the constraint.** cgo + a Rust toolchain per platform; per iroh's own language docs the Go bindings are *community* FFI bindings — not officially supported, may lag iroh releases, and cover fewer platforms than any other binding. Also embeds a Rust async runtime in the Go process. | ◐ daemon stays pure Go, but the release gains a per-platform Rust binary: a second toolchain in the pipeline, version pairing between daemon and sidecar, and #158's matrix multiplies | ● a Go library (`tailscale.com/tsnet`). `tsnet.Server.Listen` returns a plain `net.Listener`; CGO_ENABLED=0 preserved; no new toolchain | ● pure Go, but the dependency tree is very large for one feature, and it lands in every binary whether used or not |
| 5. Coordinator centralisation | ● none: discovery via DNS/pkarr-style records and public relays; no account | ● none, same | ○ by default the Tailscale coordination server is the control plane — the one genuine structural objection. Mitigation exists: Headscale (self-hosted control server, custom control URL), at the cost of running it | ● none: no coordinator; bootstrap/DHT only |
| 6. Operational sharp edges | two runtimes, one process: panics and debugging cross the FFI boundary; binding lag is a standing risk | a second process to supervise, health-check and address; "mcpx supports iroh" really means "install a thing next to mcpx" | enrolment ceremony (auth key / login URL) for a headless daemon; both ends must be on the tailnet; a large (if mature) dependency | the sharpest edges of the four: relay scarcity, discovery bootstrap, fast-moving APIs, and the operator becomes the network engineer |

## 4. Does #187 want iroh-blobs badly enough to pay for a sidecar? No.

This was the strongest argument for iroh specifically, so it gets its own
section. Read at HEAD, #187 does not ask for cross-machine transfer at all:

- #187's recommended approach (its Approach C) is **one local cache
  package**: per-kind namespaces under `<state>/cache/<kind>/<key>`,
  SHA-256 keys over canonical inputs, lease-pinning while an exec runs,
  eviction by bytes and age. Every word of that is satisfiable on one
  machine with no transport.
- Its three tenants make the point concrete. The import-graph cache is
  kilobytes. The flake-resolution tenant's problem is not moving bytes —
  the entry names a `/nix/store` path, and the problem is holding a **GC
  root** for it; nix itself is already the transfer mechanism for store
  paths. The prepared-dependencies tenant may shrink to a manifest if
  mcpx leans on the runtimes' own caches (deno/bun/npm) — #187's first
  open question — in which case there is nothing large left to transfer.
- "Fetch this script's dependency closure from whichever of my machines
  has it" is a genuinely attractive capability. It is also **a new
  requirement nobody has filed**: #187 neither states it nor needs it,
  and #297's convergence story assumes it rather than citing it.

Conclusion: iroh-blobs buys an option, not a requirement. Options do not
justify a second toolchain in the release pipeline. If cross-machine
content fetch is ever filed as a real requirement, iroh (blobs, as a
sidecar) can be adopted then, behind the same transport interface §5
recommends — this decision does not close that door, it declines to pay
for it early.

## 5. Recommendation

**Adopt `tsnet` as mcpx's first identity transport, behind a small
transport interface in the daemon. Reject iroh-via-cgo outright. Keep
go-libp2p as the named fallback if the coordination objection ever
becomes decisive; keep iroh-sidecar as the named fallback if a
cross-machine content-transfer requirement is ever actually filed.**

Reasons, in the order they decided it:

1. It is the only candidate that meets properties 1–3 **and** the pure-Go
   constraint with a first-party, maintained implementation. The cgo
   route fails constraint 4 on its face and rests on community bindings
   iroh's own docs warn may lag; the sidecar pays a standing release and
   supervision cost for a blobs benefit §4 shows is not currently real.
2. It fits the daemon's existing shape. `Server.Listen` already serves a
   slice of `net.Listener`s; a tsnet listener is one more entry, and the
   address-based listeners are untouched (#297 acceptance: unix, TCP and
   ssh keep working unchanged).
3. The relay network is the product. Relay fallback is where personal
   infrastructure goes to die; DERP is a running global network today,
   while libp2p makes mcpx's operator source their own relay capacity,
   and its discovery story for "my two machines" is the weakest of the
   four despite the strongest decentralisation.
4. It is already in this project's orbit: the fleet's machines already
   run on a tailnet, and #115 proposes Tailscale integration for mcpx
   from the other direction. The marginal dependency cost is therefore
   lower here than in a project starting from zero.
5. The centralisation cost is real but bounded and reversible: identity
   depends on a coordination service by default, Headscale removes that
   dependency without changing the data plane, and the transport
   interface (below) means tsnet is a tenant, not a foundation.

The interface is part of the recommendation, not an afterthought: the
daemon should hold *transports* — objects that produce a `net.Listener`
and a dialer for an identity — with unix/TCP as the built-in address
transports and tsnet as the first identity transport. That is what makes
§5's fallbacks cheap if the world changes, and it is the same shape the
authorisation work (#253/#298/#300) will need regardless of winner: a
policy checks an identity at a transport boundary.

## 6. #297's open questions, answered

- **More than one transport at once, or one at a time?** More than one,
  by design — the serve loop already holds several listeners, and unix +
  tsnet simultaneously is the obviously right default (local tools keep
  the fast path). Ship and *enable* one identity transport first; the
  surface each additional one adds is per-transport identity handling,
  not listener plumbing.
- **Does the TUI work over a relayed connection?** Acceptably, with a
  caveat to measure rather than debate. A DERP-relayed path adds one
  relay hop's latency to every round trip; request/response RPC does not
  notice, an interactive TUI does but remains usable, and the direct
  path is used whenever hole punching succeeds — relay-only is the bad-
  network case, where the alternative today is ssh not reaching the
  machine at all. Two requirements follow: status must show whether a
  connection is direct or relayed (no silent slow path, per §1.5), and
  relay-only TUI latency is an acceptance measurement for the
  implementation issue, not a design blocker here.
- **Phone or browser?** Phone: covered — the phone joins the tailnet
  with the Tailscale app and reaches the daemon as any other node; no
  phone-specific work exists in this design. Browser: **none** of the
  four candidates is reachable from a web page; that requirement means a
  hosted gateway/relay product, which is a different project. This doc
  proceeds on *phone yes, browser no* — and flags the browser half as
  Drew's scope decision, escalated on #297 rather than silently assumed.
- **`daemon.endpoint` scheme, or a new setting?** Grow the scheme. The
  setting already mixes forms (bare URL, `unix:///path`), so an identity
  scheme — e.g. `daemon.endpoint = "tsnet://<hostname>"` — keeps one
  setting and one mental model ("where is my daemon"), instead of two
  settings that can contradict each other.
- **If a sidecar is acceptable here, is it acceptable for the Roc
  platform host too?** Moot under this recommendation: no sidecar is
  adopted, so no precedent is set. A hypothetical second use must not be
  counted in iroh-sidecar's favour today — two issues *possibly* wanting
  a Rust artefact in future is not the same fact as one issue needing
  one now. The Roc question stands on its own when it is asked.

## 7. What this decision does not do

- No transport code, no new dependency, no prototype — a follow-up
  implementation issue (tsnet listener + `tsnet://` endpoint scheme +
  direct/relayed status, with the relay-latency measurement) is the next
  step, filed against this document.
- No authorisation work: who-may-call stays with #253/#298/#300.
- No change to address-based endpoints: unix socket, TCP, and ssh (#85)
  are untouched and remain the fallback when no identity transport is
  configured.

## Sources

- iroh language/bindings status (Go = community FFI bindings, may lag,
  fewer platforms): iroh documentation, "Languages" overview,
  docs.iroh.computer — checked 2026-10-28.
- `tsnet` package summary (embeds a Tailscale node in a Go program,
  returns standard `net.Listener`/`net.Conn`): pkg.go.dev,
  `tailscale.com/tsnet` — checked 2026-10-28.
- All mcpx claims: developing-today/code at `528886eba`, file:line as
  cited in §2.
