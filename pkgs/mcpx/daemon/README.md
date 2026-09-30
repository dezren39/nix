# mcpx daemon as a systemd user service

`mcpx.service` is a template user unit for running the mcpx daemon persistently
on a systemd host (see `../docs/configuration.md` for what the daemon does).

## Install

```sh
mkdir -p ~/.config/systemd/user
cp pkgs/mcpx/daemon/mcpx.service ~/.config/systemd/user/
# If mcpx is not on the default PATH, edit ExecStart to the full path first:
#   ExecStart=/home/USER/.nix-profile/bin/mcpx daemon
systemctl --user daemon-reload
systemctl --user enable --now mcpx
systemctl --user status mcpx
```

The daemon listens on its unix socket plus HTTP on `127.0.0.1:41001` and owns
every MCP server process; server definitions come from
`~/.config/mcpx/config.json`.

## Verify

```sh
systemctl --user is-active mcpx          # expect: active
curl -s http://127.0.0.1:41001/ | head -c 200
```

## Logs

```sh
journalctl --user -u mcpx -n 50
```

Restart behavior: `Restart=on-failure` with a 5s backoff.
