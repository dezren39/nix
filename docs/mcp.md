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
  - `github` hides `create_repository`, `fork_repository`, `delete_repository`,
    `push_files`, `create_or_update_file`, `delete_file` and `merge_pull_request`;
    `datadog` hides `delete_*`, `execute_code`, `execute_datadog_workflow` and the
    tools that change live monitors, dashboards and workflows (`excludeTools`,
    dezren39/nix#664). Issue and PR tools stay.
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
repo or the nix store. aws signs requests (SigV4) using `~/.aws/config` profiles
(with `--skip-auth` so its 8 tools load even when logged out, and `--profile`
populated dynamically from `aws configure list-profiles` so callers can switch
accounts per call via the `aws_profile` tool parameter).

## Inventory (2026-10-20)

Tool counts are from `mcpx ls` on 2026-10-20. Credential details come from token
file metadata, `gh auth status`, and `aws configure list-profiles`. No secret
values were read for this page.

| Server              | What it does                                                            | Transport                                                                                                                           | Auth                                                                                                                      | Credential (2026-10-20)                                                                                                                                       | Tools | Notable risks                                                                                                                                                                                            |
| ------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| atlassian           | Jira, Confluence, Loom                                                  | `mcp-remote` → `mcp.atlassian.com/v2/mcp?tools=all` (`--static-oauth-client-metadata` pinning 20 read/search scopes)                | OAuth                                                                                                                     | Present; access token refreshed (8h TTL), 90-day refresh token (`exp` 2027-01-18); scope pinned so `.well-known` changes or timeouts never wipe `tokens.json` | 108   | Read/search only (pinning the 20 granted scopes hides the 82 ungranted write/delete tools so calls never trigger 403 step-up browser re-auth)                                                            |
| aws                 | AWS APIs & docs                                                         | `uvx mcp-proxy-for-aws-cli@1.7.0` → `aws-mcp.us-east-1.api.aws/mcp` (`--skip-auth`, `--profile` from `aws configure list-profiles`) | SigV4, per-call `aws_profile` switching                                                                                   | `~/.aws/config` role-switch profiles (`source_profile = mfa`); sign in via `aws login` / `aws-login-auto`                                                     | 8     | `aws___run_script` can execute AWS API calls in whichever profile is passed in `aws_profile` (use `-read-only` profiles for read-only work)                                                              |
| aws-knowledge       | AWS documentation                                                       | HTTP `knowledge-mcp.global.api.aws`                                                                                                 | None                                                                                                                      | —                                                                                                                                                             | 5     | —                                                                                                                                                                                                        |
| chrome-devtools     | Headless Chrome, one per session                                        | stdio `chrome-devtools-mcp`                                                                                                         | None                                                                                                                      | —                                                                                                                                                             | 27    | `evaluate_script`, `upload_file`                                                                                                                                                                         |
| codebase-memory     | Code call graph and impact                                              | stdio `codebase-memory-mcp`                                                                                                         | None                                                                                                                      | —                                                                                                                                                             | 17    | `delete_project` (local index only)                                                                                                                                                                      |
| codedb              | Code search (symbols, callers)                                          | stdio `codedb mcp`                                                                                                                  | None                                                                                                                      | —                                                                                                                                                             | 5     | —                                                                                                                                                                                                        |
| context7            | Library documentation                                                   | `mcp-remote` → `mcp.context7.com/mcp`                                                                                               | OAuth, optional                                                                                                           | Not signed in; runs anonymously at a lower rate limit                                                                                                         | 2     | —                                                                                                                                                                                                        |
| datadog             | Logs, metrics, traces, monitors, dashboards, notebooks, workflows (US1) | `mcp-remote` → `mcp.datadoghq.com/v1/mcp?toolsets=core,workflows,alerting&omit_tools=delete_datadog_workflow`                       | OAuth                                                                                                                     | Present, refreshed that day                                                                                                                                   | 63    | `execute_datadog_workflow` (does whatever the workflow does), `update_datadog_monitor`, `upsert_datadog_dashboard`, `delete_datadog_notebook`; the token carries 173 scopes, including `api_keys_delete` |
| deepwiki            | Wikis for public GitHub repos                                           | HTTP `mcp.deepwiki.com/mcp`                                                                                                         | None                                                                                                                      | —                                                                                                                                                             | 3     | —                                                                                                                                                                                                        |
| fff                 | File and content search over `~/git`                                    | stdio `fff-mcp`                                                                                                                     | None                                                                                                                      | —                                                                                                                                                             | 3     | —                                                                                                                                                                                                        |
| fff-nix             | Same, over this repo                                                    | stdio `fff-mcp`                                                                                                                     | None                                                                                                                      | —                                                                                                                                                             | 3     | —                                                                                                                                                                                                        |
| fff-worktree        | Same, over `~/.local/share/opencode/worktree`                           | stdio `fff-mcp`                                                                                                                     | None                                                                                                                      | —                                                                                                                                                             | 3     | —                                                                                                                                                                                                        |
| github              | GitHub                                                                  | stdio `github-mcp` → `github-mcp-server stdio`                                                                                      | `gh` token (account dezren39; scopes `repo`, `workflow`, `gist`, `read:org`, `admin:public_key`, `admin:ssh_signing_key`) | Present                                                                                                                                                       | 43    | `push_files`, `delete_file`, `merge_pull_request`, issue and PR writes (#371). With `workflow` scope a push can change Actions workflows                                                                 |
| markitdown          | Converts a URL or file to Markdown                                      | stdio `markitdown-mcp`                                                                                                              | None                                                                                                                      | —                                                                                                                                                             | 1     | Can read local `file:` URIs                                                                                                                                                                              |
| nixos               | Nix packages, options, NixOS wiki                                       | stdio `mcp-nixos`                                                                                                                   | None                                                                                                                      | —                                                                                                                                                             | 2     | —                                                                                                                                                                                                        |
| playwright          | Headless browser, one per session                                       | stdio `playwright-mcp`                                                                                                              | None                                                                                                                      | —                                                                                                                                                             | 25    | `browser_run_code_unsafe`, `browser_file_upload`                                                                                                                                                         |
| sequential-thinking | Step-by-step reasoning scratchpad                                       | stdio `mcp-server-sequential-thinking`                                                                                              | None                                                                                                                      | —                                                                                                                                                             | 1     | —                                                                                                                                                                                                        |
| terraform           | Public Terraform registry lookups                                       | stdio `terraform-mcp-server stdio --toolsets=registry`                                                                              | None                                                                                                                      | —                                                                                                                                                             | 9     | —                                                                                                                                                                                                        |

mcpx's `elicit.confirmDestructive` stays off: a caller that cannot answer the
question, which is every script, waits out the deadline and the call then
proceeds, so it would only add a delay. The dangerous tools are hidden instead
(above). The table's last column was written before that: the GitHub and
Datadog tools it names are now hidden, and Datadog lists 54 tools.

## Other MCP clients: mcpx only (2026-10-22)

Every other MCP client gets one MCP server, mcpx, as
`/run/current-system/sw/bin/mcpx --config ~/.config/mcpx/config.json serve`
(stdio). `mcp-clients.py`, run by home-manager on every switch
(`home.activation.mcpClients`), sets it and changes nothing else in each file.
The first time it changes a file, it keeps a copy in
`~/.local/state/mcp-clients/backup/`. The `--config` matters: without it mcpx
searches upward from the client's working directory, a repository's
`.mcp.json` changes which daemon it looks for, and with autostart off none is
there.

A client sees mcpx's gateway, not the 18 servers' tools by name: about 80
`mcpx_*` tools (`mcpx_search`, `mcpx_types`, `mcpx_call`, `mcpx_exec`, ...).
`mcpx_call {namespace, tool, arguments}` reaches any server; namespaces use `_`
for `-` (`chrome_devtools`). The daemon also serves MCP over HTTP at `/mcp` on
its loopback port, but that port is chosen at random on each start, so the
clients use stdio.

| Client | Installed | Config | Before | Now |
|---|---|---|---|---|
| Copilot CLI | yes (Homebrew) | `~/.copilot/mcp-config.json` | codedb (`~/bin/codedb`), deepwiki, plus its built-in GitHub server | mcpx. The built-in server has no config key; the `copilot` shell alias passes `--disable-builtin-mcps`. Checked: `copilot --model gpt-6-luna -p` called `mcpx_namespaces` through it and got 18 namespaces |
| Zed | yes | `~/.config/zed/settings.json` (`context_servers`) | GitHub, Context7 and Postgres extensions (Postgres had no `database_url` and errored at every start) | mcpx. Zed started `mcpx ... serve` itself on the settings change. The three extensions were moved to `~/.local/state/mcp-clients/backup/zed-extensions/`, and Zed unloaded them |
| Claude Code | no | `~/.claude.json` (`mcpServers`) | codedb, deepwiki | mcpx |
| Codex | no | `~/.codex/config.toml` (`[mcp_servers.*]`) | codedb, deepwiki | mcpx |
| OpenCode | yes | none needed | the fork's built-in mcpx plugin | unchanged; an `mcp` entry would list the tools twice |
| OpenChamber | yes | none | drives OpenCode | unchanged |

No config was found for VS Code (application data left, app removed), Cursor (a
dangling `/usr/local/bin/cursor` link), Windsurf, Claude Desktop or Gemini CLI.
A client added later needs an entry in `mcp-clients.py`.

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
