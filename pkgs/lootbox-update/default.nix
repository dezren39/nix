{
  pkgs,
  lootboxSrc,
  lootboxPatches,
}:
let
  revision = "587a5a1b2694d0d00168665d8f1a536bc54e0f1a";
  installID = "${revision}-deno29-global-config-loopback-ui-pinned-mcps-v1";
in
pkgs.writeShellApplication {
  name = "lootbox-update";
  runtimeInputs = with pkgs; [
    coreutils
    curl
    deno
    gnupatch
    nodejs
  ];
  text = ''
    force=0
    if [ "''${1:-}" = "--force" ]; then
      force=1
      shift
    elif [ "''${1:-}" = "--if-needed" ]; then
      shift
    fi
    if [ "$#" -ne 0 ]; then
      echo "usage: lootbox-update [--if-needed|--force]" >&2
      exit 2
    fi

    install_dir="$HOME/.local/bin"
    legacy_dir="$HOME/.deno/bin"
    data_dir="$HOME/.local/share/lootbox"
    binary="$install_dir/lootbox"
    marker="$data_dir/install-id"
    ui_dir="$data_dir/ui"
    npm_dir="$data_dir/npm"

    mkdir -p "$install_dir" "$legacy_dir" "$data_dir"
    if [ "$force" -eq 0 ] && [ -x "$binary" ] && [ "$(cat "$marker" 2>/dev/null || true)" = "${installID}" ]; then
      echo "lootbox ${revision} is already installed"
    else
      tmp=$(mktemp -d)
      trap 'rm -rf "$tmp"' EXIT

      echo "Building lootbox ${revision} with $(deno --version | sed -n '1p')"
      cp -R ${lootboxSrc} "$tmp/source"
      chmod -R u+w "$tmp/source"
      ${pkgs.lib.concatMapStringsSep "\n" (
        patch: "patch -d \"$tmp/source\" -p1 < ${patch}"
      ) lootboxPatches}

      (
        cd "$tmp/source"
        deno install
      )
      (
        cd "$tmp/source/ui"
        deno install
      )
      (
        cd "$tmp/source"
        deno task compile
      )
      "$tmp/source/lootbox" --version | grep -F "lootbox v0.0.54"

      npm install --prefix "$tmp/npm" --omit=dev --ignore-scripts \
        chrome-devtools-mcp@1.6.0 \
        mcp-remote@0.1.38

      install -m755 "$tmp/source/lootbox" "$install_dir/.lootbox.new"
      mv -f "$install_dir/.lootbox.new" "$binary"
      rm -rf "$ui_dir" "$npm_dir"
      cp -R "$tmp/source/ui/dist" "$ui_dir"
      cp -R "$tmp/npm" "$npm_dir"
      printf '%s\n' "${installID}" > "$marker"
      echo "Installed $binary"
    fi
    ln -sfn "$binary" "$legacy_dir/lootbox"

    label="gui/$(id -u)/org.nixos.lootbox"
    if launchctl print "$label" >/dev/null 2>&1; then
      launchctl kickstart -k "$label"
      for _ in $(seq 1 90); do
        namespaces=$("$binary" tools 2>/dev/null || true)
        if printf '%s\n' "$namespaces" | grep -q "mcp_codedb" \
          && printf '%s\n' "$namespaces" | grep -q "mcp_fff" \
          && printf '%s\n' "$namespaces" | grep -q "mcp_chrome_devtools" \
          && printf '%s\n' "$namespaces" | grep -q "mcp_context7"; then
          echo "lootbox server is ready with all configured namespaces"
          exit 0
        fi
        sleep 1
      done
      echo "lootbox server did not become healthy" >&2
      exit 1
    fi
  '';
}
