# mcpx OAuth authorization server: embed-vs-point-at recommendation for #345

- Status: decision made 2026-10-28 (queue task 2666, lane A). No code in
  this document's PR — it answers the question #345 exists to force, so
  #253 can proceed against a settled choice.
- Recommendation in one line: **hybrid, point-at-first** — mcpx does not
  embed an authorization server; it points at external ones (that is
  #253's resource-server work), and the single-user case is covered by
  #338's optional bearer token plus the unix-socket default, not by a
  home-grown token issuer.
- Issues: dezren39/nix#345 (this decision), #253 (mcpx as OAuth client
  and as resource server — the neighbour this is not), #338 (optional
  bearer token for /v1 and /mcp — the low-end option), #298 and #300
  (principal/permission model — untouched by this decision).
- Code citations are to developing-today/code, where the mcpx source
  lives, verified at `528886eba` on 2026-10-28.

## 1. The question

In MCP's authorization model (2025-06-18 on), an MCP server is an OAuth
2.1 **resource server**: it accepts and checks access tokens issued by a
separate **authorization server** (AS), which publishes RFC 8414
metadata, runs the authorization-code grant with PKCE, registers clients
(dynamic client registration, deprecated in the 2026-07-28 revision, or
Client ID Metadata Documents), and issues tokens.

#345 asks only this: should mcpx **be** an AS — a small embedded one, so
a single-user install can protect `/mcp` without standing up
Keycloak/Auth0/Entra — or only **point at** one?

## 2. Current state, verified

- **No authorization server exists in mcpx.** At `528886eba`, searching
  `pkgs/mcpx/internal` and `pkgs/mcpx/cmd` for an authorization-server
  metadata endpoint, an `/authorize` endpoint, token issuance, or client
  registration finds none outside the conformance corpus
  (`internal/conformance/requirements.json`, which catalogues the spec,
  not an implementation).
- **The only auth package is outbound.** `pkgs/mcpx/internal/mcpauth/
  mcpauth.go:1-18`: "Package mcpauth resolves credentials for a server"
  — bearer/basic/header credentials mcpx presents *to* upstream servers.
  Of the full OAuth flow it says only that the third case has "a place
  to attach" (`mcpauth.go:17`), because "building OAuth before anything
  needs it would be guessing at a shape".
- **Conformance agrees.** Every row of the `authorization` section of
  `pkgs/mcpx/docs/spec/conformance-matrix.md` (section at :397; the
  RFC 8414 row at :410) reads "**gap** not implemented: #253" for both
  the server and client columns. The conformance suite's `authorization`
  command (`authorization-server-metadata-endpoint`,
  `authorization-code-grant`) is not run by mcpx.
- **The neighbours are all still open** (verified 2026-10-28): #253,
  #298, #300, #338. Nothing in them is waiting on an embedded AS; #253
  and #338 are each independently shippable.

## 3. Scoring

Scored against the constraints #345 names. ● meets it, ◐ partially,
○ fails or fights it.

