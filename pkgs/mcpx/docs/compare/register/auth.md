# Authorization

```
created:      2026-09-30T08:00:00-05:00
last-updated: 2026-09-30T08:00:00-05:00
increment:    1
status:       standard
tags:         area:compare
description:  Every authorization mechanism per MCP revision, what mcpx, opencode, lootbox and Cloudflare do with it.
```

2024-11-05 has no authorization; 2025-03-26 adds an optional OAuth 2.1 profile for HTTP, and every revision since
reshapes it (resource server and RFC 9728/8707 in 2025-06-18; OIDC discovery, Client ID Metadata Documents and step-up
scopes in 2025-11-25; RFC 9207 `iss`, `application_type`, issuer-keyed credentials and deprecated DCR in 2026-07-28).
For mcpx two facts dominate: as a client it performs none of the OAuth flow and does not even apply its own static
`auth:` block, so the only credential that reaches an upstream is a raw `headers:` map or an environment variable; as a
server it authenticates nothing, on `/mcp` or on the `/v1` endpoint that runs scripts.

Status column: **mcpx @ 05c78b2**. Legend and citation conventions: [../README.md](../README.md).

| ID | Difference | Labels | Where it exists | mcpx @ 05c78b2 | Value | Effort | Risk |
| --- | --- | --- | --- | --- | --- | --- | --- |
| AUTH-01 | No authorization in the core protocol | `2025-03-26 has` | `24-11 ✓ · 25-03 — · 25-06 — · 25-11 — · 26-07 —` | partial — `headers:`/`env:` work; `auth:` block inert | + low | S | low |
| AUTH-02 | OAuth 2.1 profile for HTTP; stdio takes environment credentials | `2025-03-26 has` `impl deferred` `opencode v1 has` `opencode v2 has` | `24-11 — · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v1 ✓ · v2 ✓` | ✗ — config shape only; flow deferred | + high | L | high |
| AUTH-03 | Access token as `Authorization: Bearer`, never in the query | `2025-03-26 has` | `24-11 — · 25-03 ✓ · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | partial — only via raw `headers:`; `auth.bearer` inert | + med | S | med |
| AUTH-04 | PKCE required; refuse AS without `code_challenge_methods_supported` | `2025-03-26 has` `2025-11-25 has` `impl deferred` `opencode v1 has` `opencode v2 has` | `25-03 ✓ PKCE · 25-06 ✓ · 25-11 ✓ + check · 26-07 ✓` | ✗ — deferred with the flow | + med | S | low |
| AUTH-05 | AS metadata at MCP base URL; default-endpoint fallback | `2025-03-26 has` `2025-06-18 removes` `has better replacement` `impl deferred` | `25-03 ✓ · 25-06 — · 25-11 — · 26-07 —` | ✗ — deferred | + low | S | low |
| AUTH-06 | MCP server is a resource server; RFC 9728 metadata | `2025-06-18 has` `impl deferred` | `25-03 — · 25-06 ✓ · 25-11 ✓ · 26-07 ✓` | ✗ — client discovery deferred; server has no auth | + high | M | med |
| AUTH-07 | Finding PRM: `WWW-Authenticate` header, then well-known URIs | `2025-06-18 has` `2025-11-25 has` `impl deferred` | `25-06 header MUST · 25-11 header or well-known · 26-07 same` | ✗ — deferred | + high | S | low |
| AUTH-08 | RFC 8707 `resource` parameter on authorization and token requests | `2025-06-18 has` `impl deferred` `opencode v2 has` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓ · opencode v2 ✓` | ✗ — `OAuth.Resource` field exists, unused | + med | S | med |
| AUTH-09 | Token audience validation; no token passthrough | `2025-06-18 has` | `25-06 ✓ · 25-11 ✓ · 26-07 ✓` | n/a — `/mcp` accepts no tokens to forward | + med | S | high |
| AUTH-10 | OIDC Discovery and ordered well-known probing | `2025-11-25 has` `impl deferred` | `25-11 ✓ · 26-07 ✓` | ✗ — deferred | + med | S | low |
| AUTH-11 | Dynamic Client Registration: SHOULD, then MAY, then deprecated | `2025-03-26 has` `2026-07-28 deprecates` `has better replacement` `impl deferred` `opencode v1 has` | `25-03 SHOULD · 25-06 SHOULD · 25-11 MAY · 26-07 dep · opencode v1 ✓` | ✗ — no DCR; config prefers CIMD | + low | M | low |
| AUTH-12 | `application_type` required in DCR | `2026-07-28 has` `impl deferred` | `26-07 ✓` | ✗ — deferred | + low | S | low |
| AUTH-13 | Client ID Metadata Documents as `client_id` | `2025-11-25 has` `2026-07-28 has` `impl deferred` `opencode v2 has` | `25-11 ✓ · 26-07 SHOULD · opencode v2 ✓` | ✗ — `clientId` comment prefers it; unused | + high | M | low |
| AUTH-14 | Scope selection and step-up on `insufficient_scope` | `2025-11-25 has` `impl deferred` | `25-11 ✓ · 26-07 ✓ (+ union SHOULDs)` | ✗ — `scopes` field exists, unused | + med | M | low |
| AUTH-15 | RFC 9207 `iss` check on the authorization response | `2026-07-28 has` `impl deferred` | `26-07 ✓` | ✗ — deferred | + med | S | low |
| AUTH-16 | Fetched metadata `issuer` must equal the issuer used | `2026-07-28 has` `impl deferred` | `26-07 ✓` | ✗ — deferred | + med | S | low |
| AUTH-17 | Credentials keyed by issuer; re-register on AS change | `2026-07-28 has` `impl deferred` `opencode v1 has` `opencode v2 has` | `26-07 ✓ · opencode v1 by name · v2 by name+url` | ✗ — no token store | + med | S | med |
| AUTH-18 | Refresh-token handling rules | `2026-07-28 has` `impl deferred` `opencode v1 has` | `26-07 ✓ · opencode v1 lists refresh_token grant` | ✗ — deferred | + med | S | low |
| AUTH-19 | `oauth-client-credentials` extension (machine-to-machine) | `2026-07-28 has` | `26-07 extension` | ✗ — not declared or performed | + med | M | low |
| AUTH-20 | `enterprise-managed-authorization` extension (IdP, ID-JAG) | `2026-07-28 has` `does not match mcpx's goal` | `26-07 extension` | n/a — out of scope for a local tool | − scope | L | low |
| AUTH-21 | OAuth redirect listener: fixed port vs ephemeral | `opencode v1 has` `opencode v2 has` `impl deferred` | `opencode v1 127.0.0.1:19876 · v2 configured or ephemeral` | ✗ — `redirect` field only | + med | S | low |
| AUTH-22 | Per-server `auth:` block parsed and never applied | `mcpx missing` | mcpx only | ✗ broken — `Auth.Resolve` has no caller (#205) | + high | S | high |
| AUTH-23 | `/mcp` and `/v1` authenticate nobody | `2024-11-05 has` `mcpx missing` | `24-11 SHOULD · 25-03 SHOULD · 25-06 SHOULD · 25-11 SHOULD · 26-07 SHOULD` | ✗ — loopback plus socket permissions only (#204) | + high | M | high |
| AUTH-24 | Code-execution endpoints accept cross-origin simple requests | `mcpx missing` | `mcpx /v1/exec · lootbox /ws` | ✗ — no Origin or Content-Type check (#204) | + high | S | high |
| AUTH-25 | Scripts see credentials: bindings vs inherited environment | `does not match mcpx's goal` | `Cloudflare bindings · opencode none · lootbox unreadable · mcpx full env` | n/a — scripts inherit full `os.Environ()` by design | − design | S | med |

## AUTH-01 No authorization in the core protocol

- **What.** 2024-11-05 says authentication and authorization are not part of the core specification; the parties may
  negotiate their own.
- **Where.** 2024-11-05 only. Every later revision has the OAuth profile (AUTH-02).
- **mcpx @ 05c78b2.** mcpx does not serve 2024-11-05, but as a client its only working credentials are exactly this
  "negotiate your own" route: a static `headers:` map for HTTP (`internal/pool/pool.go:379`) and an `env:` map plus the
  daemon's inherited environment for stdio (`internal/pool/pool.go:372`). The richer `auth:` declaration
  (bearer/basic/header/query/env/oauth, `internal/mcpauth/mcpauth.go:29-57`) is never applied (AUTH-22).
- **Value to mcpx.** + low: static credentials are what most servers need.
- **Effort.** S — nothing new; fix AUTH-22.
- **Risk.** None if not done.
- **Detail.** "Custom" authentication survives into 2026-07-28: clients and servers MAY still negotiate their own
  (`2026-07-28/basic/index.mdx:227`), so a static header is conformant in every revision, not a legacy crutch.
- **Sources.** `2024-11-05/basic/index.mdx:75` "Authentication and authorization are not currently part of the core MCP specification,"; `internal/mcpauth/mcpauth.go:29-57`; `internal/pool/pool.go:372` "Env:        p.cfg.Env,"

## AUTH-02 OAuth 2.1 profile for HTTP; stdio takes environment credentials

- **What.** Authorization is optional. When implemented, HTTP transports SHOULD follow the OAuth 2.1 profile; stdio
  transports SHOULD NOT and take credentials from the environment instead.
- **Where.** 2025-03-26 through 2026-07-28, framing unchanged
  (<https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization>). opencode v1 (PKCE + DCR) and v2
  (adds CIMD, RFC 8707, ephemeral callback) both perform the client flow.
- **mcpx @ 05c78b2.** The OAuth configuration shape exists (`clientId`, `scopes`, `resource`, `redirect`,
  `internal/mcpauth/mcpauth.go:59-77`); the flow does not. The type comment defers it knowingly
  (`internal/mcpauth/mcpauth.go:62-64`) and `docs/protocol.md` §6 lists it as still missing
  (`docs/protocol.md:378`). The stdio half is served: stdio children get `env:` with `InheritEnv: true`
  (`internal/pool/pool.go:369-375`).
- **Value to mcpx.** + high: GitHub, Linear and most hosted servers are unreachable without it, except through a
  wrapper such as `mcp-remote`.
- **Effort.** L — discovery, PKCE, a browser step, a callback listener, a token store, refresh.
- **Risk.** Not done: OAuth-protected servers cannot be pooled. Done: a daemon that holds tokens for many servers
  becomes a credential store and must key them correctly (AUTH-17).
- **Detail.** The comment claims a server requiring OAuth "reports that clearly rather than failing at the first
  request with a 401"; nothing calls the code that would report it (AUTH-22), so today it is exactly a bare 401.
  url-mode elicitation is the natural carrier for the browser step (a daemon has no browser of its own). The same
  HTTP/stdio split is restated in 2026-07-28 base protocol (`2026-07-28/basic/index.mdx:225`).
- **Sources.** `2025-03-26/basic/authorization.mdx:15` "Authorization is **OPTIONAL** for MCP implementations. When supported:"; `2025-03-26/basic/authorization.mdx:18` "- Implementations using an STDIO transport **SHOULD NOT** follow this specification, and"; `internal/mcpauth/mcpauth.go:63` "and is not yet implemented; a server requiring it reports that clearly"; `docs/protocol.md:378` "**OAuth for remote servers.** Declared and not performed, unchanged."; `v1:packages/opencode/src/mcp/oauth-provider.ts:11`; `v2:packages/core/src/mcp/oauth.ts:25`

## AUTH-03 Access token as `Authorization: Bearer`, never in the query

- **What.** The client MUST send the access token in the `Authorization: Bearer` header on every HTTP request, and
  MUST NOT put it in the URI query string.
- **Where.** 2025-03-26 through 2026-07-28.
- **mcpx @ 05c78b2.** A token reaches an upstream only if the user writes the header by hand in `headers:`, which the
  pool passes to the HTTP transport (`internal/pool/pool.go:379`). `auth: {type: bearer}` would build it, but is
  inert (AUTH-22).
- **Value to mcpx.** + med: this is the step every protected server needs, OAuth or not.
- **Effort.** S — call `Auth.Resolve` and merge its headers.
- **Risk.** Not done: users who follow the documented `auth.bearer` shape send no token.
- **Detail.** 2025-03-26 adds "even if they are part of the same logical session" to the every-request rule; 2026-07-28
  drops the clause because sessions are gone. mcpx's `auth` type `query` exists to put a static credential in the URL
  (`Resolved.Query`); that is legitimate for API keys but would violate the query-string MUST if it ever carried an
  OAuth access token.
- **Sources.** `2025-03-26/basic/authorization.mdx:270` "Authorization: Bearer <access-token>"; `2025-03-26/basic/authorization.mdx:276` "2. Access tokens **MUST NOT** be included in the URI query string"; `2026-07-28/basic/authorization/index.mdx:262` "1. MCP client **MUST** use the Authorization request header field defined in"; `internal/pool/pool.go:379` "Headers: p.cfg.Headers,"

## AUTH-04 PKCE required; refuse AS without `code_challenge_methods_supported`

- **What.** PKCE is required for every client from 2025-03-26. From 2025-11-25 the client MUST also check
  `code_challenge_methods_supported` in the AS or OIDC metadata and refuse to proceed if it is absent.
- **Where.** PKCE 2025-03-26 onward; the metadata check 2025-11-25 and 2026-07-28. opencode v1 and v2 use PKCE (v1
  "PKCE + DCR"; v2 adds CIMD on top).
- **mcpx @ 05c78b2.** Not implemented; part of the deferred flow (`internal/mcpauth/mcpauth.go:62-64`).
- **Value to mcpx.** + med: a guard of a few lines once the flow exists.
- **Effort.** S.
- **Risk.** None today.
- **Detail.** OIDC providers MUST include the field from 2025-11-25; an AS whose metadata omits it "does not support
  PKCE" by definition, so the refusal is not optional.
- **Sources.** `2025-03-26/basic/authorization.mdx:318` "2. PKCE is **REQUIRED** for all clients"; `2025-11-25/basic/authorization.mdx:605` "If `code_challenge_methods_supported` is absent, the authorization server does not support PKCE and MCP clients **MUST** refuse to proceed."; `v1:packages/opencode/src/mcp/oauth-provider.ts:11`

## AUTH-05 AS metadata at MCP base URL; default-endpoint fallback

- **What.** In 2025-03-26 the MCP server is also the authorization server: the client derives the authorization base URL
  by discarding the path of the MCP URL, reads RFC 8414 metadata there, and if none exists MUST fall back to
  `/authorize`, `/token` and `/register`.
- **Where.** 2025-03-26 only. 2025-06-18 separates the roles and replaces this with protected-resource metadata
  (AUTH-06).
- **mcpx @ 05c78b2.** Not implemented (deferred).
- **Value to mcpx.** + low: only 2025-03-26-era servers that never published metadata need it.
- **Effort.** S once the flow exists.
- **Risk.** None.
- **Detail.** 2025-03-26 also defines `MCP-Protocol-Version` here, as a SHOULD during metadata discovery, although
  2026-07-28 says revisions before 2025-06-18 did not define the header (the transports area records the timeline).
- **Sources.** `2025-03-26/basic/authorization.mdx:143` "The authorization base URL **MUST** be determined from the MCP server URL by discarding"; `2025-03-26/basic/authorization.mdx:158` "**MUST** use the following default endpoint paths relative to the [authorization base"; `2025-03-26/basic/authorization.mdx:135-139`

## AUTH-06 MCP server is a resource server; RFC 9728 metadata

- **What.** From 2025-06-18 an MCP server is an OAuth resource server. It MUST publish RFC 9728 Protected Resource
  Metadata naming its `authorization_servers`, and clients MUST use that document to find the AS.
- **Where.** 2025-06-18 through 2026-07-28.
- **mcpx @ 05c78b2.** As a client: deferred. As a server: `/mcp` has no authorization at all (AUTH-23), so publishing no
  metadata is consistent.
- **Value to mcpx.** + high: required to reach any modern protected server.
- **Effort.** M.
- **Risk.** None now.
- **Detail.** The role split is why AUTH-05's path-stripping disappears: the AS may live on another host entirely.
- **Sources.** `2025-06-18/basic/authorization.mdx:61` "1. MCP servers **MUST** implement OAuth 2.0 Protected Resource Metadata"; `2025-06-18/changelog.mdx:16` "Classify MCP servers as [OAuth Resource Servers]"

## AUTH-07 Finding PRM: `WWW-Authenticate` header, then well-known URIs

- **What.** 2025-06-18: servers MUST send `WWW-Authenticate` with `resource_metadata` on a 401. 2025-11-25 (SEP-985)
  relaxes the server side to "header or `/.well-known/oauth-protected-resource[/<path>]`", and makes clients support both:
  header first, then the path-inserted well-known URI, then the root one.
- **Where.** 2025-06-18 (header only), 2025-11-25 and 2026-07-28 (either).
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + high: without the fallback order, servers that publish only the well-known document look
  unprotected until the first 401.
- **Effort.** S within the flow.
- **Risk.** None now.
- **Detail.** The same header later carries `scope` and `error="insufficient_scope"` (AUTH-14).
- **Sources.** `2025-06-18/basic/authorization.mdx:87` "MCP servers **MUST** use the HTTP header `WWW-Authenticate` when returning a _401 Unauthorized_"; `2025-11-25/basic/authorization.mdx:101` "MCP clients **MUST** support both discovery mechanisms and use the resource metadata URL"

## AUTH-08 RFC 8707 `resource` parameter on authorization and token requests

- **What.** Clients MUST send `resource=<canonical MCP server URI>` in both the authorization and the token request,
  whether or not the AS understands it.
- **Where.** 2025-06-18 through 2026-07-28. opencode v2 checks that the resource named in protected-resource metadata
  covers the configured server URL, and falls back to the configured URL when the server publishes none.
- **mcpx @ 05c78b2.** The config field exists and its comment states the rule (`internal/mcpauth/mcpauth.go:72-74`);
  nothing sends it.
- **Value to mcpx.** + med: mandatory once OAuth exists.
- **Effort.** S within the flow.
- **Risk.** Not done: tokens without audience binding.
- **Detail.** Canonical URI: lowercase scheme and host, no trailing slash unless significant. opencode v2's comment
  explains the fallback: the SDK sends no `resource` when the server publishes no metadata, and some ASes require
  one. v2 also keeps the configured URL (without the `?codemode=false` it appends, see the plugin-APIs area) as the
  OAuth identity.
- **Sources.** `2025-06-18/basic/authorization.mdx:194` "MCP clients **MUST** implement Resource Indicators for OAuth 2.0 as defined in [RFC 8707]"; `internal/mcpauth/mcpauth.go:72-74`; `v2:packages/core/src/mcp/oauth.ts:141-146` "if (!checkResourceAllowed({ requestedResource: identity, configuredResource: resource }))"

## AUTH-09 Token audience validation; no token passthrough

- **What.** A server MUST validate that a token was issued for it and MUST NOT accept or relay any other token; it
  must not pass the client's token through to an upstream API.
- **Where.** 2025-06-18 through 2026-07-28.
- **mcpx @ 05c78b2.** n/a today: `/mcp` accepts no token (AUTH-23), so there is nothing to forward.
- **Value to mcpx.** + med: it fixes a rule for later. mcpx is a proxy; if `/mcp` ever takes bearer tokens, the agent's
  token must never become the upstream's `Authorization`.
- **Effort.** S.
- **Risk.** High if violated: token passthrough is the confused-deputy case the rule exists for.
- **Detail.** mcpx's upstream credentials come from its own config (`headers:`, future OAuth), which is already the
  shape the rule requires.
- **Sources.** `2025-06-18/basic/authorization.mdx:275` "MCP servers **MUST NOT** accept or transit any other tokens."

## AUTH-10 OIDC Discovery and ordered well-known probing

- **What.** From 2025-11-25 an AS MUST offer RFC 8414 or OpenID Connect Discovery, and clients MUST support both. For an
  issuer with a path they try `oauth-authorization-server/<path>`, `openid-configuration/<path>`, then
  `<path>/.well-known/openid-configuration`; without a path, the two root forms.
- **Where.** 2025-11-25 and 2026-07-28.
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + med: Google- and Entra-style issuers publish only OIDC documents.
- **Effort.** S.
- **Risk.** None now.
- **Detail.** 2026-07-28 adds an identity check on the fetched document (AUTH-16).
- **Sources.** `2025-11-25/basic/authorization.mdx:66` "5. MCP authorization servers **MUST** provide at least one of the following discovery mechanisms:"; `2025-11-25/basic/authorization.mdx:135` "For issuer URLs with path components (e.g., `https://auth.example.com/tenant1`), clients **MUST** try endpoints in the following priority order:"

## AUTH-11 Dynamic Client Registration: SHOULD, then MAY, then deprecated

- **What.** RFC 7591 registration weakens each revision: SHOULD (2025-03-26, 2025-06-18), MAY (2025-11-25), deprecated in
  2026-07-28 in favour of Client ID Metadata Documents, with removal no earlier than the first revision on or after
  2027-07-28.
- **Where.** 2025-03-26 through 2026-07-28. opencode v1 registers dynamically with `client_name: "OpenCode"`.
- **mcpx @ 05c78b2.** No DCR. The `clientId` comment already prefers a CIMD URL (`internal/mcpauth/mcpauth.go:66-68`).
- **Value to mcpx.** + low: some older ASes only do DCR; everything newer takes CIMD or a pre-registered id.
- **Effort.** M.
- **Risk.** None now.
- **Detail.** Registration priority in 2025-11-25 and 2026-07-28: pre-registered, then CIMD if the AS advertises
  `client_id_metadata_document_supported`, then DCR if it has a `registration_endpoint`. If mcpx ever does DCR it must
  also send `application_type` (AUTH-12).
- **Sources.** `2025-03-26/basic/authorization.mdx:42` "2. MCP auth implementations **SHOULD** support the OAuth 2.0 Dynamic Client Registration"; `2025-11-25/basic/authorization.mdx:60` "3. Authorization servers and MCP clients **MAY** support the OAuth 2.0 Dynamic Client Registration"; `2026-07-28/basic/authorization/client-registration.mdx:141` "Dynamic Client Registration is deprecated. New implementations should use"; `2026-07-28/deprecated.mdx:29` "| [Dynamic Client Registration]"; `v1:packages/opencode/src/mcp/oauth-provider.ts:46` "client_name: \"OpenCode\","

## AUTH-12 `application_type` required in DCR

- **What.** A client registering dynamically MUST send `application_type`: `"native"` for desktop, CLI and localhost
  redirects, `"web"` for remote browser apps.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + low: mcpx is `"native"`; DCR itself is deprecated.
- **Effort.** S.
- **Risk.** Not done: OIDC providers default to `"web"` and reject a localhost redirect URI.
- **Detail.** Clients MUST be prepared for registration failure and MAY retry with an adjusted type.
- **Sources.** `2026-07-28/basic/authorization/client-registration.mdx:160` "MCP clients **MUST** specify an appropriate `application_type`"

## AUTH-13 Client ID Metadata Documents as `client_id`

- **What.** The client's `client_id` is an HTTPS URL to a JSON document (`client_id`, `client_name`, `redirect_uris` at
  least) that the AS fetches; the AS advertises `client_id_metadata_document_supported: true`.
- **Where.** 2025-11-25 (one registration approach), 2026-07-28 (AS and clients SHOULD support). opencode v2 offers
  `https://opencode.ai/oauth/opencode/client.json`.
- **mcpx @ 05c78b2.** The config comment prefers a CIMD URL (`internal/mcpauth/mcpauth.go:66-68`); nothing uses it.
- **Value to mcpx.** + high: a daemon with no registration step, portable across ASes (no re-registration when the AS
  changes, AUTH-17). − someone has to host the document over HTTPS.
- **Effort.** M.
- **Risk.** None.
- **Detail.** 2026-07-28 security considerations: a CIMD cannot by itself stop `localhost` impersonation, so ASes SHOULD
  warn on localhost-only redirect URIs and MUST show the redirect hostname. A local daemon's redirect is exactly that
  case, so users will see the warning.
- **Sources.** `2026-07-28/basic/authorization/security-considerations.mdx:95` "Client ID Metadata Documents cannot prevent `localhost` URL impersonation by themselves."; `2025-11-25/basic/authorization.mdx:207` "2. Use Client ID Metadata Documents if the Authorization Server indicates if the server supports it"; `2026-07-28/basic/authorization/index.mdx:65` "2. Authorization servers and MCP clients **SHOULD** support [OAuth Client ID Metadata Documents]"; `v2:packages/core/src/mcp/oauth.ts:25` "export const CLIENT_METADATA_URL = \"https://opencode.ai/oauth/opencode/client.json\""

## AUTH-14 Scope selection and step-up on `insufficient_scope`

- **What.** Servers SHOULD put `scope` in the 401 `WWW-Authenticate`; clients prefer that, else all `scopes_supported`.
  At runtime a 403 with `error="insufficient_scope"` triggers step-up re-authorization.
- **Where.** 2025-11-25 and 2026-07-28. 2026-07-28 adds two SHOULDs: a challenge names every scope the operation needs,
  and the client requests the union with scopes already granted.
- **mcpx @ 05c78b2.** The `scopes` comment already says the challenge is authoritative
  (`internal/mcpauth/mcpauth.go:69-71`); nothing reads a challenge.
- **Value to mcpx.** + med: real remote servers (GitHub) scope per tool.
- **Effort.** M.
- **Risk.** None now.
- **Detail.** Clients MUST NOT assume any subset or superset relation between challenge scopes and `scopes_supported`.
  A step-up in the middle of a pooled session means re-authorizing a connection other callers share.
- **Sources.** `2025-11-25/basic/authorization.mdx:103` "MCP servers **SHOULD** include a `scope` parameter in the `WWW-Authenticate` header as defined in"; `2025-11-25/basic/authorization.mdx:541` "#### Step-Up Authorization Flow"; `internal/mcpauth/mcpauth.go:69-71`

## AUTH-15 RFC 9207 `iss` check on the authorization response

- **What.** ASes SHOULD return `iss` with the authorization code and advertise
  `authorization_response_iss_parameter_supported`. Clients MUST validate before redeeming the code: advertised and
  present, compare exactly; advertised and absent, reject; not advertised but present, compare anyway; neither,
  proceed.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + med: the mix-up-attack defence, which matters most to a client that talks to many ASes, as a
  pooling daemon does.
- **Effort.** S.
- **Risk.** None now.
- **Detail.** Plain string comparison: no case folding, default-port or trailing-slash normalisation. Expected to become
  MUST for ASes later.
- **Sources.** `2026-07-28/basic/authorization/index.mdx:196` "On receiving the authorization response, MCP clients **MUST** apply the validation in [RFC9207 Section 2.4]"

## AUTH-16 Fetched metadata `issuer` must equal the issuer used

- **What.** The `issuer` inside a fetched AS or OIDC metadata document MUST be identical to the issuer identifier the
  client used to build the well-known URL.
- **Where.** 2026-07-28.
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + med: prevents one AS impersonating another through a crafted metadata document.
- **Effort.** S.
- **Risk.** None now.
- **Detail.** Separate from AUTH-15 (which checks the authorization response); both are string-exact.
- **Sources.** `2026-07-28/basic/authorization/authorization-server-discovery.mdx:94` "the `issuer` value in the document **MUST** be identical to the issuer identifier used to construct the well-known URL"

## AUTH-17 Credentials keyed by issuer; re-register on AS change

- **What.** Pre-registered or DCR-obtained client credentials MUST be keyed by the AS's `issuer`, MUST NOT be reused
  with another AS, and the client MUST re-register when protected-resource metadata points somewhere new. CIMD ids are
  portable.
- **Where.** 2026-07-28. opencode keys differently: v1 stores tokens and client info per server name in
  `<data>/mcp-auth.json` (mode 0600); v2 stores a global Integration credential keyed by `sha1(name + url)`.
- **mcpx @ 05c78b2.** No token store exists.
- **Value to mcpx.** + med: it fixes the key of mcpx's future token store as `(issuer, client_id)`, not the server name.
- **Effort.** S.
- **Risk.** Not done: credential confusion when a server moves AS; opencode's name- or url-keyed stores would reuse a
  client id across ASes.
- **Detail.** A pre-registered id that does not match the new AS SHOULD surface an error rather than be tried. opencode
  v2's comment explains why it keys on name + url: two configs of one URL may hold different accounts. That concern and
  the issuer rule are compatible: key on issuer, then on account.
- **Sources.** `2026-07-28/basic/authorization/client-registration.mdx:186` "keyed by the authorization server's `issuer` identifier. When the"; `2026-07-28/basic/authorization/client-registration.mdx:190` "from a different authorization server and **MUST** re-register"; `v1:packages/opencode/src/mcp/auth.ts:37` "const filepath = path.join(Global.Path.data, \"mcp-auth.json\")"; `v2:packages/core/src/mcp/index.ts:165-167`

## AUTH-18 Refresh-token handling rules

- **What.** Clients MUST keep refresh tokens confidential, SHOULD list `refresh_token` in `grant_types`, MAY request
  `offline_access` when the AS lists it, and MUST NOT assume one is issued; servers SHOULD NOT put `offline_access` in
  challenges or `scopes_supported`.
- **Where.** 2026-07-28 (new section). opencode v1 already registers with
  `grant_types: ["authorization_code", "refresh_token"]`.
- **mcpx @ 05c78b2.** Deferred.
- **Value to mcpx.** + med: a long-lived daemon lives on refresh tokens.
- **Effort.** S within the flow.
- **Risk.** Not done: users re-authorize whenever an access token expires.
- **Detail.** "Confidential in storage" rules out a world-readable token file; mcpx already restricts its socket with
  `defaults.PrivateMode` (`internal/daemon/server.go:202`), and a token file would need the same.
- **Sources.** `2026-07-28/basic/authorization/index.mdx:299` "## Refresh Tokens"; `v1:packages/opencode/src/mcp/oauth-provider.ts:48` "grant_types: [\"authorization_code\", \"refresh_token\"],"

## AUTH-19 `oauth-client-credentials` extension (machine-to-machine)

- **What.** An official extension, `io.modelcontextprotocol/oauth-client-credentials`, adds the OAuth client-credentials
  grant (JWT assertion, or `client_id` + `client_secret`) so automated systems connect without a user present. Clients
  declare it in `extensions` of the per-request `clientCapabilities`.
- **Where.** 2026-07-28 extension (`docs/extensions/auth`). Never active by default.
- **mcpx @ 05c78b2.** Not declared, not performed.
- **Value to mcpx.** + med: the extension's own table lists "background service or daemon" and "CI/CD pipeline" as its
  users; that is mcpx.
- **Effort.** M.
- **Risk.** None.
- **Detail.** Declaring it is per request, in `_meta`, like every 2026 capability; a legacy session has no place to
  declare it.
- **Sources.** `docs/extensions/auth/oauth-client-credentials.mdx:6` "The OAuth Client Credentials extension (`io.modelcontextprotocol/oauth-client-credentials`)"; `docs/extensions/auth/overview.mdx:54` "| Background service or daemon accessing an MCP server | [OAuth Client Credentials]"; `docs/extensions/auth/overview.mdx:63` "they are never active by default."; `docs/extensions/auth/overview.mdx:67`

## AUTH-20 `enterprise-managed-authorization` extension (IdP, ID-JAG)

- **What.** `io.modelcontextprotocol/enterprise-managed-authorization` lets an organisation's IdP govern MCP access: the
  client exchanges an ID token for an Identity Assertion JWT Authorization Grant (ID-JAG) and trades that for an access
  token at the MCP server's AS.
- **Where.** 2026-07-28 extension.
- **mcpx @ 05c78b2.** n/a.
- **Value to mcpx.** − an enterprise IdP flow is out of scope for a local single-user daemon.
- **Effort.** L.
- **Risk.** None.
- **Detail.** Like AUTH-19, never active by default.
- **Sources.** `docs/extensions/auth/enterprise-managed-authorization.mdx:6` "The Enterprise-Managed Authorization extension (`io.modelcontextprotocol/enterprise-managed-authorization`)"; `docs/extensions/auth/enterprise-managed-authorization.mdx:42`

## AUTH-21 OAuth redirect listener: fixed port vs ephemeral

- **What.** Where the authorization code comes back. opencode v1 listens on a fixed
  `http://127.0.0.1:19876/mcp/oauth/callback` (overridable by `callbackPort`/`redirectUri`); v2 listens on
  `callback_port`, else the configured redirect's port, else an ephemeral port on 127.0.0.1.
- **Where.** opencode v1 and v2 (the spec prescribes no port).
- **mcpx @ 05c78b2.** A `redirect` config field exists (`internal/mcpauth/mcpauth.go:75-76`); the comment names "a
  callback listener" as the missing piece (`internal/mcpauth/mcpauth.go:62-63`).
- **Value to mcpx.** + med: a daemon can host the listener itself on its existing loopback server.
- **Effort.** S.
- **Risk.** A fixed port collides when two clients authorize at once; an ephemeral port conflicts with ASes that
  require exact redirect URIs.
- **Detail.** Config spelling differs: v1 camelCase (`callbackPort`, `redirectUri`), v2 snake_case (`callback_port`,
  `redirect_uri`, `client_id`).
- **Sources.** `v1:packages/opencode/src/mcp/oauth-provider.ts:11` "const OAUTH_CALLBACK_PORT = 19876"; `v1:packages/opencode/src/mcp/oauth-provider.ts:12` "const OAUTH_CALLBACK_PATH = \"/mcp/oauth/callback\""; `v2:packages/core/src/mcp/oauth.ts:353` "server.listen(oauth?.callback_port ?? redirectPort ?? 0, \"127.0.0.1\", () => {"; `v2:packages/schema/src/mcp.ts:42`

## AUTH-22 Per-server `auth:` block parsed and never applied

- **What.** A server config's `auth: {type: bearer|basic|header|query|env|oauth, …}` does nothing. `Auth.Resolve()`,
  which would build headers, query parameters and environment, and `Auth.Describe()` have no non-test caller; only the
  plain `headers:` map reaches the HTTP transport.
- **Where.** mcpx configuration only.
- **mcpx @ 05c78b2.** `Auth` field at `internal/config/config.go:138-141`; `Resolve` at
  `internal/mcpauth/mcpauth.go:93-184`; `internal/mcpauth` is imported only by `internal/config` for the type; the pool
  passes `p.cfg.Headers` (`internal/pool/pool.go:377-380`).
- **Value to mcpx.** + high: `${GITHUB_TOKEN}` bearer auth then works as documented.
- **Effort.** S.
- **Risk.** High if not done: a user who configures `auth.bearer` sends unauthenticated requests and gets the 401 the
  code comment promises will never happen.
- **Detail.** `docs/protocol.md:378` understates it: it says only OAuth is declared and not performed, but every auth
  type is inert. `Describe` claims to be "used by `mcpx config` and `mcpx doctor`" (`internal/mcpauth/mcpauth.go:186-189`);
  neither calls it. The `query` type also needs the URL rewritten (`Resolved.Query`), and `NeedsOAuth`/`Missing` exist
  so failures could be reported before a request (`internal/mcpauth/mcpauth.go:84-89`).
- **Sources.** `internal/mcpauth/mcpauth.go:93` "func (a *Auth) Resolve() (*Resolved, error) {"; `internal/pool/pool.go:379` "Headers: p.cfg.Headers,"; `internal/config/config.go:138-141`

## AUTH-23 `/mcp` and `/v1` authenticate nobody

- **What.** Whatever can reach the daemon's listener can list and call every tool and run scripts. The default bind is
  loopback, and a warning is printed when binding elsewhere.
- **Where.** Every revision says servers SHOULD authenticate all connections (the SSE transport in 2024-11-05, Streamable
  HTTP after).
- **mcpx @ 05c78b2.** No auth middleware; the only wrapper is `trackActivity` (`internal/daemon/server.go:290-293`,
  `344-353`), which records time and re-checks config. `internal/cli/root.go:196-204` prints the warning. The unix socket
  relies on filesystem permissions; TCP gets nothing.
- **Value to mcpx.** + high: `--address 0.0.0.0` is documented (`docs/protocol.md:352-355`), and even loopback is
  reachable from a browser because `Origin` is not checked (transports area; AUTH-24).
- **Effort.** M for a bearer token; L for a full OAuth resource server (AUTH-06).
- **Risk.** High: remote code execution for anything that reaches the port.
- **Detail.** If mcpx adds a 401 to `/mcp`, opencode v1 will read it as "needs OAuth": it moves the server to
  `needs_auth` and toasts "Run: opencode mcp auth <name>", partly by matching "OAuth"/"registration"/"client_id" in error
  text. Users would need `oauth: false` and a static header in opencode's config.
- **Sources.** `2024-11-05/basic/transports.mdx:57` "3. Servers **SHOULD** implement proper authentication for all connections"; `2026-07-28/basic/transports/streamable-http.mdx:65` "Servers **SHOULD** implement proper authentication"; `internal/cli/root.go:202` "the API is unauthenticated"; `v1:packages/opencode/src/mcp/index.ts:317`

## AUTH-24 Code-execution endpoints accept cross-origin simple requests

- **What.** The endpoints that run code have no authentication and do not require a JSON content type, so a web page
  can trigger them. mcpx's `POST /v1/exec` decodes JSON from the body whatever its `Content-Type`, so a cross-origin
  `text/plain` "simple" request (no CORS preflight) runs a script with full authority; the page cannot read the answer
  but the side effect happens. lootbox's `/ws` accepts a WebSocket upgrade on the fixed port 9420 with no `Origin`
  check: cross-site WebSocket hijacking, and it can read the answer.
- **Where.** mcpx and lootbox. opencode and Cloudflare expose no such endpoint.
- **mcpx @ 05c78b2.** `handleExec` calls `json.NewDecoder(r.Body).Decode(&req)` (`internal/daemon/routes_exec.go:212-213`);
  no Origin or Host check in `internal/daemon` or `internal/mcpserver`; the API is unauthenticated by design
  (`internal/daemon/server.go:206-208`). `daemon.port` defaults to 0 (ephemeral), so an attacker must find the port first
  (`internal/settings/registry.go:343`).
- **Value to mcpx.** + high: closes a drive-by remote-code-execution path on the user's workstation.
- **Effort.** S — reject a present non-loopback `Origin` with 403, require `Content-Type: application/json` on POST,
  optionally check `Host`. Unix-socket and CLI callers send no `Origin` and are unaffected.
- **Risk.** High if not done. Done: a browser UI on another origin needs an allowlist.
- **Detail.** The `Origin` MUST itself (every revision, 403 explicit since 2025-11-25) is recorded in the transports area;
  this row is the consequence for an unauthenticated server that executes code. lootbox's own dashboard hard-codes
  `ws://<host>:3000/ws` (`.lootbox/ui/src/lib/websocket-client.ts:34`), so it cannot even reach the :9420 daemon.
- **Sources.** `internal/daemon/routes_exec.go:212-213`; `internal/daemon/server.go:206-208`; `.lootbox/src/lib/rpc/websocket_server.ts:282-290`; `2025-11-25/basic/transports.mdx:78` "Servers **MUST** validate the `Origin` header on all incoming connections to prevent DNS rebinding attacks"

## AUTH-25 Scripts see credentials: bindings vs inherited environment

- **What.** Cloudflare's sandbox never holds a credential: connectors are already-authorised RPC bindings, and a
  search-and-execute server hands the script a `request()` function, not the token. mcpx scripts inherit the whole
  environment of whoever runs them, and for `/v1/exec` that is the daemon's environment.
- **Where.** Cloudflare (bindings); opencode (interpreter, no environment access); lootbox (no `--allow-env` by default,
  so the environment exists but cannot be read); mcpx (full `os.Environ()`).
- **mcpx @ 05c78b2.** `cmd.Env = append(os.Environ(), "MCPX_RUNTIME="+rt.Name)` (`internal/runner/runner.go:359`); a
  daemon-run script is a child of the daemon (`internal/daemon/routes_exec.go:182-200`).
- **Value to mcpx.** − full authority is the design (scripts need the filesystem and processes). + one sentence in
  `docs/exec.md` is owed: a `/v1/exec` caller can read every secret in the daemon's environment.
- **Effort.** S — document it, or scrub a configurable denylist from the runner's environment.
- **Risk.** Med: a daemon started with `GITHUB_TOKEN` and bound beyond loopback exposes the token to any caller
  (compounded by AUTH-23 and AUTH-24).
- **Detail.** Cloudflare's docs repeat the blog's "cannot leak keys" claim more cautiously ("never receives the connector
  credentials or client objects"), so it is documented behaviour, not marketing only.
- **Sources.** `internal/runner/runner.go:359`; <https://developers.cloudflare.com/agents/tools/codemode/how-it-works/#connectors> "The generated code never receives the connector credentials or client objects."; <https://developers.cloudflare.com/agents/model-context-protocol/codemode/#sandbox-and-authorization-boundary> "Authentication stays in the host request callback."
