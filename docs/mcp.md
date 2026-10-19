# MCP servers

Every MCP server on this machine runs under mcpx, the gateway daemon described in
[`services.md`](services.md). This page covers how the servers are wired, how they
authenticate, and an inventory of each one. The inventory states facts as of the
date in its heading; re-run its checks before relying on them.

## How it is wired

- **One list.** `mcpx.config.nix` defines every server and how mcpx pools it.
  `homeUser.nix` (`home.activation.mcpxConfig`) writes it to
  `~/.config/mcpx/config.json` as a real file. See `services.md` for why it is not a
  home-manager link.
- **Absolute commands.** Every stdio server is started by its
  `/run/current-system/sw/bin/...` path. A daemon inherits the PATH of whatever
  started it, and a bare name could fail with "executable file not found" under
  another PATH.
- **npm-only servers.** `mcp-remote` and `chrome-devtools-mcp` are built from a
  committed lockfile (`pkgs/npm-mcp-servers`, `importNpmLock`; versions 0.14.2 and
  1.9.0, pinned in its `package.json`). No package in the lockfile has an install
  script, and `npm rebuild` runs with `--ignore-scripts`. To update, change
  `package.json`, run `npm install --package-lock-only --ignore-scripts` there, and
  rebuild.
- **Pooling** (`mcpx` block per server):
  - `chrome-devtools` and `playwright` get one process per agent session
    (`sharing = "exclusive"`, `scope = "session"`); browser state must not be
    shared.
  - `aws` gets `startTimeout = "180s"`, because `uvx` downloads the proxy on its
    first start.
  - `github` hides `create_repository`, `fork_repository` and `delete_repository`
    (`excludeTools`).
- **Only the launchd agent starts a daemon** (`daemon.autostart = false`). See
  `services.md`.
- **Smoke test.** `just mcpx-check` lists every server's state and calls one tool
  each on codedb, fff, chrome-devtools and context7.

## OAuth for the remote servers

atlassian, context7 and datadog run through `mcp-remote`, which does the OAuth
sign-in in a browser and stores tokens itself:

- **Where tokens live:** `~/.mcp-auth/mcp-remote-v1/`, in files named after an MD5
  hash of the server URL (`<hash>_tokens.json`, `_client_info.json` and so on).
  `v1` is mcp-remote's `CONFIG_STORE_VERSION`. `MCP_REMOTE_CONFIG_DIR` moves the
  base directory.
