{
  alt-tab-macos,
  lib,
  rcodesign,
  writeText,
}:

let
  launcherSource = writeText "alt-tab-debug-launcher.c" ''
    #include <limits.h>
    #include <mach-o/dyld.h>
    #include <stdio.h>
    #include <stdlib.h>
    #include <unistd.h>

    int main(int argc, char **argv) {
      char executable[PATH_MAX];
      char real_executable[PATH_MAX];
      uint32_t size = sizeof(executable);

      if (_NSGetExecutablePath(executable, &size) != 0 ||
          snprintf(real_executable, sizeof(real_executable), "%s-real", executable) >= sizeof(real_executable)) {
        fputs("Unable to locate the AltTab executable\n", stderr);
        return 127;
      }

      char **arguments = calloc((size_t)argc + 2, sizeof(char *));
      if (arguments == NULL) return 127;

      arguments[0] = real_executable;
      arguments[1] = "--mock-pro";
      for (int i = 1; i < argc; i++) arguments[i + 1] = argv[i];

      execv(real_executable, arguments);
      perror("Unable to launch AltTab");
      return 127;
    }
  '';
in
alt-tab-macos.overrideAttrs (old: {
  pname = "alt-tab-debug";

  # Upstream exposes its local Pro test mode only in DEBUG builds.
  buildPhase =
    builtins.replaceStrings
      [ "-emit-executable -module-name AltTab" ]
      [ "-D DEBUG -emit-executable -module-name AltTab" ]
      old.buildPhase;

  postPatch = (old.postPatch or "") + ''
    # Keep DEBUG's diagnostics available without opening the QA window at launch.
    substituteInPlace src/App.swift \
      --replace-fail 'QAMenu.shared?.orderFront(nil)' '// QA menu opens only when requested.'
  '';

  postInstall = (old.postInstall or "") + ''
    app="$out/Applications/AltTab.app"
    mv "$app/Contents/MacOS/AltTab" "$app/Contents/MacOS/AltTab-real"
    "$CC" -Os ${launcherSource} -o "$app/Contents/MacOS/AltTab"
  '';

  # Sign the real executable before the original fixup signs the full bundle.
  postFixup = ''
    ${lib.getExe rcodesign} sign \
      --code-signature-flags runtime \
      --entitlements-xml-file ${old.src}/alt_tab_macos.entitlements \
      "$out/Applications/AltTab.app/Contents/MacOS/AltTab-real"
  ''
  + (old.postFixup or "");

  meta = old.meta // {
    description = "AltTab DEBUG build with upstream mock Pro mode enabled";
  };
})
