{
  lib,
  clang,
  writeShellApplication,
  sidepulseUnwrapped,
  name ? "sidepulse-setup",
  defaultToSetup ? true,
}:

writeShellApplication {
  inherit name;
  runtimeInputs = [
    clang
    sidepulseUnwrapped
  ];
  text = ''
    export PATH="$PATH:/usr/bin:/bin:/usr/sbin:/sbin"
    sidepulse_unwrapped=${lib.getExe sidepulseUnwrapped}
    setup_only=${if defaultToSetup then "true" else "false"}

    usage() {
      cat <<'EOF'
    usage: sidepulse [--setup|setup] [provider] [sidepulse setup options]

    Configure SidePulse hooks explicitly. With no provider, configure all
    supported providers: Codex, Claude, Grok, and OpenCode.

    Examples:
      sidepulse --setup opencode
      sidepulse setup opencode
      sidepulse setup
    EOF
    }

    setup() {
      provider="all"
      case "''${1:-}" in
        -h|--help)
          usage
          return 0
          ;;
        "")
          ;;
        all|codex|claude|grok|opencode)
          provider="$1"
          shift
          ;;
        -*)
          ;;
        *)
          printf 'sidepulse: unsupported provider: %s\n' "$1" >&2
          usage >&2
          return 2
          ;;
      esac

      exec "$sidepulse_unwrapped" setup "$provider" "$@"
    }

    if [ "$setup_only" = "true" ]; then
      setup "$@"
      exit $?
    fi

    case "''${1:-}" in
      --setup)
        shift
        setup "$@"
        ;;
      setup)
        shift
        setup "$@"
        ;;
      *)
        exec "$sidepulse_unwrapped" "$@"
        ;;
    esac
  '';
  meta = {
    description = "SidePulse CLI wrapper with native full setup and hook setup aliases";
    mainProgram = name;
    platforms = lib.platforms.darwin;
  };
}