- **The URL is the identity.** Changing a server's URL, including its query
  string (Datadog's `toolsets` and `omit_tools`), needs a new sign-in.
- **Signing in:** `just mcp-login <name>`, and press Ctrl-C once it reports it is
  connected. Context7's sign-in is optional and only raises its rate limit
  (`just context7-login`).
- **Old tokens:** token files for two earlier Datadog URLs are still in that
  directory. They are expired and unused.

github uses `gh`'s token: the `github-mcp` wrapper in `systemPackages.nix` reads
`gh auth token` from the login keychain at start, so no token is stored in the
repo or the nix store. aws signs requests (SigV4) with the default AWS profile;
set it up with `aws configure sso`, then `aws sso login`.

## Inventory (2026-10-19)

Tool counts are from `mcpx --json ls` and the aws error from mcpx itself, both
run 2026-10-19 at commit `e509884`. Credential details come from a read-only audit
that day: token file names and expiry times, `gh auth status`, and `aws sts
get-caller-identity`. No secret values were read for this page.

| Server | What it does | Transport | Auth | Credential (2026-10-19) | Tools | Notable risks |
|---|---|---|---|---|---|---|
| atlassian | Jira, Confluence, Loom | `mcp-remote` → `mcp.atlassian.com/v2/mcp?tools=all` | OAuth | Present; the access token had expired, the refresh token is present and refreshes on use | 190 | About 57 tools create, update, transition or archive; scopes include `write:jira`, `write:confluence` |
| aws | AWS APIs | `uvx mcp-proxy-for-aws-cli@1.7.0` → `aws-mcp.us-east-1.api.aws/mcp` | SigV4, default AWS profile | **None**: no `~/.aws` config or credentials, `aws sts get-caller-identity` exits 253 (NoCredentials) | 0 | Fails on every start: "No AWS credentials available" |
| aws-knowledge | AWS documentation | HTTP `knowledge-mcp.global.api.aws` | None | — | 5 | — |
| chrome-devtools | Headless Chrome, one per session | stdio `chrome-devtools-mcp` | None | — | 27 | `evaluate_script`, `upload_file` |
| codebase-memory | Code call graph and impact | stdio `codebase-memory-mcp` | None | — | 17 | `delete_project` (local index only) |
| codedb | Code search (symbols, callers) | stdio `codedb mcp` | None | — | 5 | — |
| context7 | Library documentation | `mcp-remote` → `mcp.context7.com/mcp` | OAuth, optional | Not signed in; runs anonymously at a lower rate limit | 2 | — |
| datadog | Logs, metrics, traces, monitors, dashboards, notebooks, workflows (US1) | `mcp-remote` → `mcp.datadoghq.com/v1/mcp?toolsets=core,workflows,alerting&omit_tools=delete_datadog_workflow` | OAuth | Present, refreshed that day | 63 | `execute_datadog_workflow` (does whatever the workflow does), `update_datadog_monitor`, `upsert_datadog_dashboard`, `delete_datadog_notebook`; the token carries 173 scopes, including `api_keys_delete` |
| deepwiki | Wikis for public GitHub repos | HTTP `mcp.deepwiki.com/mcp` | None | — | 3 | — |
| fff | File and content search over `~/git` | stdio `fff-mcp` | None | — | 3 | — |
| fff-nix | Same, over this repo | stdio `fff-mcp` | None | — | 3 | — |
| fff-worktree | Same, over `~/.local/share/opencode/worktree` | stdio `fff-mcp` | None | — | 3 | — |
| github | GitHub | stdio `github-mcp` → `github-mcp-server stdio` | `gh` token (account dezren39; scopes `repo`, `workflow`, `gist`, `read:org`, `admin:public_key`, `admin:ssh_signing_key`) | Present | 43 | `push_files`, `delete_file`, `merge_pull_request`, issue and PR writes (#371). With `workflow` scope a push can change Actions workflows |
| markitdown | Converts a URL or file to Markdown | stdio `markitdown-mcp` | None | — | 1 | Can read local `file:` URIs |
| nixos | Nix packages, options, NixOS wiki | stdio `mcp-nixos` | None | — | 2 | — |
| playwright | Headless browser, one per session | stdio `playwright-mcp` | None | — | 25 | `browser_run_code_unsafe`, `browser_file_upload` |
| sequential-thinking | Step-by-step reasoning scratchpad | stdio `mcp-server-sequential-thinking` | None | — | 1 | — |
| terraform | Public Terraform registry lookups | stdio `terraform-mcp-server stdio --toolsets=registry` | None | — | 9 | — |

mcpx's `elicit.confirmDestructive` is off, so destructive tools run without a
confirmation prompt.

## Other MCP configurations on this machine (2026-10-19)

- **Copilot CLI** (`~/.copilot/mcp-config.json`, installed via Homebrew): codedb and
  deepwiki. GitHub's documentation says Copilot CLI also includes a GitHub MCP
  server by default; that was not checked here.
- **Claude Code** (`~/.claude.json`) and **Codex** (`~/.codex/config.toml`):
  codedb and deepwiki. Neither CLI is installed, so these are unused.
- These three configs run `~/bin/codedb` (0.2.5860), a copy outside nix that comes
  before nix's codedb (0.2.5855) on PATH.
- **Zed:** the GitHub, Context7 and Postgres extensions are installed. GitHub signs
  in through the browser and keeps its token in memory; Postgres has no
  `database_url`.
- **OpenCode and OpenChamber** have no MCP servers of their own; every
  `opencode.json(c)` found has no or an empty `mcp` key.

## Tool namespaces that are not MCP servers

Agents in OpenCode also see these plugins:

- **`github`:** built into the OpenCode fork; uses the `gh` token. It lists 45 tools
  and hides 33 administrative ones, such as `secret_set`, which the model can turn
  on itself. Its `api` tool accepts any HTTP method. Write actions are allowed by
  default (#371).
- **`opencode` and `mcpx_*`:** built in. The `mcpx_*` tools talk to the mcpx daemon.
- **`tokenscope`:** a file plugin.
- **`openchamber*`:** four tools, added through OpenChamber's
  `opencode.managed.json`.
