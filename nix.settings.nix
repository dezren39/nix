{
  # Nix settings, delivered through the Determinate nix-darwin module.
  #
  # Determinate Nix owns the daemon and /etc/nix/nix.conf, so configuration.nix
  # sets `nix.enable = false` and nix-darwin writes no Nix config at all. This
  # file used to set `nix.settings`, which was therefore entirely inert: checked
  # 2026-10-02, `nix config show` reported experimental-features = "fetch-tree
  # flakes nix-command" and only cache.nixos.org, none of what was listed here.
  # `determinateNix.customSettings` is the supported path; the module writes it
  # to /etc/nix/nix.custom.conf, which nix.conf includes. Verify a change with
  # `nix config show <key>` after `just switch`, not by reading this file.
  #
  # The module rejects the settings Determinate manages itself
  # (bash-prompt-prefix, external-builders, extra-nix-path, netrc-file,
  # ssl-cert-file, upgrade-nix-store-path-url). Determinate Nix 3 already
  # enables lazy-trees and parallel evaluation (eval-cores = 0, all cores), and
  # flakes / nix-command are stable there, so none of those are listed.
  determinateNix.customSettings = {
    # Every one of these only *allows* derivations or expressions that opt in;
    # nothing already building changes. Each name was checked against
    # `nix __dump-xp-features` on Determinate Nix 3.22.5 (2.35.2), because an
    # unknown feature is an error. Dropped from the old list: auto-allocate-uids
    # and cgroups (Linux-only), read-only-local-store (for mounting another
    # machine's store).
    extra-experimental-features = [
      "ca-derivations" # content-addressed outputs: identical results dedupe, rebuilds stop early
      "recursive-nix" # builds may call nix themselves
      "dynamic-derivations" # derivations that produce derivations
      "fetch-closure" # builtins.fetchClosure: pull a prebuilt closure by path
      "git-hashing" # git tree/blob hashes for store objects
      "pipe-operators" # |> and <| in the Nix language
      "parse-toml-timestamps" # builtins.fromTOML keeps timestamps
      "verified-fetches" # verify signed git commits in fetchGit
      "parallel-eval" # builtins.parallel (Determinate)
      "provenance" # record where each store path came from (Determinate)
    ];

    # Binary caches. nixpkgs-terraform provides terraform-1.5.7, which is
    # BSL-licensed and absent from cache.nixos.org: checked 2026-10-02, its out
    # path is on nixpkgs-terraform.cachix.org and "not valid" on cache.nixos.org,
    # so without it terraform compiles locally. Dropped: yazi.cachix.org (yazi is
    # not installed) and fenix.cachix.org (the fenix overlay is applied but no
    # fenix toolchain is installed; see systemPackages.nix).
    extra-substituters = [ "https://nixpkgs-terraform.cachix.org" ];
    extra-trusted-public-keys = [
      "nixpkgs-terraform.cachix.org-1:8Sit092rIdAVENA3ZVeH9hzSiqI/jng6JiCrQ1Dmusw="
    ];

    # Faster substitution. Defaults are 25 connections and 16 jobs. The
    # download buffer default is 1 MiB, small enough that large NARs stall with
    # "download buffer is full"; 256 MiB is only used while downloading.
    http-connections = 100;
    max-substitution-jobs = 64;
    download-buffer-size = 268435456;
    # If a cache serves a broken substitute, build locally instead of failing.
    fallback = true;
    # Only matters with remote builders (none today); harmless otherwise.
    builders-use-substitutes = true;

    # Keep build-time dependencies of live outputs, so GC does not delete what
    # a dev shell needs and force a re-download. keep-derivations is already
    # the default; stated so the pair is explicit.
    keep-outputs = true;
    keep-derivations = true;
    # Hard-link identical files as they enter the store. Saves disk, costs a
    # little I/O per added path.
    auto-optimise-store = true;

    # Diagnostics.
    show-trace = true;
    log-lines = 128;

    # Not carried over, deliberately:
    # - use-xdg-base-directories: moves ~/.nix-profile and ~/.nix-defexpr, a
    #   migration rather than a setting. Deferred.
    # - accept-flake-config: lets any flake you run add caches and settings
    #   without asking.
    # - trusted-users: being trusted is root-equivalent through the daemon.
    # - allow-dirty, pure-eval, restrict-eval, use-registries, trace-verbose:
    #   defaults, or debug-only.
  };
}
