{
  lib,
  stdenv,
  python3,
  makeWrapper,
}:

stdenv.mkDerivation {
  pname = "laptop-agent";
  version = "0.1.0";

  # The resident fleet execution agent (code#5343): a single
  # stdlib-only Python daemon, vendored from the fleet laptop-agent
  # tree. The sandbox session proxy routes laptop-agent, secrets
  # broker, and queue broker traffic to it and its sibling brokers.
  src = ./src;

  nativeBuildInputs = [ makeWrapper ];

  dontBuild = true;

  installPhase = ''
    runHook preInstall
    install -Dm755 laptop_agent.py $out/libexec/laptop_agent.py
    makeWrapper ${python3}/bin/python3 $out/bin/laptop-agent \
      --add-flags "$out/libexec/laptop_agent.py"
    runHook postInstall
  '';

  meta = {
    description = "laptop-agent — resident fleet execution agent daemon (code#5343)";
    mainProgram = "laptop-agent";
    platforms = lib.platforms.unix;
  };
}