| Criterion | Embed a small AS | Point at external AS only | Hybrid (point-at + #338 low end) |
|---|---|---|---|
| 1. Single-user install friction | ● zero extra services; tokens out of the box | ○ standing up Keycloak for a laptop daemon is the friction that created this issue; most users will skip it and run unprotected | ● unix socket by default, one bearer token (#338) when a TCP listener needs a gate — no IdP to install |
| 2. Conformance surface | ◐ could run the `authorization` conformance command, but must then honour RFC 8414 metadata, authorization-code + PKCE, and a registration story (DCR is deprecated in 2026-07-28; CIMD requires fetching and trusting client metadata documents — an SSRF-shaped surface) | ◐ mcpx's obligations are the client/resource-server rows (#253); the AS-side rows belong to the external AS, which already passes them | ◐ same as point-at; the AS-side conformance commands stay unrun, deliberately (§5) |
| 3. Maintenance / security cost of issuing tokens | ○ the worst code a small project can own: signing-key custody, token storage, expiry, revocation, consent UI, redirect-URI validation. A bug is not a bug, it is credential forgery for every install. The cost is forever, not at ship time | ● mcpx never issues a token; it validates other issuers' tokens via JWKS (#253), a read-only, well-trodden surface | ● same as point-at |
| 4. Fit with #338 as the low-end option | ○ duplicates it: an embedded AS whose only user, client, and resource server are the same person on the same host is a token machine standing in for one shared secret | ◐ leaves the low end empty — bearer or nothing is not offered by this option alone | ● #338 *is* the low end; OAuth is the high end; nothing in between needs building |
| 5. What the security actually buys (single-user) | ○ nothing separable: AS and resource server would be the same process on the same host, so the signing key lives with the thing it protects, and "consent" is the user approving themselves. OAuth's value — delegated authorization for a third party — does not exist in this topology | ● real delegation where it exists: fleet/team installs get SSO, MFA, group claims and revocation from an IdP mcpx could never grow | ● both: delegation where it exists, a secret where it doesn't need to be more |

The decisive rows are 3 and 5. Embedding fails them not on effort but
on kind: it adds the highest-blast-radius surface in the codebase to
serve the one topology where OAuth buys nothing, because the authorizer,
the client, and the resource are one person on one machine.

## 4. Recommendation

**Hybrid, point-at-first.**

1. **Do not embed an authorization server.** No `/authorize`, no token
   endpoint, no registration endpoint, no signing keys in mcpx. This is
   a decision, not a deferral: the revisit trigger is in §6.
2. **Point at external authorization servers, through #253.** When a
   deployment needs OAuth, mcpx (as resource server) publishes RFC 9728
   protected-resource metadata naming the external AS, and validates
   that AS's tokens. Comparing point-at options is in scope; adopting
   one as a fleet standard is not this document's call — Keycloak
   (self-hosted, heaviest, most capable), Auth0/Entra (managed, SSO/MFA
   included) are deployment choices made per install, and the doc work
   under #253 should give a tested recipe for at least one self-hosted
   and one managed option rather than blessing a product.
3. **The single-user low end is #338, not OAuth.** A loopback/unix
   daemon is protected by filesystem permissions today; a daemon
   listening on TCP gets #338's optional bearer token. That is the
   honest shape of single-user protection: one secret, held by the one
   user, revocable by changing it — with none of an AS's machinery
   pretending to be delegation.

## 5. What changes in #253's scope

Explicitly, as #345's acceptance requires:

- **Nothing is removed.** #253 remains mcpx as OAuth client to remote
  servers and as resource server on its own `/mcp`, with its
  conformance rows as filed.
- **One clarification is added.** #253's resource-server half is where
  "point at an external AS" lands: RFC 9728 metadata listing external
  authorization servers, and token validation against their issuers.
  #253 does **not** grow an authorization-server half; anyone reading
  its authorization rows as "mcpx will issue tokens" is reading them
  wrong, and this document is the citation for that.
- **The conformance `authorization` command stays unrun, on purpose.**
  `authorization-server-metadata-endpoint` and
  `authorization-code-grant` test authorization servers; mcpx is not
  one (decision 1), so there is nothing to run them against. The
  conformance-matrix authorization rows remain attributed to #253 in
  its client/resource-server capacity. Because the recommendation is
  not "embed", no follow-up implementation issue for an AS is filed.

## 6. Revisit trigger

Reopen this question only when a concrete caller exists that the hybrid
cannot serve — specifically: multiple distinct clients, owned by
different people, needing delegated, revocable access to one shared
mcpx daemon *without* any external IdP being acceptable in that
deployment. If that day comes, the embedded design to evaluate first is
the narrow one: loopback-only, a single pre-registered client, CIMD for
any additional clients, authorization-code + PKCE only, no DCR. Until
then, an embedded AS is conformance cosmetics at credential-forgery
prices.

No question in this decision needs Drew: the one genuinely owner-shaped
choice — adopting a specific external IdP as a fleet standard — is
explicitly *not* made here, so nothing is escalated.
