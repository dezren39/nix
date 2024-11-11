{
  nix.optimise.automatic = true;
  nix.settings = {
    # unfortunately can't import, but this should be equal to flake.nix
    experimental-features = [
      "auto-allocate-uids"
      "ca-derivations"
      "cgroups"
      "dynamic-derivations"
      "fetch-closure"
      "flakes"
      "git-hashing"
      # "local-overlay-store" # look into this
      # "mounted-ssh-store" # look into this
      "nix-command"
      # "no-url-literals" # <- removed no-url-literals for flakehub testing
      "parse-toml-timestamps"
      "read-only-local-store"
      "recursive-nix"
      "verified-fetches"
    ];
    trusted-users = [ "root" ];
    #       trusted-users = [ "user" ];
    use-xdg-base-directories = true;
    builders-use-substitutes = true;
    substituters = [
      # TODO: priority order
      "https://cache.nixos.org"
      "https://yazi.cachix.org"
      # "https://binary.cachix.org"
      # "https://nix-community.cachix.org"
      # "https://nix-gaming.cachix.org"
      # "https://cache.m7.rs"
      # "https://nrdxp.cachix.org"
      # "https://numtide.cachix.org"
      # "https://colmena.cachix.org"
      # "https://sylvorg.cachix.org"
    ];
    trusted-substituters = [
      "https://cache.nixos.org"
      "https://yazi.cachix.org"
      # "https://binary.cachix.org"
      # "https://nix-community.cachix.org"
      # "https://nix-gaming.cachix.org"
      # "https://cache.m7.rs"
      # "https://nrdxp.cachix.org"
      # "https://numtide.cachix.org"
      # "https://colmena.cachix.org"
      # "https://sylvorg.cachix.org"
    ];
    trusted-public-keys = [
      "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY="
      "yazi.cachix.org-1:Dcdz63NZKfvUCbDGngQDAZq6kOroIrFoyO064uvLh8k="
      # "binary.cachix.org-1:66/C28mr67KdifepXFqZc+iSQcLENlwPqoRQNnc3M4I="
      # "nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs="
      # "nix-gaming.cachix.org-1:nbjlureqMbRAxR1gJ/f3hxemL9svXaZF/Ees8vCUUs4="
      # "cache.m7.rs:kszZ/NSwE/TjhOcPPQ16IuUiuRSisdiIwhKZCxguaWg="
      # "nrdxp.cachix.org-1:Fc5PSqY2Jm1TrWfm88l6cvGWwz3s93c6IOifQWnhNW4="
      # "numtide.cachix.org-1:2ps1kLBUWjxIneOy1Ik6cQjb41X0iXVXeHigGmycPPE="
      # "colmena.cachix.org-1:7BzpDnjjH8ki2CT3f6GdOk7QAzPOl+1t3LvTLXqYcSg="
      # "sylvorg.cachix.org-1:xd1jb7cDkzX+D+Wqt6TemzkJH9u9esXEFu1yaR9p8H8="
    ];
    extra-substituters = [ ];
    extra-trusted-substituters = [ ];
    extra-trusted-public-keys = [ ];
    # bash-prompt-prefix = "(nix:$name)\040";
    # build-users-group = "nixbld";
    http-connections = 100; # 128 default:25
    max-substitution-jobs = 64; # 128 default:16
    # Store:querySubstitutablePaths Store::queryMissing binary-caches-parallel-connections fileTransferSettings.httpConnections
    keep-outputs = true; # Nice for developers
    keep-derivations = true; # Idem
    accept-flake-config = true;
    #     allow-dirty = false;
    #     builders-use-substitutes = true;
    fallback = true;
    log-lines = 128;
    #     pure-eval = true;
    # run-diff-hook = true;
    # secret-key-files
    show-trace = true;
    # tarball-ttl = 0;
    # trace-function-calls = true;
    trace-verbose = true;
    # use-xdg-base-directories = true;
    allow-dirty = true;
    /*
      buildMachines = [ ];
      distributedBuilds = true;
      # optional, useful when the builder has a faster internet connection than yours
      extraOptions = ''
        builders-use-substitutes = true
      '';
    */
    #pure-eval = true;
    pure-eval = false; # sometimes home-manager needs to change manifest.nix ? idk i just code here
    restrict-eval = false; # could i even make a conclusive list of domains to allow access to?
    use-registries = true;
    #use-cgroups = true;
  };
}
