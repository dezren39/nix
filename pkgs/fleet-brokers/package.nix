{
  lib,
  rustPlatform,
  perl,
  cmake,
}:

rustPlatform.buildRustPackage {
  pname = "fleet-brokers";
  version = "0.1.0";

  # The cargo workspace (crates/secrets-broker, crates/queue-broker,
  # crates/repo-queue) vendored from the fleet-brokers tree (code#5362).
  # One package, three binaries: the two laptop brokers and the
  # sandbox-side repo-queue client.
  src = ./src;

  cargoLock.lockFile = ./src/Cargo.lock;

  # aws-lc-rs (the rustls crypto provider ureq pulls in) needs cmake
  # and perl at build time.
  nativeBuildInputs = [
    perl
    cmake
  ];

  # No test suite yet; the workspace is verified end-to-end against the
  # live queue instead (code#5362).
  doCheck = false;

  meta = {
    description = "Fleet brokers: secrets broker, queue broker, and repo-queue client (code#5362)";
    mainProgram = "repo-queue";
    platforms = lib.platforms.unix;
  };
}
